package terminal

import (
	"bytes"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	widget2 "github.com/fyne-io/terminal/internal/widget"
)

const (
	asciiBell      = 7
	asciiBackspace = 8
	asciiEscape    = 27

	noEscape = 5000
	tabWidth = 8
)

var charSetMap = map[charSet]func(rune) rune{
	charSetANSII: func(r rune) rune {
		return r
	},
	charSetDECSpecialGraphics: func(r rune) rune {
		m, ok := decSpecialGraphics[r]
		if ok {
			return m
		}
		return r
	},
	charSetAlternate: func(r rune) rune {
		return r
	},
}

var specialChars = map[rune]func(t *Terminal){
	asciiBell:      handleOutputBell,
	asciiBackspace: handleOutputBackspace,
	'\n':           handleOutputLineFeed,
	'\v':           handleOutputLineFeed,
	'\f':           handleOutputLineFeed,
	'\r':           handleOutputCarriageReturn,
	'\t':           handleOutputTab,
	0x0e:           handleShiftOut, // handle switch to G1 character set
	0x0f:           handleShiftIn,  // handle switch to G0 character set
}

// decSpecialGraphics is for ESC(0 graphics mode
// https://en.wikipedia.org/wiki/DEC_Special_Graphics
var decSpecialGraphics = map[rune]rune{
	'`': '◆', // filled in diamond
	'a': '▒', // filled in box
	'b': '␉', // horizontal tab symbol
	'c': '␌', // form feed symbol
	'd': '␍', // carriage return symbol
	'e': '␊', // line feed symbol
	'f': '°', // degree symbol
	'g': '±', // plus-minus sign
	'h': '␤', // new line symbol
	'i': '␋', // vertical tab symbol
	'j': '┘', // bottom right
	'k': '┐', // top right
	'l': '┌', // top left
	'm': '└', // bottom left
	'n': '┼', // cross
	'o': '⎺', // scan line 1
	'p': '⎻', // scan line 2
	'q': '─', // scan line 3
	'r': '─', // scan line 4
	's': '⎽', // scan line 5
	't': '├', // vertical and right
	'u': '┤', // vertical and left
	'v': '┴', // horizontal and up
	'w': '┬', // horizontal and down
	'x': '│', // vertical bar
	'y': '≤', // less or equal
	'z': '≥', // greater or equal
	'{': 'π', // pi
	'|': '≠', // not equal
	'}': '£', // Pounds currency symbol
	'~': '·', // centered dot
}

type parseState struct {
	code          string
	esc           int
	osc           bool
	vt100         rune
	apc           bool
	dcs           bool
	printing      bool
	dcsEscPending bool
	csi           bool
}

