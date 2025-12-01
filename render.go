package terminal

import (
	"context"
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	widget2 "github.com/fyne-io/terminal/internal/widget"
)

const (
	cursorWidthBlock = 0 // 0 means use full cell width for block cursor
	cursorWidthCaret = 2 // 2 pixels wide for caret cursor
)

type render struct {
	term          *Terminal
	bg            *canvas.Rectangle
	bgClearCancel context.CancelFunc
	border        *canvas.Rectangle
	ptyBackground *canvas.Rectangle // Background rectangle behind PTY content area
}

func (r *render) Layout(s fyne.Size) {
	// Compute grid size from cell size and configured rows/cols.

	// Always keep a wrapper so we can drive text size reliably
	if r.term.contentThemer == nil {
		// Use the terminal's custom theme if set, otherwise fall back to app theme
		baseTheme := r.term.customTheme
		if baseTheme == nil {
			baseTheme = r.term.Theme()
		}
		baseSize := baseTheme.Size(theme.SizeNameText)
		r.term.contentThemer = &ptyTheme{
			base:            baseTheme,
			textSize:        baseSize,
			backgroundColor: r.getPTYBackgroundColor(),
		}
	}
	if r.term.contentWrapper == nil {
		r.term.contentWrapper = container.NewThemeOverride(r.term.content, r.term.contentThemer)
	}

	// Fixed font size mode: pick a fixed font size based on the canvas/grid size
	var sizeChanged bool
	if r.term.fixedPTY {
		// Only recalculate font size if widget size has changed
		sizeChanged = r.term.lastLayoutSize.Width != s.Width || r.term.lastLayoutSize.Height != s.Height
		if sizeChanged {
			// Pick best font size for current canvas size
			best := r.term.chooseFixedFontSize(s)
			if best <= 0 {
				best = 1
			}
			r.term.fixedFontSize = best
			r.term.lastLayoutSize = s // Cache the size to avoid redundant calculations

			if r.term.contentThemer.textSize != float32(best) {
				r.term.contentThemer.textSize = float32(best)
				r.term.invalidateCellCache()

				// Force immediate refresh
				if r.term.contentWrapper != nil {
					fyne.Do(func() {
						r.term.contentWrapper.Refresh()
					})
				}

				// Force the content to re-render immediately with new font
				if r.term.content != nil {
					fyne.Do(func() {
						r.term.content.Refresh()
					})
				}
			}
		} else if r.term.fixedFontSize == 0 {
			// TODO: We never seem to use this code?

			// First time initialization - ensure we have a font size
			best := r.term.chooseFixedFontSize(s)
			if best <= 0 {
				best = 1
			}
			r.term.fixedFontSize = best
			r.term.lastLayoutSize = s

			if r.term.contentThemer.textSize != float32(best) {
				r.term.contentThemer.textSize = float32(best)
				r.term.invalidateCellCache()
				if r.term.contentWrapper != nil {
					fyne.Do(func() {
						r.term.contentWrapper.Refresh()
					})
				}
				if r.term.content != nil {
					fyne.Do(func() {
						r.term.content.Refresh()
					})
				}
			}
		}
	} else {
		// Not in fixed mode: sync to base theme size (offsets will be calculated later)
		baseSize := r.term.Theme().Size(theme.SizeNameText)
		if r.term.contentThemer.textSize != baseSize {
			r.term.contentThemer.textSize = baseSize
			sizeChanged = true
			r.term.invalidateCellCache()
			if r.term.contentWrapper != nil {
				// Schedule refresh on main thread to avoid Fyne thread errors
				fyne.Do(func() {
					r.term.contentWrapper.Refresh()
				})
			}
		}
	}

	if !sizeChanged {
		// Don't need to recalculate anything else if we're not changing the font size
		return
	}

	cell := r.term.guessCellSize()
	gridWidth := float32(r.term.config.Columns) * cell.Width
	gridHeight := float32(r.term.config.Rows) * cell.Height

	// Center within available size if there is extra room
	r.term.offsetX = 0
	r.term.offsetY = 0
	if s.Width > gridWidth {
		r.term.offsetX = (s.Width - gridWidth) / 2
	}
	if s.Height > gridHeight {
		r.term.offsetY = (s.Height - gridHeight) / 2
	}

	// Move/resize the visible content object (wrapper if present)
	var target fyne.CanvasObject = r.term.content
	if r.term.contentWrapper != nil {
		target = r.term.contentWrapper
	}
	target.Move(fyne.NewPos(r.term.offsetX, r.term.offsetY))
	target.Resize(fyne.NewSize(gridWidth, gridHeight))
	// Ensure inner content always matches grid size
	r.term.content.Resize(fyne.NewSize(gridWidth, gridHeight))

	// Update border rectangle around the grid area
	if r.border == nil {
		r.border = canvas.NewRectangle(color.Transparent)
	}

	// Update border properties from terminal configuration
	if r.term.borderEnabled {
		r.border.StrokeColor = r.term.borderColor
		r.border.StrokeWidth = r.term.borderWidth
		r.border.Hidden = false

		// Expand border to account for stroke width (stroke draws on the inside)
		borderOffset := r.term.borderWidth / 2
		r.border.Move(fyne.NewPos(r.term.offsetX-borderOffset, r.term.offsetY-borderOffset))
		r.border.Resize(fyne.NewSize(gridWidth+r.term.borderWidth, gridHeight+r.term.borderWidth))
	} else {
		r.border.Hidden = true
	}

	// Update PTY background rectangle behind the grid content
	if r.ptyBackground == nil {
		r.ptyBackground = canvas.NewRectangle(r.getPTYBackgroundColor())
	}

	// Position PTY background to exactly match the grid content area
	r.ptyBackground.Move(fyne.NewPos(r.term.offsetX, r.term.offsetY))
	r.ptyBackground.Resize(fyne.NewSize(gridWidth, gridHeight))
	r.ptyBackground.FillColor = r.getPTYBackgroundColor()
	r.ptyBackground.Hidden = false
}

