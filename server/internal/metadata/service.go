package metadata

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"

	"bingebandit/internal/store"
)

type Service struct {
	st *store.Store

	tvMu  sync.Mutex
	tv    *TVDB // kept so its login token is reused
	tvKey string
}

func NewService(st *store.Store) *Service { return &Service{st: st} }

// client returns nil when no TMDB key is configured; metadata is then skipped.
func (s *Service) client() *TMDB {
	key := s.st.Setting(store.SettingTMDBKey)
	if strings.TrimSpace(key) == "" {
		return nil
	}
	return NewTMDB(key, s.st.Setting(store.SettingMetadataLanguage))
}

func (s *Service) Enabled() bool { return s.client() != nil }

// tvdb returns nil when no TheTVDB key is configured.
func (s *Service) tvdb() *TVDB {
	key, pin := strings.TrimSpace(s.st.Setting(store.SettingTVDBKey)), s.st.Setting(store.SettingTVDBPin)
	if key == "" {
		return nil
	}
	lang := s.st.Setting(store.SettingMetadataLanguage)
	s.tvMu.Lock()
	defer s.tvMu.Unlock()
	if id := key + "\x00" + pin + "\x00" + lang; s.tv == nil || s.tvKey != id {
		s.tv, s.tvKey = NewTVDB(key, pin, lang), id
	}
	return s.tv
}

// TVDBEnabled reports whether a TheTVDB key is set.
func (s *Service) TVDBEnabled() bool { return s.tvdb() != nil }

// AnyEnabled reports whether either provider can be used.
func (s *Service) AnyEnabled() bool { return s.Enabled() || s.TVDBEnabled() }

// RefreshLibrary fetches metadata for new items, or every item when force is set.
// progress is called after each top-level item.
func (s *Service) RefreshLibrary(ctx context.Context, libraryID int64, force bool, progress func(done, total int)) error {
	c, tv := s.client(), s.tvdb()
	if c == nil && tv == nil {
		return nil
	}
	items, err := s.st.ItemsNeedingMetadata(0, libraryID, force)
	if err != nil {
		return err
	}
	for i, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.refresh(ctx, c, tv, it); err != nil {
			log.Printf("metadata: %s %q: %v", it.Kind, it.Title, err)
		}
		if progress != nil {
			progress(i+1, len(items))
		}
	}
	return nil
}

// RefreshItem re-fetches one movie or show (seasons and episodes resolve to their show).
func (s *Service) RefreshItem(ctx context.Context, id int64) error {
	c, tv := s.client(), s.tvdb()
	if c == nil && tv == nil {
		return nil
	}
	it, err := s.st.Item(0, id)
	if err != nil {
		return err
	}
	if it.ShowID != 0 {
		if it, err = s.st.Item(0, it.ShowID); err != nil {
			return err
		}
	}
	return s.refresh(ctx, c, tv, it)
}

var (
	ErrNoKey     = errors.New("add a TMDB API key in Settings first")
	ErrNoTVDBKey = errors.New("add a TheTVDB API key in Settings first")
)

// Search finds Fix Match candidates. kind is "movie" or "show".
func (s *Service) Search(ctx context.Context, kind, query string, year int) ([]SearchResult, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	res, err := c.Search(ctx, kind, query, year)
	for i := range res {
		res[i].Poster = store.ImageURL(res[i].Poster)
	}
	return res, err
}

// SearchTVDB finds TheTVDB candidates for a show.
func (s *Service) SearchTVDB(ctx context.Context, query string, year int) ([]SearchResult, error) {
	tv := s.tvdb()
	if tv == nil {
		return nil, ErrNoTVDBKey
	}
	return tv.Search(ctx, query, year)
}

// MatchTVDB switches a show to TheTVDB with the given episode order and refreshes it.
func (s *Service) MatchTVDB(ctx context.Context, id, tvdbID int64, order string) error {
	if s.tvdb() == nil {
		return ErrNoTVDBKey
	}
	if err := s.st.SetTVDBMatch(id, tvdbID, order); err != nil {
		return err
	}
	return s.RefreshItem(ctx, id)
}

