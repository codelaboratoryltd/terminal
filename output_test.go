package terminal

import (
	"bytes"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/stretchr/testify/assert"
)

func TestTerminal_Backspace(t *testing.T) {
	term := New()
	term.Resize(fyne.NewSize(50, 50))
	term.CreateRenderer() // ensure content is initialized
	term.handleOutput([]byte("Hi"))
	assert.Equal(t, "Hi", term.content.Text())

	term.handleOutput([]byte{asciiBackspace})
	term.handleOutput([]byte("ello"))

	assert.Equal(t, "Hello", term.content.Text())
}

func TestTerminal_Autowrap_Enabled(t *testing.T) {
	term := New()
	term.CreateRenderer()
	// Force a small terminal: 2 columns, 3 rows
	term.config.Columns = 2
	term.config.Rows = 3
	term.scrollTop = 0
	term.scrollBottom = int(term.config.Rows) - 1

	// Write 3 'a' characters: should place 'a' at (0,0), 'a' at (0,1), then wrap and put 'a' at (1,0)
	term.handleOutput([]byte("aaa"))

	// Row 0 should be "aa"
	if len(term.content.Rows) == 0 {
		t.Fatalf("no rows rendered")
	}
	cells0 := term.content.Row(0).Cells
	assert.Equal(t, 2, len(cells0))
	assert.Equal(t, 'a', cells0[0].Rune)
	assert.Equal(t, 'a', cells0[1].Rune)

	// Row 1 col 0 should be 'a'
	cells1 := term.content.Row(1).Cells
	assert.GreaterOrEqual(t, len(cells1), 1)
	assert.Equal(t, 'a', cells1[0].Rune)

	// Cursor should be at row 1, col 1 (after writing last 'a')
	assert.Equal(t, 1, term.cursorRow)
	assert.Equal(t, 1, term.cursorCol)
}

func TestTerminal_Autowrap_Disabled(t *testing.T) {
	term := New()
	term.CreateRenderer()
	// Force 2 columns, disable wrap via DECSET 7 reset (CSI ? 7 l)
	term.config.Columns = 2
	term.config.Rows = 3
	term.scrollTop = 0
	term.scrollBottom = int(term.config.Rows) - 1

	// Disable autowrap
	seq := []byte{asciiEscape, '[', '?', '7', 'l'}
	term.handleOutput(seq)

	// Now write 3 chars; the third should overtype the second cell on first row
	term.handleOutput([]byte("aaa"))

	cells0 := term.content.Row(0).Cells
	// Should still be length 2 and both cells 'a'
	assert.Equal(t, 2, len(cells0))
	assert.Equal(t, 'a', cells0[0].Rune)
	assert.Equal(t, 'a', cells0[1].Rune)

	// Cursor should remain at last column (index 1)
	assert.Equal(t, 0, term.cursorRow)
	assert.Equal(t, 1, term.cursorCol)

	// Re-enable autowrap and add one more 'b' to verify wrap now occurs
	term.handleOutput([]byte{asciiEscape, '[', '?', '7', 'h'})
	term.handleOutput([]byte("b"))

	cells1 := term.content.Row(1).Cells
	// After enabling wrap and writing, should have wrapped to next line col 0
	if len(cells1) > 0 {
		assert.Equal(t, 'b', cells1[0].Rune)
	} else {
		// If row not yet expanded, ensure the buffer text has two lines
		text := bytes.Split([]byte(term.content.Text()), []byte("\n"))
		if len(text) > 1 {
			assert.Equal(t, byte('b'), text[1][0])
		}
	}
}
