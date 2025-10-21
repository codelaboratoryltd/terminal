package terminal

import (
	"context"
	"fmt"
	"hash/fnv"
	"image/color"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"sync"
	"time"
	"unicode"

	widget2 "github.com/fyne-io/terminal/internal/widget"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	bufLen             = 32768 // 32KB buffer for output, to align with modern L1 cache
	highlightBitMask   = 0x55
	maxAllowedFontSize = 96
	// Do not scale font below this size when resizing to fit a fixed PTY grid
	minAllowedFontSize = 10
)

// fontSizeKey represents a unique combination of theme and font size for lookup table
type fontSizeKey struct {
	themeHash uint64 // hash of theme type and properties
	fontSize  float32
}

// Global shared font size lookup table - static values that can be shared between terminals
var (
	globalFontLookup   = make(map[fontSizeKey]fyne.Size)
	globalFontLookupMu sync.RWMutex
)

// themeHash generates a hash for a theme based on its type and font resources
func themeHash(theme fyne.Theme) uint64 {
	h := fnv.New64a()

	// Use the theme's type as part of the hash
	themeType := reflect.TypeOf(theme).String()
	h.Write([]byte(themeType))

	// Include font resources in the hash since they affect cell size
	monospaceFont := theme.Font(fyne.TextStyle{Monospace: true})
	if monospaceFont != nil {
		h.Write([]byte(monospaceFont.Name()))
	}

	return h.Sum64()
}

// getSharedCellSize retrieves a cell size from the shared lookup table
func getSharedCellSize(theme fyne.Theme, fontSize float32) (fyne.Size, bool) {
	key := fontSizeKey{
		themeHash: themeHash(theme),
		fontSize:  fontSize,
	}

	globalFontLookupMu.RLock()
	size, exists := globalFontLookup[key]
	globalFontLookupMu.RUnlock()

	return size, exists
}

// setSharedCellSize stores a cell size in the shared lookup table
func setSharedCellSize(theme fyne.Theme, fontSize float32, size fyne.Size) {
	key := fontSizeKey{
		themeHash: themeHash(theme),
		fontSize:  fontSize,
	}

	globalFontLookupMu.Lock()
	globalFontLookup[key] = size
	globalFontLookupMu.Unlock()
}

type Config struct {
	Title         string
	Rows, Columns uint
}

type charSet int

const (
	charSetANSII charSet = iota
	charSetDECSpecialGraphics
	charSetAlternate
)

type Terminal struct {
	widget.BaseWidget
	fyne.ShortcutHandler

	content        *widget2.TermGrid
	config         Config
	listenerLock   sync.Mutex
	listeners      []chan Config
	lastLayoutSize fyne.Size // Track last layout size to reduce debug spam
	startDir       string

	// Mutex to protect resize operations from race conditions
	resizeLock sync.Mutex
	// Flag to indicate cleanup is in progress
	cleaningUp bool

	pty io.Closer
	in  io.WriteCloser
	out io.Reader

	bell, bold, debug, focused bool
	currentFG, currentBG       color.Color
	cursorRow, cursorCol       int
	savedRow, savedCol         int
	scrollTop, scrollBottom    int
	cursorChangeCallback       func(x, y int)

	lastDoubleTapTime time.Time

	// Theme override for ANSI colors
	customTheme fyne.Theme
	// Custom background color override - when set, this is used instead of theme background
	backgroundColorOverride color.Color
	// OSC handlers for Operating System Commands
	oscHandlers map[int]func(string)
	// APC handlers are now per-instance to avoid cross-terminal pollution
	apcHandlers map[string]func(*Terminal, string)

	cursor                   *canvas.Rectangle
	cursorHidden, bufferMode bool   // buffer mode is an xterm extension that impacts control keys
	applicationCursorKeys    bool   // DECCKM: application cursor key mode
	cursorShape              string // "block" or "caret"
	cursorMoved              func()

	onMouseDown, onMouseUp func(int, fyne.KeyModifier, fyne.Position)
	g0Charset              charSet
	g1Charset              charSet
	useG1CharSet           bool

	selStart, selEnd *position
	blockMode        bool
	selecting        bool
	mouseCursor      desktop.Cursor

	keyboardState struct {
		shiftPressed bool
		ctrlPressed  bool
		altPressed   bool
	}
	newLineMode            bool // new line mode or line feed mode
	bracketedPasteMode     bool
	insertMode             bool // IRM: insert/replace mode
	localEchoMode          bool // local echo mode (when false, terminal doesn't echo)
	state                  *parseState
	blinking               bool
	underlined             bool
	printData              []byte
	printer                Printer
	cmd                    *exec.Cmd
	readWriterConfigurator ReadWriterConfigurator
	keyRemap               map[fyne.KeyName]fyne.KeyName

	// xterm modes/buffers
	wrapAround  bool // DECSET 7
	wrapPending bool // deferred wrap pending flag
	originMode  bool // DECOM: origin mode (CUP relative to scroll region)
	// Saved main screen buffer for DECSET 47/1049
	savedRows      []widget.TextGridRow
	savedCursorRow int
	savedCursorCol int

	// Cursor blinking management
	cursorBlinkCancel context.CancelFunc
	cursorBlinkOn     bool // internal toggle to track blink state

	// Mouse reporting modes
	mouseSGR bool // DECSET 1006

	// Optional tracing of incoming PTY bytes for debugging
	trace io.Writer

	// Fixed PTY sizing / scaling state
	fixedPTY       bool
	fixedRows      uint
	fixedCols      uint
	fixedFontSize  int
	contentThemer  *ptyTheme
	contentWrapper fyne.CanvasObject

	// layout offsets for centering grid within widget
	offsetX float32
	offsetY float32

	// border configuration
	borderColor   color.Color
	borderWidth   float32
	borderEnabled bool
}

