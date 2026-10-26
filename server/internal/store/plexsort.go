package store

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// plexOrder ranks ASCII the way Plex's sort (the Unicode collation order)
// does: spaces, then punctuation with "_" before "+", then digits, then
// letters ignoring case. Kometa configs lean on this: "+_" sorts before
// "++_", and "+_1Family" before "+_Daily".
const plexOrder = " \t_-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$0123456789abcdefghijklmnopqrstuvwxyz"

var plexRank = func() [128]int {
	var r [128]int
	for i := range r {
		r[i] = -1
	}
	for i, c := range plexOrder {
		r[c] = i
	}
	return r
}()

// PlexCompare orders two titles the way Plex orders sort titles.
func PlexCompare(a, b string) int {
	ka, kb := strings.ToLower(a), strings.ToLower(b)
	for ka != "" && kb != "" {
		ra, na := utf8.DecodeRuneInString(ka)
		rb, nb := utf8.DecodeRuneInString(kb)
		if d := plexWeight(ra) - plexWeight(rb); d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
		ka, kb = ka[na:], kb[nb:]
	}
	switch {
	case len(ka) < len(kb):
		return -1
	case len(ka) > len(kb):
		return 1
	}
	return strings.Compare(a, b)
}

func plexWeight(r rune) int {
	if r < 128 {
		if w := plexRank[r]; w >= 0 {
			return w
		}
		return int(r) - 128 // control characters: first
	}
	if unicode.IsLetter(r) {
		return len(plexOrder) + int(r)
	}
	return len(plexOrder) - 1 // other symbols: with "$", before digits
}

// PlexCollation is the SQLite collation name for PlexCompare.
const PlexCollation = "PLEXSORT"

func init() { sqlite.MustRegisterCollationUtf8(PlexCollation, PlexCompare) }
