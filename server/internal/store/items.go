package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type WatchState struct {
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	Watched   bool    `json:"watched"`
	UpdatedAt int64   `json:"updatedAt"`
}

type Credit struct {
	Name  string `json:"name"`
	Role  string `json:"role"` // character for cast, job for crew
	Kind  string `json:"kind"` // cast | crew
	Photo string `json:"photo,omitempty"`
}

// Item is a movie, show, season or episode. User-specific fields (Watch,
// UnwatchedCount) are filled for whichever user the query ran as.
type Item struct {
	ID             int64      `json:"id"`
	LibraryID      int64      `json:"libraryId"`
	Kind           string     `json:"kind"`
	ParentID       int64      `json:"parentId,omitempty"`
	ShowID         int64      `json:"showId,omitempty"`
	Title          string     `json:"title"`
	SortTitle      string     `json:"sortTitle"`
	OriginalTitle  string     `json:"originalTitle,omitempty"`
	Year           int        `json:"year,omitempty"`
	Overview       string     `json:"overview,omitempty"`
	Tagline        string     `json:"tagline,omitempty"`
	Poster         string     `json:"poster,omitempty"`
	Backdrop       string     `json:"backdrop,omitempty"`
	Logo           string     `json:"logo,omitempty"`
	Rating         float64    `json:"rating,omitempty"`
	ContentRating  string     `json:"contentRating,omitempty"`
	Genres         []string   `json:"genres,omitempty"`
	Studio         string     `json:"studio,omitempty"`
	Runtime        int        `json:"runtime,omitempty"` // minutes
	ReleaseDate    string     `json:"releaseDate,omitempty"`
	TMDBID         int64      `json:"tmdbId,omitempty"`
	CollectionID   int64      `json:"collectionId,omitempty"`
	CollectionName string     `json:"collectionName,omitempty"`
	Trailer        string     `json:"trailer,omitempty"` // YouTube video key
	Credits        []Credit   `json:"credits,omitempty"`
	SeasonNumber   *int       `json:"seasonNumber,omitempty"`
	EpisodeNumber  *int       `json:"episodeNumber,omitempty"`
	AddedAt        int64      `json:"addedAt"`
	Watch          WatchState `json:"watch"`
	ChildCount     int        `json:"childCount,omitempty"`
	UnwatchedCount int        `json:"unwatchedCount,omitempty"`
	ShowTitle      string     `json:"showTitle,omitempty"`
	ShowPoster     string     `json:"showPoster,omitempty"`
	ShowBackdrop   string     `json:"showBackdrop,omitempty"`

	MatchKey   string `json:"-"`
	MetadataAt int64  `json:"-"`
}

// Every item query binds @uid so watch state and unwatched counts are per user.
const itemSelect = `SELECT i.id, i.library_id, i.kind, i.parent_id, i.show_id, i.title, i.sort_title, i.original_title, i.year,
	i.overview, i.tagline, i.poster, i.backdrop, i.logo, i.rating, i.content_rating, i.genres, i.studio, i.runtime,
	i.release_date, i.tmdb_id, i.collection_id, i.collection_name, i.trailer, i.credits, i.season_number, i.episode_number,
	i.added_at, i.match_key, i.metadata_at,
	COALESCE(w.position, 0.0), COALESCE(w.duration, 0.0), COALESCE(w.watched, 0), COALESCE(w.updated_at, 0),
	CASE i.kind
		WHEN 'show' THEN (SELECT COUNT(*) FROM items e WHERE e.show_id = i.id AND e.kind = 'episode')
		WHEN 'season' THEN (SELECT COUNT(*) FROM items e WHERE e.parent_id = i.id)
		ELSE 0 END,
	CASE i.kind
		WHEN 'show' THEN (SELECT COUNT(*) FROM items e WHERE e.show_id = i.id AND e.kind = 'episode'
			AND NOT EXISTS (SELECT 1 FROM watch_state x WHERE x.item_id = e.id AND x.user_id = @uid AND x.watched = 1))
		WHEN 'season' THEN (SELECT COUNT(*) FROM items e WHERE e.parent_id = i.id
			AND NOT EXISTS (SELECT 1 FROM watch_state x WHERE x.item_id = e.id AND x.user_id = @uid AND x.watched = 1))
		ELSE 0 END,
	COALESCE(sh.title, ''), COALESCE(sh.poster, ''), COALESCE(sh.backdrop, '')
FROM items i
LEFT JOIN watch_state w ON w.item_id = i.id AND w.user_id = @uid
LEFT JOIN items sh ON sh.id = i.show_id`

