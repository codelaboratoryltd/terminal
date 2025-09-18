package terminal

import (
	"context"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	widget2 "github.com/fyne-io/terminal/internal/widget"
)

const (
	cursorWidthBlock = 0 // 0 means use full cell width for block cursor
	cursorWidthCaret = 2 // 2 pixels wide for caret cursor
)

type render struct {
	term          *Terminal
	bg            *canvas.Rectangle
	bgClearCancel context.CancelFunc
}

func (r *render) Layout(s fyne.Size) {
	// Ensure background covers the full widget area to clear stale canvas content
	if r.bg != nil {
		r.bg.Move(fyne.NewPos(0, 0))
		r.bg.Resize(s)
	}
	// Size the grid strictly to the configured rows/cols area so any space
	// outside is left to the background to clear. Avoid stretching grid to s.
	cell := r.term.guessCellSize()
	width := float32(r.term.config.Columns) * cell.Width
	height := float32(r.term.config.Rows) * cell.Height
	r.term.content.Resize(fyne.NewSize(width, height))
}

func (r *render) MinSize() fyne.Size {
	return fyne.NewSize(0, 0) // don't get propped open by the text cells
}

func (r *render) Refresh() {
	r.moveCursor()
	r.term.refreshCursor()

	// Keep background color in sync with theme and refresh to clear outside grid
	if r.bg != nil {
		r.bg.FillColor = theme.Color(theme.ColorNameBackground)
		r.bg.Refresh()
	}
	r.term.content.Refresh()
}

func (r *render) BackgroundColor() color.Color {
	// Use an opaque background so the canvas clears any stale pixels
	// when this widget is refreshed/resized
	return theme.Color(theme.ColorNameBackground)
}

func (r *render) Objects() []fyne.CanvasObject {
	// Draw background first so it clears canvas area outside the grid
	return []fyne.CanvasObject{r.bg, r.term.content, r.term.cursor}
}

func (r *render) Destroy() {
	if r.bgClearCancel != nil {
		r.bgClearCancel()
		r.bgClearCancel = nil
	}
}

func (r *render) moveCursor() {
	cell := r.term.guessCellSize()
	r.term.cursor.Move(fyne.NewPos(cell.Width*float32(r.term.cursorCol), cell.Height*float32(r.term.cursorRow)))
}

func (t *Terminal) refreshCursor() {
	// Hide if we don't have focus or cursor hidden flag is set, blink handling may toggle Hidden too.
	hidden := !t.focused || t.cursorHidden
	t.cursor.Hidden = hidden

	// Base color selection (bell overrides)
	if t.bell {
		t.cursor.FillColor = theme.Color(theme.ColorNameError)
	} else {
		// Use custom theme cursor color if available, otherwise use primary
		if t.customTheme != nil {
			if cursorColor := t.customTheme.Color("cursor", theme.VariantDark); cursorColor != nil {
				t.cursor.FillColor = cursorColor
			} else {
				t.cursor.FillColor = theme.Color(theme.ColorNamePrimary)
			}
		} else {
			t.cursor.FillColor = theme.Color(theme.ColorNamePrimary)
		}
	}

	// Determine cursor width/height based on shape
	cellSize := t.guessCellSize()
	var width float32
	if t.cursorShape == "caret" {
		width = float32(cursorWidthCaret)
	} else {
		// Default to block cursor
		width = cellSize.Width
	}

	t.cursor.Resize(fyne.NewSize(width, cellSize.Height))

	// Cursor visual adjustments:
	// - For caret: solid thin bar
	// - For block: semi-transparent fill so text remains visible beneath, giving an invert-like emphasis
	if t.cursorShape == "caret" {
		// Solid caret, ensure full opacity
		if col, ok := t.cursor.FillColor.(color.NRGBA); ok {
			col.A = 0xFF
			t.cursor.FillColor = col
		} else if col, ok := t.cursor.FillColor.(color.RGBA); ok {
			col.A = 0xFF
			t.cursor.FillColor = col
		}
		t.cursor.StrokeColor = color.Transparent
		t.cursor.StrokeWidth = 0
	} else {
		// Block: translucent overlay + subtle outline to keep glyphs readable
		primary := t.cursor.FillColor
		switch c := primary.(type) {
		case color.NRGBA:
			if c.A > 0x88 {
				c.A = 0x88
			}
			t.cursor.FillColor = c
		case color.RGBA:
			if c.A > 0x88 {
				c.A = 0x88
			}
			t.cursor.FillColor = c
		default:
			// Fallback: use theme primary with ~50% alpha
			pc := theme.Color(theme.ColorNamePrimary)
			if col, ok := pc.(color.NRGBA); ok {
				col.A = 0x88
				t.cursor.FillColor = col
			} else if col, ok := pc.(color.RGBA); ok {
				col.A = 0x88
				t.cursor.FillColor = col
			}
		}
		t.cursor.StrokeColor = theme.Color(theme.ColorNamePrimary)
		t.cursor.StrokeWidth = 1
	}

	// Ensure blinking is active/paused based on current state
	t.ensureCursorBlinking()

	fyne.Do(func() {
		t.cursor.Refresh()
	})

}

// CreateRenderer requests a new renderer for this terminal (just a wrapper around the TextGrid)
func (t *Terminal) CreateRenderer() fyne.WidgetRenderer {
	r := &render{term: t}
	t.cursorMoved = r.moveCursor

	if t.content != nil {
		// Term already has content, just return the renderer with the existing term,
		// leaving its content and setup intact
		// Create background to ensure out-of-bounds canvas area is cleared
		r.bg = canvas.NewRectangle(theme.Color(theme.ColorNameBackground))
		// Start periodic background refresh to clear any stale canvas outside grid
		r.runBgClear()
		return r
	}

	t.ExtendBaseWidget(t)

	t.content = widget2.NewTermGrid()

	t.setupShortcuts()

	t.cursor = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	t.cursor.Hidden = true

	// Determine cursor width based on shape
	cellSize := t.guessCellSize()
	var width float32
	if t.cursorShape == "caret" {
		width = float32(cursorWidthCaret)
	} else {
		// Default to block cursor
		width = cellSize.Width
	}
	t.cursor.Resize(fyne.NewSize(width, cellSize.Height))

	// Background rectangle to clear outside-grid canvas area
	r.bg = canvas.NewRectangle(theme.Color(theme.ColorNameBackground))
	// Start periodic background refresh to clear any stale canvas outside grid
	r.runBgClear()

	return r
}

// runBgClear starts a periodic refresh of the background rectangle to
// ensure canvas area outside the grid is regularly cleared. This helps
// in cases where asynchronous draws occur after a resize.
func (r *render) runBgClear() {
	if r.bg == nil {
		return
	}
	if r.bgClearCancel != nil {
		r.bgClearCancel()
		r.bgClearCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.bgClearCancel = cancel
	interval := 500 * time.Millisecond
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fyne.Do(func() {
					if r.bg != nil {
						r.bg.FillColor = theme.Color(theme.ColorNameBackground)
						r.bg.Refresh()
					}
				})
			}
		}
	}()
}
