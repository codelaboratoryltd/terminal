package terminal

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	widget2 "github.com/fyne-io/terminal/internal/widget"
)

var escapes = map[rune]func(*Terminal, string){
	'@': escapeInsertChars,
	'A': escapeMoveCursorUp,
	'B': escapeMoveCursorDown,
	'C': escapeMoveCursorRight,
	'D': escapeMoveCursorLeft,
	'd': escapeMoveCursorRow,
	'H': escapeMoveCursor,
	'f': escapeMoveCursor,
	'G': escapeMoveCursorCol,
	'h': escapePrivateModeOn,
	'L': escapeInsertLines,
	'l': escapePrivateModeOff,
	// Note: 'h'/'l' without '?' are SM/RM; we'll handle '20h/20l' etc.
	'm': escapeColorMode,
	'n': escapeDeviceStatusReport,
	'J': escapeEraseInScreen,
	'K': escapeEraseInLine,
	'P': escapeDeleteChars,
	'r': escapeSetScrollArea,
	's': escapeSaveCursor,
	'S': escapeScrollUp,
	'T': escapeScrollDown,
	'u': escapeRestoreCursor,
	'i': escapePrinterMode,
	'c': escapeDeviceAttribute,
}

func (t *Terminal) handleEscape(code string) {
	code = trimLeftZeros(code)
	if code == "" {
		return
	}

	runes := []rune(code)
	if esc, ok := escapes[runes[len(code)-1]]; ok {
		esc(t, code[:len(code)-1])
	} else if t.debug {
		log.Println("Unrecognised Escape:", code)
	}
}

func (t *Terminal) clearScreen() {
	width := int(t.config.Columns)
	rows := int(t.config.Rows)
	blankCell := widget.TextGridCell{Rune: ' ', Style: widget2.NewTermTextGridStyle(t.currentFG, t.currentBG, highlightBitMask, t.blinking, t.bold, t.underlined)}
	for i := 0; i < rows; i++ {
		line := make([]widget.TextGridCell, width)
		for j := range line {
			line[j] = blankCell
		}
		t.content.SetRow(i, widget.TextGridRow{Cells: line})
	}
	t.moveCursor(0, 0)
}

func (t *Terminal) clearScreenFromCursor() {
	row := t.content.Row(t.cursorRow)
	from := t.cursorCol
	if t.cursorCol > len(row.Cells) {
		from = len(row.Cells)
	}
	// Build a full-width row: keep left segment, blank the rest
	width := int(t.config.Columns)
	blankCell := widget.TextGridCell{Rune: ' ', Style: widget2.NewTermTextGridStyle(t.currentFG, t.currentBG, highlightBitMask, t.blinking, t.bold, t.underlined)}
	left := []widget.TextGridCell{}
	if from > 0 {
		left = row.Cells[:from]
	}
	rightLen := 0
	if width > from {
		rightLen = width - from
	}
	right := make([]widget.TextGridCell, rightLen)
	for i := range right {
		right[i] = blankCell
	}
	t.content.SetRow(t.cursorRow, widget.TextGridRow{Cells: append(append([]widget.TextGridCell{}, left...), right...)})

	// Clear following rows to full-width blanks
	for i := t.cursorRow + 1; i < int(t.config.Rows); i++ {
		line := make([]widget.TextGridCell, width)
		for j := range line {
			line[j] = blankCell
		}
		t.content.SetRow(i, widget.TextGridRow{Cells: line})
	}
}

func (t *Terminal) clearScreenToCursor() {
	row := t.content.Row(t.cursorRow)
	width := int(t.config.Columns)
	blankCell := widget.TextGridCell{Rune: ' ', Style: widget2.NewTermTextGridStyle(t.currentFG, t.currentBG, highlightBitMask, t.blinking, t.bold, t.underlined)}

	// Keep right segment (from cursor), blank left up to cursor, and pad to full width
	right := []widget.TextGridCell{}
	if t.cursorCol < len(row.Cells) {
		right = row.Cells[t.cursorCol:]
	}
	combined := make([]widget.TextGridCell, 0, width)
	leftBlanks := t.cursorCol
	if leftBlanks > width {
		leftBlanks = width
	}
	for j := 0; j < leftBlanks; j++ {
		combined = append(combined, blankCell)
	}
	combined = append(combined, right...)
	if len(combined) < width {
		tail := make([]widget.TextGridCell, width-len(combined))
		for j := range tail {
			tail[j] = blankCell
		}
		combined = append(combined, tail...)
	}

	fyne.Do(func() {
		t.content.SetRow(t.cursorRow, widget.TextGridRow{Cells: combined})
		for i := 0; i < t.cursorRow; i++ {
			line := make([]widget.TextGridCell, width)
			for j := range line {
				line[j] = blankCell
			}
			t.content.SetRow(i, widget.TextGridRow{Cells: line})
		}
	})
}

