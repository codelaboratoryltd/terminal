package widget

import (
	"image"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TestRenderGlyphDrawsInkAndClipsToCell exercises the direct face.Glyph +
// draw.DrawMask path (the allocation-free replacement for font.Drawer.DrawString):
// a glyph must actually paint ink inside its own cell and must not bleed into the
// adjacent cell's column.
func TestRenderGlyphDrawsInkAndClipsToCell(t *testing.T) {
	app := test.NewApp()
	th := app.Settings().Theme()
	v := app.Settings().ThemeVariant()
	fg := th.Color(theme.ColorNameForeground, v)
	bg := th.Color(theme.ColorNameBackground, v)
	mono := th.Font(fyne.TextStyle{Monospace: true})
	bold := th.Font(fyne.TextStyle{Bold: true})
	textSizePt := th.Size(theme.SizeNameText)

	cw, ch := RasterCellSize(textSizePt, mono, bold)
	if cw <= 0 || ch <= 0 {
		t.Fatalf("invalid cell size %vx%v", cw, ch)
	}

	const cols, rows = 4, 1
	w := int(cw) * cols
	h := int(ch) * rows
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// 'M' fills most of a cell; the rest are spaces.
	grid := []widget.TextGridRow{{Cells: []widget.TextGridCell{
		{Rune: 'M'}, {Rune: ' '}, {Rune: ' '}, {Rune: ' '},
	}}}

	_, dirty := RenderTermToImage(grid, img, cols, rows, fg, bg, textSizePt, mono, bold, 1, nil)
	if dirty.Empty() {
		t.Fatal("expected a non-empty dirty rect on full redraw (font load failure?)")
	}

	cellW := w / cols
	// Reference background sampled from the far-right space cell.
	ref := img.RGBAAt(3*cellW+cellW/2, h/2)

	countInk := func(x0, x1 int) int {
		n := 0
		for y := 0; y < h; y++ {
			for x := x0; x < x1; x++ {
				if img.RGBAAt(x, y) != ref {
					n++
				}
			}
		}
		return n
	}

	if got := countInk(0, cellW); got == 0 {
		t.Error("glyph cell has no ink — glyph was not drawn")
	}
	if got := countInk(cellW, 2*cellW); got != 0 {
		t.Errorf("ink bled into the adjacent space cell column: %d px", got)
	}
}
