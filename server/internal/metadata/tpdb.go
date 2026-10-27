package metadata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"image"
	"image/jpeg"
	_ "image/png" // decode PNG posters for thumbnails
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ThePosterDB has no public API, so this reads its search and title pages.
// Their markup can change at any time; the patterns below are deliberately
// loose (any link or attribute that names a poster id) so small changes don't
// break it, and errors say so plainly so the admin can fall back to pasting a link.

const tpdbBase = "https://theposterdb.com"

// ErrTPDbBlocked means ThePosterDB refused the request (usually Cloudflare's bot check).
var ErrTPDbBlocked = errors.New("ThePosterDB refused the request (it may be blocking automated searches). Open the search on their site and paste a poster link instead")

// TPDbTitle is a movie, show or collection page on ThePosterDB with its posters.
type TPDbTitle struct {
	ID      int64        `json:"id"`
	Title   string       `json:"title"`
	URL     string       `json:"url"`
	Posters []TPDbPoster `json:"posters"`
}

// TPDbPoster is one poster; Download is the image itself.
type TPDbPoster struct {
	ID       int64  `json:"id"`
	Title    string `json:"title,omitempty"`
	Uploader string `json:"uploader,omitempty"`
	Download string `json:"download"`
}

type TPDb struct {
	base     string // https://theposterdb.com, or a stand-in for testing
	hc       *http.Client
	thumbDir string
	mu       sync.Mutex
	cache    map[string]tpdbCached
}

type tpdbCached struct {
	at  time.Time
	res []TPDbTitle
}

func NewTPDb(base, thumbDir string) *TPDb {
	if base == "" {
		base = tpdbBase
	}
	return &TPDb{base: strings.TrimSuffix(base, "/"), hc: &http.Client{Timeout: 20 * time.Second}, thumbDir: thumbDir, cache: map[string]tpdbCached{}}
}

// AssetURL is where a poster's full image is downloaded from.
func (t *TPDb) AssetURL(id int64) string { return fmt.Sprintf("%s/api/assets/%d", t.base, id) }

var (
	reTPDbTitleLink  = regexp.MustCompile(`(?is)<a\b[^>]*href="(?:https?://theposterdb\.com)?/posters/(\d+)"[^>]*>(.*?)</a>`)
	reTPDbPosterID   = regexp.MustCompile(`(?i)data-poster-id="(\d+)"|/api/assets/(\d+)|theposterdb\.com/poster/(\d+)\b|href="/poster/(\d+)"`)
	reTPDbUser       = regexp.MustCompile(`(?i)/user/([A-Za-z0-9_.-]+)`)
	reTPDbPosterText = regexp.MustCompile(`(?is)class="[^"]*text-break[^"]*"[^>]*>(.*?)</`)
	reTags           = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpace          = regexp.MustCompile(`\s+`)
	reYear           = regexp.MustCompile(`\((\d{4})\)`)
	// Links an admin might paste: a poster, a title page or a set.
	reTPDbLink = regexp.MustCompile(`^https?://(?:www\.)?theposterdb\.com/(poster|posters|set|api/assets)/(\d+)`)
)

func cleanText(s string) string {
	return strings.TrimSpace(reSpace.ReplaceAllString(html.UnescapeString(reTags.ReplaceAllString(s, " ")), " "))
}

// parseTPDbSearch returns the title pages a search page links to, in page order.
func (t *TPDb) parseSearch(page string) []TPDbTitle {
	var out []TPDbTitle
	seen := map[int64]int{}
	for _, m := range reTPDbTitleLink.FindAllStringSubmatch(page, -1) {
		id, _ := strconv.ParseInt(m[1], 10, 64)
		text := cleanText(m[2])
		if i, ok := seen[id]; ok {
			if out[i].Title == "" {
				out[i].Title = text
			}
			continue
		}
		seen[id] = len(out)
		out = append(out, TPDbTitle{ID: id, Title: text, URL: fmt.Sprintf("%s/posters/%d", t.base, id)})
	}
	return out
}

// parseTPDbPosters returns the posters on a title or set page. Each poster's
// uploader and caption come from the markup between it and the next poster.
func (t *TPDb) parsePosters(page string) []TPDbPoster {
	locs := reTPDbPosterID.FindAllStringSubmatchIndex(page, -1)
	ids := make([]int64, len(locs))
	for i, loc := range locs {
		for g := 1; g < len(loc)/2; g++ {
			if loc[2*g] >= 0 {
				ids[i], _ = strconv.ParseInt(page[loc[2*g]:loc[2*g+1]], 10, 64)
				break
			}
		}
	}
	var out []TPDbPoster
	seen := map[int64]bool{}
	for i, loc := range locs {
		id := ids[i]
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		end := len(page)
		for j := i + 1; j < len(locs); j++ {
			if ids[j] != id {
				end = locs[j][0]
				break
			}
		}
		seg := page[loc[1]:max(loc[1], min(end, loc[1]+6000))]
		p := TPDbPoster{ID: id, Download: t.AssetURL(id)}
		if m := reTPDbUser.FindStringSubmatch(seg); m != nil {
			p.Uploader = m[1]
		}
		if m := reTPDbPosterText.FindStringSubmatch(seg); m != nil {
			p.Title = cleanText(m[1])
		}
		out = append(out, p)
	}
	return out
}

func (t *TPDb) get(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BingeBandit)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en")
	resp, err := t.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach ThePosterDB: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	page := string(body)
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests ||
		strings.Contains(page, "cf-chl") || strings.Contains(page, "Just a moment...") {
		return "", ErrTPDbBlocked
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ThePosterDB answered %s", resp.Status)
	}
	return page, nil
}

