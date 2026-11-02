package metadata

import (
	"context"
	"encoding/xml"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bingebandit/internal/store"
)

// YouTube channels downloaded by Cinemask (or anything else that writes
// Jellyfin-style .nfo files) are shows whose details come from the files
// next to the videos, not from TMDB:
//
//	Channel/tvshow.nfo                    the channel's name and description
//	Channel/poster.jpg                    its avatar
//	Channel/Season 2024/Channel - 2024-05-03 - Title [videoId].mkv
//	Channel/Season 2024/… [videoId].nfo   the video's title, description, date, length
//	Channel/Season 2024/… [videoId].jpg   its thumbnail
//
// Seasons are upload years. Episodes are numbered 1, 2, 3… by upload date
// within their year, oldest first.

type nfoUniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type showNFO struct {
	XMLName   xml.Name      `xml:"tvshow"`
	Title     string        `xml:"title"`
	Plot      string        `xml:"plot"`
	Studio    string        `xml:"studio"`
	UniqueIDs []nfoUniqueID `xml:"uniqueid"`
}

type episodeNFO struct {
	XMLName xml.Name `xml:"episodedetails"`
	Title   string   `xml:"title"`
	Plot    string   `xml:"plot"`
	Aired   string   `xml:"aired"`
	Runtime int      `xml:"runtime"` // minutes
}

func readNFO(path string, v any) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return xml.NewDecoder(io.LimitReader(f, 1<<20)).Decode(v) == nil
}

func (n *showNFO) youTube() bool {
	for _, u := range n.UniqueIDs {
		if strings.EqualFold(u.Type, "youtube") && strings.TrimSpace(u.Value) != "" {
			return true
		}
	}
	return false
}

// IsYouTubeShowDir says whether a show folder is a YouTube channel: its
// tvshow.nfo carries a YouTube channel id, as Cinemask writes it.
func IsYouTubeShowDir(dir string) bool {
	var n showNFO
	return readNFO(filepath.Join(dir, "tvshow.nfo"), &n) && n.youTube()
}

var (
	// ReYouTubeID finds the video id yt-dlp puts at the end of a file name.
	ReYouTubeID = regexp.MustCompile(`\[([A-Za-z0-9_-]{11})\]$`)
	reYMD       = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})-(\d{2})-(\d{2})(?:\D|$)`)
)

// YouTubeDate is the upload date in a file name ("… - 2024-05-03 - …").
func YouTubeDate(name string) string {
	if m := reYMD.FindStringSubmatch(name); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3]
	}
	return ""
}

func stem(path string) string { return strings.TrimSuffix(path, filepath.Ext(path)) }

// localImage copies the first of paths that exists into the artwork folder.
func (s *Service) localImage(paths ...string) string {
	if s.Artwork == nil {
		return ""
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxArtworkBytes {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		ref, err := s.Artwork.Save(f)
		f.Close()
		if err == nil {
			return ref
		}
	}
	return ""
}

type ytEpisode struct {
	it    *store.Item
	nfo   episodeNFO
	path  string
	aired string
}

// refreshYouTube reads a YouTube channel's details from its folder.
func (s *Service) refreshYouTube(ctx context.Context, show *store.Item) error {
	seasons, err := s.st.Children(0, show.ID)
	if err != nil {
		return err
	}
	showDir := ""
	newestThumb, newestAired, firstAired := "", "", ""
	for _, season := range seasons {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		episodes, err := s.st.Children(0, season.ID)
		if err != nil {
			return err
		}
		eps := make([]*ytEpisode, 0, len(episodes))
		for _, it := range episodes {
			e := &ytEpisode{it: it}
			if files, err := s.st.FilesForItem(it.ID); err == nil && len(files) > 0 {
				e.path = files[0].Path
				readNFO(stem(e.path)+".nfo", &e.nfo)
				if showDir == "" {
					showDir = filepath.Dir(filepath.Dir(e.path))
				}
			}
			e.aired = strings.TrimSpace(e.nfo.Aired)
			if len(e.aired) < 10 {
				e.aired = YouTubeDate(filepath.Base(e.path))
			} else {
				e.aired = e.aired[:10]
			}
			eps = append(eps, e)
		}
		// Oldest first; videos from one day keep a steady order.
		sort.SliceStable(eps, func(i, j int) bool {
			if eps[i].aired != eps[j].aired {
				return eps[i].aired < eps[j].aired
			}
			return filepath.Base(eps[i].path) < filepath.Base(eps[j].path)
		})
		for i, e := range eps {
			if e.it.EpisodeNumber == nil || *e.it.EpisodeNumber != i+1 {
				if err := s.st.SetEpisodeNumber(e.it.ID, i+1); err != nil {
					return err
				}
			}
			title := strings.TrimSpace(e.nfo.Title)
			if title == "" {
				title = e.it.Title
			}
			thumb := ""
			if e.path != "" {
				st := stem(e.path)
				thumb = s.localImage(st+".jpg", st+".webp", st+".png", st+"-thumb.jpg")
			}
			// The newest video's thumbnail is the channel's background.
			if thumb != "" && e.aired >= newestAired {
				newestThumb, newestAired = thumb, e.aired
			}
			if err := s.st.UpdateMetadata(e.it.ID, &store.Item{
				Title: title, Overview: strings.TrimSpace(e.nfo.Plot), Backdrop: thumb, ReleaseDate: e.aired,
				Year: yearOf(e.aired, 0), Runtime: e.nfo.Runtime,
			}); err != nil {
				return err
			}
			if e.aired != "" && (firstAired == "" || e.aired < firstAired) {
				firstAired = e.aired
			}
		}
		year := 0
		if season.SeasonNumber != nil {
			year = *season.SeasonNumber
		}
		seasonAired := ""
		if len(eps) > 0 {
			seasonAired = eps[0].aired
		}
		if err := s.st.UpdateMetadata(season.ID, &store.Item{
			Title: season.Title, ReleaseDate: seasonAired, Year: year,
		}); err != nil {
			return err
		}
	}

	var n showNFO
	u := &store.Item{Title: show.Title, Studio: "YouTube", Genres: []string{"YouTube"}, ReleaseDate: firstAired, Year: yearOf(firstAired, show.Year)}
	if showDir != "" {
		if readNFO(filepath.Join(showDir, "tvshow.nfo"), &n) {
			if t := strings.TrimSpace(n.Title); t != "" {
				u.Title = t
			}
			u.Overview = strings.TrimSpace(n.Plot)
		}
		u.Poster = s.localImage(filepath.Join(showDir, "poster.jpg"), filepath.Join(showDir, "folder.jpg"), filepath.Join(showDir, "poster.png"))
		u.Backdrop = s.localImage(filepath.Join(showDir, "fanart.jpg"), filepath.Join(showDir, "backdrop.jpg"))
	}
	if u.Backdrop == "" {
		u.Backdrop = newestThumb
	}
	if err := s.st.UpdateMetadata(show.ID, u); err != nil {
		return err
	}
	if showDir == "" {
		log.Printf("metadata: YouTube channel %q has no videos to read", show.Title)
	}
	return nil
}
