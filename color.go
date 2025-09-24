package terminal

import (
	"image/color"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// getBasicColor returns a basic ANSI color (0-7) from the theme
func (t *Terminal) getBasicColor(index int) color.Color {
	colorNames := []fyne.ThemeColorName{
		"ansiBlack", "ansiRed", "ansiGreen", "ansiYellow",
		"ansiBlue", "ansiMagenta", "ansiCyan", "ansiWhite",
	}
	fallbackColors := []color.Color{
		&color.RGBA{0, 0, 0, 255},       // Black
		&color.RGBA{170, 0, 0, 255},     // Red
		&color.RGBA{0, 170, 0, 255},     // Green
		&color.RGBA{170, 170, 0, 255},   // Yellow
		&color.RGBA{0, 0, 170, 255},     // Blue
		&color.RGBA{170, 0, 170, 255},   // Magenta
		&color.RGBA{0, 255, 255, 255},   // Cyan
		&color.RGBA{170, 170, 170, 255}, // White
	}

	if index < 0 || index >= len(colorNames) {
		return color.White
	}

	// Use custom theme if set, otherwise fall back to global theme
	if t.customTheme != nil {
		themeColor := t.customTheme.Color(colorNames[index], theme.VariantDark)
		if themeColor != nil && themeColor != color.Transparent {
			return themeColor
		}
	} else {
		themeColor := theme.Color(colorNames[index])
		if themeColor != nil && themeColor != color.Transparent {
			return themeColor
		}
	}

	// Fall back to hardcoded colors if theme doesn't support ANSI colors
	return fallbackColors[index]
}

// getBrightColor returns a bright ANSI color (8-15) from the theme
func (t *Terminal) getBrightColor(index int) color.Color {
	colorNames := []fyne.ThemeColorName{
		"ansiBrightBlack", "ansiBrightRed", "ansiBrightGreen", "ansiBrightYellow",
		"ansiBrightBlue", "ansiBrightMagenta", "ansiBrightCyan", "ansiBrightWhite",
	}
	fallbackColors := []color.Color{
		&color.RGBA{85, 85, 85, 255},    // Bright Black (Gray)
		&color.RGBA{255, 85, 85, 255},   // Bright Red
		&color.RGBA{85, 255, 85, 255},   // Bright Green
		&color.RGBA{255, 255, 85, 255},  // Bright Yellow
		&color.RGBA{85, 85, 255, 255},   // Bright Blue
		&color.RGBA{255, 85, 255, 255},  // Bright Magenta
		&color.RGBA{85, 255, 255, 255},  // Bright Cyan
		&color.RGBA{255, 255, 255, 255}, // Bright White
	}

	if index < 0 || index >= len(colorNames) {
		return color.White
	}

	// Use custom theme if set, otherwise fall back to global theme
	if t.customTheme != nil {
		themeColor := t.customTheme.Color(colorNames[index], theme.VariantDark)
		if themeColor != nil && themeColor != color.Transparent {
			return themeColor
		}
	} else {
		themeColor := theme.Color(colorNames[index])
		if themeColor != nil && themeColor != color.Transparent {
			return themeColor
		}
	}

	// Fall back to hardcoded colors if theme doesn't support ANSI colors
	return fallbackColors[index]
}

// applyThemeAdjustments applies brightness and contrast adjustments from the custom theme
func (t *Terminal) applyThemeAdjustments(baseColor color.RGBA, isForeground bool) color.Color {
	if t.customTheme == nil {
		return &baseColor
	}

	// Check if the custom theme has brightness/contrast adjustment methods
	// We need to access the TermTheme's adjustment methods
	if termTheme, ok := t.customTheme.(interface {
		GetBrightnessBoost() float32
		GetContrastBoost() float32
	}); ok {
		brightnessBoost := termTheme.GetBrightnessBoost()
		contrastBoost := termTheme.GetContrastBoost()

		if brightnessBoost == 0 && contrastBoost == 0 {
			return &baseColor
		}

		r, g, b := float32(baseColor.R), float32(baseColor.G), float32(baseColor.B)

		// Apply brightness adjustment (positive = brighter, negative = dimmer)
		if brightnessBoost != 0 {
			if brightnessBoost > 0 {
				// Positive: brighten by moving towards white
				r += (255 - r) * brightnessBoost
				g += (255 - g) * brightnessBoost
				b += (255 - b) * brightnessBoost
			} else {
				// Negative: dim by moving towards black
				factor := 1 + brightnessBoost // Convert negative boost to factor
				r *= factor
				g *= factor
				b *= factor
			}
		}

		// Apply contrast adjustment (positive = more contrast, negative = less contrast)
		if contrastBoost != 0 {
			midpoint := float32(127.5)

			if contrastBoost > 0 {
				// Positive: increase contrast by pushing away from middle gray
				if isForeground {
					// Push bright colors towards white
					if r > midpoint {
						r += (255 - r) * contrastBoost
					}
					if g > midpoint {
						g += (255 - g) * contrastBoost
					}
					if b > midpoint {
						b += (255 - b) * contrastBoost
					}
				} else {
					// For background colors, push towards black for more contrast
					r *= (1 - contrastBoost)
					g *= (1 - contrastBoost)
					b *= (1 - contrastBoost)
				}
			} else {
				// Negative: decrease contrast by moving towards middle gray
				factor := -contrastBoost // Convert negative to positive factor
				r += (midpoint - r) * factor
				g += (midpoint - g) * factor
				b += (midpoint - b) * factor
			}
		}

		// Clamp values to valid range
		if r > 255 {
			r = 255
		}
		if g > 255 {
			g = 255
		}
		if b > 255 {
			b = 255
		}
		if r < 0 {
			r = 0
		}
		if g < 0 {
			g = 0
		}
		if b < 0 {
			b = 0
		}

		return &color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: baseColor.A}
	}

	return &baseColor
}