func (r *render) MinSize() fyne.Size {
	return fyne.NewSize(0, 0) // don't get propped open by the text cells
}

func (r *render) Refresh() {
	r.moveCursor()
	r.term.refreshCursor()

	// Keep background colour in sync with theme
	if r.bg != nil {
		r.bg.FillColor = r.getBackgroundColor()
	}
	if r.border != nil && r.term.borderEnabled {
		r.border.StrokeColor = r.term.borderColor
	}

	// Keep PTY background colour in sync
	if r.ptyBackground != nil {
		r.ptyBackground.FillColor = r.getPTYBackgroundColor()
	}

	if r.term.content != nil {
		r.term.content.Refresh()
	}
}

func (r *render) BackgroundColor() color.Color {
	// Use an opaque background so the canvas clears any stale pixels
	// when this widget is refreshed/resized
	return r.getBackgroundColor()
}

// getBackgroundColor returns the canvas background color (outside PTY area)
// This respects the theme color for UI consistency
func (r *render) getBackgroundColor() color.Color {
	// Always use theme background color for canvas area
	return theme.Color(theme.ColorNameBackground)
}

// getPTYBackgroundColor returns the PTY cell area background color
// This uses the override color if set, ensuring terminals are always dark
func (r *render) getPTYBackgroundColor() color.Color {
	// Use custom background color if set (for PTY cells)
	if r.term.backgroundColorOverride != nil {
		return r.term.backgroundColorOverride
	}
	// Fall back to theme background color
	return theme.Color(theme.ColorNameBackground)
}

func (r *render) Objects() []fyne.CanvasObject {
	// Draw background first so it clears canvas area outside the grid
	// Always return the wrapper to keep object tree stable
	// Ensure all objects are initialized to prevent nil pointer race conditions
	if r.bg == nil {
		newBg := canvas.NewRectangle(r.getBackgroundColor())
		if newBg != nil {
			r.bg = newBg
		} else {
			// Fallback if canvas creation fails
			r.bg = canvas.NewRectangle(r.getBackgroundColor())
		}
	}
	if r.border == nil {
		newBorder := canvas.NewRectangle(color.Transparent)
		if newBorder != nil {
			r.border = newBorder
		} else {
			// Fallback if canvas creation fails
			r.border = canvas.NewRectangle(color.Transparent)
		}
	}
	if r.term.contentWrapper == nil {
		// This should not happen, but prevent crashes
		if r.term.content != nil && r.term.contentThemer != nil {
			r.term.contentWrapper = container.NewThemeOverride(r.term.content, r.term.contentThemer)
		} else {
			// Create a minimal fallback to prevent crash
			r.term.content = widget2.NewTermGrid()
			baseTheme := r.term.customTheme
			if baseTheme == nil {
				baseTheme = r.term.Theme()
			}
			r.term.contentThemer = &ptyTheme{
				base:            baseTheme,
				textSize:        12,
				backgroundColor: r.getPTYBackgroundColor(),
			}
			r.term.contentWrapper = container.NewThemeOverride(r.term.content, r.term.contentThemer)
		}
	}
	if r.term.cursor == nil {
		newCursor := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
		if newCursor != nil {
			newCursor.Hidden = true
			r.term.cursor = newCursor
		} else {
			// Fallback if canvas creation fails
			r.term.cursor = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
			r.term.cursor.Hidden = true
		}
	}

	// Return all initialized objects - only add non-nil objects to prevent crashes
	objects := []fyne.CanvasObject{}

	if r.bg != nil {
		objects = append(objects, r.bg)
	}

	// Add PTY background behind the terminal content
	if r.ptyBackground != nil {
		objects = append(objects, r.ptyBackground)
	}

	if r.term.contentWrapper != nil {
		objects = append(objects, r.term.contentWrapper)
	}

	if r.border != nil {
		objects = append(objects, r.border)
	}

	if r.term.cursor != nil {
		objects = append(objects, r.term.cursor)
	}

	// Ensure we always return at least some objects to prevent empty slice issues
	if len(objects) == 0 {
		// Emergency fallback - create minimal objects
		r.bg = canvas.NewRectangle(r.getBackgroundColor())
		r.ptyBackground = canvas.NewRectangle(r.getPTYBackgroundColor())
		r.border = canvas.NewRectangle(color.Transparent)
		r.term.cursor = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
		r.term.cursor.Hidden = true
		objects = []fyne.CanvasObject{r.bg, r.ptyBackground, r.border, r.term.cursor}
	}

	return objects
}

