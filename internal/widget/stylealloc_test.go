package widget

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/test"
)

// These benchmarks measure per-character style allocation. handleOutputRune calls
// NewTermTextGridStyle for every output character, so its allocs/op directly drives
// the per-keystroke GC churn the terminal exhibits. A uniform screen (one attribute
// combo) and a small palette (a handful of combos) are the realistic cases — whole
// runs of cells share attributes.

func BenchmarkNewTermTextGridStyle_UniformScreen(b *testing.B) {
	test.NewApp()
	fg := color.RGBA{R: 200, G: 200, B: 200, A: 255}
	bg := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewTermTextGridStyle(fg, bg, 0, false, false, false)
	}
}

func BenchmarkNewTermTextGridStyle_FewCombos(b *testing.B) {
	test.NewApp()
	palette := []color.RGBA{
		{R: 200, G: 200, B: 200, A: 255}, {R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255},
		{R: 255, G: 255, A: 255}, {G: 255, B: 255, A: 255}, {R: 255, B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255},
	}
	bg := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fg := palette[i%len(palette)]
		_ = NewTermTextGridStyle(fg, bg, 0, false, false, false)
	}
}
