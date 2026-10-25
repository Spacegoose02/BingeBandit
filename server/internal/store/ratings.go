package store

import (
	"database/sql"
	"errors"
)

// ExternalRatings are scores from other sites for a movie or show. Zero
// means unknown.
type ExternalRatings struct {
	IMDbID     string  `json:"imdbId,omitempty"`
	IMDb       float64 `json:"imdb,omitempty"` // out of 10
	IMDbVotes  int     `json:"imdbVotes,omitempty"`
	RT         int     `json:"rottenTomatoes,omitempty"`
	RTAudience int     `json:"rottenTomatoesAudience,omitempty"`
	Metacritic int     `json:"metacritic,omitempty"`
	FetchedAt  int64   `json:"fetchedAt"`
}

// Empty reports whether the lookup found no scores at all.
func (r *ExternalRatings) Empty() bool {
	return r.IMDb == 0 && r.RT == 0 && r.RTAudience == 0 && r.Metacritic == 0
}

// ExternalRatings returns the stored scores for an item, or ErrNotFound
// when it was never looked up.
func (s *Store) ExternalRatings(itemID int64) (*ExternalRatings, error) {
	var r ExternalRatings
	err := s.db.QueryRow(`SELECT imdb_id, imdb, imdb_votes, rt, rt_audience, metacritic, fetched_at
		FROM external_ratings WHERE item_id = ?`, itemID).
		Scan(&r.IMDbID, &r.IMDb, &r.IMDbVotes, &r.RT, &r.RTAudience, &r.Metacritic, &r.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) SaveExternalRatings(itemID int64, r *ExternalRatings) error {
	_, err := s.db.Exec(`INSERT INTO external_ratings (item_id, imdb_id, imdb, imdb_votes, rt, rt_audience, metacritic, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET imdb_id = excluded.imdb_id, imdb = excluded.imdb, imdb_votes = excluded.imdb_votes,
			rt = excluded.rt, rt_audience = excluded.rt_audience, metacritic = excluded.metacritic, fetched_at = excluded.fetched_at`,
		itemID, r.IMDbID, r.IMDb, r.IMDbVotes, r.RT, r.RTAudience, r.Metacritic, r.FetchedAt)
	return err
}

// RatingTarget is a movie or show to look up.
type RatingTarget struct {
	ID     int64
	Kind   string
	TMDBID int64
}

// ItemsNeedingRatings lists matched movies and shows never looked up, then
// those last looked up before the given time, oldest first.
func (s *Store) ItemsNeedingRatings(before int64, limit int) ([]RatingTarget, error) {
	rows, err := s.db.Query(`SELECT i.id, i.kind, i.tmdb_id FROM items i
		LEFT JOIN external_ratings r ON r.item_id = i.id
		WHERE i.kind IN ('movie', 'show') AND i.tmdb_id > 0 AND (r.item_id IS NULL OR r.fetched_at < ?)
		ORDER BY COALESCE(r.fetched_at, 0), i.id LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RatingTarget
	for rows.Next() {
		var t RatingTarget
		if err := rows.Scan(&t.ID, &t.Kind, &t.TMDBID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
