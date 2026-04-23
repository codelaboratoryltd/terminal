//go:build !windows

package terminal

import "fyne.io/fyne/v2"

// numpadToArrow is a no-op on non-Windows platforms. The virtual keyboard
// arrow key problem (numpad keys sent as Key2/4/6/8) is Windows-specific.
func numpadToArrow(_ *fyne.KeyEvent, _ fyne.KeyName) (fyne.KeyName, bool) {
	return "", false
}