func (t *Terminal) handleOutput(buf []byte) []byte {
	if t.hasSelectedText() {
		t.clearSelectedText()
	}
	if t.state == nil {
		t.state = &parseState{
			esc: noEscape,
		}
	}
	var (
		size int
		r    rune
		i    = -1
	)
	if t.trace != nil && len(buf) > 0 {
		// Log raw bytes in hex for debugging
		_, _ = t.trace.Write([]byte("IN "))
		for i := 0; i < len(buf); i++ {
			b := buf[i]
			hi := "0123456789ABCDEF"[b>>4]
			lo := "0123456789ABCDEF"[b&0x0F]
			t.trace.Write([]byte{hi, lo})
			if i != len(buf)-1 {
				t.trace.Write([]byte{' '})
			}
		}
		t.trace.Write([]byte{'\n'})
	}

	for {
		i += size
		buf = buf[size:]
		r, size = utf8.DecodeRune(buf)
		if size == 0 {
			break
		}
		if r == utf8.RuneError && size == 1 { // not UTF-8
			if !t.state.printing {
				if t.debug {
					log.Println("Invalid UTF-8", buf[0])
				}
				continue
			}
		}

		if t.state.printing {
			t.parsePrinting(buf, size)
			continue
		}

		// Handle 8-bit C1 controls that map to CSI/OSC/DCS/APC and single-byte IND/NEL/RI
		switch r {
		case 0x84: // IND
			if t.cursorRow < t.scrollBottom {
				t.moveCursor(t.cursorRow+1, t.cursorCol)
			} else {
				fyne.Do(t.scrollDown)
			}
			continue
		case 0x85: // NEL
			if t.cursorRow < t.scrollBottom {
				t.moveCursor(t.cursorRow+1, 0)
			} else {
				fyne.Do(t.scrollDown)
				t.moveCursor(t.scrollBottom, 0)
			}
			continue
		case 0x8d: // RI
			if t.cursorRow > t.scrollTop {
				t.moveCursor(t.cursorRow-1, t.cursorCol)
			} else {
				fyne.Do(t.scrollUp)
			}
			continue
		case 0x90: // DCS
			t.state.dcs = true
			continue
		case 0x9b: // CSI
			t.state.csi = true
			continue
		case 0x9d: // OSC
			t.state.osc = true
			continue
		case 0x9f: // APC
			t.state.apc = true
			continue
		}
		// While inside DCS, accumulate bytes; on ESC check if next is '\\' to terminate
		if t.state.dcs {
			if t.state.dcsEscPending {
				if r == '\\' {
					t.handleDCS(t.state.code)
					t.state.dcs = false
					t.state.code = ""
					t.state.dcsEscPending = false
					continue
				}
				// The ESC was not a terminator, include it and current char
				t.state.code += string(rune(asciiEscape))
				t.state.dcsEscPending = false
			}
			if r == asciiEscape {
				t.state.dcsEscPending = true
				continue
			}
			t.parseDCS(r)
			continue
		}
		if t.state.csi {
			t.parseEscape(r)
			if t.state.esc == noEscape {
				t.state.csi = false
			}
			continue
		}
		if r == asciiEscape {
			t.state.esc = i
			continue
		}
		if t.state.esc == i-1 {
			if cont := t.parseEscState(r); cont {
				continue
			}
			t.state.esc = noEscape
			continue
		}
		if t.state.apc {
			t.parseAPC(r)
			continue
		}
		if t.state.osc {
			t.parseOSC(r)
			continue
		} else if t.state.vt100 != 0 {
			t.handleVT100(string([]rune{t.state.vt100, r}))
			t.state.vt100 = 0
			continue
		} else if t.state.esc != noEscape {
			t.parseEscape(r)
			continue
		}

		if out, ok := specialChars[r]; ok {
			if out == nil {
				continue
			}
			out(t)
		} else {
			// check to see which charset to use
			if t.useG1CharSet {
				t.handleOutputChar(charSetMap[t.g1Charset](r))
			} else {
				t.handleOutputChar(charSetMap[t.g0Charset](r))
			}
		}
	}

	// record progress for next chunk of buffer
	if t.state.esc != noEscape {
		t.state.esc = t.state.esc - i
	}
	return buf
}

func (t *Terminal) parseEscState(r rune) (shouldContinue bool) {
	switch r {
	case '[':
		return true
	case '\\':
		if t.state.osc {
			t.handleOSC(t.state.code)
			t.state.osc = false
		}
		if t.state.apc {
			t.handleAPC(t.state.code)
			t.state.apc = false
		}
		if t.state.dcs {
			t.handleDCS(t.state.code)
			t.state.dcs = false
		}
		t.state.code = ""
	case ']':
		t.state.osc = true
	case 'P':
		t.state.dcs = true
	case '(', ')':
		t.state.vt100 = r
	case '7':
		t.savedRow = t.cursorRow
		t.savedCol = t.cursorCol
	case '8':
		t.cursorRow = t.savedRow
		t.cursorCol = t.savedCol
	case 'D':
		// IND: Index (move cursor down, scroll up within scroll region if at bottom margin)
		if t.cursorRow < t.scrollBottom {
			t.moveCursor(t.cursorRow+1, t.cursorCol)
		} else {
			t.scrollDown()
			// Cursor stays at bottom margin
		}
	case 'E':
		// NEL: Next Line (like CR+LF): move to first column of next line, scrolling within region if needed
		if t.cursorRow < t.scrollBottom {
			t.moveCursor(t.cursorRow+1, 0)
		} else {
			t.scrollDown()
			t.moveCursor(t.scrollBottom, 0)
		}
	case 'M':
		// RI: Reverse Index (move cursor up, scroll down within scroll region if at top margin)
		if t.cursorRow > t.scrollTop {
			t.moveCursor(t.cursorRow-1, t.cursorCol)
		} else {
			t.scrollUp()
			// Cursor stays at top margin
		}
	case 'c':
		// RIS: Full reset
		t.resetTerminal()
	case '_':
		t.state.apc = true
	case '=', '>':
	}
	return false
}

