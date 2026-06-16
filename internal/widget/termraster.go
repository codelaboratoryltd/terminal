package widget

// termraster.go — single-texture terminal rendering.
//
// Replaces the TextGrid-based renderer (~3840 GL draw calls per 80×24 frame)
// with a single canvas.Raster that composites the entire terminal into one
// image.RGBA buffer. Fyne uploads it as a single GL texture and issues one
// draw call per repaint — essential on Mesa/llvmpipe where each GL state-change
// costs ~250µs (3840 × 250µs ≈ 960ms per repaint → 2-second visible lag).
//
// go-text/render is intentionally NOT used here. Its DrawStringAt creates an
// rasterx.ScannerGV sized to the full output image on every call:
//   (imgH+1)*(imgW+1) float32 values ≈ 1.2MB for a typical terminal window.
// Drawing 1920 characters per frame would allocate 2.3 GB / frame, causing
// the GC to pause for seconds.
//
// golang.org/x/image/font/opentype uses golang.org/x/image/vector which
// allocates ~100-200 bytes per glyph (glyph bounding box only), making the
// total allocation per frame ~500 KB — fast to GC.

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	"fyne.io/fyne/v2"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"fyne.io/fyne/v2/widget"
)

// ------------------- font state (process-global, loaded once per size+face) -

type fontCacheKey struct {
	size     float32
	fontName string
	scale    float32
}

type rasterFontData struct {
	mono   xfont.Face
	bold   xfont.Face // DejaVu Sans Mono Bold has the same advance as Regular
	ascent int        // pixels from cell top to text baseline
	cellW  int        // monospace advance width in pixels
	cellH  int        // line height in pixels
}

var (
	rasterFontMu    sync.RWMutex
	rasterFontCache = map[fontCacheKey]*rasterFontData{}
)

func getRasterFont(textSizePt float32, monoRes, boldRes fyne.Resource, scale float32) *rasterFontData {
	key := fontCacheKey{textSizePt, monoRes.Name(), scale}
	rasterFontMu.RLock()
	if d, ok := rasterFontCache[key]; ok {
		rasterFontMu.RUnlock()
		return d
	}
	rasterFontMu.RUnlock()

	d := loadRasterFont(textSizePt, monoRes, boldRes, scale)

	rasterFontMu.Lock()
	rasterFontCache[key] = d
	rasterFontMu.Unlock()
	return d
}

func loadRasterFont(textSizePt float32, monoRes, boldRes fyne.Resource, scale float32) *rasterFontData {
	if scale <= 0 {
		scale = 1
	}
	opts := &opentype.FaceOptions{
		Size:    float64(textSizePt),
		DPI:     float64(72 * scale),
		Hinting: xfont.HintingFull,
	}

	d := &rasterFontData{}

	if f, err := opentype.Parse(monoRes.Content()); err == nil {
		if face, err := opentype.NewFace(f, opts); err == nil {
			d.mono = face
			m := face.Metrics()
			d.ascent = m.Ascent.Round()
			d.cellH = m.Height.Round()
			if adv, ok := face.GlyphAdvance('M'); ok {
				d.cellW = adv.Round()
			}
		}
	}
	if d.mono == nil {
		return d
	}

	// Bold face: DejaVu Sans Mono Bold has the same advance width as Regular,
	// so we can use it directly without synthetic bold.
	d.bold = d.mono
	if f, err := opentype.Parse(boldRes.Content()); err == nil {
		if face, err := opentype.NewFace(f, opts); err == nil {
			d.bold = face
		}
	}

	return d
}

// RasterCellSize returns the cell size in Fyne logical points for the given
// text size and font. Always measured at scale=1 (72 DPI) so the result is
// device-independent and suitable for MinSize calculations.
func RasterCellSize(textSizePt float32, monoRes, boldRes fyne.Resource) (width, height float32) {
	d := getRasterFont(textSizePt, monoRes, boldRes, 1)
	if d == nil || d.mono == nil {
		return 0, 0
	}
	return float32(d.cellW), float32(d.cellH)
}

// ------------------- color uniform cache -----------------------------------

// colorUniformCache maps RGBA values to pre-allocated image.Uniform objects so
// we don't allocate one per cell per frame.
var (
	colorUniformMu    sync.RWMutex
	colorUniformCache = map[color.Color]*image.Uniform{}
)

func getUniform(c color.Color) *image.Uniform {
	colorUniformMu.RLock()
	if u, ok := colorUniformCache[c]; ok {
		colorUniformMu.RUnlock()
		return u
	}
	colorUniformMu.RUnlock()

	u := image.NewUniform(c)
	colorUniformMu.Lock()
	colorUniformCache[c] = u
	colorUniformMu.Unlock()
	return u
}

