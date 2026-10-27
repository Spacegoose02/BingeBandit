package overlay

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:10])
}

var (
	fontOnce sync.Once
	boldFont *opentype.Font
)

func face(px float64) font.Face {
	fontOnce.Do(func() { boldFont, _ = opentype.Parse(gobold.TTF) })
	f, err := opentype.NewFace(boldFont, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return f
}

var (
	badgeBG   = color.NRGBA{0, 0, 0, 185}
	white     = color.NRGBA{255, 255, 255, 255}
	nearBlack = color.NRGBA{20, 20, 20, 255}
	dvBG      = color.NRGBA{0, 0, 0, 215}
	sources   = map[string]struct {
		label  string
		bg, fg color.NRGBA
	}{
		"imdb": {"IMDb", color.NRGBA{245, 197, 24, 255}, nearBlack},
		"rt":   {"RT", color.NRGBA{250, 50, 10, 255}, white},
		"tmdb": {"TMDB", color.NRGBA{1, 180, 228, 255}, nearBlack},
	}
)

// Images an uploaded overlay is drawn from, keyed by overlay id.
type Images map[int64]image.Image

// Draw scales base to width w and draws the plan on it.
func Draw(base image.Image, w int, p Plan, imgs Images) *image.RGBA {
	b := base.Bounds()
	h := int(math.Round(float64(b.Dy()) * float64(w) / float64(b.Dx())))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), base, b, draw.Src, nil)

	u := float64(w) / 100 // one percent of the width
	bh := int(math.Max(12, math.Round(9*u)))
	margin := int(math.Round(3 * u))
	gap := int(math.Max(2, math.Round(1.5*u)))
	fc := face(float64(bh) * 0.58)
	if fc == nil {
		return dst
	}
	defer fc.Close()

	// Uploaded overlays first, so the built-in badges stay readable on top
	// of a frame; turn a built-in off to put your own in its place.
	for _, o := range p.Custom {
		if img := imgs[o.ID]; img != nil {
			drawCustom(dst, img, o.Position, o.Size, o.Margin)
		}
	}

	x := margin
	if p.Resolution != "" {
		x += badge(dst, fc, x, margin, bh, p.Resolution, badgeBG, white) + gap
	}
	if p.Range != "" {
		bg := badgeBG
		if p.Range == "DOLBY VISION" {
			bg = dvBG
		}
		badge(dst, fc, x, margin, bh, p.Range, bg, white)
	}
	if p.Audio != "" {
		tw := badgeWidth(fc, bh, p.Audio)
		badge(dst, fc, w-margin-tw, margin, bh, p.Audio, badgeBG, white)
	}
	if len(p.Ratings) > 0 {
		drawRatings(dst, fc, w, h, bh, margin, gap, p.Ratings)
	}
	return dst
}

// drawRatings centres the score pills along the bottom, shrinking them until
// they fit between the margins: three scores used to run off a narrow poster.
func drawRatings(dst *image.RGBA, fc font.Face, w, h, bh, margin, gap int, ratings []Rating) {
	measure := func(fc font.Face, bh, gap int) ([][2]int, int) {
		total := gap * (len(ratings) - 1)
		widths := make([][2]int, len(ratings))
		for i, r := range ratings {
			widths[i] = [2]int{badgeWidth(fc, bh, sources[r.Source].label), badgeWidth(fc, bh, r.Text)}
			total += widths[i][0] + widths[i][1]
		}
		return widths, total
	}
	avail := w - 2*margin
	widths, total := measure(fc, bh, gap)
	for i := 0; i < 4 && total > avail && bh > 8; i++ {
		scale := float64(avail) / float64(total)
		nbh := int(math.Floor(float64(bh) * scale))
		if nbh >= bh {
			nbh = bh - 1
		}
		small := face(float64(nbh) * 0.58)
		if small == nil {
			break
		}
		defer small.Close()
		fc, bh = small, nbh
		gap = int(math.Max(1, math.Floor(float64(gap)*scale)))
		widths, total = measure(fc, bh, gap)
	}
	x := (w - total) / 2
	if x < margin {
		x = margin
	}
	y := h - margin - bh
	for i, r := range ratings {
		s := sources[r.Source]
		// One pill: the source's colour with its name, then the score.
		roundRect(dst, image.Rect(x, y, x+widths[i][0]+widths[i][1], y+bh), bh/4, badgeBG)
		roundRect(dst, image.Rect(x, y, x+widths[i][0], y+bh), bh/4, s.bg)
		text(dst, fc, x, y, widths[i][0], bh, s.label, s.fg)
		text(dst, fc, x+widths[i][0], y, widths[i][1], bh, r.Text, white)
		x += widths[i][0] + widths[i][1] + gap
	}
}

