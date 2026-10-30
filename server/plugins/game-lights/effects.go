package main

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Colour is a light setting: CIE xy and a brightness from 1 to 254.
type Colour struct {
	X, Y float64
	Bri  int
}

// Palette is a team's two light colours, and the hex they came from.
type Palette struct {
	Primary, Secondary Colour
	Hex                [2]string
}

// gamutC is the colour range of current Hue colour bulbs.
var gamutC = [3][2]float64{{0.6915, 0.3083}, {0.17, 0.7}, {0.1532, 0.0475}}

func parseHex(hex string) (r, g, b float64, ok bool) {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64(n>>16&0xff) / 255, float64(n>>8&0xff) / 255, float64(n&0xff) / 255, true
}

func linear(c float64) float64 {
	if c > 0.04045 {
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return c / 12.92
}

// hexColour turns a colour like #861F41 into Hue xy, clamped to the bulbs'
// gamut, with a brightness floored so dark maroon still shows.
func hexColour(hex string) Colour {
	r, g, b, ok := parseHex(hex)
	if !ok {
		r, g, b = 1, 1, 1
	}
	R, G, B := linear(r), linear(g), linear(b)
	X := R*0.664511 + G*0.154324 + B*0.162028
	Y := R*0.283881 + G*0.668433 + B*0.047685
	Z := R*0.000088 + G*0.07231 + B*0.986039
	sum := X + Y + Z
	if sum == 0 {
		sum = 1
	}
	p := clampGamut([2]float64{X / sum, Y / sum})
	bri := math.Min(1, math.Max(0.35, math.Sqrt(Y)))
	return Colour{X: round4(p[0]), Y: round4(p[1]), Bri: int(math.Round(bri * 254))}
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

func cross(a, b [2]float64) float64  { return a[0]*b[1] - a[1]*b[0] }
func sub(a, b [2]float64) [2]float64 { return [2]float64{a[0] - b[0], a[1] - b[1]} }

func clampGamut(p [2]float64) [2]float64 {
	r, g, b := gamutC[0], gamutC[1], gamutC[2]
	v1, v2, q := sub(g, r), sub(b, r), sub(p, r)
	s := cross(q, v2) / cross(v1, v2)
	t := cross(v1, q) / cross(v1, v2)
	if s >= 0 && t >= 0 && s+t <= 1 {
		return p
	}
	best, bestD := p, math.Inf(1)
	for _, seg := range [][2][2]float64{{r, g}, {g, b}, {b, r}} {
		a, ab := seg[0], sub(seg[1], seg[0])
		k := math.Max(0, math.Min(1, ((p[0]-a[0])*ab[0]+(p[1]-a[1])*ab[1])/(ab[0]*ab[0]+ab[1]*ab[1])))
		c := [2]float64{a[0] + ab[0]*k, a[1] + ab[1]*k}
		if d := math.Hypot(c[0]-p[0], c[1]-p[1]); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func luminance(hex string) float64 {
	r, g, b, _ := parseHex(hex)
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// weak colours make poor flashes: black is "off", white looks like the room.
func weak(hex string) bool { l := luminance(hex); return l < 0.02 || l > 0.85 }

// teamOverrides are official colours for teams whose ESPN colours look
// wrong on a bulb.
var teamOverrides = map[string][2]string{
	"VT": {"#861F41", "#E5751F"}, // Virginia Tech: Chicago Maroon, Burnt Orange
}

// teamHex picks a team's two colours: an admin's choice, the overrides
// above, then ESPN's, swapping out black and white.
func teamHex(abbr, color, alt string, custom *[2]string) [2]string {
	if custom != nil {
		return *custom
	}
	pair, ok := teamOverrides[abbr]
	if !ok {
		pair = [2]string{"#" + strings.TrimPrefix(firstOr(color, "ffffff"), "#"), "#" + strings.TrimPrefix(firstOr(alt, "ffffff"), "#")}
	}
	w1, w2 := weak(pair[0]), weak(pair[1])
	switch {
	case w1 && w2:
		return [2]string{"#FFFFFF", "#FFFFFF"}
	case w1:
		return [2]string{pair[1], pair[1]}
	case w2:
		return [2]string{pair[0], pair[0]}
	}
	return pair
}

func firstOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// palette builds the light colours. When both colours are the same (crimson
// and white, say), the second is a brighter or dimmer copy so alternating
// effects still flash.
func palette(hex [2]string) Palette {
	p := Palette{Primary: hexColour(hex[0]), Secondary: hexColour(hex[1]), Hex: hex}
	if strings.EqualFold(hex[0], hex[1]) {
		p.Secondary = p.Primary
		if p.Primary.Bri >= 152 {
			p.Secondary.Bri = 50
		} else {
			p.Secondary.Bri = 254
		}
	}
	return p
}

var (
	red   = dim(hexColour("#FF0000"), 254)
	amber = dim(hexColour("#FFB000"), 254)
	green = dim(hexColour("#00FF00"), 254)
	white = Colour{X: 0.3127, Y: 0.329, Bri: 254} // cold white, nothing like any team's colours
)

// Step is one light setting held for a while.
type Step struct {
	Colour Colour
	Hold   time.Duration
}

func dim(c Colour, bri int) Colour { c.Bri = bri; return c }

func alternate(a, b Colour, n int, hold time.Duration) []Step {
	out := make([]Step, n)
	for i := range out {
		c := a
		if i%2 == 1 {
			c = b
		}
		out[i] = Step{c, hold}
	}
	return out
}

// Effects are the light effects, by the key the setup page turns them on and off with.
var Effects = []struct{ Key, Label string }{
	{"touchdown", "We score a touchdown, a goal or a home run: team colours alternate"},
	{"field_goal", "We kick a field goal or score a run: team colour pulses"},
	{"take_lead", "We take the lead in basketball: team colours alternate briefly"},
	{"scoring_run", "We go on a 10–0 run in basketball: team colours alternate"},
	{"takeaway", "We intercept, recover a fumble, stop them on downs or score a safety: green flash"},
	{"sack_for", "We sack the opponent: team colours alternate for 4 seconds"},
	{"big_play", "We gain 20 yards or more: second team colour flashes"},
	{"turnover_against", "We turn it over: red and white strobe"},
	{"sack_against", "We get sacked: amber pulse"},
	{"opp_score", "The other team scores (or, in basketball, takes the lead or goes on a run): dim red pulse"},
	{"win", "We win: a long celebration in team colours"},
}

// Event is something that happened for the followed team.
type Event struct {
	Kind string // touchdown, field_goal, safety, score, goal, run, home_run, lead, scoring_run, interception, fumble_lost, turnover_on_downs, sack, big_play, win
	Good bool   // good for the followed team
	Mine bool   // a score by the followed team
}

// effectKey says which effect an event fires, or "".
func effectKey(e Event) string {
	switch e.Kind {
	case "touchdown", "goal", "home_run", "score":
		if e.Mine {
			return "touchdown"
		}
		return "opp_score"
	case "field_goal", "run":
		if e.Mine {
			return "field_goal"
		}
		return "opp_score"
	case "lead":
		if e.Mine {
			return "take_lead"
		}
		return "opp_score"
	case "scoring_run":
		if e.Mine {
			return "scoring_run"
		}
		return "opp_score"
	case "safety":
		if e.Mine {
			return "takeaway"
		}
		return "opp_score"
	case "interception", "fumble_lost", "turnover_on_downs":
		if e.Good {
			return "takeaway"
		}
		return "turnover_against"
	case "sack":
		if e.Good {
			return "sack_for"
		}
		return "sack_against"
	case "big_play":
		return "big_play"
	case "win":
		return "win"
	}
	return ""
}

// effectSteps is the light show for an effect. Flashes stay under about
// three a second, and slower still with reduced flashing.
func effectSteps(key string, p Palette, reduced bool) []Step {
	hold := 350 * time.Millisecond
	if reduced {
		hold = 900 * time.Millisecond
	}
	n := func(full, calm int) int {
		if reduced {
			return calm
		}
		return full
	}
	P, S := p.Primary, p.Secondary
	switch key {
	case "touchdown":
		return append(alternate(P, S, n(20, 6), hold), Step{P, 800 * time.Millisecond})
	case "field_goal":
		return alternate(P, dim(P, 20), 6, hold)
	case "take_lead":
		return alternate(P, S, n(6, 4), hold)
	case "scoring_run":
		return append(alternate(P, S, n(12, 6), hold), Step{P, 800 * time.Millisecond})
	case "takeaway":
		return alternate(green, dim(green, 30), n(12, 4), hold)
	case "sack_for":
		return alternate(P, S, int(math.Ceil(float64(4*time.Second)/float64(hold))), hold)
	case "big_play":
		return alternate(S, P, 4, hold)
	case "turnover_against":
		return alternate(red, white, n(12, 4), hold)
	case "sack_against":
		return alternate(amber, dim(amber, 38), 2, 500*time.Millisecond)
	case "opp_score":
		return []Step{{dim(red, 76), 700 * time.Millisecond}, {dim(red, 20), 700 * time.Millisecond}}
	case "opp_colour": // the other team scores, for people who chose their colours over red
		return []Step{{dim(P, 76), 700 * time.Millisecond}, {dim(P, 20), 700 * time.Millisecond}}
	case "win":
		return append(alternate(P, S, n(40, 12), hold), Step{P, 2 * time.Second})
	}
	return nil
}