func (t *Terminal) parseEscape(r rune) {
	// Accumulate all CSI parameter and intermediate bytes until we reach a final byte.
	// CSI grammar: parameters 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E.
	t.state.code += string(r)
	if r >= '@' && r <= '~' { // final byte reached
		t.handleEscape(t.state.code)
		t.state.code = ""
		t.state.esc = noEscape
	}
}

func (t *Terminal) parsePrinting(buf []byte, size int) {
	for i := 0; i < size; i++ {
		b := buf[i]

		// Simple state machine to detect ESC[4i
		if b == asciiEscape {
			// Potential start of end sequence
			if i+3 < size && buf[i+1] == '[' && buf[i+2] == '4' && buf[i+3] == 'i' {
				// Found complete ESC[4i sequence, end printing mode
				escapePrinterMode(t, "4")
				t.state.esc = noEscape
				return
			}
			// Not the end sequence, add to print data
			t.printData = append(t.printData, b)
		} else {
			// Regular data, add to print buffer
			t.printData = append(t.printData, b)
		}
	}

	// Also check if we have ESC[4i at the end of accumulated print data
	// (handles case where sequence was split across multiple calls)
	if len(t.printData) >= 4 && bytes.HasSuffix(t.printData, []byte{asciiEscape, '[', '4', 'i'}) {
		// Remove the escape sequence and end printing
		t.printData = t.printData[:len(t.printData)-4]
		escapePrinterMode(t, "4")
		t.state.esc = noEscape
	}
}

func (t *Terminal) parseAPC(r rune) {
	if r == 0 {
		t.handleAPC(t.state.code)
		t.state.code = ""
		t.state.apc = false
	} else {
		t.state.code += string(r)
	}
}

func (t *Terminal) parseDCS(r rune) {
	// DCS content accumulation; termination handled when ESC \\ arrives via parseEscState
	if r == 0 {
		return
	}
	t.state.code += string(r)
}

func (t *Terminal) parseOSC(r rune) {
	if r == asciiBell || r == 0 {
		t.handleOSC(t.state.code)
		t.state.code = ""
		t.state.osc = false
	} else {
		t.state.code += string(r)
	}
}

func (t *Terminal) handleOutputChar(r rune) {
	// Deferred wrap: if a wrap is pending from the previous character, perform it now
	if t.wrapPending {
		t.wrapPending = false
		if t.wrapAround {
			// move to next line (respecting scroll region) and start at column 0
			t.cursorCol = 0
			handleOutputLineFeed(t)
		} else {
			// wrap disabled: keep at last column and overtype
			if t.config.Columns > 0 {
				t.cursorCol = int(t.config.Columns) - 1
			}
		}
	}

	for len(t.content.Rows)-1 < t.cursorRow {
		t.content.Rows = append(t.content.Rows, widget.TextGridRow{})
	}

	// Safety check: ensure cursorRow is within bounds
	if t.cursorRow < 0 || t.cursorRow >= len(t.content.Rows) {
		if t.debug {
			println(fmt.Sprintf("WARNING: handleOutputRune cursorRow %d out of bounds for Rows length %d", t.cursorRow, len(t.content.Rows)))
		}
		return
	}

	cellStyle := widget2.NewTermTextGridStyle(t.currentFG, t.currentBG, highlightBitMask, t.blinking, t.bold, t.underlined)
	for len(t.content.Rows[t.cursorRow].Cells)-1 < t.cursorCol {
		newCell := widget.TextGridCell{
			Rune:  ' ',
			Style: cellStyle,
		}
		t.content.Rows[t.cursorRow].Cells = append(t.content.Rows[t.cursorRow].Cells, newCell)
	}

	if t.blinking {
		cellStyle = widget2.NewTermTextGridStyle(t.currentFG, t.currentBG, highlightBitMask, t.blinking, t.bold, t.underlined)
	}

	// Place the character at the current position (manually to avoid TextGrid internal assumptions)
	// Double-check bounds again before final access
	if t.cursorRow >= 0 && t.cursorRow < len(t.content.Rows) && t.cursorCol >= 0 && t.cursorCol < len(t.content.Rows[t.cursorRow].Cells) {
		t.content.Rows[t.cursorRow].Cells[t.cursorCol] = widget.TextGridCell{Rune: r, Style: cellStyle}
	} else {
		if t.debug {
			println(fmt.Sprintf("WARNING: handleOutputRune final bounds check failed - cursorRow:%d cursorCol:%d rowsLen:%d cellsLen:%d",
				t.cursorRow, t.cursorCol, len(t.content.Rows),
				func() int {
					if t.cursorRow >= 0 && t.cursorRow < len(t.content.Rows) {
						return len(t.content.Rows[t.cursorRow].Cells)
					} else {
						return -1
					}
				}()))
		}
	}

	// Advance cursor/defer wrap according to xterm rules
	lastCol := int(t.config.Columns) - 1
	if t.config.Columns == 0 {
		lastCol = -1
	}
	if t.cursorCol == lastCol {
		if t.wrapAround {
			// Do not move now; set wrap pending so next character triggers LF to next line
			t.wrapPending = true
			// Maintain legacy behavior where cursorCol advances one past the last column
			// so that tests expecting cursorCol == Columns still pass.
			if t.config.Columns > 0 {
				t.cursorCol = int(t.config.Columns)
			}
		} else {
			// No wrap: stay at last column (overtype)
			// cursorCol unchanged
		}
	} else {
		// Normal advance within the line
		t.cursorCol++
	}
}

