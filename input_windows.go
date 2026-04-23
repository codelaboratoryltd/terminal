//go:build windows

package terminal

import "fyne.io/fyne/v2"

// numpadToArrow returns the corresponding arrow key for a numpad directional key press,
// using Windows hardware scan codes to distinguish numpad keys from regular number keys.
//
// When NumLock is ON, the Windows virtual keyboard (and physical numpad) sends
// fyne.Key2/4/6/8 — the same key name as the top-row number keys. The hardware scan
// code is the only reliable way to tell them apart:
//
//	Numpad 2 → scan code 80 (0x50)  → cursor down
//	Numpad 4 → scan code 75 (0x4B)  → cursor left
//	Numpad 6 → scan code 77 (0x4D)  → cursor right
//	Numpad 8 → scan code 72 (0x48)  → cursor up
//
// A scan code of 0 is also accepted as a numpad/virtual-keyboard indicator because
// some on-screen keyboards inject key events via SendInput without a scan code.
// Regular top-row keys 2/4/6/8 always carry scan codes 3/5/7/9 on Windows.
//
// Returns ("", false) when the key is not a numpad directional key.
func numpadToArrow(e *fyne.KeyEvent, key fyne.KeyName) (fyne.KeyName, bool) {
	sc := e.Physical.ScanCode
	switch key {
	case fyne.Key8:
		if sc == 72 || sc == 0 { // numpad 8: scan code 0x48
			return fyne.KeyUp, true
		}
	case fyne.Key2:
		if sc == 80 || sc == 0 { // numpad 2: scan code 0x50
			return fyne.KeyDown, true
		}
	case fyne.Key4:
		if sc == 75 || sc == 0 { // numpad 4: scan code 0x4B
			return fyne.KeyLeft, true
		}
	case fyne.Key6:
		if sc == 77 || sc == 0 { // numpad 6: scan code 0x4D
			return fyne.KeyRight, true
		}
	}
	return "", false
}