// Printer is used for spooling print data when its received.
type Printer interface {
	Print([]byte)
}

// PrinterFunc is a helper function to enable easy implementation of printers.
type PrinterFunc func([]byte)

// Print calls the PrinterFunc.
func (p PrinterFunc) Print(d []byte) {
	p(d)
}

// Cursor is used for displaying a specific cursor.
func (t *Terminal) Cursor() desktop.Cursor {
	if t == nil || t.mouseCursor == nil {
		return desktop.DefaultCursor
	}
	return t.mouseCursor
}

// GridCursorRowCol returns the term grid row and column of the cursor.
func (t *Terminal) GridCursorRowCol() (x, y int) {
	return t.cursorRow, t.cursorCol
}

// GridCursorChangeCallback sets a callback function that will be called when the cursor changes position.
func (t *Terminal) GridCursorChangeCallback(f func(x, y int)) {
	if f != nil {
		t.cursorChangeCallback = f
	}
}

// AcceptsTab indicates that this widget will use the Tab key (avoids loss of focus).
func (t *Terminal) AcceptsTab() bool {
	return true
}

// AddListener registers a new outgoing channel that will have our Config sent each time it changes.
func (t *Terminal) AddListener(listener chan Config) {
	t.listenerLock.Lock()
	defer t.listenerLock.Unlock()

	t.listeners = append(t.listeners, listener)
}

// RegisterOSCHandler registers a callback function for specific OSC command.
// command: OSC command number (0, 1, 2, etc.)
// handler: callback function that receives the title data
func (t *Terminal) RegisterOSCHandler(command int, handler func(data string)) {
	if t.oscHandlers == nil {
		t.oscHandlers = make(map[int]func(string))
	}
	t.oscHandlers[command] = handler
}

// MinSize provides a size large enough that a terminal could technically function.
func (t *Terminal) MinSize() fyne.Size {
	s := t.guessCellSize()
	return fyne.NewSize(s.Width*2.5, s.Height*1.2) // just enough to get a terminal init
}

// MouseDown handles the down action for desktop mouse events.
func (t *Terminal) MouseDown(ev *desktop.MouseEvent) {
	t.clearSelectedText()

	if ev.Button == desktop.MouseButtonSecondary {
		t.pasteText(fyne.CurrentApp().Clipboard())
	}

	if t.onMouseDown == nil {
		return
	}

	if ev.Button == desktop.MouseButtonPrimary {
		t.onMouseDown(1, ev.Modifier, ev.Position)
	} else if ev.Button == desktop.MouseButtonSecondary {
		t.onMouseDown(2, ev.Modifier, ev.Position)
	}
}

// MouseUp handles the up action for desktop mouse events.
func (t *Terminal) MouseUp(ev *desktop.MouseEvent) {

	if t.onMouseDown == nil {
		return
	}

	if t.hasSelectedText() {
		t.copySelectedText(fyne.CurrentApp().Clipboard(), false)
	}

	if ev.Button == desktop.MouseButtonPrimary {
		t.onMouseUp(1, ev.Modifier, ev.Position)
	} else if ev.Button == desktop.MouseButtonSecondary {
		t.onMouseUp(2, ev.Modifier, ev.Position)
	}
}

