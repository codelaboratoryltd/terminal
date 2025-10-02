package widget

import (
	"context"
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyne.io/fyne/v2"
)

const blinkingInterval = 500 * time.Millisecond

// TermGrid is a monospaced grid of characters.
// This is designed to be used by our terminal emulator.
type TermGrid struct {
	widget.TextGrid

	tickerCancel context.CancelFunc
}

// TermGridRenderer provides custom rendering for the terminal grid with enhanced underscore visibility
type TermGridRenderer struct {
	grid               *TermGrid
	baseRenderer       fyne.WidgetRenderer
	underscoreOverlays []*canvas.Rectangle
}

// CreateRenderer is a private method to Fyne which links this widget to it's renderer
func (t *TermGrid) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)

	// Create the base TextGrid renderer
	baseRenderer := t.TextGrid.CreateRenderer()

	// Return our custom renderer that wraps the base renderer
	return &TermGridRenderer{
		grid:               t,
		baseRenderer:       baseRenderer,
		underscoreOverlays: make([]*canvas.Rectangle, 0),
	}
}

// Layout implements the WidgetRenderer interface
func (r *TermGridRenderer) Layout(size fyne.Size) {
	r.baseRenderer.Layout(size)
	r.updateUnderscoreOverlays(size)
}

// MinSize implements the WidgetRenderer interface
func (r *TermGridRenderer) MinSize() fyne.Size {
	return r.baseRenderer.MinSize()
}

// Refresh implements the WidgetRenderer interface with enhanced underscore rendering
func (r *TermGridRenderer) Refresh() {
	r.baseRenderer.Refresh()
	r.updateUnderscoreOverlays(r.grid.Size())
}

// Objects implements the WidgetRenderer interface
func (r *TermGridRenderer) Objects() []fyne.CanvasObject {
	objects := r.baseRenderer.Objects()

	// Add underscore overlay rectangles
	for _, overlay := range r.underscoreOverlays {
		objects = append(objects, overlay)
	}

	return objects
}

// Destroy implements the WidgetRenderer interface
func (r *TermGridRenderer) Destroy() {
	r.baseRenderer.Destroy()
	r.underscoreOverlays = nil
}

// updateUnderscoreOverlays creates visible overlay rectangles for underscore characters
func (r *TermGridRenderer) updateUnderscoreOverlays(size fyne.Size) {
	// Clear existing overlays
	r.underscoreOverlays = r.underscoreOverlays[:0]

	if len(r.grid.Rows) == 0 {
		return
	}

	// Calculate cell dimensions
	rows := float32(len(r.grid.Rows))
	cols := float32(0)
	if rows > 0 && len(r.grid.Rows[0].Cells) > 0 {
		cols = float32(len(r.grid.Rows[0].Cells))
	}

	if rows == 0 || cols == 0 {
		return
	}

	cellWidth := size.Width / cols
	cellHeight := size.Height / rows

	// Create overlay rectangles for each underscore character
	for rowIdx, row := range r.grid.Rows {
		if row.Cells == nil {
			continue
		}
		for colIdx, cell := range row.Cells {
			if cell.Rune == '_' {
				// Create a more visible rectangle for the underscore
				overlay := canvas.NewRectangle(r.getUnderscoreColor(cell))

				// Position the overlay at the bottom of the cell (like an underscore)
				x := float32(colIdx) * cellWidth
				y := float32(rowIdx)*cellHeight + cellHeight*0.90 // Position near bottom
				width := cellWidth
				height := cellHeight * 0.10 // Make it thicker than typical underscore

				overlay.Move(fyne.NewPos(x, y))
				overlay.Resize(fyne.NewSize(width, height))

				r.underscoreOverlays = append(r.underscoreOverlays, overlay)
			}
		}
	}
}

// getUnderscoreColor determines the appropriate color for underscore overlays
func (r *TermGridRenderer) getUnderscoreColor(cell widget.TextGridCell) color.Color {
	if cell.Style != nil {
		if textColor := cell.Style.TextColor(); textColor != nil {
			return textColor
		}
	}
	// Fallback to theme foreground color
	return theme.Color(theme.ColorNameForeground)
}

// NewTermGrid creates a new empty TextGrid widget.
func NewTermGrid() *TermGrid {
	grid := &TermGrid{}
	grid.ExtendBaseWidget(grid)

	grid.Scroll = container.ScrollNone
	return grid
}

// Refresh will be called when this grid should update.
// We update our blinking status and then call the TextGrid we extended to refresh too.
func (t *TermGrid) Refresh() {
	// Safety check: don't refresh if Rows is nil (during cleanup)
	if t.Rows == nil {
		return
	}
	t.refreshBlink(false)
}

func (t *TermGrid) refreshBlink(blink bool) {
	// reset shouldBlink which can be set by setCellRune if a cell with BlinkEnabled is found
	shouldBlink := false

	// Safety check: ensure Rows is not nil before accessing
	if t.Rows == nil {
		return
	}

	for _, row := range t.Rows {
		if row.Cells == nil {
			continue
		}
		for _, r := range row.Cells {
			if s, ok := r.Style.(*TermTextGridStyle); ok && s != nil && s.BlinkEnabled {
				shouldBlink = true

				s.blink(blink)
			}
		}
	}

	fyne.Do(func() {
		t.TextGrid.Refresh()
	})

	switch {
	case shouldBlink && t.tickerCancel == nil:
		t.runBlink()
	case !shouldBlink && t.tickerCancel != nil:
		t.tickerCancel()
		t.tickerCancel = nil
	}
}

// StopBlink stops any active blinking animation
func (t *TermGrid) StopBlink() {
	if t.tickerCancel != nil {
		t.tickerCancel()
		t.tickerCancel = nil
	}
}

func (t *TermGrid) runBlink() {
	if t.tickerCancel != nil {
		t.tickerCancel()
		t.tickerCancel = nil
	}
	var tickerContext context.Context
	tickerContext, t.tickerCancel = context.WithCancel(context.Background())
	ticker := time.NewTicker(blinkingInterval)
	blinking := false
	go func() {
		defer func() {
			ticker.Stop()
			if r := recover(); r != nil {
				// Log panic but don't crash the application
				fmt.Printf("Panic in TermGrid blink goroutine: %v\n", r)
			}
		}()

		for {
			select {
			case <-tickerContext.Done():
				return
			case <-ticker.C:
				blinking = !blinking
				fyne.Do(func() {
					t.refreshBlink(blinking)
				})
			}
		}
	}()
}