func (t *Terminal) handleVT100(code string) {
	switch code {
	case "(A":
		t.g0Charset = charSetAlternate
	case ")A":
		t.g1Charset = charSetAlternate
	case "(B":
		t.g0Charset = charSetANSII
	case ")B":
		t.g1Charset = charSetANSII
	case "(0":
		t.g0Charset = charSetDECSpecialGraphics
	case ")0":
		t.g1Charset = charSetDECSpecialGraphics
	default:
		if t.debug {
			log.Println("Unhandled VT100:", code)
		}
	}
}

// resetTerminal performs a full reset equivalent to RIS (ESC c)
func (t *Terminal) resetTerminal() {
	// Clear modes
	t.wrapAround = true
	t.wrapPending = false
	t.newLineMode = false
	t.cursorHidden = false
	t.applicationCursorKeys = false
	t.onMouseDown = nil
	t.onMouseUp = nil

	// Reset attributes
	t.currentBG = nil
	t.currentFG = nil
	t.bold = false
	t.blinking = false
	t.underlined = false

	// Reset charsets
	t.g0Charset = charSetANSII
	t.g1Charset = charSetANSII
	t.useG1CharSet = false

	// Reset scroll region
	t.scrollTop = 0
	if t.config.Rows > 0 {
		t.scrollBottom = int(t.config.Rows) - 1
	}

	// Reset buffers to main screen
	if t.savedRows != nil {
		t.savedRows = nil
	}
	t.bufferMode = false

	// Reset cursor position
	t.moveCursor(0, 0)

	// Clear screen
	t.clearScreen()
}

func (t *Terminal) moveCursor(row, col int) {
	if t.config.Columns == 0 || t.config.Rows == 0 {
		return
	}
	if col < 0 {
		col = 0
	} else if col >= int(t.config.Columns) {
		col = int(t.config.Columns) - 1
	}

	if row < 0 {
		row = 0
	} else if row >= int(t.config.Rows) {
		row = int(t.config.Rows) - 1
	}

	// Any explicit cursor movement clears a pending wrap, per xterm deferred-wrap rules
	t.wrapPending = false

	t.cursorCol = col
	t.cursorRow = row

	if t.cursorMoved != nil {
		fyne.Do(t.cursorMoved)
	}
}

func escapeColorMode(t *Terminal, msg string) {
	t.handleColorEscape(msg)
}

func escapeDeleteChars(t *Terminal, msg string) {
	i, _ := strconv.Atoi(msg)
	if i == 0 {
		i = 1
	}
	right := t.cursorCol + i

	row := t.content.Row(t.cursorRow)
	cells := row.Cells[:t.cursorCol]
	if right < len(row.Cells) {
		cells = append(cells, row.Cells[right:]...)
	}

	t.content.SetRow(t.cursorRow, widget.TextGridRow{Cells: cells})
}

func escapeEraseInLine(t *Terminal, msg string) {
	mode, _ := strconv.Atoi(msg)
	switch mode {
	case 0:
		row := t.content.Row(t.cursorRow)
		if t.cursorCol >= len(row.Cells) {
			return
		}
		t.content.SetRow(t.cursorRow, widget.TextGridRow{Cells: row.Cells[:t.cursorCol]})
	case 1:
		row := t.content.Row(t.cursorRow)
		if t.cursorCol >= len(row.Cells) {
			return
		}
		cells := make([]widget.TextGridCell, t.cursorCol)
		t.content.SetRow(t.cursorRow, widget.TextGridRow{Cells: append(cells, row.Cells[t.cursorCol:]...)})
	case 2:
		row := t.content.Row(t.cursorRow)
		if t.cursorCol >= len(row.Cells) {
			return
		}
		cells := make([]widget.TextGridCell, len(row.Cells))
		t.content.SetRow(t.cursorRow, widget.TextGridRow{Cells: cells})
	}
}