// DoubleTapped handles the double tapped event.
func (t *Terminal) DoubleTapped(pe *fyne.PointEvent) {
	// Support quad-tap for copy-whole-screen
	if time.Since(t.lastDoubleTapTime) < 500*time.Millisecond {
		fyne.CurrentApp().Clipboard().SetContent(t.Text())
		t.clearSelectedText()
		return
	} else {
		t.lastDoubleTapTime = time.Now()
	}

	pos := t.sanitizePosition(pe.Position)
	termPos := t.getTermPosition(*pos)
	row, col := termPos.Row, termPos.Col

	if t.hasSelectedText() {
		t.clearSelectedText()
	}

	if row < 1 || row > len(t.content.Rows) {
		return
	}

	// Additional safety check to prevent index out of bounds
	rowIndex := row - 1
	if rowIndex < 0 || rowIndex >= len(t.content.Rows) {
		println(fmt.Sprintf("WARNING: DoubleTapped rowIndex %d out of bounds for Rows length %d (row=%d)", rowIndex, len(t.content.Rows), row))
		return
	}

	rowContent := t.content.Rows[rowIndex].Cells

	if col < 0 || col >= len(rowContent) {
		return // No valid character under the cursor, do nothing
	}

	start, end := col-1, col-1

	if !unicode.IsLetter(rowContent[start].Rune) && !unicode.IsDigit(rowContent[start].Rune) {
		return
	}

	for start > 0 && (unicode.IsLetter(rowContent[start-1].Rune) || unicode.IsDigit(rowContent[start-1].Rune)) {
		start--
	}
	if start < len(rowContent) && !unicode.IsLetter(rowContent[start].Rune) && !unicode.IsDigit(rowContent[start].Rune) {
		start++
	}
	for end < len(rowContent) && (unicode.IsLetter(rowContent[end].Rune) || unicode.IsDigit(rowContent[end].Rune)) {
		end++
	}
	if start == end {
		return
	}

	t.selStart = &position{Row: row, Col: start + 1}
	t.selEnd = &position{Row: row, Col: end}

	t.highlightSelectedText()

	if t.hasSelectedText() {
		t.copySelectedText(fyne.CurrentApp().Clipboard(), false)
	}
}

// RemoveListener de-registers a Config channel and closes it
func (t *Terminal) RemoveListener(listener chan Config) {
	t.listenerLock.Lock()
	defer t.listenerLock.Unlock()

	for i, l := range t.listeners {
		if l == listener {
			if i < len(t.listeners)-1 {
				t.listeners = append(t.listeners[:i], t.listeners[i+1:]...)
			} else {
				t.listeners = t.listeners[:i]
			}
			close(l)
			return
		}
	}
}

// Resize is called when this terminal widget has been resized.
// It ensures that the virtual terminal is within the bounds of the widget.
func (t *Terminal) Resize(s fyne.Size) {
	// Protect resize operations with a mutex to prevent race conditions
	t.resizeLock.Lock()
	defer t.resizeLock.Unlock()

	// In fixed PTY mode we do not change rows/cols on resize; instead we scale font
	if t.fixedPTY {
		oldSize := t.Size()
		// If pixel size unchanged, do nothing
		if oldSize == s {
			return
		}

		t.BaseWidget.Resize(s)
		// Keep PTY rows/cols, but update pixel size (X/Y) to match canvas
		t.updatePTYSize()
		// Ensure content is re-laid out with new size; do not alter t.config
		t.Refresh()
		return
	}

	cellSize := t.guessCellSize()
	cols := uint(math.Floor(float64(s.Width) / float64(cellSize.Width)))
	rows := uint(math.Floor(float64(s.Height) / float64(cellSize.Height)))
	// Ensure we never end up with a 0x0 grid which can cause misalignment/races
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	sameGrid := (t.config.Columns == cols) && (t.config.Rows == rows)
	samePixel := t.Size() == s
	if sameGrid && samePixel {
		return
	}

	t.BaseWidget.Resize(s)
	if t.content != nil {
		t.content.Resize(fyne.NewSize(float32(cols)*cellSize.Width, float32(rows)*cellSize.Height))
	}

	oldRows := int(t.config.Rows)
	t.config.Columns, t.config.Rows = cols, rows
	if t.scrollBottom == 0 || t.scrollBottom == oldRows-1 {
		t.scrollBottom = int(t.config.Rows) - 1
	}
	if !sameGrid {
		t.onConfigure()
		t.updatePTYSize()
	}
}

// SetDebug turns on output about terminal codes and other errors if the parameter is `true`.
func (t *Terminal) SetDebug(debug bool) {
	t.debug = debug
}

// SetStartDir can be called before one of the Run calls to specify the initial directory.
func (t *Terminal) SetStartDir(path string) {
	t.startDir = path
}

// SetBorderColor sets the color of the terminal border.
func (t *Terminal) SetBorderColor(c color.Color) {
	t.borderColor = c
	t.Refresh()
}

// SetBackgroundColor sets a custom background color for the terminal.
// When set, this overrides the theme background color for PTY cells.
// Pass nil to revert to using the theme background color.
func (t *Terminal) SetBackgroundColor(c color.Color) {
	t.backgroundColorOverride = c

	// Update the content themer to use the new background color for PTY cells
	if t.contentThemer != nil {
		if c != nil {
			t.contentThemer.backgroundColor = c
		} else {
			t.contentThemer.backgroundColor = theme.Color(theme.ColorNameBackground)
		}
	}

	// Update current background color for new cells
	// Only update if currentBG is currently nil (default background)
	if t.currentBG == nil {
		t.currentBG = c
	}

	t.Refresh()
}

// SetBorderWidth sets the width of the terminal border in pixels.
func (t *Terminal) SetBorderWidth(width float32) {
	t.borderWidth = width
	t.Refresh()
}

// EnableBorder enables or disables the terminal border.
func (t *Terminal) EnableBorder(enabled bool) {
	t.borderEnabled = enabled
	t.Refresh()
}

// GetBorderColor returns the current border color.
func (t *Terminal) GetBorderColor() color.Color {
	return t.borderColor
}