// ------------------- per-frame pixel rendering -----------------------------

// cellSnap records the resolved visual state of one terminal cell.
// Pointer equality on fgU/bgU is valid because getUniform returns one
// canonical *image.Uniform per distinct RGBA value.
type cellSnap struct {
	r         rune
	fgU       *image.Uniform
	bgU       *image.Uniform // nil = use default background
	bold      bool
	underline bool
}

// RenderTermToImage composites terminal cells into img.
// img must already be allocated to the correct pixel dimensions.
// cols / numRows are the grid dimensions.
// monoRes and boldRes must be the same font resources the theme returns for
// Monospace and Bold+Monospace styles respectively.
// scale is the canvas pixel-to-point ratio (1.0 on standard displays, 2.0 on HiDPI/Retina).
//
// snaps is the cell snapshot from the previous frame (nil or wrong length forces
// a full redraw). Returns the updated snapshot and the pixel-space bounding
// rectangle of all cells that were repainted. On a full redraw the dirty rect
// equals img.Bounds(). On a no-op frame (nothing changed) it is image.Rectangle{}.
func RenderTermToImage(
	rows []widget.TextGridRow,
	img *image.RGBA,
	cols, numRows int,
	defaultFG, defaultBG color.Color,
	textSizePt float32,
	monoRes, boldRes fyne.Resource,
	scale float32,
	snaps []cellSnap,
) ([]cellSnap, image.Rectangle) {
	fd := getRasterFont(textSizePt, monoRes, boldRes, scale)
	if fd == nil || fd.mono == nil {
		return snaps, image.Rectangle{}
	}

	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if w == 0 || h == 0 || cols == 0 || numRows == 0 {
		return snaps, image.Rectangle{}
	}

	cw := w / cols
	ch := h / numRows
	if cw <= 0 || ch <= 0 {
		return snaps, image.Rectangle{}
	}

	total := cols * numRows
	fullRedraw := len(snaps) != total
	if fullRedraw {
		// Fill the whole buffer with the default background colour.
		draw.Draw(img, img.Bounds(), getUniform(defaultBG), image.Point{}, draw.Src)
		snaps = make([]cellSnap, total)
	}

	defaultFGUniform := getUniform(defaultFG)
	defaultBGUniform := getUniform(defaultBG)

	// dirtyRect accumulates the pixel bounds of every cell repainted this frame.
	// For a full redraw it is set to img.Bounds() upfront; otherwise it grows
	// as dirty cells are found so callers can use it for sub-texture uploads.
	var dirtyRect image.Rectangle
	if fullRedraw {
		dirtyRect = img.Bounds()
	}

	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		var cells []widget.TextGridCell
		if rowIdx < len(rows) {
			cells = rows[rowIdx].Cells
		}

		for colIdx := 0; colIdx < cols; colIdx++ {
			var r rune = ' '
			fgUniform := defaultFGUniform
			var bgUniform *image.Uniform
			useBold := false
			useUnderline := false

			if colIdx < len(cells) {
				cell := cells[colIdx]
				if cell.Rune != 0 {
					r = cell.Rune
				}
				if cell.Style != nil {
					if c := cell.Style.TextColor(); c != nil {
						fgUniform = getUniform(c)
					}
					if c := cell.Style.BackgroundColor(); c != nil {
						bgUniform = getUniform(c)
					}
					s := cell.Style.Style()
					useBold = s.Bold
					useUnderline = s.Underline
				}
			}

			snap := cellSnap{r, fgUniform, bgUniform, useBold, useUnderline}
			snapIdx := rowIdx*cols + colIdx
			if !fullRedraw {
				if snaps[snapIdx] == snap {
					continue // cell unchanged — leave pixel buffer as-is
				}
				// Restore default background before overpainting this cell.
				draw.Draw(img, image.Rect(colIdx*cw, rowIdx*ch, colIdx*cw+cw, rowIdx*ch+ch), defaultBGUniform, image.Point{}, draw.Src)
				// Expand dirty rect to include this cell (allow +1px for descenders).
				cellBounds := image.Rect(colIdx*cw, rowIdx*ch, colIdx*cw+cw, min(rowIdx*ch+ch+1, h))
				dirtyRect = dirtyRect.Union(cellBounds)
			}
			snaps[snapIdx] = snap

			x0 := colIdx * cw
			y0 := rowIdx * ch

			// Per-cell background (only when it differs from the default).
			if bgUniform != nil {
				draw.Draw(img, image.Rect(x0, y0, x0+cw, y0+ch), bgUniform, image.Point{}, draw.Src)
			}

			if useUnderline {
				uy := y0 + fd.ascent + 1
				draw.Draw(img, image.Rect(x0, uy, x0+cw, uy+1), fgUniform, image.Point{}, draw.Src)
			}

			// Glyph — skip space (saves a shaping + glyph-mask round-trip per cell).
			if r == ' ' || r == 0 {
				continue
			}

			face := fd.mono
			if useBold {
				face = fd.bold
			}

			// Skip glyphs absent from the face rather than letting font.Drawer
			// substitute the □ replacement character. Some fonts (e.g. DejaVu
			// Sans Mono Bold) lack box-drawing/block glyphs that the Regular
			// face has, so fall back to the regular face before giving up —
			// those runes are weight-agnostic line art and look fine unbolded.
			if _, ok := face.GlyphAdvance(r); !ok {
				if face != fd.mono {
					if _, ok = fd.mono.GlyphAdvance(r); ok {
						face = fd.mono
					}
				}
				if _, ok := face.GlyphAdvance(r); !ok {
					continue
				}
			}

			// Clip to cell bounds so any glyph side-bearing never bleeds into
			// the adjacent column. font.Drawer respects the destination bounds.
			// Allow 1 extra pixel at the bottom so fonts that place descenders
			// (e.g. '_') at the very edge of cellH aren't clipped — mirrors
			// fyne's TextVectorPad = 1 for canvas.Text. The overflow pixel falls
			// in the next row's top-most line; if that cell has default background
			// and its glyph has no ink there, the overflow dot remains visible.
			// This is the same trade-off fyne's own fix accepts.
			cellDst := img.SubImage(image.Rect(x0, y0, x0+cw, min(y0+ch+1, h))).(draw.Image)
			d := xfont.Drawer{
				Dst:  cellDst,
				Src:  fgUniform,
				Face: face,
				Dot:  fixed.P(x0, y0+fd.ascent),
			}
			d.DrawString(string(r))
		}
	}
	return snaps, dirtyRect
}

