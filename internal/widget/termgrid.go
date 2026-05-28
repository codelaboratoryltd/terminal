package widget

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyne.io/fyne/v2"
)

const blinkingInterval = 500 * time.Millisecond

// TermGrid is a monospaced grid of characters.
// This is designed to be used by our terminal emulator.
type TermGrid struct {
	widget.TextGrid

	tickerCancel context.CancelFunc
	// mayContainBlink is true until a refresh finds no BlinkEnabled cells; then false skips O(n) scans.
	mayContainBlink atomic.Bool
}

// CreateRenderer is a private method to Fyne which links this widget to it's renderer
func (t *TermGrid) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)

	return t.TextGrid.CreateRenderer()
}

// NewTermGrid creates a new empty TextGrid widget.
func NewTermGrid() *TermGrid {
	grid := &TermGrid{}
	grid.ExtendBaseWidget(grid)
	grid.mayContainBlink.Store(true)

	grid.Scroll = container.ScrollNone
	return grid
}

// InvalidateBlinkCache forces the next refresh to scan for blinking cells (call after SGR blink changes).
func (t *TermGrid) InvalidateBlinkCache() {
	t.mayContainBlink.Store(true)
}

// Refresh will be called when this grid should update.
// We update our blinking status and then call the TextGrid we extended to refresh too.
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
			t.TextGrid.Refresh()
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
		t.TextGrid.Refresh()
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