// Match points an item (movie or show) at a TMDB id and refreshes it. A
// show that used TheTVDB goes back to TMDB.
func (s *Service) Match(ctx context.Context, id, tmdbID int64) error {
	if s.client() == nil {
		return ErrNoKey
	}
	if err := s.st.SetTVDBMatch(id, 0, ""); err != nil {
		return err
	}
	if err := s.st.SetMatch(id, tmdbID); err != nil {
		return err
	}
	return s.RefreshItem(ctx, id)
}

// ArtworkOptions lists TMDB images of imageType for an item. Episodes have
// no alternatives on TMDB, so they return an empty list.
func (s *Service) ArtworkOptions(ctx context.Context, it *store.Item, imageType string) ([]ImageOption, error) {
	c := s.client()
	if c == nil {
		return nil, ErrNoKey
	}
	switch it.Kind {
	case "movie", "show":
		if it.TMDBID == 0 {
			return []ImageOption{}, nil
		}
		return c.Images(ctx, it.Kind, it.TMDBID, 0, imageType)
	case "season":
		show, err := s.st.Item(0, it.ShowID)
		if err != nil || show.TMDBID == 0 || it.SeasonNumber == nil || imageType != "poster" {
			return []ImageOption{}, err
		}
		return c.Images(ctx, "season", show.TMDBID, *it.SeasonNumber, imageType)
	}
	return []ImageOption{}, nil
}

// refresh updates a movie or show. c or tv is nil when its key isn't set.
//
// A title is matched by the first of: its TMDB id (from Fix Match, an
// earlier match or a {tmdb-…} folder hint); a TheTVDB or IMDb id from a
// folder hint, looked up on TMDB; a TMDB search by title and year; and for
// shows, TheTVDB (by hint or search) when TMDB doesn't know it. Titles none
// of them know, and those left unmatched on purpose, keep their folder name.
func (s *Service) refresh(ctx context.Context, c *TMDB, tv *TVDB, it *store.Item) error {
	if it.MetadataSource == "none" {
		s.markTree(it.ID)
		return nil
	}
	if it.Kind == "show" {
		if tvdbID, order, err := s.st.ShowTVDB(it.ID); err == nil && tvdbID > 0 && tv != nil {
			return s.refreshTVDBShow(ctx, c, tv, it, tvdbID, order)
		}
	}
	switch it.Kind {
	case "movie":
		if c == nil {
			return nil
		}
		id, err := s.findTMDB(ctx, c, it)
		if err != nil {
			return err
		}
		if id == 0 {
			log.Printf("metadata: no match for movie %q (%d)", it.Title, it.Year)
			return s.st.MarkMetadataChecked(it.ID)
		}
		return s.refreshMovie(ctx, c, it, id)
	case "show":
		if c != nil {
			id, err := s.findTMDB(ctx, c, it)
			if err != nil {
				return err
			}
			if id > 0 {
				return s.refreshShow(ctx, c, it, id)
			}
		}
		if tv != nil {
			tvdbID := it.TVDBID
			if tvdbID == 0 {
				res, err := tv.Search(ctx, it.Title, it.Year)
				if err != nil {
					return err
				}
				if len(res) > 0 {
					tvdbID = res[0].TVDBID
				}
			}
			if tvdbID > 0 {
				// From now on the show's details come from TheTVDB, in aired order.
				if err := s.st.SetTVDBMatch(it.ID, tvdbID, ""); err != nil {
					return err
				}
				return s.refreshTVDBShow(ctx, c, tv, it, tvdbID, "")
			}
		}
		log.Printf("metadata: no match for show %q", it.Title)
		s.markTree(it.ID)
	}
	return nil
}