func escapeEraseInScreen(t *Terminal, msg string) {
	mode, _ := strconv.Atoi(msg)
	switch mode {
	case 0:
		t.clearScreenFromCursor()
	case 1:
		t.clearScreenToCursor()
	case 2:
		t.clearScreen()
	case 3:
		// xterm extension: Erase saved lines (scrollback). We also clear the
		// visible screen to ensure consistent behavior inside/outside tmux.
		t.content.Rows = []widget.TextGridRow{}
		t.scrollTop = 0
		if t.config.Rows > 0 {
			t.scrollBottom = int(t.config.Rows) - 1
		} else {
			t.scrollBottom = 0
		}
		t.moveCursor(0, 0)
		t.content.Refresh()
	}
}

func escapeInsertChars(t *Terminal, msg string) {
	chars, _ := strconv.Atoi(msg)
	if chars == 0 {
		chars = 1
	}

	newCells := make([]widget.TextGridCell, chars)
	cellStyle := &widget.CustomTextGridStyle{FGColor: t.currentFG, BGColor: t.currentBG}
	for i := range newCells {
		newCells[i] = widget.TextGridCell{
			Rune:  ' ',
			Style: cellStyle,
		}
	}

	row := &t.content.Rows[t.cursorRow]
	row.Cells = append(row.Cells[:t.cursorCol], append(newCells, row.Cells[t.cursorCol:]...)...)
}

func escapeInsertLines(t *Terminal, msg string) {
	rows, _ := strconv.Atoi(msg)
	if rows == 0 {
		rows = 1
	}
	i := t.scrollBottom
	for ; i > t.cursorRow-rows+1; i-- {
		t.content.SetRow(i, t.content.Row(i-rows))
	}
	for ; i >= t.cursorRow; i-- {
		t.content.SetRow(i, widget.TextGridRow{})
	}
}

func escapeMoveCursorUp(t *Terminal, msg string) {
	rows, _ := strconv.Atoi(msg)
	if rows == 0 {
		rows = 1
	}
	t.moveCursor(t.cursorRow-rows, t.cursorCol)
}

func escapeMoveCursorDown(t *Terminal, msg string) {
	rows, _ := strconv.Atoi(msg)
	if rows == 0 {
		rows = 1
	}
	t.moveCursor(t.cursorRow+rows, t.cursorCol)
}

func escapeMoveCursorRight(t *Terminal, msg string) {
	cols, _ := strconv.Atoi(msg)
	if cols == 0 {
		cols = 1
	}
	t.moveCursor(t.cursorRow, t.cursorCol+cols)
}

func escapeMoveCursorLeft(t *Terminal, msg string) {
	cols, _ := strconv.Atoi(msg)
	if cols == 0 {
		cols = 1
	}
	t.moveCursor(t.cursorRow, t.cursorCol-cols)
}

func escapeMoveCursorRow(t *Terminal, msg string) {
	row, _ := strconv.Atoi(msg)
	if t.originMode {
		base := t.scrollTop
		t.moveCursor(base+(row-1), t.cursorCol)
	} else {
		t.moveCursor(row-1, t.cursorCol)
	}
}

func escapeMoveCursorCol(t *Terminal, msg string) {
	col, _ := strconv.Atoi(msg)
	t.moveCursor(t.cursorRow, col-1)
}

