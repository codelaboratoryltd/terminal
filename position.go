package terminal

import (
	"fmt"

	"fyne.io/fyne/v2"
)

type position struct {
	Col, Row int
}

func (r position) String() string {
	return fmt.Sprintf("row: %x, col: %x", r.Row, r.Col)
}

func (t *Terminal) getTermPosition(pos fyne.Position) position {
	cell := t.guessCellSize()
	// account for centering offsets in fixed mode (stored on terminal)
	x := pos.X - t.offsetX
	y := pos.Y - t.offsetY
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	col := int(x/cell.Width) + 1
	row := int(y/cell.Height) + 1
	return position{col, row}
}

// getTextPosition converts a terminal position (row and col) to fyne coordinates.
func (t *Terminal) getTextPosition(pos position) fyne.Position {
	cell := t.guessCellSize()
	x := (pos.Col - 1) * int(cell.Width)
	y := (pos.Row - 1) * int(cell.Height)
	return fyne.NewPos(t.offsetX+float32(x), t.offsetY+float32(y))
}