func (r *render) Destroy() {
	if r.bgClearCancel != nil {
		r.bgClearCancel()
		r.bgClearCancel = nil
	}
}

func (r *render) moveCursor() {
	// Safety check: ensure cursor exists before trying to move it
	if r.term.cursor == nil {
		return
	}

	cell := r.term.guessCellSize()
	r.term.cursor.Move(fyne.NewPos(r.term.offsetX+cell.Width*float32(r.term.cursorCol), r.term.offsetY+cell.Height*float32(r.term.cursorRow)))
}

func (t *Terminal) refreshCursor() {
	// Safety check: ensure cursor exists before trying to refresh it
	if t.cursor == nil {
		t.cursor = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
		t.cursor.Hidden = true
		return
	}

	// Hide if we don't have focus or cursor hidden flag is set, blink handling may toggle Hidden too.
	hidden := !t.focused || t.cursorHidden
	t.cursor.Hidden = hidden

	// Base color selection (bell overrides)
	if t.bell {
		t.cursor.FillColor = theme.Color(theme.ColorNameError)
	} else {
		// Use custom theme cursor color if available, otherwise use primary
		if t.customTheme != nil {
			if cursorColor := t.customTheme.Color("cursor", theme.VariantDark); cursorColor != nil {
				t.cursor.FillColor = cursorColor
			} else {
				t.cursor.FillColor = theme.Color(theme.ColorNamePrimary)
			}
		} else {
			t.cursor.FillColor = theme.Color(theme.ColorNamePrimary)
		}
	}

	// Only recalculate cursor size if it's not been set yet or if we're in a size-changing operation
	// This prevents cursor blinking from triggering expensive cell size calculations
	currentSize := t.cursor.Size()
	if currentSize.Width <= 0 || currentSize.Height <= 0 {
		// Cursor size not initialized, calculate it
		cellSize := t.guessCellSize()
		var width float32
		if t.cursorShape == "caret" {
			width = float32(cursorWidthCaret)
		} else {
			// Default to block cursor
			width = cellSize.Width
		}
		t.cursor.Resize(fyne.NewSize(width, cellSize.Height))
	}
	// Otherwise, keep the existing cursor size to avoid triggering layout on every blink

	// Cursor visual adjustments:
	// - For caret: solid thin bar
	// - For block: semi-transparent fill so text remains visible beneath, giving an invert-like emphasis
	if t.cursorShape == "caret" {
		// Solid caret, ensure full opacity
		if col, ok := t.cursor.FillColor.(color.NRGBA); ok {
			col.A = 0xFF
			t.cursor.FillColor = col
		} else if col, ok := t.cursor.FillColor.(color.RGBA); ok {
			col.A = 0xFF
			t.cursor.FillColor = col
		}
		t.cursor.StrokeColor = color.Transparent
		t.cursor.StrokeWidth = 0
	} else {
		// Block: translucent overlay + subtle outline to keep glyphs readable
		primary := t.cursor.FillColor
		switch c := primary.(type) {
		case color.NRGBA:
			if c.A > 0x88 {
				c.A = 0x88
			}
			t.cursor.FillColor = c
		case color.RGBA:
			if c.A > 0x88 {
				c.A = 0x88
			}
			t.cursor.FillColor = c
		default:
			// Fallback: use theme primary with ~50% alpha
			pc := theme.Color(theme.ColorNamePrimary)
			if col, ok := pc.(color.NRGBA); ok {
				col.A = 0x88
				t.cursor.FillColor = col
			} else if col, ok := pc.(color.RGBA); ok {
				col.A = 0x88
				t.cursor.FillColor = col
			}
		}
		t.cursor.StrokeColor = theme.Color(theme.ColorNamePrimary)
		t.cursor.StrokeWidth = 1
	}

	// Ensure blinking is active/paused based on current state
	t.ensureCursorBlinking()

	fyne.Do(func() {
		t.cursor.Refresh()
	})

}