func badgeWidth(fc font.Face, bh int, s string) int {
	return font.MeasureString(fc, s).Ceil() + bh*2/3
}

// badge draws a rounded label at x, y and returns its width.
func badge(dst *image.RGBA, fc font.Face, x, y, bh int, s string, bg, fg color.Color) int {
	bw := badgeWidth(fc, bh, s)
	roundRect(dst, image.Rect(x, y, x+bw, y+bh), bh/4, bg)
	text(dst, fc, x, y, bw, bh, s, fg)
	return bw
}

// text centres s in the box at x, y.
func text(dst *image.RGBA, fc font.Face, x, y, bw, bh int, s string, c color.Color) {
	m := fc.Metrics()
	tw := font.MeasureString(fc, s)
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: fc}
	d.Dot = fixed.Point26_6{
		X: fixed.I(x) + (fixed.I(bw)-tw)/2,
		Y: fixed.I(y) + (fixed.I(bh)-(m.Ascent+m.Descent))/2 + m.Ascent,
	}
	d.DrawString(s)
}

// roundRect fills r with c, rounding its corners.
func roundRect(dst *image.RGBA, r image.Rectangle, radius int, c color.Color) {
	mask := image.NewAlpha(image.Rect(0, 0, r.Dx(), r.Dy()))
	rad := float64(radius)
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			a := 1.0
			cx, cy := -1.0, -1.0
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if fx < rad {
				cx = rad
			} else if fx > float64(r.Dx())-rad {
				cx = float64(r.Dx()) - rad
			}
			if fy < rad {
				cy = rad
			} else if fy > float64(r.Dy())-rad {
				cy = float64(r.Dy()) - rad
			}
			if cx >= 0 && cy >= 0 {
				// Inside a corner square: soften the edge over one pixel.
				a = math.Max(0, math.Min(1, rad-math.Hypot(fx-cx, fy-cy)+0.5))
			}
			mask.SetAlpha(x, y, color.Alpha{uint8(a * 255)})
		}
	}
	draw.DrawMask(dst, r, image.NewUniform(c), image.Point{}, mask, image.Point{}, draw.Over)
}

// drawCustom places an uploaded image: size is its width as a percentage of
// the poster's, margin the gap to the edges it sits against.
func drawCustom(dst *image.RGBA, img image.Image, pos string, size, margin int) {
	W, H := dst.Bounds().Dx(), dst.Bounds().Dy()
	ib := img.Bounds()
	if ib.Dx() == 0 || ib.Dy() == 0 {
		return
	}
	var r image.Rectangle
	if pos == "full" {
		r = dst.Bounds()
	} else {
		if size <= 0 || size > 100 {
			size = 25
		}
		w := W * size / 100
		h := int(math.Round(float64(ib.Dy()) * float64(w) / float64(ib.Dx())))
		m := W * margin / 100
		x, y := (W-w)/2, (H-h)/2
		switch pos {
		case "top-left", "left", "bottom-left":
			x = m
		case "top-right", "right", "bottom-right":
			x = W - m - w
		}
		switch pos {
		case "top-left", "top", "top-right":
			y = m
		case "bottom-left", "bottom", "bottom-right":
			y = H - m - h
		}
		r = image.Rect(x, y, x+w, y+h)
	}
	xdraw.CatmullRom.Scale(dst, r, img, ib, draw.Over, nil)
}