// GetBorderWidth returns the current border width.
func (t *Terminal) GetBorderWidth() float32 {
	return t.borderWidth
}

// IsBorderEnabled returns whether the border is currently enabled.
func (t *Terminal) IsBorderEnabled() bool {
	return t.borderEnabled
}

// Tapped makes sure we ask for focus if user taps us.
func (t *Terminal) Tapped(ev *fyne.PointEvent) {
	if a := fyne.CurrentApp(); a != nil {
		if d := a.Driver(); d != nil {
			if c := d.CanvasForObject(t); c != nil {
				c.Focus(t)
			}
		}
	}

}

// Text returns the contents of the buffer as a single string joined with `\n` (no style information).
func (t *Terminal) Text() string {
	return t.content.Text()
}

// ExitCode returns the exit code from the terminal's shell.
// Returns -1 if called before shell was started or before shell exited.
// Also returns -1 if shell was terminated by a signal.
func (t *Terminal) ExitCode() int {
	if t.cmd == nil {
		return -1
	}
	return t.cmd.ProcessState.ExitCode()
}

// TouchCancel handles the tap action for mobile apps that lose focus during tap.
func (t *Terminal) TouchCancel(ev *mobile.TouchEvent) {
	if t.onMouseUp != nil {
		t.onMouseUp(1, 0, ev.Position)
	}
}

// TouchDown handles the down action for mobile touch events.
func (t *Terminal) TouchDown(ev *mobile.TouchEvent) {
	if t.onMouseDown != nil {
		t.onMouseDown(1, 0, ev.Position)
	}
}

// TouchUp handles the up action for mobile touch events.
func (t *Terminal) TouchUp(ev *mobile.TouchEvent) {
	if t.onMouseUp != nil {
		t.onMouseUp(1, 0, ev.Position)
	}
}

func (t *Terminal) onConfigure() {
	t.listenerLock.Lock()
	for _, l := range t.listeners {
		select {
		case l <- t.config:
		default:
			// channel blocked, might be closed
		}
	}
	t.listenerLock.Unlock()
}

func (t *Terminal) open() error {
	in, out, pty, err := t.startPTY()
	if err != nil {
		return err
	}

	t.in, t.out = in, out
	if t.readWriterConfigurator != nil {
		t.out, t.in = t.readWriterConfigurator.SetupReadWriter(out, in)
	}

	t.pty = pty

	t.updatePTYSize()
	return nil
}

// Exit requests that this terminal exits.
// If there are embedded shells it will exit the child one only.
func (t *Terminal) Exit() {
	_, _ = t.Write([]byte{0x4})
}

func (t *Terminal) close() error {
	if t.in != t.pty {
		_ = t.in.Close() // we may already be closed
	}
	if t.pty == nil {
		return nil
	}

	return t.pty.Close()
}

// guessCellSize is called extremely frequently, so we use a shared lookup table for efficiency
// guessCellSize is called extremely frequently, so we use a shared lookup table for efficiency
func (t *Terminal) guessCellSize() fyne.Size {
	// Determine the effective theme and font size
	var baseTheme fyne.Theme
	var fontSize float32

	if t.fixedPTY && t.fixedFontSize > 0 {
		// Fixed PTY mode: use the computed fixed font size
		fontSize = float32(t.fixedFontSize)
	} else if t.contentThemer != nil {
		// Use contentThemer if available (dynamic mode with theme override)
		fontSize = t.contentThemer.Size(theme.SizeNameText)
	} else {
		// Fallback to base theme size
		fontSize = t.Theme().Size(theme.SizeNameText)
	}

	// Get the base theme for lookup key
	if t.contentThemer != nil {
		baseTheme = t.contentThemer.base
	} else {
		baseTheme = t.customTheme
		if baseTheme == nil {
			baseTheme = t.Theme()
		}
	}

	// Check shared lookup table first
	if size, exists := getSharedCellSize(baseTheme, fontSize); exists {
		return size
	}

	// Cell size not in cache - measure it and store for future use
	cellSize, _ := fyne.CurrentApp().Driver().RenderedTextSize("M", fontSize, fyne.TextStyle{Monospace: true}, baseTheme.Font(fyne.TextStyle{Monospace: true}))
	size := fyne.NewSize(float32(math.Round(float64(cellSize.Width))), float32(math.Round(float64(cellSize.Height))))

	// Store in shared lookup table for future use by any terminal
	setSharedCellSize(baseTheme, fontSize, size)

	return size
}