// ScanDirtyBounds returns the pixel bounding rect of all cells that differ from
// snaps without performing any pixel rendering. Used by DirtyPixelBounds() to
// compute the CURRENT frame's dirty rect before drawToImage runs, avoiding the
// one-frame stale lag that lastDirtyBounds would produce.
//
// Returns the full image bounds when snaps are nil/wrong length (full redraw).
// Returns image.Rectangle{} when nothing has changed.
func ScanDirtyBounds(
	rows []widget.TextGridRow,
	imgW, imgH int,
	cols, numRows int,
	defaultFG, defaultBG color.Color,
	textSizePt float32,
	monoRes, boldRes fyne.Resource,
	scale float32,
	snaps []cellSnap,
) image.Rectangle {
	if imgW == 0 || imgH == 0 || cols == 0 || numRows == 0 {
		return image.Rectangle{}
	}
	if len(snaps) != cols*numRows {
		return image.Rect(0, 0, imgW, imgH) // full redraw
	}
	cw := imgW / cols
	ch := imgH / numRows
	if cw <= 0 || ch <= 0 {
		return image.Rectangle{}
	}

	defaultFGUniform := getUniform(defaultFG)
	_ = getUniform(defaultBG)

	fd := getRasterFont(textSizePt, monoRes, boldRes, scale)

	var dirty image.Rectangle
	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		var cells []widget.TextGridCell
		if rowIdx < len(rows) {
			cells = rows[rowIdx].Cells
		}
		for colIdx := 0; colIdx < cols; colIdx++ {
			var r rune = ' '
			fgUniform := defaultFGUniform
			var bgUniform *image.Uniform
			useBold := false
			useUnderline := false
			if colIdx < len(cells) {
				cell := cells[colIdx]
				if cell.Rune != 0 {
					r = cell.Rune
				}
				if cell.Style != nil {
					if c := cell.Style.TextColor(); c != nil {
						fgUniform = getUniform(c)
					}
					if c := cell.Style.BackgroundColor(); c != nil {
						bgUniform = getUniform(c)
					}
					s := cell.Style.Style()
					useBold = s.Bold && fd != nil && fd.bold != nil
					useUnderline = s.Underline
				}
			}
			snap := cellSnap{r, fgUniform, bgUniform, useBold, useUnderline}
			if snaps[rowIdx*cols+colIdx] == snap {
				continue
			}
			cellBounds := image.Rect(colIdx*cw, rowIdx*ch, colIdx*cw+cw, min(rowIdx*ch+ch+1, imgH))
			dirty = dirty.Union(cellBounds)
		}
	}
	return dirty
}
