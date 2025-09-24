package main

import (
	"context"
	"embed"
	"flag"
	"image/color"
	"runtime"

	"github.com/fyne-io/terminal"
	"github.com/fyne-io/terminal/cmd/fyneterm/data"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	termOverlay = fyne.ThemeColorName("termOver")

	// ANSI basic colors (0-7)
	termColorBlack   = fyne.ThemeColorName("ansiBlack")
	termColorRed     = fyne.ThemeColorName("ansiRed")
	termColorGreen   = fyne.ThemeColorName("ansiGreen")
	termColorYellow  = fyne.ThemeColorName("ansiYellow")
	termColorBlue    = fyne.ThemeColorName("ansiBlue")
	termColorMagenta = fyne.ThemeColorName("ansiMagenta")
	termColorCyan    = fyne.ThemeColorName("ansiCyan")
	termColorWhite   = fyne.ThemeColorName("ansiWhite")

	// ANSI bright colors (8-15)
	termColorBrightBlack   = fyne.ThemeColorName("ansiBrightBlack")
	termColorBrightRed     = fyne.ThemeColorName("ansiBrightRed")
	termColorBrightGreen   = fyne.ThemeColorName("ansiBrightGreen")
	termColorBrightYellow  = fyne.ThemeColorName("ansiBrightYellow")
	termColorBrightBlue    = fyne.ThemeColorName("ansiBrightBlue")
	termColorBrightMagenta = fyne.ThemeColorName("ansiBrightMagenta")
	termColorBrightCyan    = fyne.ThemeColorName("ansiBrightCyan")
	termColorBrightWhite   = fyne.ThemeColorName("ansiBrightWhite")
)

//go:embed translation
var translations embed.FS

func setupListener(t *terminal.Terminal, w fyne.Window) {
	listen := make(chan terminal.Config)
	go func() {
		for {
			config := <-listen

			fyne.Do(func() {
				if config.Title == "" {
					w.SetTitle(termTitle())
				} else {
					w.SetTitle(termTitle() + ": " + config.Title)
				}
			})
		}
	}()
	t.AddListener(listen)
}

func termTitle() string {
	return lang.L("Title")
}

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Show terminal debug messages")
	flag.Parse()

	lang.AddTranslationsFS(translations, "translation")

	a := app.New()
	a.SetIcon(data.Icon)
	w := newTerminalWindow(a, debug)
	w.ShowAndRun()
}

var globalTheme *termTheme

func newTerminalWindow(a fyne.App, debug bool) fyne.Window {
	w := a.NewWindow(termTitle())
	w.SetPadded(false)
	th := newTermTheme()
	if globalTheme == nil {
		globalTheme = th
	}

	bg := canvas.NewRectangle(theme.Color(theme.ColorNameBackground))
	img := canvas.NewImageFromResource(data.FyneLogo)
	img.FillMode = canvas.ImageFillContain
	over := canvas.NewRectangle(th.Color(termOverlay, a.Settings().ThemeVariant()))

	a.Settings().AddListener(func(s fyne.Settings) {
		bg.FillColor = theme.Color(theme.ColorNameBackground)
		bg.Refresh()
		over.FillColor = th.Color(termOverlay, s.ThemeVariant())
		over.Refresh()
	})

	t := terminal.New()
	t.EnableFixedPTYSize(uint(24), uint(80))
	t.SetBorderColor(color.NRGBA{R: 120, G: 120, B: 120, A: 255})
	t.SetBorderWidth(2.0)
	t.EnableBorder(true)
	t.SetDebug(true)
	setupListener(t, w)
	sizeOverride := container.NewThemeOverride(
		container.NewPadded(container.NewStack(bg, img, over, t), container.NewCenter(widget.NewButton("Test", func() {
			if t.GetFixedPTYSizeMode() {
				t.DisableFixedPTYSize()
			} else {
				t.EnableFixedPTYSize(uint(24), uint(80))
			}
		}))), th)
	w.SetContent(sizeOverride)

	w.Resize(fyne.NewSize(640, 480))
	w.Canvas().Focus(t)

	newTerm := func(_ fyne.Shortcut) {
		w := newTerminalWindow(a, debug)
		w.Show()
	}

	t.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift}, newTerm)
	if runtime.GOOS == "darwin" {
		t.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: fyne.KeyModifierSuper}, newTerm)
	}
	t.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyEqual, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift},
		func(_ fyne.Shortcut) {
			th.fontSize++
			sizeOverride.Theme = th
			sizeOverride.Refresh()
		})
	t.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: fyne.KeyModifierShortcutDefault},
		func(_ fyne.Shortcut) {
			th.fontSize--
			sizeOverride.Theme = th
			sizeOverride.Refresh()
		})

	go func() {
		err := t.RunLocalShell(context.TODO(), nil)
		if err != nil {
			fyne.LogError("Failure in terminal", err)
		}
		fyne.Do(w.Close)
	}()

	return w
}
