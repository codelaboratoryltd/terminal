package terminal

import (
	"strings"

	"fyne.io/fyne/v2"

	widget2 "github.com/fyne-io/terminal/internal/widget"
)

// appliedHighlightRange records the range previously passed to HighlightRange
// so the next clear can target exactly those cells, regardless of subsequent
// changes to selStart/selEnd or the row Cells slices.
type appliedHighlightRange struct {
	blockMode                          bool
	startRow, startCol, endRow, endCol int
}

// getSelectedRange returns the current selection range, start row, start col, end row, end col
// It always returns a positive selection
func (t *Terminal) getSelectedRange() (int, int, int, int) {
	if t.selStart == nil || t.selEnd == nil {
		return 0, 0, 0, 0
	}

	startRow := t.selStart.Row
	startCol := t.selStart.Col
	endRow := t.selEnd.Row
	endCol := t.selEnd.Col

	if t.blockMode {
		if startCol > endCol {
			// Swap the start and end colums
			startCol, endCol = endCol, startCol
		}

		if startRow > endRow {
			// Swap the start and end rows
			startRow, endRow = endRow, startRow
		}

		return startRow - 1, startCol - 1, endRow - 1, endCol - 1
	}
	// Check if the user has selected in reverse
	if startRow > endRow || (startRow == endRow && startCol > endCol) {
		// Swap the start and end rows and columns
		startRow, endRow = endRow, startRow
		startCol, endCol = endCol, startCol
	}

	return startRow - 1, startCol - 1, endRow - 1, endCol - 1
}

func (t *Terminal) highlightSelectedText() {
	if t.appliedHighlight != nil {
		a := t.appliedHighlight
		widget2.ClearHighlightRange(t.content, a.blockMode, a.startRow, a.startCol, a.endRow, a.endCol)
	}
	sr, sc, er, ec := t.getSelectedRange()
	widget2.HighlightRange(t.content, t.blockMode, sr, sc, er, ec, highlightBitMask)
	t.appliedHighlight = &appliedHighlightRange{
		blockMode: t.blockMode,
		startRow:  sr, startCol: sc, endRow: er, endCol: ec,
	}
	t.Refresh()
}

func (t *Terminal) clearSelectedText() {
	if t.appliedHighlight != nil {
		a := t.appliedHighlight
		widget2.ClearHighlightRange(t.content, a.blockMode, a.startRow, a.startCol, a.endRow, a.endCol)
		t.appliedHighlight = nil
	}
	t.Refresh()
	t.blockMode = false
	t.selecting = false
	t.selStart = nil
	t.selEnd = nil
}

// SelectedText gets the text that is currently selected.
func (t *Terminal) SelectedText() string {
	sr, sc, er, ec := t.getSelectedRange()
	return widget2.GetTextRange(t.content, t.blockMode, sr, sc, er, ec)
}

func (t *Terminal) copySelectedText(clipboard fyne.Clipboard, clearSelection bool) {
	// copy start and end sel to clipboard and clear the sel style
	text := t.SelectedText()
	fyne.CurrentApp()
	clipboard.SetContent(text)
	if clearSelection {
		t.clearSelectedText()
	}
}

func (t *Terminal) pasteText(clipboard fyne.Clipboard) {
	content := clipboard.Content()

	// Normalise line endings for terminal
	content = strings.ReplaceAll(content, "\r\n", "\r")
	content = strings.ReplaceAll(content, "\n", "\r")

	if t.bracketedPasteMode {
		// In bracketed paste mode, wrap content with escape sequences
		_, _ = t.in.Write(append(
			append(
				[]byte{asciiEscape, '[', '2', '0', '0', '~'},
				[]byte(content)...),
			[]byte{asciiEscape, '[', '2', '0', '1', '~'}...),
		)
		return
	}

	// Normal paste without bracketing
	_, _ = t.in.Write([]byte(content))
}

func (t *Terminal) hasSelectedText() bool {
	return t.selStart != nil && t.selEnd != nil
}