func scanItem(r interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var parent, show, season, episode sql.NullInt64
	var genres, credits string
	var watched int
	err := r.Scan(&it.ID, &it.LibraryID, &it.Kind, &parent, &show, &it.Title, &it.SortTitle, &it.OriginalTitle, &it.Year,
		&it.Overview, &it.Tagline, &it.Poster, &it.Backdrop, &it.Logo, &it.Rating, &it.ContentRating, &genres, &it.Studio, &it.Runtime,
		&it.ReleaseDate, &it.TMDBID, &it.CollectionID, &it.CollectionName, &it.Trailer, &credits, &season, &episode,
		&it.AddedAt, &it.MatchKey, &it.MetadataAt,
		&it.Watch.Position, &it.Watch.Duration, &watched, &it.Watch.UpdatedAt,
		&it.ChildCount, &it.UnwatchedCount,
		&it.ShowTitle, &it.ShowPoster, &it.ShowBackdrop)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	it.ParentID = parent.Int64
	it.ShowID = show.Int64
	if season.Valid {
		n := int(season.Int64)
		it.SeasonNumber = &n
	}
	if episode.Valid {
		n := int(episode.Int64)
		it.EpisodeNumber = &n
	}
	it.Watch.Watched = watched == 1
	json.Unmarshal([]byte(genres), &it.Genres)
	json.Unmarshal([]byte(credits), &it.Credits)
	it.Poster = ImageURL(it.Poster)
	it.Backdrop = ImageURL(it.Backdrop)
	it.Logo = ImageURL(it.Logo)
	it.ShowPoster = ImageURL(it.ShowPoster)
	it.ShowBackdrop = ImageURL(it.ShowBackdrop)
	for i := range it.Credits {
		it.Credits[i].Photo = ImageURL(it.Credits[i].Photo)
	}
	return &it, nil
}

func (s *Store) queryItems(query string, args ...any) ([]*Item, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func uidArg(uid int64) sql.NamedArg { return sql.Named("uid", uid) }

func (s *Store) Item(uid, id int64) (*Item, error) {
	return scanItem(s.db.QueryRow(itemSelect+` WHERE i.id = @id`, uidArg(uid), sql.Named("id", id)))
}

func (s *Store) Children(uid, parentID int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE i.parent_id = @p
		ORDER BY COALESCE(i.season_number, 0), COALESCE(i.episode_number, 0), i.sort_title COLLATE NOCASE`,
		uidArg(uid), sql.Named("p", parentID))
}

func (s *Store) ShowEpisodes(uid, showID int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE i.show_id = @show AND i.kind = 'episode'
		ORDER BY i.season_number = 0, i.season_number, i.episode_number`,
		uidArg(uid), sql.Named("show", showID))
}

var librarySorts = map[string]string{
	"title":   "i.sort_title COLLATE NOCASE",
	"added":   "i.added_at DESC",
	"year":    "i.year DESC, i.sort_title COLLATE NOCASE",
	"release": "i.release_date DESC",
	"rating":  "i.rating DESC",
}

