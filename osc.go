package terminal

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"strconv"
	"strings"

	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
)

func (t *Terminal) handleOSC(code string) {
	if len(code) == 0 {
		return
	}

	// Parse the command number and data
	parts := strings.SplitN(code, ";", 2)
	if len(parts) < 2 {
		if t.debug {
			log.Println("Invalid OSC format:", code)
		}
		return
	}

	commandNum, err := strconv.Atoi(parts[0])
	if err != nil {
		if t.debug {
			log.Println("Invalid OSC command number:", parts[0])
		}
		return
	}
	data := parts[1]

	// Check if there's a registered handler for this command
	if t.oscHandlers != nil {
		if handler, exists := t.oscHandlers[commandNum]; exists {
			handler(data)
			return
		}
	}

	// Fall back to default behavior for built-in commands
	switch commandNum {
	case 0:
		// set icon name and window title
		t.setTitle(data)
	case 1:
		// set icon name, if Fyne supports in the future
	case 2:
		// set window title
		t.setTitle(data)
	case 7:
		t.setDirectory(data)
	case 10:
		// Set/query default foreground colour
		t.handleOSCColor(10, data)
	case 11:
		// Set/query default background colour
		t.handleOSCColor(11, data)
	case 12:
		// Set/query cursor colour
		t.handleOSCColor(12, data)
	case 133:
		// Shell integration sequences for prompt marking
		t.handleOSC133(data)
	default:
		if t.debug {
			log.Println("Unrecognised OSC:", code)
		}
	}
}

// handleOSCColor implements OSC 10/11/12 (set/query default fg, bg and cursor colour).
// A data value of "?" is a query: we report the current colour back to the PTY. Otherwise
// data is an X11 colour spec ("#rrggbb", "rgb:rrrr/gggg/bbbb", …) that sets the colour.
//
// xterm allows several specs in one OSC (each applying to the next item); that is rare, so
// we handle the first/only spec here and ignore trailing ones.
func (t *Terminal) handleOSCColor(code int, data string) {
	spec := data
	if i := strings.IndexByte(spec, ';'); i >= 0 {
		spec = spec[:i]
	}

	if spec == "?" {
		t.reportOSCColor(code)
		return
	}

	c, ok := parseXColor(spec)
	if !ok {
		if t.debug {
			log.Printf("OSC %d: unparseable colour spec %q", code, spec)
		}
		return
	}

	switch code {
	case 10: // default foreground
		t.foregroundColorOverride = c
		if t.contentThemer != nil {
			t.contentThemer.foregroundColor = c
		}
		if t.currentFG == nil {
			t.currentFG = c
		}
		t.invalidateBlinkGridCache()
	case 11: // default background
		t.backgroundColorOverride = c
		if t.contentThemer != nil {
			t.contentThemer.backgroundColor = c
		}
		if t.currentBG == nil {
			t.currentBG = c
		}
		t.invalidateBlinkGridCache()
	case 12: // cursor
		t.cursorColorOverride = c
		t.refreshCursor()
	}
}

// reportOSCColor writes the current fg/bg/cursor colour back to the PTY in response to an
// OSC ?-query, using xterm's "rgb:RRRR/GGGG/BBBB" reply format terminated by ST.
func (t *Terminal) reportOSCColor(code int) {
	var c color.Color
	switch code {
	case 10:
		if t.foregroundColorOverride != nil {
			c = t.foregroundColorOverride
		} else {
			c = theme.Color(theme.ColorNameForeground)
		}
	case 11:
		if t.backgroundColorOverride != nil {
			c = t.backgroundColorOverride
		} else {
			c = theme.Color(theme.ColorNameBackground)
		}
	case 12:
		if t.cursorColorOverride != nil {
			c = t.cursorColorOverride
		} else {
			c = theme.Color(theme.ColorNamePrimary)
		}
	default:
		return
	}
	if c == nil {
		return
	}
	r, g, b, _ := c.RGBA() // 16-bit pre-multiplied; opaque colours unaffected
	resp := fmt.Sprintf("%c]%d;rgb:%04x/%04x/%04x%c\\", asciiEscape, code, r, g, b, asciiEscape)
	_, _ = t.writeInput([]byte(resp))
}

