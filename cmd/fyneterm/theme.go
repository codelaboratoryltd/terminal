package main

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	notosansmono "github.com/fyne-io/terminal/cmd/fyneterm/font/NotoSansMono"
)

type termTheme struct {
	fyne.Theme

	fontSize   float32
	brightness float32
	contrast   float32
}

func newTermTheme() *termTheme {
	return &termTheme{
		Theme:      fyne.CurrentApp().Settings().Theme(),
		fontSize:   12,
		brightness: 1.0,
		contrast:   1.0,
	}
}

// applyBrightnessContrast adjusts a color with brightness and contrast
func (t *termTheme) applyBrightnessContrast(c color.Color) color.Color {
	r, g, b, a := c.RGBA()

	// Convert to 0-1 range
	rf := float64(r) / 65535.0
	gf := float64(g) / 65535.0
	bf := float64(b) / 65535.0

	// Apply brightness (additive)
	rf += float64(t.brightness - 1.0)
	gf += float64(t.brightness - 1.0)
	bf += float64(t.brightness - 1.0)

	// Apply contrast (multiplicative around midpoint)
	rf = (rf-0.5)*float64(t.contrast) + 0.5
	gf = (gf-0.5)*float64(t.contrast) + 0.5
	bf = (bf-0.5)*float64(t.contrast) + 0.5

	// Clamp to valid range
	rf = math.Max(0, math.Min(1, rf))
	gf = math.Max(0, math.Min(1, gf))
	bf = math.Max(0, math.Min(1, bf))

	return color.NRGBA{
		R: uint8(rf * 255),
		G: uint8(gf * 255),
		B: uint8(bf * 255),
		A: uint8(a >> 8),
	}
}

// Color fixes a bug < 2.1 where theme.DarkTheme() would not override user preference.
func (t *termTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case termOverlay:
		if c := t.Color("fynedeskPanelBackground", v); c != color.Transparent {
			return c
		}
		if v == theme.VariantLight {
			return color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xf6}
		}
		return color.NRGBA{R: 0x0a, G: 0x0a, B: 0x0a, A: 0xf6}

	// ANSI basic colors (0-7)
	case termColorBlack:
		return t.applyBrightnessContrast(color.NRGBA{0, 0, 0, 255})
	case termColorRed:
		return t.applyBrightnessContrast(color.NRGBA{170, 0, 0, 255})
	case termColorGreen:
		return t.applyBrightnessContrast(color.NRGBA{0, 170, 0, 255})
	case termColorYellow:
		return t.applyBrightnessContrast(color.NRGBA{170, 170, 0, 255})
	case termColorBlue:
		return t.applyBrightnessContrast(color.NRGBA{0, 0, 170, 255})
	case termColorMagenta:
		return t.applyBrightnessContrast(color.NRGBA{170, 0, 170, 255})
	case termColorCyan:
		return t.applyBrightnessContrast(color.NRGBA{0, 255, 255, 255})
	case termColorWhite:
		return t.applyBrightnessContrast(color.NRGBA{170, 170, 170, 255})

	// ANSI bright colors (8-15)
	case termColorBrightBlack:
		return t.applyBrightnessContrast(color.NRGBA{85, 85, 85, 255})
	case termColorBrightRed:
		return t.applyBrightnessContrast(color.NRGBA{255, 85, 85, 255})
	case termColorBrightGreen:
		return t.applyBrightnessContrast(color.NRGBA{85, 255, 85, 255})
	case termColorBrightYellow:
		return t.applyBrightnessContrast(color.NRGBA{255, 255, 85, 255})
	case termColorBrightBlue:
		return t.applyBrightnessContrast(color.NRGBA{85, 85, 255, 255})
	case termColorBrightMagenta:
		return t.applyBrightnessContrast(color.NRGBA{255, 85, 255, 255})
	case termColorBrightCyan:
		return t.applyBrightnessContrast(color.NRGBA{85, 255, 255, 255})
	case termColorBrightWhite:
		return t.applyBrightnessContrast(color.NRGBA{255, 255, 255, 255})

	case theme.ColorNameBackground, theme.ColorNameForeground:
		return t.Theme.Color(n, v)
	}
	return t.Theme.Color(n, theme.VariantDark)
}

// SetBrightness adjusts the brightness of ANSI colors
func (t *termTheme) SetBrightness(brightness float32) {
	t.brightness = brightness
}

// SetContrast adjusts the contrast of ANSI colors
func (t *termTheme) SetContrast(contrast float32) {
	t.contrast = contrast
}

func (t *termTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameText {
		return t.fontSize
	}

	return t.Theme.Size(n)
}

func (t *termTheme) Font(style fyne.TextStyle) fyne.Resource {
	switch {
	case style.Bold:
		return notosansmono.Bold
	default:
		return notosansmono.Regular
	}
}