func (s *Store) LibraryItems(uid, libraryID int64, sort string, offset, limit int) ([]*Item, int, error) {
	order, ok := librarySorts[sort]
	if !ok {
		order = librarySorts["title"]
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_id = ? AND kind IN ('movie','show')`, libraryID).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := s.queryItems(itemSelect+` WHERE i.library_id = @lib AND i.kind IN ('movie','show')
		ORDER BY `+order+` LIMIT @limit OFFSET @offset`,
		uidArg(uid), sql.Named("lib", libraryID), sql.Named("limit", limit), sql.Named("offset", offset))
	return items, total, err
}

// RecentlyAdded returns movies by add date, or shows ordered by their newest episode.
func (s *Store) RecentlyAdded(uid int64, kind string, limit int) ([]*Item, error) {
	order := "i.added_at DESC"
	if kind == "show" {
		order = "(SELECT MAX(e.added_at) FROM items e WHERE e.show_id = i.id) DESC"
	}
	return s.queryItems(itemSelect+` WHERE i.kind = @kind ORDER BY `+order+` LIMIT @limit`,
		uidArg(uid), sql.Named("kind", kind), sql.Named("limit", limit))
}

func (s *Store) RecentlyReleased(uid int64, kind string, limit int) ([]*Item, error) {
	today := time.Now().Format("2006-01-02")
	return s.queryItems(itemSelect+` WHERE i.kind = @kind AND i.release_date != '' AND i.release_date <= @today
		ORDER BY i.release_date DESC LIMIT @limit`,
		uidArg(uid), sql.Named("kind", kind), sql.Named("today", today), sql.Named("limit", limit))
}

// InProgress is everything the user started and has not finished.
func (s *Store) InProgress(uid int64, limit int) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE w.position > 15 AND i.kind IN ('movie','episode')
		ORDER BY w.updated_at DESC LIMIT @limit`, uidArg(uid), sql.Named("limit", limit))
}

type ShowProgress struct {
	ShowID    int64
	Season    int
	Episode   int
	UpdatedAt int64
}

// RecentlyFinishedShows reports, per show, the most recently completed episode.
func (s *Store) RecentlyFinishedShows(uid int64, limit int) ([]ShowProgress, error) {
	rows, err := s.db.Query(`SELECT e.show_id, e.season_number, e.episode_number, MAX(w.updated_at)
		FROM watch_state w JOIN items e ON e.id = w.item_id
		WHERE w.user_id = ? AND w.watched = 1 AND e.kind = 'episode'
		GROUP BY e.show_id ORDER BY MAX(w.updated_at) DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShowProgress
	for rows.Next() {
		var p ShowProgress
		if err := rows.Scan(&p.ShowID, &p.Season, &p.Episode, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// NextEpisode is the first regular-season episode after the given one.
func (s *Store) NextEpisode(uid, showID int64, season, episode int) (*Item, error) {
	return scanItem(s.db.QueryRow(itemSelect+` WHERE i.show_id = @show AND i.kind = 'episode' AND i.season_number > 0
		AND (i.season_number > @s OR (i.season_number = @s AND i.episode_number > @e))
		ORDER BY i.season_number, i.episode_number LIMIT 1`,
		uidArg(uid), sql.Named("show", showID), sql.Named("s", season), sql.Named("e", episode)))
}

func (s *Store) CollectionItems(uid, collectionID int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE i.collection_id = @c AND i.kind = 'movie' ORDER BY i.release_date`,
		uidArg(uid), sql.Named("c", collectionID))
}

func (s *Store) Search(uid int64, q string, limit int) ([]*Item, error) {
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	return s.queryItems(itemSelect+` WHERE i.kind IN ('movie','show') AND (i.title LIKE @q ESCAPE '\' OR i.original_title LIKE @q ESCAPE '\')
		ORDER BY i.sort_title COLLATE NOCASE LIMIT @limit`, uidArg(uid), sql.Named("q", like), sql.Named("limit", limit))
}

