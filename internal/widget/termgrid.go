package widget

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyne.io/fyne/v2"
)

const blinkingInterval = 500 * time.Millisecond

// TermGrid is a monospaced grid of characters.
// This is designed to be used by our terminal emulator.
//
// Rendering: instead of delegating to widget.TextGrid's per-cell renderer
// (~1920 GL draw calls per frame on a typical 80×24 terminal), TermGrid uses a
// single canvas.Raster that composites all cells into one pixel buffer. Fyne
// uploads it as a single texture and issues one GL draw call per repaint —
// essential for Mesa/llvmpipe on VDI hardware where each GL state-change costs
// ~250µs.
type TermGrid struct {
	widget.TextGrid

	tickerCancel context.CancelFunc
	// mayContainBlink is true until a refresh finds no BlinkEnabled cells; then false skips O(n) scans.
	mayContainBlink atomic.Bool

	// Raster renderer state.
	raster  *canvas.Raster
	pixBuf  *image.RGBA
	termCols int // set by Terminal when the grid is sized
	termRows int
}

// CreateRenderer is a private method to Fyne which links this widget to its renderer.
// We override TextGrid.CreateRenderer to substitute a single canvas.Raster for
// TextGrid's 1920+ per-cell canvas objects.
func (t *TermGrid) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)

	t.raster = canvas.NewRaster(t.drawToImage)
	return widget.NewSimpleRenderer(t.raster)
}

