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
	t.cursor.Hidden = !t.focused || t.cursorHidden
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
	fyne.Do(func() {
		t.cursor.Refresh()
	})

}

// CreateRenderer requests a new renderer for this terminal (just a wrapper around the TextGrid)
func (t *Terminal) CreateRenderer() fyne.WidgetRenderer {
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

	r := &render{term: t}
	t.cursorMoved = r.moveCursor
	return r
}