// LeafIDs expands a show or season to its episodes; movies and episodes map to themselves.
func (s *Store) LeafIDs(id int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM items WHERE (id = ?1 AND kind IN ('movie','episode'))
		OR (kind = 'episode' AND (show_id = ?1 OR parent_id = ?1))`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		ids = append(ids, n)
	}
	return ids, rows.Err()
}

// UpsertItem creates the item for a match key or re-parents an existing one.
// Titles on existing rows are left alone so metadata and edits survive rescans.
func (s *Store) UpsertItem(it *Item) (int64, error) {
	t := now()
	var parent, show, season, episode any
	if it.ParentID != 0 {
		parent = it.ParentID
	}
	if it.ShowID != 0 {
		show = it.ShowID
	}
	if it.SeasonNumber != nil {
		season = *it.SeasonNumber
	}
	if it.EpisodeNumber != nil {
		episode = *it.EpisodeNumber
	}
	var id int64
	err := s.db.QueryRow(`INSERT INTO items (library_id, kind, parent_id, show_id, match_key, title, sort_title, year, tmdb_id,
			season_number, episode_number, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (library_id, match_key) DO UPDATE SET parent_id = excluded.parent_id, show_id = excluded.show_id
		RETURNING id`,
		it.LibraryID, it.Kind, parent, show, it.MatchKey, it.Title, SortTitle(it.Title), it.Year, it.TMDBID,
		season, episode, t, t).Scan(&id)
	return id, err
}

// ItemsNeedingMetadata lists top-level items that are new, or shows with new seasons/episodes.
func (s *Store) ItemsNeedingMetadata(uid, libraryID int64, force bool) ([]*Item, error) {
	cond := `(i.metadata_at = 0 OR (i.kind = 'show' AND EXISTS (SELECT 1 FROM items c WHERE c.show_id = i.id AND c.metadata_at = 0)))`
	if force {
		cond = "1"
	}
	return s.queryItems(itemSelect+` WHERE i.library_id = @lib AND i.kind IN ('movie','show') AND `+cond+`
		ORDER BY i.added_at DESC`, uidArg(uid), sql.Named("lib", libraryID))
}

// UpdateMetadata writes provider metadata onto an item. Image fields hold
// raw references (e.g. "tmdb:/abc.jpg"), not URLs.
func (s *Store) UpdateMetadata(id int64, m *Item) error {
	genres, _ := json.Marshal(nonNil(m.Genres))
	credits, _ := json.Marshal(nonNilCredits(m.Credits))
	_, err := s.db.Exec(`UPDATE items SET title = ?, sort_title = ?, original_title = ?, year = ?, overview = ?, tagline = ?,
		poster = ?, backdrop = ?, logo = ?, rating = ?, content_rating = ?, genres = ?, studio = ?, runtime = ?,
		release_date = ?, tmdb_id = ?, collection_id = ?, collection_name = ?, trailer = ?, credits = ?,
		metadata_at = ?, updated_at = ? WHERE id = ?`,
		m.Title, SortTitle(m.Title), m.OriginalTitle, m.Year, m.Overview, m.Tagline,
		m.Poster, m.Backdrop, m.Logo, m.Rating, m.ContentRating, string(genres), m.Studio, m.Runtime,
		m.ReleaseDate, m.TMDBID, m.CollectionID, m.CollectionName, m.Trailer, string(credits),
		now(), now(), id)
	return err
}

// MarkMetadataChecked records a lookup that found nothing so it is not retried every scan.
func (s *Store) MarkMetadataChecked(id int64) error {
	_, err := s.db.Exec(`UPDATE items SET metadata_at = ? WHERE id = ?`, now(), id)
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilCredits(c []Credit) []Credit {
	if c == nil {
		return []Credit{}
	}
	return c
}

// SortTitle drops a leading English article so "The Matrix" sorts under M.
func SortTitle(title string) string {
	lower := strings.ToLower(title)
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, a) && len(title) > len(a) {
			return strings.TrimSpace(title[len(a):])
		}
	}
	return title
}

type Stats struct {
	Movies    int     `json:"movies"`
	Shows     int     `json:"shows"`
	Seasons   int     `json:"seasons"`
	Episodes  int     `json:"episodes"`
	Files     int     `json:"files"`
	TotalSize int64   `json:"totalSize"`
	TotalHours float64 `json:"totalHours"`
	Users     int     `json:"users"`
}

func (s *Store) Stats() (*Stats, error) {
	var st Stats
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM items WHERE kind = 'movie'),
		(SELECT COUNT(*) FROM items WHERE kind = 'show'),
		(SELECT COUNT(*) FROM items WHERE kind = 'season'),
		(SELECT COUNT(*) FROM items WHERE kind = 'episode'),
		(SELECT COUNT(*) FROM media_files),
		(SELECT COALESCE(SUM(size), 0) FROM media_files),
		(SELECT COALESCE(SUM(duration), 0) / 3600.0 FROM media_files),
		(SELECT COUNT(*) FROM users)`).
		Scan(&st.Movies, &st.Shows, &st.Seasons, &st.Episodes, &st.Files, &st.TotalSize, &st.TotalHours, &st.Users)
	return &st, err
}
