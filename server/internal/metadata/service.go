package metadata

import (
	"context"
	"log"
	"strings"

	"bingebandit/internal/store"
)

type Service struct {
	st *store.Store
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

// RefreshLibrary fetches metadata for new items, or every item when force is set.
// progress is called after each top-level item.
func (s *Service) RefreshLibrary(ctx context.Context, libraryID int64, force bool, progress func(done, total int)) error {
	c := s.client()
	if c == nil {
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
		if err := s.refresh(ctx, c, it); err != nil {
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
	c := s.client()
	if c == nil {
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
	return s.refresh(ctx, c, it)
}

func (s *Service) refresh(ctx context.Context, c *TMDB, it *store.Item) error {
	switch it.Kind {
	case "movie":
		return s.refreshMovie(ctx, c, it)
	case "show":
		return s.refreshShow(ctx, c, it)
	}
	return nil
}

func img(path string) string {
	if path == "" {
		return ""
	}
	return "tmdb:" + path
}

func (s *Service) refreshMovie(ctx context.Context, c *TMDB, it *store.Item) error {
	id := it.TMDBID
	if id == 0 {
		var err error
		if id, err = c.SearchMovie(ctx, it.Title, it.Year); err != nil {
			return err
		}
		if id == 0 {
			log.Printf("metadata: no TMDB match for movie %q (%d)", it.Title, it.Year)
			return s.st.MarkMetadataChecked(it.ID)
		}
	}
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
		Trailer:       pickTrailer(m.Videos),
		Credits:       credits(m.Credits),
	}
	if len(m.ProductionCompanies) > 0 {
		u.Studio = m.ProductionCompanies[0].Name
	}
	if m.BelongsToCollection != nil {
		u.CollectionID, u.CollectionName = m.BelongsToCollection.ID, m.BelongsToCollection.Name
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

func (s *Service) refreshShow(ctx context.Context, c *TMDB, it *store.Item) error {
	id := it.TMDBID
	if id == 0 {
		var err error
		if id, err = c.SearchShow(ctx, it.Title, it.Year); err != nil {
			return err
		}
		if id == 0 {
			log.Printf("metadata: no TMDB match for show %q", it.Title)
			s.markTree(it.ID)
			return nil
		}
	}
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
