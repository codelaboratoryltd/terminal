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
	"strings"
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

// underscoreShiftPx is how many device pixels to raise the '_' glyph when
// rendering DejaVuSansMono. That font places the underscore at or below the
// descender line; at certain point sizes the rounding causes Fyne to clip it
// entirely. The shift keeps it visible without modifying the font files.
const underscoreShiftPx = 2

// underscoreShiftFace wraps an xfont.Face and shifts the '_' glyph up by
// underscoreShiftPx device pixels by adjusting the destination rectangle
// returned from Glyph(). All other runes are passed through unchanged.
type underscoreShiftFace struct {
	xfont.Face
}

func (f underscoreShiftFace) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	dr, mask, maskp, advance, ok = f.Face.Glyph(dot, r)
	if ok && r == '_' {
		dr.Min.Y -= underscoreShiftPx
		dr.Max.Y -= underscoreShiftPx
	}
	return
}

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

	// DejaVuSansMono places '_' at or below the descender line, which causes
	// Fyne to clip it at certain point sizes. Wrap both faces so the glyph
	// renders a couple of pixels higher than the font data specifies.
	if strings.Contains(monoRes.Name(), "DejaVu") {
		d.mono = underscoreShiftFace{d.mono}
		d.bold = underscoreShiftFace{d.bold}
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

// RenderTermToImage composites all terminal cells into img.
// img must already be allocated to the correct pixel dimensions.
// cols / numRows are the grid dimensions.
// monoRes and boldRes must be the same font resources the theme returns for
// Monospace and Bold+Monospace styles respectively.
// scale is the canvas pixel-to-point ratio (1.0 on standard displays, 2.0 on HiDPI/Retina).
func RenderTermToImage(
	rows []widget.TextGridRow,
	img *image.RGBA,
	cols, numRows int,
	defaultFG, defaultBG color.Color,
	textSizePt float32,
	monoRes, boldRes fyne.Resource,
	scale float32,
) {
	fd := getRasterFont(textSizePt, monoRes, boldRes, scale)
	if fd == nil || fd.mono == nil {
		return
	}

	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if w == 0 || h == 0 || cols == 0 || numRows == 0 {
		return
	}

	cw := w / cols
	ch := h / numRows
	if cw <= 0 || ch <= 0 {
		return
	}

	// Fill the whole buffer with the default background colour.
	draw.Draw(img, img.Bounds(), getUniform(defaultBG), image.Point{}, draw.Src)

	defaultFGUniform := getUniform(defaultFG)

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
			// substitute the □ replacement character.
			if _, ok := face.GlyphAdvance(r); !ok {
				continue
			}

			// Clip to cell bounds so any glyph side-bearing never bleeds into
			// the adjacent column. font.Drawer respects the destination bounds.
			cellDst := img.SubImage(image.Rect(x0, y0, x0+cw, y0+ch)).(draw.Image)
			d := xfont.Drawer{
				Dst:  cellDst,
				Src:  fgUniform,
				Face: face,
				Dot:  fixed.P(x0, y0+fd.ascent),
			}
			d.DrawString(string(r))
		}
	}
}
