package metadata

import (
	"context"
	"log"
	"sort"
	"strconv"

	"bingebandit/internal/store"
)

// refreshTVDBShow fills a show, its seasons and episodes from TheTVDB, in the
// show's episode order. When TMDB is set up and knows the show, it still adds
// what TheTVDB lacks: the logo, trailer, rating and tagline (and cast or a
// backdrop if TheTVDB has none). c may be nil.
func (s *Service) refreshTVDBShow(ctx context.Context, c *TMDB, tv *TVDB, it *store.Item, tvdbID int64, order string) error {
	sh, err := tv.Series(ctx, tvdbID)
	if err != nil {
		return err
	}
	eps, err := tv.Episodes(ctx, tvdbID, order)
	if err != nil {
		return err
	}

	u := &store.Item{
		Title:         sh.Name,
		OriginalTitle: sh.OriginalName,
		Year:          yearOf(sh.FirstAired, it.Year),
		Overview:      sh.Overview,
		Poster:        tv.bestArtwork(sh.Artworks, tvdbPoster),
		Backdrop:      tv.bestArtwork(sh.Artworks, tvdbBackground),
		Logo:          tv.bestArtwork(sh.Artworks, tvdbClearLogo),
		ReleaseDate:   sh.FirstAired,
		Runtime:       sh.AverageRun,
		TMDBID:        it.TMDBID,
	}
	if u.Poster == "" {
		u.Poster = sh.Image
	}
	for _, g := range sh.Genres {
		u.Genres = append(u.Genres, g.Name)
	}
	if sh.OriginalNetwork != nil {
		u.Studio = sh.OriginalNetwork.Name
	} else if sh.LatestNetwork != nil {
		u.Studio = sh.LatestNetwork.Name
	}
	for _, r := range sh.ContentRatings {
		if r.Country == "usa" || r.Country == "us" {
			u.ContentRating = r.Name
		}
	}
	for _, r := range sh.RemoteIDs {
		if r.SourceName == "TheMovieDB.com" && u.TMDBID == 0 {
			u.TMDBID, _ = strconv.ParseInt(r.ID, 10, 64)
		}
	}
	chars := sh.Characters
	sort.SliceStable(chars, func(i, j int) bool { return chars[i].Sort < chars[j].Sort })
	for _, ch := range chars {
		if ch.PeopleType != "Actor" || ch.PersonName == "" || len(u.Credits) >= 40 {
			continue
		}
		u.Credits = append(u.Credits, store.Credit{Name: ch.PersonName, Role: ch.Name, Kind: "cast", Photo: ch.PersonImgURL})
	}

	if c != nil && u.TMDBID > 0 {
		if t, err := c.Show(ctx, u.TMDBID); err == nil {
			u.Tagline = t.Tagline
			u.Rating = t.VoteAverage
			u.Trailer = pickTrailer(t.Videos)
			if u.Logo == "" {
				u.Logo = pickLogo(t.Images, c.imageLang())
			}
			if u.Backdrop == "" {
				u.Backdrop = img(t.BackdropPath)
			}
			if len(u.Credits) == 0 {
				u.Credits = credits(t.Credits)
			}
		} else {
			log.Printf("metadata: TMDB extras for %q: %v", sh.Name, err)
		}
	}
	if err := s.st.UpdateMetadata(it.ID, u); err != nil {
		return err
	}

	seasonType := order
	if seasonType == "" {
		seasonType = "official"
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
		su := &store.Item{Title: season.Title, TMDBID: u.TMDBID}
		for _, ts := range sh.Seasons {
			if ts.Number == n && ts.Type.Type == seasonType {
				su.Poster = ts.Image
				if ts.Name != "" {
					su.Title = ts.Name
				}
			}
		}
		episodes, err := s.st.Children(0, season.ID)
		if err != nil {
			return err
		}
		for _, ep := range episodes {
			var te *tvdbEpisode
			for i := range eps {
				if ep.EpisodeNumber != nil && eps[i].SeasonNumber == n && eps[i].Number == *ep.EpisodeNumber {
					te = &eps[i]
					break
				}
			}
			if te == nil {
				s.st.MarkMetadataChecked(ep.ID)
				continue
			}
			if su.ReleaseDate == "" || (te.Aired != "" && te.Aired < su.ReleaseDate) {
				su.ReleaseDate = te.Aired
			}
			title := te.Name
			if title == "" {
				title = ep.Title
			}
			if err := s.st.UpdateMetadata(ep.ID, &store.Item{
				Title: title, Overview: te.Overview, Backdrop: te.Image, ReleaseDate: te.Aired,
				Year: yearOf(te.Aired, 0), Runtime: te.Runtime, TMDBID: u.TMDBID,
			}); err != nil {
				return err
			}
		}
		su.Year = yearOf(su.ReleaseDate, 0)
		if err := s.st.UpdateMetadata(season.ID, su); err != nil {
			return err
		}
	}
	return nil
}