func (t *TermGrid) drawToImage(w, h int) image.Image {
	if t.pixBuf == nil || t.pixBuf.Bounds().Dx() != w || t.pixBuf.Bounds().Dy() != h {
		t.pixBuf = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	cols := t.termCols
	rows := t.termRows
	if cols == 0 || rows == 0 || w == 0 || h == 0 {
		return t.pixBuf
	}

	th := t.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	defaultFG := th.Color(theme.ColorNameForeground, v)
	defaultBG := th.Color(theme.ColorNameBackground, v)
	textSizePt := th.Size(theme.SizeNameText)
	monoFont := th.Font(fyne.TextStyle{Monospace: true})
	boldFont := th.Font(fyne.TextStyle{Bold: true})

	// Derive the pixel-to-point scale from the ratio of physical pixels (w)
	// to the widget's logical size. This works for any scale factor and does
	// not rely on CanvasForObject, which returns nil when the widget is not
	// the direct canvas object (the Raster child is).
	scale := float32(1.0)
	if sz := t.Size(); sz.Width > 0 {
		s := float32(w) / sz.Width
		if s >= 0.25 {
			// Snap to nearest quarter to keep the font cache hit rate high
			// even when integer division produces slightly non-round ratios.
			scale = float32(math.Round(float64(s)*4)) / 4
		}
	}

	RenderTermToImage(t.Rows, t.pixBuf, cols, rows, defaultFG, defaultBG, textSizePt, monoFont, boldFont, scale)
	return t.pixBuf
}

// SetGridDimensions tells the raster renderer how many columns and rows the
// terminal is configured for. Call this whenever the terminal is resized.
func (t *TermGrid) SetGridDimensions(cols, rows int) {
	t.termCols = cols
	t.termRows = rows
}

// MinSize returns the minimum pixel size for the configured grid dimensions.
// This must agree with guessCellSize so that Terminal.Layout positions the grid
// in the right place.
func (t *TermGrid) MinSize() fyne.Size {
	if t.termCols == 0 || t.termRows == 0 {
		return t.TextGrid.MinSize()
	}
	th := t.Theme()
	textSizePt := th.Size(theme.SizeNameText)
	monoFont := th.Font(fyne.TextStyle{Monospace: true})
	boldFont := th.Font(fyne.TextStyle{Bold: true})
	cw, ch := RasterCellSize(textSizePt, monoFont, boldFont)
	if cw == 0 {
		return t.TextGrid.MinSize()
	}
	return fyne.NewSize(
		float32(math.Round(float64(float32(t.termCols)*cw))),
		float32(math.Round(float64(float32(t.termRows)*ch))),
	)
}

// NewTermGrid creates a new empty TermGrid widget.
func NewTermGrid() *TermGrid {
	grid := &TermGrid{}
	grid.ExtendBaseWidget(grid)
	grid.mayContainBlink.Store(true)
	return grid
}

// InvalidateBlinkCache forces the next refresh to scan for blinking cells (call after SGR blink changes).
func (t *TermGrid) InvalidateBlinkCache() {
	t.mayContainBlink.Store(true)
}

// Refresh will be called when this grid should update.
// We update our blinking status and then redraw the raster.
func (t *TermGrid) Refresh() {
	// Safety check: don't refresh if Rows is nil (during cleanup)
	if t.Rows == nil {
		return
	}
	t.refreshBlink(false)
}

func (t *TermGrid) refreshBlink(blink bool) {
	// Safety check: ensure Rows is not nil before accessing
	if t.Rows == nil {
		return
	}

	if !t.mayContainBlink.Load() {
		if t.tickerCancel != nil {
			t.tickerCancel()
			t.tickerCancel = nil
		}
		fyne.Do(func() {
			if t.raster != nil {
				t.raster.Refresh()
			}
		})
		return
	}

	shouldBlink := false
	for _, row := range t.Rows {
		if row.Cells == nil {
			continue
		}
		for _, r := range row.Cells {
			if s, ok := r.Style.(*TermTextGridStyle); ok && s != nil && s.BlinkEnabled {
				shouldBlink = true
				s.blink(blink)
			}
		}
	}

	if !shouldBlink {
		t.mayContainBlink.Store(false)
	}

	fyne.Do(func() {
		if t.raster != nil {
			t.raster.Refresh()
		}
	})

	switch {
	case shouldBlink && t.tickerCancel == nil:
		t.runBlink()
	case !shouldBlink && t.tickerCancel != nil:
		t.tickerCancel()
		t.tickerCancel = nil
	}
}

// StopBlink stops any active blinking animation
func (t *TermGrid) StopBlink() {
	if t.tickerCancel != nil {
		t.tickerCancel()
		t.tickerCancel = nil
	}
}

// slowBlinkVisible is how long cells stay visible between heartbeat pulses in
// slow-blink mode.
const slowBlinkVisible = 3 * time.Second

// slowBlinkInvisible is the duration of the "off" pulse in slow-blink mode.
const slowBlinkInvisible = 500 * time.Millisecond

func (t *TermGrid) runBlink() {
	if t.tickerCancel != nil {
		t.tickerCancel()
		t.tickerCancel = nil
	}
	var tickerContext context.Context
	tickerContext, t.tickerCancel = context.WithCancel(context.Background())

	if IsSlowBlinkMode() {
		go t.runBlinkSlow(tickerContext)
		return
	}

	ticker := time.NewTicker(blinkingInterval)
	blinking := false
	go func() {
		defer func() {
			ticker.Stop()
			if r := recover(); r != nil {
				// Log panic but don't crash the application
				fmt.Printf("Panic in TermGrid blink goroutine: %v\n", r)
			}
		}()

		for {
			select {
			case <-tickerContext.Done():
				return
			case <-ticker.C:
				blinking = !blinking
				fyne.Do(func() {
					t.refreshBlink(blinking)
				})
			}
		}
	}()
}

// runBlinkSlow drives blinking text in low-graphics mode: cells are visible
// for slowBlinkVisible, then invisible for slowBlinkInvisible, and repeat.
// Combined with the underline marker injected by TermTextGridStyle.Style()
// this preserves the "draw attention" semantic while reducing full-window
// repaints from ~2 Hz to ~0.57 Hz.
func (t *TermGrid) runBlinkSlow(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in TermGrid slow blink goroutine: %v\n", r)
		}
	}()

	visible := true
	timer := time.NewTimer(slowBlinkVisible)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			visible = !visible
			blinking := !visible // refreshBlink treats true == "in the off phase"
			fyne.Do(func() {
				t.refreshBlink(blinking)
			})
			if visible {
				timer.Reset(slowBlinkVisible)
			} else {
				timer.Reset(slowBlinkInvisible)
			}
		}
	}
}