var (
	colourBands = []uint8{
		0x00,
		0x5f,
		0x87,
		0xaf,
		0xd7,
		0xff,
	}
)

func (t *Terminal) handleColorEscape(message string) {
	if message == "" || message == "0" {
		// Use custom background color if set, otherwise nil
		if t.backgroundColorOverride != nil {
			t.currentBG = t.backgroundColorOverride
		} else {
			t.currentBG = nil
		}
		t.currentFG = nil
		t.bold = false
		t.blinking = false
		t.underlined = false
		return
	}
	if message[0] == '>' || message[0] == '?' {
		if t.debug {
			log.Println("Strange colour mode", message)
		}
		return
	}
	modes := strings.Split(message, ";")
	for i := 0; i < len(modes); i++ {
		mode := modes[i]
		if mode == "" {
			continue
		}

		if (mode == "38" || mode == "48") && i+1 < len(modes) {
			nextMode := modes[i+1]
			if nextMode == "5" && i+2 < len(modes) {
				t.handleColorModeMap(mode, modes[i+2])
				i += 2
			} else if nextMode == "2" && i+4 < len(modes) {
				t.handleColorModeRGB(mode, modes[i+2], modes[i+3], modes[i+4])
				i += 4
			}
		} else {
			t.handleColorMode(mode)
		}
	}
}

