package widget

import (
	"image"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TestRowsLockGuardsConcurrentRenderAndRealloc reproduces the resize-while-output
// crash: the PTY goroutine reallocates TermGrid.Rows (append rows / swap the Rows
// slice / replace a row's Cells) while the paint goroutine reads Rows through
// RenderTermToImage. Before the rowsMu guard, a read overlapping a reallocation
// could observe a torn slice header and fault. Run under -race; it must stay clean
// and must not panic.
func TestRowsLockGuardsConcurrentRenderAndRealloc(t *testing.T) {
	app := test.NewApp()

	th := app.Settings().Theme()
	v := app.Settings().ThemeVariant()
	fg := th.Color(theme.ColorNameForeground, v)
	bg := th.Color(theme.ColorNameBackground, v)
	mono := th.Font(fyne.TextStyle{Monospace: true})
	bold := th.Font(fyne.TextStyle{Bold: true})

	const cols, rows = 40, 20
	grid := NewTermGrid()
	grid.SetGridDimensions(cols, rows)
	pix := image.NewRGBA(image.Rect(0, 0, cols*10, rows*18))

	var writerWg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: mimic handleOutput mutating Rows under the write lock.
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			grid.LockRows()
			// Reassign the whole slice (clear), then grow it back, replacing each
			// row's Cells — the realloc patterns handleOutput actually performs.
			grid.Rows = []widget.TextGridRow{}
			for r := 0; r < rows; r++ {
				cells := make([]widget.TextGridCell, cols)
				for c := range cells {
					cells[c] = widget.TextGridCell{Rune: rune('A' + (i+c)%26)}
				}
				grid.Rows = append(grid.Rows, widget.TextGridRow{Cells: cells})
			}
			grid.UnlockRows()
		}
	}()

	// Reader (this goroutine): mimic the paint path reading Rows under the read lock.
	var snaps []cellSnap
	for i := 0; i < 3000; i++ {
		grid.RLockRows()
		snaps, _ = RenderTermToImage(grid.Rows, pix, cols, rows, fg, bg, 12, mono, bold, 1, snaps)
		grid.RUnlockRows()
	}

	close(stop)
	writerWg.Wait()
}
