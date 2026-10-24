package main

import "math"

// Zones are the parts of the picture a light can follow.
var Zones = []string{"all", "left", "centre", "right", "top", "bottom"}

// Frame size the sampler asks ffmpeg for: small, 16:9, divisible by 3.
const frameW, frameH = 48, 27

// Colour is a light setting: CIE xy and a 0–1 brightness.
type Colour struct {
	X, Y float64
	Bri  float64
}

// zoneColours turns one rgb24 frame into a colour per zone.
func zoneColours(rgb []byte) map[string]Colour {
	out := make(map[string]Colour, len(Zones))
	for _, z := range Zones {
		x0, x1, y0, y1 := 0, frameW, 0, frameH
		switch z {
		case "left":
			x1 = frameW / 3
		case "right":
			x0 = frameW * 2 / 3
		case "centre":
			x0, x1, y0, y1 = frameW/3, frameW*2/3, frameH/3, frameH*2/3
		case "top":
			y1 = frameH / 3
		case "bottom":
			y0 = frameH * 2 / 3
		}
		out[z] = average(rgb, x0, x1, y0, y1)
	}
	return out
}

// average mixes a region's pixels, weighting saturated ones up so a red
// coat on a grey street still shows as red.
func average(rgb []byte, x0, x1, y0, y1 int) Colour {
	var r, g, b, w, lum float64
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*frameW + x) * 3
			if i+2 >= len(rgb) {
				continue
			}
			pr, pg, pb := float64(rgb[i])/255, float64(rgb[i+1])/255, float64(rgb[i+2])/255
			mx, mn := math.Max(pr, math.Max(pg, pb)), math.Min(pr, math.Min(pg, pb))
			sat := 0.0
			if mx > 0 {
				sat = (mx - mn) / mx
			}
			weight := 0.15 + sat*mx // dark and grey pixels count, but little
			r, g, b, w = r+pr*weight, g+pg*weight, b+pb*weight, w+weight
			lum += 0.2126*pr + 0.7152*pg + 0.0722*pb
			n++
		}
	}
	if n == 0 || w == 0 {
		return Colour{X: 0.4573, Y: 0.41, Bri: 0}
	}
	x, y := rgbToXY(r/w, g/w, b/w)
	return Colour{X: x, Y: y, Bri: lum / float64(n)}
}

// rgbToXY converts sRGB (0–1) to CIE xy the way Philips documents for Hue,
// clamped into gamut C (current colour bulbs).
func rgbToXY(r, g, b float64) (float64, float64) {
	lin := func(c float64) float64 {
		if c > 0.04045 {
			return math.Pow((c+0.055)/1.055, 2.4)
		}
		return c / 12.92
	}
	r, g, b = lin(r), lin(g), lin(b)
	X := r*0.4124 + g*0.3576 + b*0.1805
	Y := r*0.2126 + g*0.7152 + b*0.0722
	Z := r*0.0193 + g*0.1192 + b*0.9505
	sum := X + Y + Z
	if sum == 0 {
		return 0.3127, 0.3290 // D65 white
	}
	return clampGamut(X/sum, Y/sum)
}

// Gamut C corners (red, green, blue).
var gamut = [3][2]float64{{0.6915, 0.3083}, {0.17, 0.7}, {0.1532, 0.0475}}

func clampGamut(x, y float64) (float64, float64) {
	if inTriangle(x, y) {
		return x, y
	}
	best, bx, by := math.Inf(1), x, y
	for i := 0; i < 3; i++ {
		a, b := gamut[i], gamut[(i+1)%3]
		px, py := closestOnSegment(x, y, a, b)
		if d := math.Hypot(px-x, py-y); d < best {
			best, bx, by = d, px, py
		}
	}
	return bx, by
}

func inTriangle(x, y float64) bool {
	cross := func(a, b [2]float64) float64 { return (b[0]-a[0])*(y-a[1]) - (b[1]-a[1])*(x-a[0]) }
	d1, d2, d3 := cross(gamut[0], gamut[1]), cross(gamut[1], gamut[2]), cross(gamut[2], gamut[0])
	neg, pos := d1 < 0 || d2 < 0 || d3 < 0, d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func closestOnSegment(x, y float64, a, b [2]float64) (float64, float64) {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := ((x-a[0])*dx + (y-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return a[0] + t*dx, a[1] + t*dy
}

// hueBri maps picture brightness to the bridge's 1–254 scale. Dark scenes
// dim the lights but never quite to off, and maxPct caps them.
func hueBri(lum float64, maxPct float64) int {
	level := 0.08 + 0.92*math.Sqrt(math.Max(0, math.Min(1, lum)))
	b := int(math.Round(level * maxPct / 100 * 254))
	return max(1, min(254, b))
}
