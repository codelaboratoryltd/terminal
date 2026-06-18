package widget

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
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
//
// Dirty rendering: each frame compares cell visual state against the previous
// frame's snapshot and repaints only changed cells, so idle or mostly-static
// screens (cursor blink, slow output) do near-zero pixel work.
type TermGrid struct {
	widget.TextGrid

	tickerCancel context.CancelFunc
	// mayContainBlink is true until a refresh finds no BlinkEnabled cells; then false skips O(n) scans.
	mayContainBlink atomic.Bool

	// rowsMu guards the embedded TextGrid.Rows slice (and the Cells slices it
	// holds) against concurrent access. The PTY reader goroutine mutates Rows via
	// the Terminal's handleOutput (appends rows, reallocates Cells, swaps the
	// Rows slice on scroll/clear), while the Fyne paint goroutine reads Rows
	// during raster generation (drawToImage / DirtyPixelBounds) and UI handlers
	// read it on the main goroutine. Without this lock a paint that overlaps a
	// reallocation can observe a torn slice header (new pointer, stale length)
	// and fault — the resize-while-output crash. Writers take Lock; readers RLock.
	rowsMu sync.RWMutex

	// Raster renderer state.
	raster          *canvas.Raster
	pixBuf          *image.RGBA
	termCols        int // set by Terminal when the grid is sized
	termRows        int
	stretchFontSize float32 // non-zero: render at this font size and let GL stretch to fill

	// Dirty-render snapshot. nil forces a full redraw on the next frame.
	cellSnaps       []cellSnap
	lastDirtyBounds image.Rectangle
	prevDefaultFGU  *image.Uniform
	prevDefaultBGU  *image.Uniform
	prevScale       float32

	// lastRenderParams caches the inputs from the most recent drawToImage call
	// so that DirtyPixelBounds() can pre-scan dirty cells without re-deriving
	// theme/font parameters from scratch.
	lastRenderParams renderParams
}

// renderParams records the inputs needed to call ScanDirtyBounds.
type renderParams struct {
	defaultFG, defaultBG color.Color
	textSizePt           float32
	monoRes, boldRes     fyne.Resource
	scale                float32
	imgW, imgH           int // render-buffer pixel size (may be smaller than target in stretch mode)
	targetW, targetH     int // actual on-screen pixel size of the widget
	cols, rows           int
}

// CreateRenderer is a private method to Fyne which links this widget to its renderer.
// We override TextGrid.CreateRenderer to substitute a single canvas.Raster for
// TextGrid's 1920+ per-cell canvas objects.
func (t *TermGrid) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)

	t.raster = canvas.NewRaster(t.drawToImage)
	t.raster.DirtyReporter = t
	return widget.NewSimpleRenderer(t.raster)
}

