package terminal

import (
	"image/color"

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
	term *Terminal
}

func (r *render) Layout(s fyne.Size) {
	r.term.content.Resize(s)
}

func (r *render) MinSize() fyne.Size {
	return fyne.NewSize(0, 0) // don't get propped open by the text cells
}

func (r *render) Refresh() {
	r.moveCursor()
	r.term.refreshCursor()

	r.term.content.Refresh()
}

func (r *render) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *render) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.term.content, r.term.cursor}
}

func (r *render) Destroy() {
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

	return r
}