// parseXColor parses the X11 colour specs xterm accepts in OSC 10/11/12:
//   - "#rgb", "#rrggbb", "#rrrgggbbb", "#rrrrggggbbbb" (3/6/9/12 hex digits)
//   - "rgb:r/g/b" where each component is 1-4 hex digits, scaled to 8-bit
//
// Named colours (e.g. "red") are not supported. Returns ok=false on any parse failure.
func parseXColor(s string) (color.Color, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "rgb:") {
		parts := strings.Split(s[len("rgb:"):], "/")
		if len(parts) != 3 {
			return nil, false
		}
		var v [3]uint8
		for i, p := range parts {
			c, ok := scaleHexComponent(p)
			if !ok {
				return nil, false
			}
			v[i] = c
		}
		return &color.NRGBA{R: v[0], G: v[1], B: v[2], A: 0xff}, true
	}
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex)%3 != 0 || len(hex) == 0 {
			return nil, false
		}
		n := len(hex) / 3 // digits per component
		var v [3]uint8
		for i := 0; i < 3; i++ {
			c, ok := scaleHexComponent(hex[i*n : i*n+n])
			if !ok {
				return nil, false
			}
			v[i] = c
		}
		return &color.NRGBA{R: v[0], G: v[1], B: v[2], A: 0xff}, true
	}
	return nil, false
}

// scaleHexComponent parses a 1-4 digit hex colour component and scales it to 8 bits so that,
// e.g., "f"/"ff"/"ffff" all map to 0xff.
func scaleHexComponent(p string) (uint8, bool) {
	if len(p) == 0 || len(p) > 4 {
		return 0, false
	}
	val, err := strconv.ParseUint(p, 16, 32)
	if err != nil {
		return 0, false
	}
	maxVal := (uint64(1) << (4 * len(p))) - 1
	return uint8(val * 0xff / maxVal), true
}

func (t *Terminal) setDirectory(uri string) {
	u, err := storage.ParseURI(uri)
	if err != nil {
		// working around a Fyne bug where file URI does not parse host
		off := 4
		count := 0
		for count < 3 && off < len(uri) {
			off++
			if uri[off] == '/' {
				count++
			}

		}
		os.Chdir(uri[off:])
		return
	}

	// fallback to guessing it's a path
	os.Chdir(u.Path())
}

func (t *Terminal) setTitle(title string) {
	t.config.Title = title
	t.onConfigure()
}

// handleOSC133 handles shell integration sequences for prompt marking
func (t *Terminal) handleOSC133(data string) {
	switch data {
	case "A":
		// Mark the start of a command prompt
		t.handlePromptStart()
	case "B":
		// Mark the end of a command prompt (start of command input)
		t.handlePromptEnd()
	case "C":
		// Mark the start of command output
		t.handleCommandStart()
	case "D":
		// Mark the end of command output
		t.handleCommandEnd()
	default:
		// For other OSC 133 sequences (like D;exit_code), we can ignore them
		// or handle them in the future if needed
		if t.debug {
			log.Println("OSC 133 sequence not implemented:", data)
		}
	}
}

// handlePromptStart marks the beginning of a command prompt
func (t *Terminal) handlePromptStart() {
	// This could be used to mark prompt positions for navigation features
	// For now, we'll just silently handle it to prevent the "Unrecognised OSC" messages
	if t.debug {
		log.Println("Shell integration: Prompt start")
	}
}

// handlePromptEnd marks the end of a command prompt (start of command input)
func (t *Terminal) handlePromptEnd() {
	// This marks where the user starts typing commands
	if t.debug {
		log.Println("Shell integration: Prompt end / Command input start")
	}
}

// handleCommandStart marks the start of command output
func (t *Terminal) handleCommandStart() {
	// This marks where command output begins
	if t.debug {
		log.Println("Shell integration: Command output start")
	}
}

// handleCommandEnd marks the end of command output
func (t *Terminal) handleCommandEnd() {
	// This marks where command output ends
	if t.debug {
		log.Println("Shell integration: Command output end")
	}
}