func escapePrivateMode(t *Terminal, msg string, enable bool) {
	modes := strings.Split(msg, ";")
	for _, mode := range modes {
		switch mode {
		case "1":
			// DECCKM: Application Cursor Keys
			t.applicationCursorKeys = enable
		case "7":
			// Autowrap mode (DECSET/DECRST 7)
			t.wrapAround = enable
		case "6":
			// DECOM: Origin mode
			t.originMode = enable
		case "20":
			t.newLineMode = enable
		case "25":
			t.cursorHidden = !enable
			t.refreshCursor()
		case "1048":
			// Save/restore cursor only
			if enable {
				t.savedCursorRow, t.savedCursorCol = t.cursorRow, t.cursorCol
			} else {
				t.moveCursor(t.savedCursorRow, t.savedCursorCol)
			}
		case "9":
			if enable {
				t.onMouseDown = t.handleMouseDownX10
				t.onMouseUp = t.handleMouseUpX10
			} else {
				t.onMouseDown = nil
				t.onMouseUp = nil
			}
		case "1000":
			if enable {
				t.onMouseDown = t.handleMouseDownV200
				t.onMouseUp = t.handleMouseUpV200
			} else {
				t.onMouseDown = nil
				t.onMouseUp = nil
			}
		case "1006":
			t.mouseSGR = enable
		case "1049":
			// 1049 = 1047 + 1048
			if enable {
				// save cursor, then switch to alt buffer
				t.savedCursorRow, t.savedCursorCol = t.cursorRow, t.cursorCol
			}
			// behave like 47 around buffers
			fallthrough
		case "47":
			if enable {
				// Save current screen and switch to alternate (clear)
				if t.savedRows == nil {
					rows := make([]widget.TextGridRow, len(t.content.Rows))
					for i, row := range t.content.Rows {
						cells := make([]widget.TextGridCell, len(row.Cells))
						copy(cells, row.Cells)
						rows[i] = widget.TextGridRow{Cells: cells}
					}
					t.savedRows = rows
					t.savedCursorRow, t.savedCursorCol = t.cursorRow, t.cursorCol
				}
				// clear to alternate buffer
				t.content.Rows = []widget.TextGridRow{}
				t.moveCursor(0, 0)
				t.content.Refresh()
			} else {
				// Restore saved screen
				if t.savedRows != nil {
					rows := make([]widget.TextGridRow, len(t.savedRows))
					for i, row := range t.savedRows {
						cells := make([]widget.TextGridCell, len(row.Cells))
						copy(cells, row.Cells)
						rows[i] = widget.TextGridRow{Cells: cells}
					}
					t.content.Rows = rows
					// if 1049 was set, we also restore cursor
					t.moveCursor(t.savedCursorRow, t.savedCursorCol)
					t.savedRows = nil
					t.content.Refresh()
				}
			}
		case "2004":
			t.bracketedPasteMode = enable
		default:
			m := "l"
			if enable {
				m = "h"
			}
			if t.debug {
				log.Println("Unknown private escape code", fmt.Sprintf("%s%s", mode, m))
			}
		}
	}
}

func escapePrivateModeOff(t *Terminal, msg string) {
	if strings.HasPrefix(msg, "?") {
		escapePrivateMode(t, msg[1:], false)
		return
	}
	escapeMode(t, msg, false)
}

func escapePrivateModeOn(t *Terminal, msg string) {
	if strings.HasPrefix(msg, "?") {
		escapePrivateMode(t, msg[1:], true)
		return
	}
	escapeMode(t, msg, true)
}

// escapeMode handles standard SM/RM (without the DEC private '?' prefix)
func escapeMode(t *Terminal, msg string, enable bool) {
	modes := strings.Split(msg, ";")
	for _, mode := range modes {
		switch mode {
		case "20":
			// LNM: New Line Mode
			t.newLineMode = enable
		default:
			if t.debug {
				m := 'l'
				if enable {
					m = 'h'
				}
				log.Println("Unknown SM/RM code", mode+string(m))
			}
		}
	}
}

func escapeMoveCursor(t *Terminal, msg string) {
	if !strings.Contains(msg, ";") {
		t.moveCursor(0, 0)
		return
	}

	parts := strings.Split(msg, ";")
	row, _ := strconv.Atoi(parts[0])
	col := 1
	if len(parts) == 2 {
		col, _ = strconv.Atoi(parts[1])
	}
	// Respect DECOM: if origin mode, positions are relative to scroll region
	if t.originMode {
		base := t.scrollTop
		t.moveCursor(base+(row-1), (col - 1))
	} else {
		t.moveCursor(row-1, col-1)
	}
}

func escapeRestoreCursor(t *Terminal, s string) {
	if s != "" {
		if t.debug {
			log.Println("Corrupt restore cursor escape", s+"u")
		}
		return
	}
	t.moveCursor(t.savedRow, t.savedCol)
}

func escapeSaveCursor(t *Terminal, _ string) {
	t.savedRow = t.cursorRow
	t.savedCol = t.cursorCol
}