// CreateRenderer requests a new renderer for this terminal (just a wrapper around the TextGrid)
func (t *Terminal) CreateRenderer() fyne.WidgetRenderer {
	r := &render{term: t}
	t.cursorMoved = r.moveCursor

	// Initialize all canvas objects immediately to prevent race conditions
	r.bg = canvas.NewRectangle(r.getBackgroundColor())
	r.ptyBackground = canvas.NewRectangle(r.getPTYBackgroundColor())
	r.border = canvas.NewRectangle(color.Transparent)

	// Ensure cursor is initialized early
	if t.cursor == nil {
		t.cursor = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
		t.cursor.Hidden = true
	}

	if t.content != nil {
		// Term already has content, just return the renderer with the existing term,
		// leaving its content and setup intact
		// Start periodic background refresh to clear any stale canvas outside grid
		r.runBgClear()
		// Ensure theme override exists
		if t.contentThemer == nil {
			baseTheme := t.customTheme
			if baseTheme == nil {
				baseTheme = t.Theme()
			}
			var ptyBgColor color.Color
			if t.backgroundColorOverride != nil {
				ptyBgColor = t.backgroundColorOverride
			} else {
				ptyBgColor = theme.Color(theme.ColorNameBackground)
			}
			t.contentThemer = &ptyTheme{
				base:            baseTheme,
				textSize:        baseTheme.Size(theme.SizeNameText),
				backgroundColor: ptyBgColor,
			}
		}
		if t.contentWrapper == nil {
			t.contentWrapper = container.NewThemeOverride(t.content, t.contentThemer)
		}
		// Schedule an initial layout refresh shortly after creation to ensure
		// correct sizing once the widget is attached to a canvas.
		go func() {
			time.Sleep(10 * time.Millisecond)
			fyne.Do(func() { r.Refresh() })
		}()
		return r
	}

	t.ExtendBaseWidget(t)

	t.content = widget2.NewTermGrid()
	// Prepare theme override wrapper (always present)
	if t.contentThemer == nil {
		baseTheme := t.customTheme
		if baseTheme == nil {
			baseTheme = t.Theme()
		}
		var ptyBgColor color.Color
		if t.backgroundColorOverride != nil {
			ptyBgColor = t.backgroundColorOverride
		} else {
			ptyBgColor = theme.Color(theme.ColorNameBackground)
		}
		t.contentThemer = &ptyTheme{
			base:            baseTheme,
			textSize:        baseTheme.Size(theme.SizeNameText),
			backgroundColor: ptyBgColor,
		}
	}
	t.contentWrapper = container.NewThemeOverride(t.content, t.contentThemer)

	t.setupShortcuts()

	t.cursor = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	t.cursor.Hidden = true

	// Determine cursor width based on shape
	cellSize := t.guessCellSize()
	var width float32
	if t.cursorShape == "caret" {
		width = float32(cursorWidthCaret)
	} else {
		// Default to block cursor
		width = cellSize.Width
	}
	t.cursor.Resize(fyne.NewSize(width, cellSize.Height))

	// Canvas objects already initialized at the top of CreateRenderer
	// Start periodic background refresh to clear any stale canvas outside grid
	r.runBgClear()

	return r
}

// runBgClear starts a periodic refresh of the background rectangle to
// ensure canvas area outside the grid is regularly cleared. This helps
// in cases where asynchronous draws occur after a resize.
// DISABLED: This was causing frequent Layout calls which interfered with font scaling
func (r *render) runBgClear() {
	if r.bg == nil {
		return
	}
	if r.bgClearCancel != nil {
		r.bgClearCancel()
		r.bgClearCancel = nil
	}
	// Background refresh disabled to prevent frequent Layout triggering
	// The background will still be refreshed during normal render operations
	/*
		ctx, cancel := context.WithCancel(context.Background())
		r.bgClearCancel = cancel
		interval := 500 * time.Millisecond
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					fyne.Do(func() {
						if r.bg != nil {
							r.bg.FillColor = theme.Color(theme.ColorNameBackground)
							r.bg.Refresh()
						}
					})
				}
			}
		}()
	*/
}
