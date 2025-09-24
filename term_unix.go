//go:build !windows
// +build !windows

package terminal

import (
	"io"
	"os"
	"os/exec"

	"fyne.io/fyne/v2"
	"github.com/creack/pty"
)

func (t *Terminal) updatePTYSize() {
	if t.pty == nil { // SSH or other direct connection?
		return
	}
	scale := float32(1.0)
	c := fyne.CurrentApp().Driver().CanvasForObject(t)
	if c != nil {
		scale = c.Scale()
	}
	// Use the actual grid pixel size (cols/rows * cell size), not the widget size,
	// so upstream programs see accurate pixel dimensions for the drawable area.
	cell := t.guessCellSize()
	gridW := float32(t.config.Columns) * cell.Width
	gridH := float32(t.config.Rows) * cell.Height
	_ = pty.Setsize(t.pty.(*os.File), &pty.Winsize{
		Rows: uint16(t.config.Rows), Cols: uint16(t.config.Columns),
		X: uint16(gridW * scale), Y: uint16(gridH * scale)})
}

func (t *Terminal) startPTY() (io.WriteCloser, io.Reader, io.Closer, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}

	env := os.Environ()
	env = append(env, "TERM=xterm-256color")
	c := exec.Command(shell)
	c.Dir = t.startingDir()
	c.Env = env
	t.cmd = c

	// Start the command with a pty.
	f, err := pty.Start(c)
	return f, f, f, err
}