func (t *Terminal) handleColorMode(modeStr string) {
	// Trim any incidental whitespace first
	modeStr = strings.TrimSpace(modeStr)
	if modeStr == "" {
		return
	}
	// Handle extended SGR parameters that use colon separators, e.g. "4:3"
	// According to ECMA-48/xterm extensions, 4:<n> sets underline style.
	// We don't support different styles yet, but we can enable underline and avoid parse errors.
	if strings.HasPrefix(modeStr, "4:") {
		t.underlined = true
		return
	}
	// Ignore other unsupported extended forms like "38:..." to avoid noisy logs
	if strings.Contains(modeStr, ":") {
		if t.debug {
			log.Println("Unsupported extended graphics mode", modeStr)
		}
		return
	}
	// Ignore any non-numeric tokens defensively (e.g. stray "(")
	for _, r := range modeStr {
		if r < '0' || r > '9' {
			if t.debug {
				log.Println("Ignoring non-numeric graphics mode", modeStr)
			}
			return
		}
	}
	// Handle extended SGR parameters that use colon separators, e.g. "4:3"
	// According to ECMA-48/xterm extensions, 4:<n> sets underline style.
	// We don't support different styles yet, but we can enable underline and avoid parse errors.
	if strings.HasPrefix(modeStr, "4:") {
		t.underlined = true
		return
	}
	// Ignore other unsupported extended forms like "38:..." to avoid noisy logs
	if strings.Contains(modeStr, ":") {
		if t.debug {
			log.Println("Unsupported extended graphics mode", modeStr)
		}
		return
	}
	mode, err := strconv.Atoi(modeStr)
	if err != nil {
		fyne.LogError("Failed to parse color mode: "+modeStr, err)
		return
	}
	switch mode {
	case 0: // Reset - clear all formatting and colors
		t.currentBG, t.currentFG = nil, nil
		t.bold = false
		t.blinking = false
		t.underlined = false
	case 1: // Bold/bright text
		t.bold = true
	case 4: // Underlined text
		t.underlined = true
	case 5: // Blinking text
		t.blinking = true
	case 24: // Not underlined - remove underline
		t.underlined = false
	case 7: // Reverse video - swap foreground and background colors
		bg, fg := t.currentBG, t.currentFG
		if fg == nil {
			t.currentBG = theme.Color(theme.ColorNameForeground)
		} else {
			t.currentBG = fg
		}
		if bg == nil {
			t.currentFG = theme.Color(theme.ColorNameDisabledButton)
		} else {
			t.currentFG = bg
		}
	case 27: // Not reversed - turn off reverse video
		bg, fg := t.currentBG, t.currentFG
		if fg != nil {
			// Use custom background color if set, otherwise nil
			if t.backgroundColorOverride != nil {
				t.currentBG = t.backgroundColorOverride
			} else {
				t.currentBG = nil
			}
		} else {
			t.currentBG = fg
		}
		if bg != nil {
			t.currentFG = nil
		} else {
			t.currentFG = bg
		}
	case 30, 31, 32, 33, 34, 35, 36, 37:
		// Standard foreground colors (black, red, green, yellow, blue, magenta, cyan, white)
		t.currentFG = t.getBasicColor(mode - 30)
	case 39: // Default foreground color
		t.currentFG = nil
	case 40, 41, 42, 43, 44, 45, 46, 47:
		// Standard background colors (black, red, green, yellow, blue, magenta, cyan, white)
		t.currentBG = t.getBasicColor(mode - 40)
	case 49: // Default background color
		// Use custom background color if set, otherwise nil
		if t.backgroundColorOverride != nil {
			t.currentBG = t.backgroundColorOverride
		} else {
			t.currentBG = nil
		}
	case 90, 91, 92, 93, 94, 95, 96, 97:
		// Bright foreground colors (bright black/gray, bright red, etc.)
		t.currentFG = t.getBrightColor(mode - 90)
	case 100, 101, 102, 103, 104, 105, 106, 107:
		// Bright background colors (bright black/gray, bright red, etc.)
		t.currentBG = t.getBrightColor(mode - 100)
	default:
		if t.debug {
			log.Println("Unsupported graphics mode", mode)
		}
	}
}

func (t *Terminal) handleColorModeMap(mode, ids string) {
	var c color.Color
	id, err := strconv.Atoi(ids)
	if err != nil {
		if t.debug {
			log.Println("Invalid color map ID", ids)
		}
		return
	}
	if id <= 7 {
		c = t.getBasicColor(id)
	} else if id <= 15 {
		c = t.getBrightColor(id - 8)
	} else if id <= 231 {
		id -= 16
		b := id % 6
		id = (id - b) / 6
		g := id % 6
		r := (id - g) / 6
		baseColor := &color.RGBA{colourBands[r], colourBands[g], colourBands[b], 255}
		// Apply theme adjustments to 256-color palette
		c = t.applyThemeAdjustments(*baseColor, mode == "38")
	} else if id <= 255 {
		id -= 232
		inc := 256 / 24
		y := id * inc
		// For grayscale colors, use color.Gray when no theme adjustments are needed
		if t.customTheme == nil {
			c = &color.Gray{uint8(y)}
		} else {
			baseColor := &color.RGBA{uint8(y), uint8(y), uint8(y), 255}
			// Apply theme adjustments to grayscale colors
			c = t.applyThemeAdjustments(*baseColor, mode == "38")
		}
	} else if t.debug {
		log.Println("Invalid colour map ID", id)
	}

	if mode == "38" {
		t.currentFG = c
	} else if mode == "48" {
		t.currentBG = c
	}
}

func (t *Terminal) handleColorModeRGB(mode, rs, gs, bs string) {
	r, _ := strconv.Atoi(rs)
	g, _ := strconv.Atoi(gs)
	b, _ := strconv.Atoi(bs)
	baseColor := &color.RGBA{uint8(r), uint8(g), uint8(b), 255}

	// Apply theme adjustments to 24-bit RGB colors
	c := t.applyThemeAdjustments(*baseColor, mode == "38")

	if mode == "38" {
		t.currentFG = c
	} else if mode == "48" {
		t.currentBG = c
	}
}