func escapeSetScrollArea(t *Terminal, msg string) {
	parts := strings.Split(msg, ";")
	start := 0
	end := int(t.config.Rows) - 1
	if len(parts) == 2 {
		if parts[0] != "" {
			start, _ = strconv.Atoi(parts[0])
			start--
		}
		if parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
			end--
		}
	}

	t.scrollTop = start
	t.scrollBottom = end
}

func escapeScrollUp(t *Terminal, msg string) {
	lines, _ := strconv.Atoi(msg)
	if lines == 0 {
		lines = 1
	}

	// Calculate the new cursor position after scrolling
	newCursorRow := t.cursorRow - lines
	// Make sure we never end up negative cursor row
	if newCursorRow < 0 {
		newCursorRow = 0
	}

	// Make sure we don't scroll above the scroll top
	if newCursorRow < t.scrollTop {
		newCursorRow = t.scrollTop
	}

	// Move the cursor to the new position
	t.moveCursor(newCursorRow, t.cursorCol)

	// Perform the actual scrolling action
	for i := t.scrollTop; i <= t.scrollBottom-lines; i++ {
		t.content.SetRow(i, t.content.Row(i+lines))
	}
	for i := t.scrollBottom - lines + 1; i <= t.scrollBottom; i++ {
		t.content.SetRow(i, widget.TextGridRow{}) // Clear the last lines
	}
}

// CSI T: Scroll down (reverse index in region by N lines)
func escapeScrollDown(t *Terminal, msg string) {
	lines, _ := strconv.Atoi(msg)
	if lines == 0 {
		lines = 1
	}
	for i := t.scrollBottom; i >= t.scrollTop+lines; i-- {
		t.content.SetRow(i, t.content.Row(i-lines))
	}
	for i := t.scrollTop; i < t.scrollTop+lines && i <= t.scrollBottom; i++ {
		t.content.SetRow(i, widget.TextGridRow{})
	}
}

// escapeDeviceStatusReport handles CSI ... n queries
// Supports 5n (status) and 6n (cursor position)
func escapeDeviceStatusReport(t *Terminal, msg string) {
	// msg can be a single number like "5" or "6"
	if msg == "5" {
		// Device Status Report: ready
		_, _ = t.in.Write([]byte{asciiEscape, '[', '0', 'n'})
		return
	}
	if msg == "6" {
		// Cursor position report: 1-based row;col
		row := t.cursorRow + 1
		col := t.cursorCol + 1
		resp := []byte{asciiEscape, '['}
		resp = append(resp, []byte(strconv.Itoa(row))...)
		resp = append(resp, ';')
		resp = append(resp, []byte(strconv.Itoa(col))...)
		resp = append(resp, 'R')
		_, _ = t.in.Write(resp)
		return
	}
	if t.debug {
		log.Println("Unhandled DSR", msg)
	}
}

func trimLeftZeros(s string) string {
	if s == "" {
		return s
	}

	i := 0
	for _, r := range s {
		if r > '0' {
			break
		}
		i++
	}

	return s[i:]
}

func escapePrinterMode(t *Terminal, code string) {
	switch code {
	case "5":
		t.state.printing = true
	case "4":
		t.state.printing = false
		if t.printData != nil {
			if t.printer != nil {
				// spool the printer
				t.printer.Print(t.printData)
			} else if t.debug {
				log.Println("Print data was received but no printer has been set")
			}

		}
		t.printData = nil
	default:
		if t.debug {
			log.Println("Unknown printer mode", code)
		}
	}
}

func escapeDeviceAttribute(t *Terminal, code string) {
	if len(code) == 0 {
		return
	}

	// Respond to primary/secondary DA queries conservatively
	switch code[0] {
	case '>':
		// DA2: Identify terminal type/version. Reply as xterm-256color-ish: CSI > 0 ; 115 ; 0 c
		_, _ = t.in.Write([]byte{asciiEscape, '[', '>', '0', ';', '1', '1', '5', ';', '0', 'c'})
	default:
		// DA1: Report VT220 (CSI ? 1 ; 2 c would be explicit). Use simple VT220 response: CSI ? 6 c
		_, _ = t.in.Write([]byte{asciiEscape, '[', '?', '6', 'c'})
	}
}