func (t *TermGrid) drawToImage(w, h int) image.Image {
	if t.pixBuf == nil || t.pixBuf.Bounds().Dx() != w || t.pixBuf.Bounds().Dy() != h {
		t.pixBuf = image.NewRGBA(image.Rect(0, 0, w, h))
		t.cellSnaps = nil // buffer resized — full redraw required
	}

	// Full-refresh mode (non-Mesa backends): drop the dirty snapshot every frame so
	// RenderTermToImage re-rasterises the entire grid instead of only changed cells
	// — the pre-dirty-region path. See forceFullRefresh in termgridhelper.go.
	if IsForceFullRefresh() {
		t.cellSnaps = nil
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

	// Invalidate the dirty snapshot when theme colours or scale change so that
	// cells relying on the default FG/BG are correctly repainted.
	fgu := getUniform(defaultFG)
	bgu := getUniform(defaultBG)
	if fgu != t.prevDefaultFGU || bgu != t.prevDefaultBGU || scale != t.prevScale {
		t.cellSnaps = nil
		t.prevDefaultFGU = fgu
		t.prevDefaultBGU = bgu
		t.prevScale = scale
	}

	// Stretch mode: render at the natural font resolution and return a
	// (possibly smaller) image; Fyne/GL stretches the texture to fill (w, h).
	if t.stretchFontSize > 0 {
		cw, ch := RasterCellSize(t.stretchFontSize, monoFont, boldFont)
		renderW := int(math.Round(float64(float32(cols) * cw * scale)))
		renderH := int(math.Round(float64(float32(rows) * ch * scale)))
		if renderW < 1 {
			renderW = 1
		}
		if renderH < 1 {
			renderH = 1
		}
		if t.pixBuf.Bounds().Dx() != renderW || t.pixBuf.Bounds().Dy() != renderH {
			t.pixBuf = image.NewRGBA(image.Rect(0, 0, renderW, renderH))
			t.cellSnaps = nil
		}
		t.RLockRows()
		t.cellSnaps, t.lastDirtyBounds = RenderTermToImage(t.Rows, t.pixBuf, cols, rows, defaultFG, defaultBG, t.stretchFontSize, monoFont, boldFont, scale, t.cellSnaps)
		t.RUnlockRows()
		b := t.pixBuf.Bounds()
		t.lastRenderParams = renderParams{defaultFG, defaultBG, t.stretchFontSize, monoFont, boldFont, scale, b.Dx(), b.Dy(), w, h, cols, rows}
		return t.pixBuf
	}

	t.RLockRows()
	t.cellSnaps, t.lastDirtyBounds = RenderTermToImage(t.Rows, t.pixBuf, cols, rows, defaultFG, defaultBG, textSizePt, monoFont, boldFont, scale, t.cellSnaps)
	t.RUnlockRows()
	b := t.pixBuf.Bounds()
	t.lastRenderParams = renderParams{defaultFG, defaultBG, textSizePt, monoFont, boldFont, scale, b.Dx(), b.Dy(), b.Dx(), b.Dy(), cols, rows}
	return t.pixBuf
}

// SetStretchFontSize enables stretch-to-fit rendering. When pt > 0 the grid
// rasterises at the pixel dimensions implied by that font size and returns the
// result to Fyne, which then scales it to the widget's actual on-screen area
// via the GL texture quad. Set to 0 to restore normal (1:1) rendering.
func (t *TermGrid) SetStretchFontSize(pt float32) {
	t.stretchFontSize = pt
}

// SetGridDimensions tells the raster renderer how many columns and rows the
// terminal is configured for. Call this whenever the terminal is resized.
func (t *TermGrid) SetGridDimensions(cols, rows int) {
	t.termCols = cols
	t.termRows = rows
}

// LockRows / UnlockRows acquire the write side of the Rows guard. Callers that
// reallocate or reassign Rows / a row's Cells (the PTY output path) must hold it.
func (t *TermGrid) LockRows()   { t.rowsMu.Lock() }
func (t *TermGrid) UnlockRows() { t.rowsMu.Unlock() }

// RLockRows / RUnlockRows acquire the read side of the Rows guard. Callers that
// iterate or index Rows off the writer goroutine (paint, UI reads) must hold it.
// Do not nest RLock on the same goroutine — RWMutex deadlocks a recursive RLock
// if a writer is waiting.
func (t *TermGrid) RLockRows()   { t.rowsMu.RLock() }
func (t *TermGrid) RUnlockRows() { t.rowsMu.RUnlock() }

// DirtyPixelBounds implements canvas.DirtyRegionReporter.
// Pre-scans the current terminal state against cell snaps to return the pixel
// bounds of what will change in the CURRENT frame's render, not the previous
// frame's lastDirtyBounds. This prevents the one-frame stale lag that would
// cause the scissor to miss newly dirty cells (cursor movement, new text, etc.).
func (t *TermGrid) DirtyPixelBounds() image.Rectangle {
	p := t.lastRenderParams
	if p.imgW == 0 {
		return image.Rectangle{} // no render yet; FBO will be fresh, full repaint guaranteed
	}
	// Full-refresh mode (non-Mesa backends): repaint the whole widget so the scissor
	// never clips to a partial dirty rect — the pre-dirty-region path. See
	// forceFullRefresh in termgridhelper.go.
	if IsForceFullRefresh() {
		return image.Rect(0, 0, p.targetW, p.targetH)
	}
	// Return the full raster pixel footprint when a full redraw is imminent so
	// that computeDirtyRect includes the entire raster in the dirty rect rather
	// than skipping it (an empty return skips the raster, leaving a small cursor-
	// only scissor that clips the raster repaint to the wrong area).
	//
	// Full redraw is imminent when:
	//   • cellSnaps is nil — buffer was resized or scale/theme changed
	//   • grid dimensions changed — SetGridDimensions was called since last render
	if t.cellSnaps == nil || p.cols != t.termCols || p.rows != t.termRows {
		return image.Rect(0, 0, p.targetW, p.targetH)
	}
	t.RLockRows()
	bounds := ScanDirtyBounds(t.Rows, p.imgW, p.imgH, p.cols, p.rows,
		p.defaultFG, p.defaultBG, p.textSizePt, p.monoRes, p.boldRes, p.scale, t.cellSnaps)
	t.RUnlockRows()
	if bounds.Empty() || p.targetW == p.imgW {
		return bounds
	}
	// Stretch mode: the render buffer is smaller than the widget's on-screen
	// footprint. Scale dirty bounds from render-buffer space to widget pixel
	// space so that computeDirtyRect places the scissor correctly.
	scaleX := float64(p.targetW) / float64(p.imgW)
	scaleY := float64(p.targetH) / float64(p.imgH)
	return image.Rect(
		int(float64(bounds.Min.X)*scaleX),
		int(float64(bounds.Min.Y)*scaleY),
		int(math.Ceil(float64(bounds.Max.X)*scaleX)),
		int(math.Ceil(float64(bounds.Max.Y)*scaleY)),
	)
}

// LastDirtyBounds reports, in the same widget-pixel space as DirtyPixelBounds,
// the region the most recent drawToImage actually repainted. The renderer uses
// this to self-heal a scissor race: DirtyPixelBounds is scanned to build the FBO
// dirty rect, but PTY output can mutate Rows between that scan and the render, so
// the render may touch rows outside the scissor. Those rows are clipped out of
// the FBO yet marked clean here, so without a follow-up full repaint they stay
// stale. Comparing this against the scissor lets the renderer detect the spill.
//
// Same-goroutine as the paint walk (called right after the raster is drawn), so
// no lock is needed — it only reads cached render outputs, not Rows.
func (t *TermGrid) LastDirtyBounds() image.Rectangle {
	p := t.lastRenderParams
	b := t.lastDirtyBounds
	if b.Empty() || p.imgW == 0 || p.imgH == 0 || p.targetW == p.imgW {
		return b // non-stretch (or nothing rendered): render-buffer space == widget space
	}
	// Stretch mode: scale render-buffer bounds up to widget space, matching the
	// tail of DirtyPixelBounds so the comparison spaces agree.
	scaleX := float64(p.targetW) / float64(p.imgW)
	scaleY := float64(p.targetH) / float64(p.imgH)
	return image.Rect(
		int(float64(b.Min.X)*scaleX),
		int(float64(b.Min.Y)*scaleY),
		int(math.Ceil(float64(b.Max.X)*scaleX)),
		int(math.Ceil(float64(b.Max.Y)*scaleY)),
	)
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
		if t.raster != nil {
			t.raster.Refresh()
		}
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

	if t.raster != nil {
		t.raster.Refresh()
	}

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
