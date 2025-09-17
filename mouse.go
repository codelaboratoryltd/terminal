package terminal

import (
	"strconv"

	"fyne.io/fyne/v2"
)

func (t *Terminal) handleMouseDownV200(btn int, mods fyne.KeyModifier, pos fyne.Position) {
	_, _ = t.Write(t.encodeMouse(btn, mods, pos))
}

func (t *Terminal) handleMouseDownX10(btn int, _ fyne.KeyModifier, pos fyne.Position) {
	_, _ = t.Write(t.encodeMouse(btn, 0, pos))
}

func (t *Terminal) handleMouseUpV200(btn int, mods fyne.KeyModifier, pos fyne.Position) {
	_, _ = t.Write(t.encodeMouse(0, mods, pos))
}

func (t *Terminal) handleMouseUpX10(_ int, _ fyne.KeyModifier, _ fyne.Position) {
	// no-op for X10 mode
}

func (t *Terminal) encodeMouse(button int, mods fyne.KeyModifier, pos fyne.Position) []byte {
	p := t.getTermPosition(pos)
	var btn byte
	if button == 0 {
		btn = 3
	} else {
		btn = byte(button) - 1
	}

	if mods&fyne.KeyModifierShift != 0 {
		btn += 4
	}
	if mods&fyne.KeyModifierAlt != 0 {
		btn += 8
	}
	if mods&fyne.KeyModifierControl != 0 {
		btn += 16
	}

	if t.mouseSGR {
		// SGR extended mouse protocol: CSI < btn;x;y M/m
		// Button encodes modifiers
		b := int(btn)
		if mods&fyne.KeyModifierShift != 0 {
			b += 4
		}
		if mods&fyne.KeyModifierAlt != 0 {
			b += 8
		}
		if mods&fyne.KeyModifierControl != 0 {
			b += 16
		}
		// Press/drag -> 'M', release -> 'm'
		suffix := byte('M')
		if button == 0 {
			suffix = 'm'
		}
		// 1-based coords
		x := int(p.Col)
		y := int(p.Row)
		buf := []byte{asciiEscape, '['}
		buf = append(buf, '<')
		buf = append(buf, []byte(strconv.Itoa(b))...)
		buf = append(buf, ';')
		buf = append(buf, []byte(strconv.Itoa(x))...)
		buf = append(buf, ';')
		buf = append(buf, []byte(strconv.Itoa(y))...)
		buf = append(buf, suffix)
		return buf
	}
	return []byte{asciiEscape, '[', 'M', 32 + btn, 32 + byte(p.Col), 32 + byte(p.Row)}
}