func (t *Terminal) run() {
	buf := make([]byte, bufLen)
	var leftOver []byte
	for {
		// Check if cleanup is in progress before attempting to read
		if t.cleaningUp {
			return
		}

		num, err := t.out.Read(buf)
		if err != nil {
			if t.cmd != nil {
				// wait for cmd (shell) to exit, populates ProcessState.ExitCode
				t.cmd.Wait()
			}

			// Check for common exit conditions
			errMsg := err.Error()
			if err == io.EOF || errMsg == "EOF" {
				break // term exit on macOS
			} else if pathErr, ok := err.(*os.PathError); ok && pathErr.Err != nil &&
				(pathErr.Err.Error() == "input/output error" || pathErr.Err.Error() == "file already closed") {
				break // broken pipe, terminal exit
			} else if errMsg == "io: read/write on closed pipe" {
				break // pipe closed during cleanup
			}

			fyne.LogError("pty read error", err)
		}

		lenLeftOver := len(leftOver)
		fullBuf := buf
		if lenLeftOver > 0 {
			fullBuf = append(leftOver, buf[:num]...)
			num += lenLeftOver
		}

		if t.content == nil || t.cleaningUp {
			return
		}

		leftOver = t.handleOutput(fullBuf[:num])
		if len(leftOver) == 0 {
			fyne.Do(t.Refresh)
		}
	}
}

// RunLocalShell starts the terminal by loading a shell and starting to process the input/output.
func (t *Terminal) RunLocalShell(ctx context.Context, cancel context.CancelFunc) error {
	for t.config.Columns == 0 { // don't load the TTY until our output is configured
		time.Sleep(time.Millisecond * 50)
	}

	err := t.open()
	if err != nil {
		return err
	}

	if ctx != nil {
		go func() {
			if cancel != nil {
				defer cancel()
			}
			t.run()
		}()
		<-ctx.Done()
	} else {
		// No channel, run in standard blocking mode
		t.run()
	}

	return t.close()
}

// RunWithConnection starts the terminal by connecting to an external resource like an SSH connection.
func (t *Terminal) RunWithConnection(in io.WriteCloser, out io.Reader) error {
	for t.config.Columns == 0 { // don't load the TTY until our output is configured
		time.Sleep(time.Millisecond * 50)
	}
	t.in, t.out = in, out
	if t.readWriterConfigurator != nil {
		t.out, t.in = t.readWriterConfigurator.SetupReadWriter(out, in)
	}

	t.run()

	return t.close()
}

// Write is used to send commands into an open terminal connection.
// Errors will be returned if the connection is not established, has closed, or there was a problem in transmission.
func (t *Terminal) Write(b []byte) (int, error) {
	if t.in == nil {
		return 0, io.EOF
	}

	return t.in.Write(b)
}

func (t *Terminal) setupShortcuts() {
	// == PASTE == //
	// Handle standard paste shortcut (Ctrl+V or Cmd+V depending on platform)
	t.ShortcutHandler.AddShortcut(&fyne.ShortcutPaste{},
		func(_ fyne.Shortcut) {
			t.pasteText(fyne.CurrentApp().Clipboard())
		},
	)

	if runtime.GOOS != "windows" {
		// We handle shift insert in input.go due to an issue with the shortcut handler on Windows.
		t.ShortcutHandler.AddShortcut(
			&desktop.CustomShortcut{KeyName: fyne.KeyInsert, Modifier: fyne.KeyModifierShift},
			func(_ fyne.Shortcut) {
				t.pasteText(fyne.CurrentApp().Clipboard())
			},
		)
	}

	// Handle Ctrl+Shift+V shortcut (common on Linux and some Windows apps)
	t.ShortcutHandler.AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyV, Modifier: fyne.KeyModifierShift | fyne.KeyModifierShortcutDefault},
		func(_ fyne.Shortcut) {
			t.pasteText(fyne.CurrentApp().Clipboard())
		},
	)

	// == COPY == //
	t.ShortcutHandler.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: fyne.KeyModifierShift | fyne.KeyModifierShortcutDefault},
		func(_ fyne.Shortcut) {
			t.copySelectedText(fyne.CurrentApp().Clipboard(), false)
		})
}

func (t *Terminal) startingDir() string {
	if t.startDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}

	return t.startDir
}

// New sets up a new terminal instance with the bash shell
func New() *Terminal {
	t := &Terminal{
		mouseCursor:   desktop.DefaultCursor,
		in:            discardWriter{},
		keyRemap:      map[fyne.KeyName]fyne.KeyName{},
		oscHandlers:   make(map[int]func(string)),
		cursorShape:   "block", // Default to block cursor
		wrapAround:    true,    // xterm default
		localEchoMode: true,    // Default to local echo enabled
		borderEnabled: true,
		borderWidth:   1.0,
		borderColor:   theme.Color(theme.ColorNameForeground),
	}
	t.ExtendBaseWidget(t)

	// Enable raw byte tracing if requested via environment
	if os.Getenv("FYNE_TERM_TRACE") != "" {
		f, err := os.OpenFile("/tmp/fyneterm-trace.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			t.trace = f
		}
	}

	return t
}

