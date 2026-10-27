package livetv

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// Guide data kept around "now": a little history for the guide's left edge
// and a week ahead.
const (
	guideBehind = 12 * time.Hour
	guideAhead  = 8 * 24 * time.Hour
)

var fetchClient = &http.Client{Timeout: 5 * time.Minute, Transport: providerTransport()}

// open reads a source from an http(s) URL or a local file, transparently
// un-gzipping (many XMLTV guides are .xml.gz).
func open(ctx context.Context, url, userAgent string) (io.ReadCloser, error) {
	var rc io.ReadCloser
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
		resp, err := fetchClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("download failed: %s", resp.Status)
		}
		rc = resp.Body
	} else {
		f, err := os.Open(url)
		if err != nil {
			return nil, err
		}
		rc = f
	}
	br := bufio.NewReaderSize(rc, 64*1024)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			rc.Close()
			return nil, err
		}
		return struct {
			io.Reader
			io.Closer
		}{gz, rc}, nil
	}
	return struct {
		io.Reader
		io.Closer
	}{br, rc}, nil
}

// spool downloads a source to a temporary file before it's imported, so the
// import's transaction (which holds the database's write lock) lasts as long
// as the parsing, not as long as a slow provider takes to send a big
// playlist. Saving watch progress and the like waits on that lock.
func spool(ctx context.Context, url, userAgent string) (io.ReadCloser, error) {
	rc, err := open(ctx, url, userAgent)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	f, err := os.CreateTemp("", "bb-livetv-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { f.Close(); os.Remove(f.Name()) }
	if _, err := io.Copy(f, rc); err != nil {
		cleanup()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{bufio.NewReaderSize(f, 64*1024), closerFunc(cleanup)}, nil
}

type closerFunc func()

func (c closerFunc) Close() error { c(); return nil }

// Refresh downloads and imports one source.
func Refresh(ctx context.Context, st *store.Store, src *store.LiveSource) error {
	count, err := refresh(ctx, st, src)
	if serr := st.SetSourceRefreshed(src.ID, count, err); serr != nil && err == nil {
		err = serr
	}
	if err != nil {
		log.Printf("live tv: refresh %q failed: %v", src.Name, err)
	} else {
		log.Printf("live tv: refreshed %q: %d entries", src.Name, count)
	}
	return err
}

func refresh(ctx context.Context, st *store.Store, src *store.LiveSource) (int, error) {
	rc, err := spool(ctx, src.URL, src.UserAgent)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	switch src.Kind {
	case "m3u":
		im, err := st.BeginStreamImport(src.ID)
		if err != nil {
			return 0, err
		}
		err = ParseM3U(rc, func(e Entry) error {
			return im.Add(store.StreamEntry{Name: e.Name, URL: e.URL, Logo: e.Logo, Group: e.Group, TvgID: e.TvgID, TvgChno: e.TvgChno})
		})
		if err != nil {
			im.Abort()
			return 0, err
		}
		if im.Total() == 0 {
			im.Abort()
			return 0, fmt.Errorf("the playlist has no streams")
		}
		return im.Finish()
	case "epg":
		gi, err := st.BeginGuideImport(src.ID)
		if err != nil {
			return 0, err
		}
		now := time.Now()
		err = ParseXMLTV(rc, now.Add(-guideBehind), now.Add(guideAhead),
			func(c GuideChannel) error { return gi.Channel(c.ID, c.DisplayName, c.Icon) },
			func(p Program) error {
				return gi.Program(&store.GuideProgram{EPGID: p.ChannelID, Start: p.Start.Unix(), Stop: p.Stop.Unix(),
					Title: p.Title, Subtitle: p.Subtitle, Description: p.Description, Category: p.Category, Icon: p.Icon, Episode: p.Episode,
					Year: p.Year, Rating: p.Rating, Stars: p.Stars, Genres: p.Genres})
			})
		if err != nil {
			gi.Abort()
			return 0, err
		}
		return gi.Finish()
	}
	return 0, fmt.Errorf("unknown source kind %q", src.Kind)
}

// RefreshDue refreshes every enabled source whose refresh interval has passed
// and drops guide data that has aired. Used by the scheduled task.
func RefreshDue(ctx context.Context, st *store.Store) (string, error) {
	sources, err := st.LiveSources("")
	if err != nil {
		return "", err
	}
	done, failed := 0, 0
	for _, src := range sources {
		hours := max(src.RefreshHours, 1)
		if !src.Enabled || time.Since(time.Unix(src.LastRefresh, 0)) < time.Duration(hours)*time.Hour {
			continue
		}
		if Refresh(ctx, st, src) != nil {
			failed++
		} else {
			done++
		}
	}
	if err := st.PruneProgramsBefore(time.Now().Add(-guideBehind).Unix()); err != nil {
		return "", err
	}
	if failed > 0 {
		return "", fmt.Errorf("%d sources failed to refresh (see Logs)", failed)
	}
	return fmt.Sprintf("refreshed %d sources", done), nil
}
