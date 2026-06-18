package widget

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TestLastDirtyBoundsTracksRenderedRegion verifies the signal the renderer's
// scissor self-heal depends on: LastDirtyBounds must report the full image on a
// first (full) render, and tighten to just the changed region on an incremental
// render. If it ever over-reported (e.g. returned full bounds every frame) the
// self-heal could never detect a spill; if it under-reported it would heal
// constantly.
func TestLastDirtyBoundsTracksRenderedRegion(t *testing.T) {
	app := test.NewApp()
	th := app.Settings().Theme()
	mono := th.Font(fyne.TextStyle{Monospace: true})
	bold := th.Font(fyne.TextStyle{Bold: true})
	textSizePt := th.Size(theme.SizeNameText)

	cw, ch := RasterCellSize(textSizePt, mono, bold)
	if cw <= 0 || ch <= 0 {
		t.Fatalf("invalid cell size %vx%v", cw, ch)
	}

	const cols, rows = 10, 4
	w := int(cw) * cols
	h := int(ch) * rows

	grid := NewTermGrid()
	grid.SetGridDimensions(cols, rows)
	grid.Rows = make([]widget.TextGridRow, rows)
	for r := range grid.Rows {
		cells := make([]widget.TextGridCell, cols)
		for c := range cells {
			cells[c] = widget.TextGridCell{Rune: 'a'}
		}
		grid.Rows[r] = widget.TextGridRow{Cells: cells}
	}

	// First render is a full redraw.
	grid.drawToImage(w, h)
	full := grid.LastDirtyBounds()
	if full.Dx() != w || full.Dy() != h {
		t.Fatalf("first render should be full %dx%d, got %v", w, h, full)
	}

	// Change a single cell in the top row; the next render must report a region
	// confined to that row, strictly shorter than the full image.
	grid.Rows[0].Cells[0].Rune = 'X'
	grid.drawToImage(w, h)
	partial := grid.LastDirtyBounds()
	if partial.Empty() {
		t.Fatal("single-cell change produced an empty dirty rect")
	}
	if partial.Dy() >= h {
		t.Errorf("incremental dirty rect not tighter than full: got height %d, full %d", partial.Dy(), h)
	}
	if !partial.In(full) {
		t.Errorf("incremental dirty rect %v escaped full bounds %v", partial, full)
	}
}