// EnableFixedPTYSize enables fixed rows/cols for the PTY and scales font to fit.
// It sets the grid to the provided rows/cols and resizes the PTY accordingly when available.
func (t *Terminal) EnableFixedPTYSize(rows, cols uint) {
	if rows == 0 || cols == 0 {
		return
	}

	// Protect font scaling configuration from race conditions
	t.resizeLock.Lock()
	defer t.resizeLock.Unlock()

	t.fixedPTY = true
	t.fixedRows, t.fixedCols = rows, cols
	// Update config immediately; renderer will size/center and pick font to fit
	t.config.Rows, t.config.Columns = rows, cols
	if t.scrollBottom == 0 || t.scrollBottom >= int(rows) {
		t.scrollBottom = int(rows) - 1
	}
	// Clear cached layout size to force font size calculation with new fixed dimensions
	t.lastLayoutSize = fyne.NewSize(0, 0)
	// Font lookup will be lazy-loaded when needed - no need to rebuild on resize
	// Ensure PTY is resized to fixed grid if already running
	t.updatePTYSize()
	// Trigger a refresh to apply scaling & layout
	fyne.Do(t.Refresh)
}

// GetFixedPTYSizeMode returns true if the terminal is in fixed PTY size mode.
func (t *Terminal) GetFixedPTYSizeMode() bool {
	return t.fixedPTY
}

// DisableFixedPTYSize returns the terminal to dynamic sizing based on widget size.
func (t *Terminal) DisableFixedPTYSize() {
	// Protect font scaling configuration from race conditions
	t.resizeLock.Lock()
	defer t.resizeLock.Unlock()

	t.fixedPTY = false
	// Clear fixed font to allow dynamic
	t.fixedFontSize = 0
	// Clear cached layout size to force recalculation on next layout
	t.lastLayoutSize = fyne.NewSize(0, 0)
	// Trigger re-layout which will recompute rows/cols and PTY size on next resize
	fyne.Do(t.Refresh)
}

// initFontLookup ensures the shared font lookup table is populated for this terminal's theme.
// This is called once when entering fixed PTY mode to pre-populate all possible font sizes.
func (t *Terminal) initFontLookup() {
	// Use the terminal's custom theme if set, otherwise fall back to app theme
	baseTheme := t.customTheme
	if baseTheme == nil {
		baseTheme = t.Theme()
	}

	if t.debug {
		log.Printf("FontLookup: [%p] initFontLookup populating shared cache for theme %p\n", t, baseTheme)
	}

	// Pre-populate the shared lookup table with all font sizes we might need
	for i := 1; i <= maxAllowedFontSize; i++ {
		fontSize := float32(i)

		// Check if already cached
		if _, exists := getSharedCellSize(baseTheme, fontSize); exists {
			continue
		}

		// Measure and cache this font size
		cellSize, _ := fyne.CurrentApp().Driver().RenderedTextSize("M", fontSize, fyne.TextStyle{Monospace: true}, baseTheme.Font(fyne.TextStyle{Monospace: true}))
		size := fyne.NewSize(float32(math.Round(float64(cellSize.Width))), float32(math.Round(float64(cellSize.Height))))
		setSharedCellSize(baseTheme, fontSize, size)

		if t.debug && (i == 1 || i == 14 || i == 36 || i == 96) {
			log.Printf("FontLookup: Font size %d -> cell size %.1fx%.1f (stored in shared cache)\n", i, size.Width, size.Height)
		}
	}

	if t.debug {
		log.Printf("FontLookup: [%p] Shared cache populated\n", t)
	}

	// Prepare a theme wrapper we can tweak for content rendering
	if t.contentThemer == nil {
		var ptyBgColor color.Color
		if t.backgroundColorOverride != nil {
			ptyBgColor = t.backgroundColorOverride
		} else {
			ptyBgColor = theme.Color(theme.ColorNameBackground)
		}
		t.contentThemer = &ptyTheme{
			base:            baseTheme,
			textSize:        float32(theme.TextSize()),
			backgroundColor: ptyBgColor,
		}
		if t.debug {
			log.Printf("FontLookup: [%p] contentThemer created %p with base %p\n", t, t.contentThemer, baseTheme)
		}
	}
}

