package terminal

import (
	"bytes"
	"image/color"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// captureWriter is an io.WriteCloser that records everything written to the PTY input,
// used to assert on query responses (DSR-style replies).
type captureWriter struct{ buf bytes.Buffer }

func (c *captureWriter) Write(p []byte) (int, error) { return c.buf.Write(p) }
func (c *captureWriter) Close() error                { return nil }

func newSizedTerm() *Terminal {
	term := New()
	term.CreateRenderer()
	term.config.Columns = 10
	term.config.Rows = 3
	term.scrollTop = 0
	term.scrollBottom = int(term.config.Rows) - 1
	return term
}

func TestREP_RepeatsPrecedingChar(t *testing.T) {
	term := newSizedTerm()
	// Print 'a' then CSI 4 b -> one original plus four repeats == "aaaaa".
	term.handleOutput([]byte("a"))
	term.handleOutput([]byte{asciiEscape, '[', '4', 'b'})

	cells := term.content.Row(0).Cells
	for i := 0; i < 5; i++ {
		assert.Equalf(t, 'a', cells[i].Rune, "cell %d", i)
	}
	assert.Equal(t, 5, term.cursorCol)
}

func TestREP_DefaultCountIsOne(t *testing.T) {
	term := newSizedTerm()
	term.handleOutput([]byte("x"))
	term.handleOutput([]byte{asciiEscape, '[', 'b'}) // no param -> repeat once

	cells := term.content.Row(0).Cells
	assert.Equal(t, 'x', cells[0].Rune)
	assert.Equal(t, 'x', cells[1].Rune)
	assert.Equal(t, 2, term.cursorCol)
}

func TestREP_NoOpAfterCursorMove(t *testing.T) {
	term := newSizedTerm()
	term.handleOutput([]byte("a"))
	// Move the cursor home; this ends the graphic-character run.
	term.handleOutput([]byte{asciiEscape, '[', '1', ';', '1', 'H'})
	term.handleOutput([]byte{asciiEscape, '[', '3', 'b'})

	// Nothing should have been repeated; cursor stays at home.
	assert.Equal(t, 0, term.cursorCol)
	assert.Equal(t, 0, term.cursorRow)
}

func TestOSCColor_SetForeground(t *testing.T) {
	term := New()
	term.handleOSC("10;#ff0000")
	c, ok := term.foregroundColorOverride.(*color.NRGBA)
	assert.True(t, ok)
	assert.Equal(t, color.NRGBA{R: 0xff, G: 0, B: 0, A: 0xff}, *c)
}

func TestOSCColor_SetBackgroundRGBForm(t *testing.T) {
	term := New()
	// rgb:0000/8080/ffff -> 0, ~0x80, 0xff after 16->8 bit scaling.
	term.handleOSC("11;rgb:0000/8080/ffff")
	c, ok := term.backgroundColorOverride.(*color.NRGBA)
	assert.True(t, ok)
	assert.Equal(t, uint8(0), c.R)
	assert.Equal(t, uint8(0x80), c.G)
	assert.Equal(t, uint8(0xff), c.B)
}

func TestOSCColor_SetCursor(t *testing.T) {
	term := New()
	term.handleOSC("12;#00ff00")
	c, ok := term.cursorColorOverride.(*color.NRGBA)
	assert.True(t, ok)
	assert.Equal(t, color.NRGBA{R: 0, G: 0xff, B: 0, A: 0xff}, *c)
}

func TestOSCColor_ShortHexForm(t *testing.T) {
	term := New()
	term.handleOSC("10;#f00") // 1 digit per channel: f -> 0xff
	c := term.foregroundColorOverride.(*color.NRGBA)
	assert.Equal(t, color.NRGBA{R: 0xff, G: 0, B: 0, A: 0xff}, *c)
}

func TestOSCColor_InvalidSpecIgnored(t *testing.T) {
	term := New()
	term.handleOSC("10;notacolor")
	assert.Nil(t, term.foregroundColorOverride)
}

func TestOSCColor_Query(t *testing.T) {
	term := New()
	cw := &captureWriter{}
	term.in = cw

	term.handleOSC("11;?")
	resp := cw.buf.String()
	assert.True(t, strings.HasPrefix(resp, string(rune(asciiEscape))+"]11;rgb:"),
		"unexpected response %q", resp)
	assert.True(t, strings.HasSuffix(resp, string(rune(asciiEscape))+"\\"),
		"response not ST-terminated %q", resp)
}

func TestOSCColor_QueryReflectsSetValue(t *testing.T) {
	term := New()
	cw := &captureWriter{}
	term.in = cw

	term.handleOSC("11;#ff0000")
	term.handleOSC("11;?")
	resp := cw.buf.String()
	// Foreground/background reports use 16-bit components; red is ffff.
	assert.Contains(t, resp, "rgb:ffff/0000/0000")
}