// findTMDB returns the TMDB id of a movie or show: the one it has, else one
// found by its TheTVDB or IMDb id, else the top search hit. 0 means TMDB
// doesn't know it.
func (s *Service) findTMDB(ctx context.Context, c *TMDB, it *store.Item) (int64, error) {
	if it.TMDBID > 0 {
		return it.TMDBID, nil
	}
	type ext struct{ id, source string }
	var by []ext
	if it.IMDbID != "" {
		by = append(by, ext{it.IMDbID, "imdb_id"})
	}
	if it.Kind == "show" && it.TVDBID > 0 {
		by = append(by, ext{strconv.FormatInt(it.TVDBID, 10), "tvdb_id"})
	}
	for _, e := range by {
		movie, show, err := c.Find(ctx, e.id, e.source)
		if err != nil {
			return 0, err
		}
		if it.Kind == "movie" && movie > 0 {
			return movie, nil
		}
		if it.Kind == "show" && show > 0 {
			return show, nil
		}
	}
	if it.Kind == "movie" {
		return c.SearchMovie(ctx, it.Title, it.Year)
	}
	return c.SearchShow(ctx, it.Title, it.Year)
}

func img(path string) string {
	if path == "" {
		return ""
	}
	return "tmdb:" + path
}

func (s *Service) refreshMovie(ctx context.Context, c *TMDB, it *store.Item, id int64) error {
	m, err := c.Movie(ctx, id)
	if err != nil {
		return err
	}
	u := &store.Item{
		Title:         m.Title,
		OriginalTitle: m.OriginalTitle,
		Year:          yearOf(m.ReleaseDate, it.Year),
		Overview:      m.Overview,
		Tagline:       m.Tagline,
		Poster:        img(m.PosterPath),
		Backdrop:      img(m.BackdropPath),
		Logo:          pickLogo(m.Images, c.imageLang()),
		Rating:        m.VoteAverage,
		Genres:        names(m.Genres),
		Runtime:       m.Runtime,
		ReleaseDate:   m.ReleaseDate,
		TMDBID:        m.ID,
		IMDbID:        m.IMDbID,
		Trailer:       pickTrailer(m.Videos),
		Credits:       credits(m.Credits),
	}
	if len(m.ProductionCompanies) > 0 {
		u.Studio = m.ProductionCompanies[0].Name
	}
	if bc := m.BelongsToCollection; bc != nil {
		u.CollectionID, u.CollectionName = bc.ID, bc.Name
		if err := s.st.LinkTMDBCollection(it.ID, it.LibraryID, bc.ID, bc.Name, img(bc.PosterPath), img(bc.BackdropPath)); err != nil {
			log.Printf("metadata: collection %q: %v", bc.Name, err)
		}
	}
	for _, r := range m.ReleaseDates.Results {
		if r.Country == "US" {
			for _, d := range r.ReleaseDates {
				if d.Certification != "" {
					u.ContentRating = d.Certification
					break
				}
			}
		}
	}
	return s.st.UpdateMetadata(it.ID, u)
}