// chooseFixedFontSize selects the largest font size that fits the available widget size for fixed rows/cols.
func (t *Terminal) chooseFixedFontSize(avail fyne.Size) int {
	// Ensure shared lookup is populated
	baseTheme := t.customTheme
	if baseTheme == nil {
		baseTheme = t.Theme()
	}

	// Make sure we have all the font sizes cached
	// Check if at least some entries exist, otherwise populate
	if _, exists := getSharedCellSize(baseTheme, float32(minAllowedFontSize)); !exists {
		// Not populated yet, do it now
		for i := minAllowedFontSize; i <= maxAllowedFontSize; i++ {
			fontSize := float32(i)
			if _, exists := getSharedCellSize(baseTheme, fontSize); !exists {
				cellSize, _ := fyne.CurrentApp().Driver().RenderedTextSize("M", fontSize, fyne.TextStyle{Monospace: true}, baseTheme.Font(fyne.TextStyle{Monospace: true}))
				size := fyne.NewSize(float32(math.Round(float64(cellSize.Width))), float32(math.Round(float64(cellSize.Height))))
				setSharedCellSize(baseTheme, fontSize, size)
			}
		}
	}

	cols := int(t.fixedCols)
	rows := int(t.fixedRows)
	best := minAllowedFontSize

	// Add a safety margin to account for measurement precision issues and borders
	safeWidth := avail.Width * 0.99   // 1% margin
	safeHeight := avail.Height * 0.99 // 1% margin

	for i := minAllowedFontSize; i <= maxAllowedFontSize; i++ {
		s, _ := getSharedCellSize(baseTheme, float32(i))
		gw := float32(cols) * s.Width
		gh := float32(rows) * s.Height
		if gw <= safeWidth && gh <= safeHeight {
			best = i
		} else {
			break
		}
	}

	// Ensure we never go below the minimum allowed font size
	if best < minAllowedFontSize {
		best = minAllowedFontSize
	}

	// Double-check that our chosen font size actually fits
	if best > minAllowedFontSize {
		s, _ := getSharedCellSize(baseTheme, float32(best))
		gw := float32(cols) * s.Width
		gh := float32(rows) * s.Height
		if gw > safeWidth || gh > safeHeight {
			// If it doesn't fit, fall back to minimum
			best = minAllowedFontSize
		}
	}

	if t.debug {
		s, _ := getSharedCellSize(baseTheme, float32(best))
		gw := float32(cols) * s.Width
		gh := float32(rows) * s.Height
		println(fmt.Sprintf("[chooseFixedFontSize] Font Size %d, Cell Size: %.1fx%.1f -> Grid Size: %.1fx%.1f (Avail: %.1fx%.1f)",
			best, s.Width, s.Height, gw, gh, avail.Width, avail.Height))
	}

	return best
}

// invalidateCellCache resets cached cell size data so recalculation uses latest theme size
func (t *Terminal) invalidateCellCache() {
	// Also invalidate cursor size so it gets recalculated with new cell size
	if t.cursor != nil {
		t.cursor.Resize(fyne.NewSize(0, 0)) // Force recalculation on next refresh
	}
}

// fontOverrideTheme is a widget-local theme that allows overriding the base text size.
type fontOverrideTheme struct {
	base     fyne.Theme
	textSize float32
}

// ptyTheme is a widget-local theme that overrides both text size and background color for PTY content
type ptyTheme struct {
	base            fyne.Theme
	textSize        float32
	backgroundColor color.Color
}

func (f *fontOverrideTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return f.base.Color(n, v)
}

func (f *fontOverrideTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return f.base.Icon(n)
}

func (f *fontOverrideTheme) Font(style fyne.TextStyle) fyne.Resource {
	return f.base.Font(style)
}

func (f *fontOverrideTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameText {
		return f.textSize
	}
	return f.base.Size(n)
}

// ptyTheme methods - override background color and text size for PTY content
func (p *ptyTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameBackground {
		return p.backgroundColor
	}
	return p.base.Color(n, v)
}

func (p *ptyTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return p.base.Icon(n)
}

func (p *ptyTheme) Font(style fyne.TextStyle) fyne.Resource {
	return p.base.Font(style)
}

func (p *ptyTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameText {
		return p.textSize
	}
	return p.base.Size(n)
}

// sanitizePosition ensures that the given position p is within the bounds of the terminal.
// If the position is outside the bounds, it adjusts the coordinates to the nearest valid values.
// The adjusted position is then returned.
func (t *Terminal) sanitizePosition(p fyne.Position) *fyne.Position {
	size := t.Size()
	width, height := size.Width, size.Height
	if p.X < 0 {
		p.X = 0
	} else if p.X > width {
		p.X = width
	}

	if p.Y < 0 {
		p.Y = 0
	} else if p.Y > height {
		p.Y = height
	}

	return &p
}

// Dragged is called by fyne when the left mouse is down and moved whilst over the widget.
func (t *Terminal) Dragged(d *fyne.DragEvent) {
	pos := t.sanitizePosition(d.Position)
	if !t.selecting {
		if t.keyboardState.altPressed {
			t.blockMode = true
		}
		p := t.getTermPosition(*pos)
		t.selStart = &p
		t.selEnd = nil
	}

	// clear any previous selection
	sr, sc, er, ec := t.getSelectedRange()
	widget2.ClearHighlightRange(t.content, t.blockMode, sr, sc, er, ec)

	// make sure that x,y,x1,y1 are always positive
	t.selecting = true
	t.mouseCursor = desktop.TextCursor
	p := t.getTermPosition(*pos)
	t.selEnd = &p
	t.highlightSelectedText()
}

// DragEnd is called by fyne when the left mouse is released after a Drag event.
func (t *Terminal) DragEnd() {
	t.selecting = false
	if t.hasSelectedText() {
		t.copySelectedText(fyne.CurrentApp().Clipboard(), false)
	}
}

// SetReadWriter sets the readWriterConfigurator function that will be used when creating a new terminal.
// The readWriterConfigurator function is responsible for setting up the I/O readers and writers.
func (t *Terminal) SetReadWriter(mw ReadWriterConfigurator) {
	t.readWriterConfigurator = mw
}