// Search finds titles on ThePosterDB and the posters on the best few. kind is
// "movie" or "show"; year (0 if unknown) puts titles from that year first. A
// pasted ThePosterDB link is opened directly instead of searched.
func (t *TPDb) Search(ctx context.Context, kind, query string, year int) ([]TPDbTitle, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("enter a title to search for")
	}
	key := fmt.Sprintf("%s|%d|%s", kind, year, strings.ToLower(query))
	t.mu.Lock()
	if c, ok := t.cache[key]; ok && time.Since(c.at) < time.Hour {
		t.mu.Unlock()
		return c.res, nil
	}
	t.mu.Unlock()

	res, err := t.search(ctx, kind, query, year)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	if len(t.cache) > 200 {
		t.cache = map[string]tpdbCached{}
	}
	t.cache[key] = tpdbCached{time.Now(), res}
	t.mu.Unlock()
	return res, nil
}

func (t *TPDb) search(ctx context.Context, kind, query string, year int) ([]TPDbTitle, error) {
	if m := reTPDbLink.FindStringSubmatch(query); m != nil {
		id, _ := strconv.ParseInt(m[2], 10, 64)
		if m[1] == "poster" || m[1] == "api/assets" {
			return []TPDbTitle{{Title: "Pasted poster", URL: query, Posters: []TPDbPoster{{ID: id, Download: t.AssetURL(id)}}}}, nil
		}
		page, err := t.get(ctx, fmt.Sprintf("%s/%s/%d", t.base, m[1], id))
		if err != nil {
			return nil, err
		}
		title := TPDbTitle{ID: id, Title: pageTitle(page), URL: query, Posters: t.parsePosters(page)}
		if title.Posters == nil {
			title.Posters = []TPDbPoster{}
		}
		return []TPDbTitle{title}, nil
	}

	section := "movies"
	if kind == "show" {
		section = "shows"
	}
	page, err := t.get(ctx, fmt.Sprintf("%s/search?term=%s&section=%s", t.base, url.QueryEscape(query), section))
	if err != nil {
		return nil, err
	}
	titles := t.parseSearch(page)
	if year > 0 {
		// Stable: titles from the right year first, otherwise in ThePosterDB's order.
		var match, rest []TPDbTitle
		for _, ti := range titles {
			if m := reYear.FindStringSubmatch(ti.Title); m != nil && m[1] == strconv.Itoa(year) {
				match = append(match, ti)
			} else {
				rest = append(rest, ti)
			}
		}
		titles = append(match, rest...)
	}
	if len(titles) > 3 {
		titles = titles[:3] // each is another page load; three is plenty to find the right one
	}
	for i := range titles {
		page, err := t.get(ctx, titles[i].URL)
		if err != nil {
			if i == 0 {
				return nil, err
			}
			break
		}
		titles[i].Posters = t.parsePosters(page)
		if titles[i].Title == "" {
			titles[i].Title = pageTitle(page)
		}
	}
	// Empty lists, not null, so the web picker can count them.
	if titles == nil {
		titles = []TPDbTitle{}
	}
	for i := range titles {
		if titles[i].Posters == nil {
			titles[i].Posters = []TPDbPoster{}
		}
	}
	return titles, nil
}

var reHTMLTitle = regexp.MustCompile(`(?is)<title>(.*?)</title>`)

func pageTitle(page string) string {
	if m := reHTMLTitle.FindStringSubmatch(page); m != nil {
		t := cleanText(m[1])
		if i := strings.LastIndex(t, " | "); i > 0 {
			t = t[:i]
		}
		return t
	}
	return ""
}

// ParseTPDbPosterLink turns a pasted ThePosterDB poster link into the image's
// download URL ("" if it isn't one), so "Use URL" works with poster pages too.
func (t *TPDb) PosterLink(raw string) string {
	if m := reTPDbLink.FindStringSubmatch(strings.TrimSpace(raw)); m != nil && m[1] == "poster" {
		id, _ := strconv.ParseInt(m[2], 10, 64)
		return t.AssetURL(id)
	}
	return ""
}

// Thumb returns a small JPEG of a poster for the picker, cached on disk.
func (t *TPDb) Thumb(ctx context.Context, id int64) (string, error) {
	path := filepath.Join(t.thumbDir, fmt.Sprintf("%d.jpg", id))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.AssetURL(id), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BingeBandit)")
	resp, err := t.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poster download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxArtworkBytes))
	if err != nil {
		return "", err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("ThePosterDB didn't send an image")
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, shrink(src, 342), &jpeg.Options{Quality: 82}); err != nil {
		return "", err
	}
	if err := os.MkdirAll(t.thumbDir, 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, buf.Bytes(), 0o644)
}

// shrink scales an image down to the given width by averaging blocks of pixels.
func shrink(src image.Image, width int) image.Image {
	b := src.Bounds()
	if b.Dx() <= width {
		return src
	}
	height := b.Dy() * width / b.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/height, b.Min.Y+(y+1)*b.Dy()/height
		for x := 0; x < width; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/width, b.Min.X+(x+1)*b.Dx()/width
			var r, g, bl, n uint32
			for sy := y0; sy < max(y1, y0+1); sy++ {
				for sx := x0; sx < max(x1, x0+1); sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+cr, g+cg, bl+cb, n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), 255
		}
	}
	return dst
}