func (t *Terminal) ringBell() {
	t.bell = true
	fyne.Do(t.Refresh)

	go func() {
		time.Sleep(time.Millisecond * 300)
		t.bell = false
		fyne.Do(t.Refresh)
	}()
}

func (t *Terminal) scrollUp() {
	// Ensure the buffer has at least bottom margin rows
	needed := t.scrollBottom + 1
	for len(t.content.Rows) < needed {
		t.content.Rows = append(t.content.Rows, widget.TextGridRow{})
	}
	// Scroll the region down by one line: shift rows [top+1..bottom] down
	for i := t.scrollBottom; i > t.scrollTop; i-- {
		t.content.Rows[i] = t.content.Row(i - 1)
	}
	// Clear the top line of the region
	t.content.Rows[t.scrollTop] = widget.TextGridRow{}
	t.content.Refresh()
}

func (t *Terminal) scrollDown() {
	// Ensure the buffer has at least bottom margin rows
	needed := t.scrollBottom + 1
	for len(t.content.Rows) < needed {
		t.content.Rows = append(t.content.Rows, widget.TextGridRow{})
	}
	// Scroll the region up by one line: shift rows [top..bottom-1] up
	for i := t.scrollTop; i < t.scrollBottom; i++ {
		t.content.Rows[i] = t.content.Row(i + 1)
	}
	// Clear the bottom line of the region
	t.content.Rows[t.scrollBottom] = widget.TextGridRow{}
	t.content.Refresh()
}

func handleOutputBackspace(t *Terminal) {
	row := t.content.Row(t.cursorRow)
	if len(row.Cells) == 0 {
		return
	}
	t.moveCursor(t.cursorRow, t.cursorCol-1)
}

func handleOutputBell(t *Terminal) {
	t.ringBell()
}

func handleOutputCarriageReturn(t *Terminal) {
	t.moveCursor(t.cursorRow, 0)
}

func handleOutputLineFeed(t *Terminal) {
	if t.cursorRow == t.scrollBottom {
		t.scrollDown()
		if t.newLineMode {
			t.moveCursor(t.cursorRow, 0)
		}
		return
	}
	if t.newLineMode {
		t.moveCursor(t.cursorRow+1, 0)
		return
	}
	t.moveCursor(t.cursorRow+1, t.cursorCol)
}

func handleOutputTab(t *Terminal) {
	end := t.cursorCol - t.cursorCol%tabWidth + tabWidth
	for t.cursorCol < end {
		t.handleOutputChar(' ')
	}
}

func handleShiftOut(t *Terminal) {
	t.useG1CharSet = true
}

func handleShiftIn(t *Terminal) {
	t.useG1CharSet = false
}

// SetPrinterFunc sets the printer function which is executed when printing.
func (t *Terminal) SetPrinterFunc(printerFunc PrinterFunc) {
	t.printer = printerFunc
}