// ReadWriterConfigurator is an interface that defines the methods required to set up
// the input (reader) and output (writer) streams for the terminal.
// Implementations of this interface can modify or wrap the reader and writer.
type ReadWriterConfigurator interface {
	// SetupReadWriter configures the input and output streams for the terminal.
	// It takes an input reader (r) and an output writer (w) as arguments.
	// The function returns a possibly modified reader and writer that
	// the terminal will use for I/O operations.
	SetupReadWriter(r io.Reader, w io.WriteCloser) (io.Reader, io.WriteCloser)
}

// ReadWriterConfiguratorFunc is a function type that matches the signature of the
// SetupReadWriter method in the Middleware interface.
type ReadWriterConfiguratorFunc func(r io.Reader, w io.WriteCloser) (io.Reader, io.WriteCloser)

// SetupReadWriter allows ReadWriterConfiguratorFunc to satisfy the Middleware interface.
// It calls the ReadWriterConfiguratorFunc itself.
func (m ReadWriterConfiguratorFunc) SetupReadWriter(r io.Reader, w io.WriteCloser) (io.Reader, io.WriteCloser) {
	return m(r, w)
}

// RemapKey remaps a key when processing input.
func (t *Terminal) RemapKey(key fyne.KeyName, remap fyne.KeyName) {
	t.keyRemap[key] = remap
}

// SetTheme sets a custom theme for this terminal's ANSI colors
func (t *Terminal) SetTheme(th fyne.Theme) {
	t.customTheme = th
	// When theme changes, the shared lookup table will automatically handle
	// the new theme via its hash-based key system. No need to invalidate.
	// Clear cached layout size to force font size recalculation with new theme
	if t.fixedPTY {
		t.lastLayoutSize = fyne.NewSize(0, 0)
		if t.debug {
			println(fmt.Sprintf("Terminal SetTheme Debug: [%p] Layout cache cleared for new theme %p", t, th))
		}
	}
}

// SetCursorShape sets the cursor shape ("block" or "caret")
func (t *Terminal) SetCursorShape(shape string) {
	t.cursorShape = shape
	if t.cursor != nil {
		t.refreshCursor()
	}
}

// Focus management to start/stop cursor blinking.
func (t *Terminal) FocusGained() {
	t.focused = true
	t.ensureCursorBlinking()
	// Only refresh if we're not in cleanup mode
	if !t.cleaningUp {
		t.Refresh()
	}
}

func (t *Terminal) FocusLost() {
	t.focused = false
	t.stopCursorBlink()
	if t.cursor != nil {
		t.cursor.Hidden = true
	}
	// Only refresh if we're not in cleanup mode
	if !t.cleaningUp {
		t.Refresh()
	}
}

// ensureCursorBlinking toggles the blinking loop based on visibility/focus and shape.
func (t *Terminal) ensureCursorBlinking() {
	// Blink when focused and cursor is not permanently hidden.
	shouldBlink := t.focused && !t.cursorHidden

	if !shouldBlink {
		t.stopCursorBlink()
		return
	}

	// Start if not running
	if t.cursorBlinkCancel == nil {
		t.startCursorBlink()
	}
}

func (t *Terminal) startCursorBlink() {
	if t.cursorBlinkCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cursorBlinkCancel = cancel
	interval := 500 * time.Millisecond
	t.cursorBlinkOn = true

	go func() {
		ticker := time.NewTicker(interval)
		defer func() {
			ticker.Stop()
			if r := recover(); r != nil {
				// Log panic but don't crash the application
				fmt.Printf("Panic in cursor blink goroutine: %v\n", r)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Toggle visibility
				t.cursorBlinkOn = !t.cursorBlinkOn
				if t.cursor != nil {
					// Only toggle if still appropriate to blink
					if t.focused && !t.cursorHidden {
						t.cursor.Hidden = !t.cursorBlinkOn
						fyne.Do(func() {
							t.cursor.Refresh()
						})
					}
				}
			}
		}
	}()
}

func (t *Terminal) stopCursorBlink() {
	if t.cursorBlinkCancel != nil {
		t.cursorBlinkCancel()
		t.cursorBlinkCancel = nil
	}
	// Ensure cursor is shown when we stop blinking (if focused state would want it)
	if t.cursor != nil {
		t.cursor.Hidden = !t.focused || t.cursorHidden
	}
}

// Cleanup performs resource cleanup for the terminal
// This should be called when the terminal is being destroyed
func (t *Terminal) Cleanup() {
	// Set cleanup flag to stop run loop processing
	t.cleaningUp = true

	// Stop cursor blinking first
	t.stopCursorBlink()

	// Close all listeners and channels
	t.listenerLock.Lock()
	for _, l := range t.listeners {
		// Check if channel is not already closed
		select {
		case <-l:
			// Channel already closed
		default:
			close(l)
		}
	}
	t.listeners = nil
	t.listenerLock.Unlock()

	// Stop any blinking on the content before clearing it
	if t.content != nil {
		t.content.StopBlink()
	}

	// Note: Don't close PTY or I/O streams here as they may still be in use by run()
	// The run() method will handle proper cleanup when it detects the closed pipe
	// Just clear references to prevent memory leaks
	t.content = nil
	t.customTheme = nil
	t.contentThemer = nil
	t.contentWrapper = nil
}
