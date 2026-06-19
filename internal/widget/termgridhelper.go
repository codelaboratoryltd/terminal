package widget

import (
	"image/color"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// slowBlinkMode, when true, switches ANSI text blink to a heartbeat cadence
// (3s visible / 500ms invisible) and forces BlinkEnabled cells to render with
// an underline as a persistent attention marker. Toggled by SetSlowBlinkMode,
// driven by the app's low-graphics setting. Process-wide because the app only
// ever runs one render mode at a time.
var slowBlinkMode atomic.Bool

// SetSlowBlinkMode enables or disables slow-pulse rendering for blinking text.
// See the slowBlinkMode docs.
func SetSlowBlinkMode(on bool) {
	slowBlinkMode.Store(on)
}

// IsSlowBlinkMode reports the current slowBlinkMode state. Exposed so the
// TermGrid blink loop can choose its cadence.
func IsSlowBlinkMode() bool {
	return slowBlinkMode.Load()
}

// forceFullRefresh, when true, disables the dirty-region render optimisation and
// repaints the whole grid on every frame (the pre-dirty-region behaviour). The app
// enables this when the bundled Mesa software renderer is NOT the active GL backend:
// the dirty-region / scissor path has shown artifacts on some hardware GL drivers,
// while the full-refresh path is known-good there; under Mesa (software rendering)
// the dirty-region path is kept because full CPU repaints are expensive. Process-
// wide for the same reason as slowBlinkMode — one render mode at a time.
var forceFullRefresh atomic.Bool

// SetForceFullRefresh enables or disables whole-grid repaints. See forceFullRefresh.
func SetForceFullRefresh(on bool) {
	forceFullRefresh.Store(on)
}

// IsForceFullRefresh reports the current forceFullRefresh state. Read by the
// TermGrid render path (drawToImage / DirtyPixelBounds) to bypass dirty-region
// rendering.
func IsForceFullRefresh() bool {
	return forceFullRefresh.Load()
}

// HighlightRange highlight options to the given range
// if highlighting has previously been applied it is enabled
func HighlightRange(t *TermGrid, blockMode bool, startRow, startCol, endRow, endCol int, bitmask byte) {
	applyHighlight := func(cell *widget.TextGridCell) {
		if h, ok := cell.Style.(*TermTextGridStyle); !ok {
			var base widget.TextGridStyle
			if cell.Style != nil {
				base = NewTermTextGridStyle(cell.Style.TextColor(), cell.Style.BackgroundColor(), bitmask, false, cell.Style.Style().Bold, cell.Style.Style().Underline)
			} else {
				base = NewTermTextGridStyle(nil, nil, bitmask, false, false, false)
			}
			// NewTermTextGridStyle returns a shared, interned instance, so clone
			// before flipping Highlighted rather than mutating it in place.
			cloned := *base.(*TermTextGridStyle)
			cloned.Highlighted = true
			cell.Style = &cloned
		} else if !h.Highlighted {
			// Clone before mutating to avoid affecting other cells that share
			// this *TermTextGridStyle pointer (bulk blank padding from cursor
			// jumps or erase sequences creates many cells sharing one pointer).
			cloned := *h
			cloned.Highlighted = true
			cell.Style = &cloned
		}
	}

	forRange(t, blockMode, startRow, startCol, endRow, endCol, applyHighlight, nil)
}

// ClearHighlightRange disables the highlight style for the given range
func ClearHighlightRange(t *TermGrid, blockMode bool, startRow, startCol, endRow, endCol int) {
	clearHighlight := func(cell *widget.TextGridCell) {
		// Only highlighted cells hold a unique (cloned) style; interned instances
		// are never Highlighted, so guard the write to avoid mutating a shared
		// instance (and to avoid a needless write race on it).
		if h, ok := cell.Style.(*TermTextGridStyle); ok && h.Highlighted {
			h.Highlighted = false
		}
	}
	forRange(t, blockMode, startRow, startCol, endRow, endCol, clearHighlight, nil)
}

// GetTextRange retrieves a text range from the TextGrid. It collects the text
// within the specified grid coordinates, starting from (startRow, startCol) and
// ending at (endRow, endCol), and returns it as a string. The behavior of the
// selection depends on the blockMode parameter. If blockMode is true, then
// startCol and endCol apply to each row in the range, creating a block selection.
// If blockMode is false, startCol applies only to the first row, and endCol
// applies only to the last row, resulting in a continuous range.
//
// Parameters:
//   - blockMode: A boolean flag indicating whether to use block mode.
//   - startRow:  The starting row index of the text range.
//   - startCol:  The starting column index of the text range.
//   - endRow:    The ending row index of the text range.
//   - endCol:    The ending column index of the text range.
//
// Returns:
//   - string: The text content within the specified range as a string.
func GetTextRange(t *TermGrid, blockMode bool, startRow, startCol, endRow, endCol int) string {
	var result []rune

	forRange(t, blockMode, startRow, startCol, endRow, endCol, func(cell *widget.TextGridCell) {
		result = append(result, cell.Rune)
	}, func(row *widget.TextGridRow) {
		result = append(result, '\n')
	})

	return string(result)
}

// forRange iterates over a range of cells and rows within a TermGrid, optionally applying a function to each cell and row.
//
// Parameters:
// - blockMode (bool): If true, the iteration is done in block mode, meaning it iterates through rows and applies the cell function for each cell in the specified column range.
// - startRow (int): The starting row index for the iteration. Rows are 0-indexed.
// - startCol (int): The starting column index for the iteration within the starting row. Columns are 0-indexed.
// - endRow (int): The ending row index for the iteration.
// - endCol (int): The ending column index for the iteration within the ending row.
// - eachCell (func(cell *widget.TextGridCell)): A function that takes a pointer to a TextGridCell and is applied to each cell in the specified range. Pass `nil` if you don't want to apply a cell function.
// - eachRow (func(row *widget.TextGridRow)): A function that takes a pointer to a TextGridRow and is applied to each row in the specified range. Pass `nil` if you don't want to apply a row function.
//
// Note:
// - If startRow or endRow are out of bounds (negative or greater/equal to the number of rows in the TextGrid), they will be adjusted to valid values.
// - If startRow and endRow are the same, the iteration will be limited to the specified column range within that row.
// - When blockMode is true, it iterates through rows from startRow to endRow, applying the cell function for each cell in the specified column range.
// - When blockMode is false, it iterates through individual cells row by row, applying the cell function for each cell and optionally applying the row function for each row.
//
// Example Usage:
// forRange(termGrid, true, 0, 1, 2, 3, cellFunc, rowFunc) // Iterate in block mode, applying cellFunc to cells in columns 1 to 3 and rowFunc to rows 0 to 2.
// forRange(termGrid, false, 1, 0, 3, 2, cellFunc, rowFunc) // Iterate cell by cell, applying cellFunc to all cells and rowFunc to rows 1 and 2.
func forRange(t *TermGrid, blockMode bool, startRow, startCol, endRow, endCol int, eachCell func(cell *widget.TextGridCell), eachRow func(row *widget.TextGridRow)) {
	if startRow >= len(t.Rows) || endRow < 0 {
		return
	}
	if startRow < 0 {
		startRow = 0
		startCol = 0
	}
	if endRow >= len(t.Rows) {
		endRow = len(t.Rows) - 1
		endCol = len(t.Rows[endRow].Cells) - 1
	}

	if startRow == endRow {
		if len(t.Rows[startRow].Cells)-1 < endCol {
			endCol = len(t.Rows[startRow].Cells) - 1
		}
		for col := startCol; col <= endCol; col++ {
			if eachCell != nil {
				eachCell(&t.Rows[startRow].Cells[col])
			}
		}
		return
	}

	if blockMode {
		// Iterate through the rows
		for rowNum := startRow; rowNum <= endRow; rowNum++ {
			row := &t.Rows[rowNum]
			if rowNum != startRow && eachRow != nil {
				eachRow(row)
			}

			// Apply the cell function for the cells in the given column range
			for col := startCol; col <= endCol && col < len(row.Cells); col++ {
				if eachCell != nil {
					eachCell(&row.Cells[col])
				}
			}
		}
		return
	}

	// first row
	if eachCell != nil {
		for col := startCol; col < len(t.Rows[startRow].Cells); col++ {
			eachCell(&t.Rows[startRow].Cells[col])
		}
	}

	// possible middle rows
	for rowNum := startRow + 1; rowNum < endRow; rowNum++ {
		if eachRow != nil {
			eachRow(&t.Rows[rowNum])
		}
		for col := 0; col < len(t.Rows[rowNum].Cells); col++ {
			if eachCell != nil {
				eachCell(&t.Rows[rowNum].Cells[col])
			}
		}
	}

	if len(t.Rows[endRow].Cells)-1 < endCol {
		endCol = len(t.Rows[endRow].Cells) - 1
	}
	if eachRow != nil {
		eachRow(&t.Rows[endRow])
	}
	// last row
	for col := 0; col <= endCol; col++ {
		if eachCell != nil {
			eachCell(&t.Rows[endRow].Cells[col])
		}
	}
}

// TermTextGridStyle defines a style that can be original or highlighted.
type TermTextGridStyle struct {
	TextStyle               fyne.TextStyle
	OriginalTextColor       color.Color
	OriginalBackgroundColor color.Color
	InvertedTextColor       color.Color
	InvertedBackgroundColor color.Color
	Highlighted             bool
	BlinkEnabled            bool
	blinked                 bool
}

// Style is the text style a cell should use.
func (h *TermTextGridStyle) Style() fyne.TextStyle {
	if h == nil {
		return fyne.TextStyle{}
	}
	if h.BlinkEnabled && slowBlinkMode.Load() {
		// In slow-blink (low-graphics) mode, give BlinkEnabled cells a
		// persistent underline so they keep drawing attention between
		// the infrequent heartbeat pulses. Preserve existing Bold/etc.
		s := h.TextStyle
		s.Underline = true
		return s
	}
	return h.TextStyle
}

// TextColor returns the color of the text, depending on whether it is highlighted.
func (h *TermTextGridStyle) TextColor() color.Color {
	if h == nil {
		return nil
	}
	if h.Highlighted {
		if h.blinked {
			return h.InvertedBackgroundColor
		}
		return h.InvertedTextColor
	}
	if h.blinked {
		if h.OriginalBackgroundColor == nil {
			return color.Transparent
		}
		return h.OriginalBackgroundColor
	}
	return h.OriginalTextColor
}

// BackgroundColor returns the background color, depending on whether it is highlighted.
func (h *TermTextGridStyle) BackgroundColor() color.Color {
	if h == nil {
		return nil
	}
	if h.Highlighted {
		return h.InvertedBackgroundColor
	}
	return h.OriginalBackgroundColor
}

func (h *TermTextGridStyle) blink(b bool) {
	if h == nil {
		return
	}
	h.blinked = b
}

// Bold is the text bold or not.
func (h *TermTextGridStyle) Bold() bool {
	return h.Style().Bold
}

// Underlined is the text underlined or not.
func (h *TermTextGridStyle) Underlined() bool {
	return h.Style().Underline
}

// HighlightOption defines a function type that can modify a TermTextGridStyle.
type HighlightOption func(h *TermTextGridStyle)

// styleCacheKey identifies a TermTextGridStyle by its immutable attributes. The
// fg/bg interface values compare by their concrete (comparable) color type — the
// same assumption getUniform's colorUniformCache already relies on.
type styleCacheKey struct {
	fg, bg                 color.Color
	bitmask                byte
	blinkEnabled, bold, ul bool
}

// Styles are immutable apart from the transient Highlighted/blinked state, which
// is always applied to a clone (HighlightRange) or shared in-sync across cells
// with identical attributes (blink). That makes one instance per attribute combo
// safe to share, so we intern them: handleOutputRune builds a style for every
// output character, but whole runs of cells share attributes, so without this the
// terminal allocates a fresh *TermTextGridStyle plus two boxed inverted colours
// per character — the dominant per-keystroke GC churn, in both render backends.
const maxStyleCacheEntries = 8192 // bounds growth for 24-bit-colour apps; ~96B each

var (
	styleCacheMu sync.RWMutex
	styleCache   = map[styleCacheKey]*TermTextGridStyle{}
)

// invalidateStyleCache drops all interned styles. Called when the default theme
// colours change, because styles built with a nil fg/bg derive their inverted
// colour from the theme at build time.
func invalidateStyleCache() {
	styleCacheMu.Lock()
	if len(styleCache) > 0 {
		styleCache = make(map[styleCacheKey]*TermTextGridStyle)
	}
	styleCacheMu.Unlock()
}

// NewTermTextGridStyle returns a TextGridStyle with the specified foreground (fg)
// and background (bg) colors and a bitmask controlling colour inversion. If fg or
// bg is nil, the theme's default colour is used. Returned instances are interned
// and shared by attribute combo (see styleCache); callers must not mutate them in
// place — clone first (as HighlightRange does).
func NewTermTextGridStyle(fg, bg color.Color, bitmask byte, blinkEnabled, bold, underlined bool) widget.TextGridStyle {
	key := styleCacheKey{fg, bg, bitmask, blinkEnabled, bold, underlined}

	styleCacheMu.RLock()
	if s, ok := styleCache[key]; ok {
		styleCacheMu.RUnlock()
		return s
	}
	styleCacheMu.RUnlock()

	s := buildTermTextGridStyle(fg, bg, bitmask, blinkEnabled, bold, underlined)

	styleCacheMu.Lock()
	if existing, ok := styleCache[key]; ok { // built concurrently — keep the first
		styleCacheMu.Unlock()
		return existing
	}
	if len(styleCache) < maxStyleCacheEntries {
		styleCache[key] = s
	}
	styleCacheMu.Unlock()
	return s
}

func buildTermTextGridStyle(fg, bg color.Color, bitmask byte, blinkEnabled, bold, underlined bool) *TermTextGridStyle {
	// calculate the inverted colors
	var invertedFg, invertedBg color.Color
	if fg == nil {
		invertedFg = invertColor(safeThemeColor(theme.ColorNameForeground), bitmask)
	} else {
		invertedFg = invertColor(fg, bitmask)
	}
	if bg == nil {
		invertedBg = invertColor(safeThemeColor(theme.ColorNameBackground), bitmask)
	} else {
		invertedBg = invertColor(bg, bitmask)
	}

	return &TermTextGridStyle{
		OriginalTextColor:       fg,
		OriginalBackgroundColor: bg,
		InvertedTextColor:       invertedFg,
		InvertedBackgroundColor: invertedBg,
		Highlighted:             false,
		BlinkEnabled:            blinkEnabled,
		TextStyle: fyne.TextStyle{
			Bold:      bold,
			Underline: underlined,
			Italic:    false, // Not implemented?
			Monospace: true,  // Terminal should always be monospace, otherwise it'd not work correctly
			Symbol:    false,
		},
	}
}

func safeThemeColor(name fyne.ThemeColorName) (col color.Color) {
	defer func() {
		if r := recover(); r != nil {
			col = theme.DarkTheme().Color(name, theme.VariantDark)
		}
	}()

	col = theme.Color(name)
	if col == nil {
		return theme.DarkTheme().Color(name, theme.VariantDark)
	}

	return col
}

// invertColor inverts a color c with the given bitmask
func invertColor(c color.Color, bitmask uint8) color.Color {
	r, g, b, a := c.RGBA()
	return color.RGBA{
		R: uint8(r>>8) ^ bitmask,
		G: uint8(g>>8) ^ bitmask,
		B: uint8(b>>8) ^ bitmask,
		A: uint8(a >> 8),
	}
}