func (s *Service) refreshShow(ctx context.Context, c *TMDB, it *store.Item, id int64) error {
	sh, err := c.Show(ctx, id)
	if err != nil {
		return err
	}
	u := &store.Item{
		Title:         sh.Name,
		OriginalTitle: sh.OriginalName,
		Year:          yearOf(sh.FirstAirDate, it.Year),
		Overview:      sh.Overview,
		Tagline:       sh.Tagline,
		Poster:        img(sh.PosterPath),
		Backdrop:      img(sh.BackdropPath),
		Logo:          pickLogo(sh.Images, c.imageLang()),
		Rating:        sh.VoteAverage,
		Genres:        names(sh.Genres),
		ReleaseDate:   sh.FirstAirDate,
		TMDBID:        sh.ID,
		TVDBID:        sh.ExternalIDs.TVDBID,
		IMDbID:        sh.ExternalIDs.IMDbID,
		Trailer:       pickTrailer(sh.Videos),
		Credits:       credits(sh.Credits),
	}
	if len(sh.EpisodeRunTime) > 0 {
		u.Runtime = sh.EpisodeRunTime[0]
	}
	if len(sh.Networks) > 0 {
		u.Studio = sh.Networks[0].Name
	}
	for _, r := range sh.ContentRatings.Results {
		if r.Country == "US" {
			u.ContentRating = r.Rating
		}
	}
	if err := s.st.UpdateMetadata(it.ID, u); err != nil {
		return err
	}

	seasons, err := s.st.Children(0, it.ID)
	if err != nil {
		return err
	}
	for _, season := range seasons {
		if season.SeasonNumber == nil {
			continue
		}
		n := *season.SeasonNumber
		tv, err := c.Season(ctx, sh.ID, n)
		if err != nil {
			log.Printf("metadata: %q season %d: %v", sh.Name, n, err)
			s.markTree(season.ID)
			continue
		}
		title := tv.Name
		if title == "" {
			title = season.Title
		}
		if err := s.st.UpdateMetadata(season.ID, &store.Item{
			Title: title, Overview: tv.Overview, Poster: img(tv.PosterPath), ReleaseDate: tv.AirDate,
			Year: yearOf(tv.AirDate, 0), TMDBID: sh.ID,
		}); err != nil {
			return err
		}
		episodes, err := s.st.Children(0, season.ID)
		if err != nil {
			return err
		}
		for _, ep := range episodes {
			matched := false
			for _, te := range tv.Episodes {
				if ep.EpisodeNumber != nil && te.EpisodeNumber == *ep.EpisodeNumber {
					matched = true
					if err := s.st.UpdateMetadata(ep.ID, &store.Item{
						Title: te.Name, Overview: te.Overview, Backdrop: img(te.StillPath), ReleaseDate: te.AirDate,
						Year: yearOf(te.AirDate, 0), Rating: te.VoteAverage, Runtime: te.Runtime, TMDBID: sh.ID,
					}); err != nil {
						return err
					}
					break
				}
			}
			if !matched {
				s.st.MarkMetadataChecked(ep.ID)
			}
		}
	}
	return nil
}

// markTree stamps a show or season and everything below it as checked.
func (s *Service) markTree(id int64) {
	s.st.MarkMetadataChecked(id)
	children, _ := s.st.Children(0, id)
	for _, c := range children {
		s.markTree(c.ID)
	}
}

func yearOf(date string, fallback int) int {
	if len(date) >= 4 {
		y := 0
		for _, r := range date[:4] {
			if r < '0' || r > '9' {
				return fallback
			}
			y = y*10 + int(r-'0')
		}
		return y
	}
	return fallback
}

func names(ns []named) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}

func pickLogo(imgs tmdbImages, lang string) string {
	for _, l := range imgs.Logos {
		if l.Lang == lang {
			return img(l.FilePath)
		}
	}
	if len(imgs.Logos) > 0 {
		return img(imgs.Logos[0].FilePath)
	}
	return ""
}

func pickTrailer(v tmdbVideos) string {
	best := ""
	for _, r := range v.Results {
		if r.Site != "YouTube" || r.Type != "Trailer" {
			continue
		}
		if r.Official {
			return r.Key
		}
		if best == "" {
			best = r.Key
		}
	}
	return best
}

var crewJobs = map[string]bool{"Director": true, "Screenplay": true, "Writer": true, "Creator": true}

func credits(c tmdbCredits) []store.Credit {
	out := []store.Credit{}
	for i, p := range c.Cast {
		if i >= 20 {
			break
		}
		out = append(out, store.Credit{Name: p.Name, Role: p.Character, Kind: "cast", Photo: img(p.ProfilePath)})
	}
	seen := map[string]bool{}
	for _, p := range c.Crew {
		if !crewJobs[p.Job] || seen[p.Name+p.Job] || len(seen) >= 8 {
			continue
		}
		seen[p.Name+p.Job] = true
		out = append(out, store.Credit{Name: p.Name, Role: p.Job, Kind: "crew", Photo: img(p.ProfilePath)})
	}
	return out
}
