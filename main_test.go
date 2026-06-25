package terminal

import (
	"image/color"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// ansiTestTheme wraps the default test theme and supplies the ANSI palette
// color names that the terminal looks up. The default test theme does not
// define these names, so without this every color lookup spams the log with
// "color ansiRed not defined in theme" errors (the code falls back to a
// hardcoded palette, so tests still pass — but the noise is misleading).
//
// The values mirror cmd/fyneterm's termTheme and color.go's fallback palette,
// so tests that assert against the fallback colors keep passing while
// exercising the theme path that production actually uses.
type ansiTestTheme struct {
	fyne.Theme
}

var ansiTestColors = map[fyne.ThemeColorName]color.Color{
	"ansiBlack":   &color.RGBA{0, 0, 0, 255},
	"ansiRed":     &color.RGBA{170, 0, 0, 255},
	"ansiGreen":   &color.RGBA{0, 170, 0, 255},
	"ansiYellow":  &color.RGBA{170, 170, 0, 255},
	"ansiBlue":    &color.RGBA{0, 0, 170, 255},
	"ansiMagenta": &color.RGBA{170, 0, 170, 255},
	"ansiCyan":    &color.RGBA{0, 255, 255, 255},
	"ansiWhite":   &color.RGBA{170, 170, 170, 255},

	"ansiBrightBlack":   &color.RGBA{85, 85, 85, 255},
	"ansiBrightRed":     &color.RGBA{255, 85, 85, 255},
	"ansiBrightGreen":   &color.RGBA{85, 255, 85, 255},
	"ansiBrightYellow":  &color.RGBA{255, 255, 85, 255},
	"ansiBrightBlue":    &color.RGBA{85, 85, 255, 255},
	"ansiBrightMagenta": &color.RGBA{255, 85, 255, 255},
	"ansiBrightCyan":    &color.RGBA{85, 255, 255, 255},
	"ansiBrightWhite":   &color.RGBA{255, 255, 255, 255},
}

func (a ansiTestTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if c, ok := ansiTestColors[n]; ok {
		return c
	}
	return a.Theme.Color(n, v)
}

func TestMain(m *testing.M) {
	app := test.NewApp()
	app.Settings().SetTheme(ansiTestTheme{test.Theme()})
	os.Exit(m.Run())
}
