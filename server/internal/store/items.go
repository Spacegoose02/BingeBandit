package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	Edition        string     `json:"edition,omitempty"`
	Labels         []string   `json:"labels,omitempty"`
	Locked         []string   `json:"locked,omitempty"`       // fields metadata refreshes must not overwrite
	EpisodeOrder   string     `json:"episodeOrder,omitempty"` // shows: "" (aired) or "desc"
	SeasonNumber   *int       `json:"seasonNumber,omitempty"`
	EpisodeNumber  *int       `json:"episodeNumber,omitempty"`
	AddedAt        int64      `json:"addedAt"`
	Watch          WatchState `json:"watch"`
	ChildCount     int        `json:"childCount,omitempty"`
	UnwatchedCount int        `json:"unwatchedCount,omitempty"`
	ShowTitle      string     `json:"showTitle,omitempty"`
	ShowPoster     string     `json:"showPoster,omitempty"`
	ShowBackdrop   string     `json:"showBackdrop,omitempty"`
	OnWatchlist    bool       `json:"onWatchlist,omitempty"`

	MatchKey   string `json:"-"`
	MetadataAt int64  `json:"-"`
}

// visible restricts items to libraries the @uid user may see. @uid = 0 is
// the server itself (scanner, metadata), which sees everything.
const visible = `(@uid = 0
	OR EXISTS (SELECT 1 FROM users u WHERE u.id = @uid AND (u.all_libraries = 1 OR u.is_admin = 1))
	OR i.library_id IN (SELECT ul.library_id FROM user_libraries ul WHERE ul.user_id = @uid))`

// Every item query binds @uid so watch state and unwatched counts are per user.
const itemSelect = `SELECT i.id, i.library_id, i.kind, i.parent_id, i.show_id, i.title, i.sort_title, i.original_title, i.year,
	i.overview, i.tagline, i.poster, i.backdrop, i.logo, i.rating, i.content_rating, i.genres, i.studio, i.runtime,
	i.release_date, i.tmdb_id, i.collection_id, i.collection_name, i.trailer, i.credits,
	i.edition, i.labels, i.locked, i.episode_order, i.season_number, i.episode_number,
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
	COALESCE(sh.title, ''), COALESCE(sh.poster, ''), COALESCE(sh.backdrop, ''),
	EXISTS (SELECT 1 FROM watchlist wl WHERE wl.user_id = @uid AND wl.item_id = i.id)
FROM items i
LEFT JOIN watch_state w ON w.item_id = i.id AND w.user_id = @uid
LEFT JOIN items sh ON sh.id = i.show_id`

func scanItem(r interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var parent, show, season, episode sql.NullInt64
	var genres, credits, labels, locked string
	var watched int
	err := r.Scan(&it.ID, &it.LibraryID, &it.Kind, &parent, &show, &it.Title, &it.SortTitle, &it.OriginalTitle, &it.Year,
		&it.Overview, &it.Tagline, &it.Poster, &it.Backdrop, &it.Logo, &it.Rating, &it.ContentRating, &genres, &it.Studio, &it.Runtime,
		&it.ReleaseDate, &it.TMDBID, &it.CollectionID, &it.CollectionName, &it.Trailer, &credits,
		&it.Edition, &labels, &locked, &it.EpisodeOrder, &season, &episode,
		&it.AddedAt, &it.MatchKey, &it.MetadataAt,
		&it.Watch.Position, &it.Watch.Duration, &watched, &it.Watch.UpdatedAt,
		&it.ChildCount, &it.UnwatchedCount,
		&it.ShowTitle, &it.ShowPoster, &it.ShowBackdrop, &it.OnWatchlist)
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
	json.Unmarshal([]byte(labels), &it.Labels)
	json.Unmarshal([]byte(locked), &it.Locked)
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

// Item returns ErrNotFound for items in libraries the user cannot see.
func (s *Store) Item(uid, id int64) (*Item, error) {
	return scanItem(s.db.QueryRow(itemSelect+` WHERE i.id = @id AND `+visible, uidArg(uid), sql.Named("id", id)))
}

// Children lists seasons of a show or episodes of a season, honouring the
// show's episode order setting.
func (s *Store) Children(uid, parentID int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE i.parent_id = @p AND `+visible+`
		ORDER BY COALESCE(i.season_number, 0),
			CASE WHEN COALESCE(sh.episode_order, '') = 'desc' THEN -COALESCE(i.episode_number, 0) ELSE COALESCE(i.episode_number, 0) END,
			i.sort_title COLLATE NOCASE`,
		uidArg(uid), sql.Named("p", parentID))
}

func (s *Store) ShowEpisodes(uid, showID int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE i.show_id = @show AND i.kind = 'episode' AND `+visible+`
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
	items, err := s.queryItems(itemSelect+` WHERE i.library_id = @lib AND i.kind IN ('movie','show') AND `+visible+`
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
	return s.queryItems(itemSelect+` WHERE i.kind = @kind AND `+visible+` ORDER BY `+order+` LIMIT @limit`,
		uidArg(uid), sql.Named("kind", kind), sql.Named("limit", limit))
}

func (s *Store) RecentlyReleased(uid int64, kind string, limit int) ([]*Item, error) {
	today := time.Now().Format("2006-01-02")
	return s.queryItems(itemSelect+` WHERE i.kind = @kind AND i.release_date != '' AND i.release_date <= @today AND `+visible+`
		ORDER BY i.release_date DESC LIMIT @limit`,
		uidArg(uid), sql.Named("kind", kind), sql.Named("today", today), sql.Named("limit", limit))
}

// TrailerCandidates are the newest movies the user can see, hasn't watched
// and that have a trailer, other than exceptID.
func (s *Store) TrailerCandidates(uid, exceptID int64, limit int) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE i.kind = 'movie' AND i.trailer != '' AND i.id != @except AND COALESCE(w.watched, 0) = 0 AND `+visible+`
		ORDER BY i.added_at DESC LIMIT @limit`, uidArg(uid), sql.Named("except", exceptID), sql.Named("limit", limit))
}

// InProgress is everything the user started and has not finished.
func (s *Store) InProgress(uid int64, limit int) ([]*Item, error) {
	return s.queryItems(itemSelect+` WHERE w.position > 15 AND i.kind IN ('movie','episode') AND `+visible+`
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
	return scanItem(s.db.QueryRow(itemSelect+` WHERE i.show_id = @show AND i.kind = 'episode' AND i.season_number > 0 AND `+visible+`
		AND (i.season_number > @s OR (i.season_number = @s AND i.episode_number > @e))
		ORDER BY i.season_number, i.episode_number LIMIT 1`,
		uidArg(uid), sql.Named("show", showID), sql.Named("s", season), sql.Named("e", episode)))
}

func (s *Store) Search(uid int64, q string, limit int) ([]*Item, error) {
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	return s.queryItems(itemSelect+` WHERE i.kind IN ('movie','show') AND `+visible+`
		AND (i.title LIKE @q ESCAPE '\' OR i.original_title LIKE @q ESCAPE '\')
		ORDER BY i.sort_title COLLATE NOCASE LIMIT @limit`, uidArg(uid), sql.Named("q", like), sql.Named("limit", limit))
}

// LeafIDs expands a show or season to its episodes; movies and episodes map to themselves.
func (s *Store) LeafIDs(id int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM items WHERE (id = ?1 AND kind IN ('movie','episode'))
		OR (kind = 'episode' AND (show_id = ?1 OR parent_id = ?1))
		ORDER BY season_number = 0, season_number, episode_number`, id)
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

// LockableFields are the field names an admin can lock (JSON names, as the
// web UI uses them). Locked fields survive metadata refreshes.
var LockableFields = map[string]bool{
	"title": true, "sortTitle": true, "originalTitle": true, "releaseDate": true, "overview": true, "tagline": true,
	"poster": true, "backdrop": true, "logo": true, "rating": true, "contentRating": true, "genres": true,
	"studio": true, "collections": true,
}

// unlessLocked keeps a column's current value when any of fields is locked.
func unlessLocked(col string, fields ...string) string {
	return fmt.Sprintf(`%s = CASE WHEN EXISTS (SELECT 1 FROM json_each(locked) WHERE value IN ('%s')) THEN %s ELSE ? END`,
		col, strings.Join(fields, "','"), col)
}

var updateMetadataSQL = `UPDATE items SET ` + strings.Join([]string{
	unlessLocked("title", "title"),
	unlessLocked("sort_title", "title", "sortTitle"),
	unlessLocked("original_title", "originalTitle"),
	unlessLocked("year", "releaseDate"),
	unlessLocked("overview", "overview"),
	unlessLocked("tagline", "tagline"),
	unlessLocked("poster", "poster"),
	unlessLocked("backdrop", "backdrop"),
	unlessLocked("logo", "logo"),
	unlessLocked("rating", "rating"),
	unlessLocked("content_rating", "contentRating"),
	unlessLocked("genres", "genres"),
	unlessLocked("studio", "studio"),
	unlessLocked("release_date", "releaseDate"),
	"runtime = ?", "tmdb_id = ?", "collection_id = ?", "collection_name = ?", "trailer = ?", "credits = ?",
	"metadata_at = ?", "updated_at = ?",
}, ", ") + ` WHERE id = ?`

// UpdateMetadata writes provider metadata onto an item, skipping locked
// fields. Image fields hold raw references (e.g. "tmdb:/abc.jpg"), not URLs.
func (s *Store) UpdateMetadata(id int64, m *Item) error {
	genres, _ := json.Marshal(nonNil(m.Genres))
	credits, _ := json.Marshal(nonNilCredits(m.Credits))
	_, err := s.db.Exec(updateMetadataSQL,
		m.Title, SortTitle(m.Title), m.OriginalTitle, m.Year, m.Overview, m.Tagline,
		m.Poster, m.Backdrop, m.Logo, m.Rating, m.ContentRating, string(genres), m.Studio, m.ReleaseDate,
		m.Runtime, m.TMDBID, m.CollectionID, m.CollectionName, m.Trailer, string(credits),
		now(), now(), id)
	return err
}

// Details are the admin-editable text fields of an item.
type Details struct {
	Title         string   `json:"title"`
	SortTitle     string   `json:"sortTitle"`
	OriginalTitle string   `json:"originalTitle"`
	Edition       string   `json:"edition"`
	ReleaseDate   string   `json:"releaseDate"`
	ContentRating string   `json:"contentRating"`
	Rating        float64  `json:"rating"`
	Studio        string   `json:"studio"`
	Tagline       string   `json:"tagline"`
	Overview      string   `json:"overview"`
	Genres        []string `json:"genres"`
	Labels        []string `json:"labels"`
	EpisodeOrder  string   `json:"episodeOrder"`
	Locked        []string `json:"locked"`
}

// UpdateDetails saves an admin edit, replacing the item's lock list.
func (s *Store) UpdateDetails(id int64, d *Details) error {
	if d.SortTitle == "" {
		d.SortTitle = SortTitle(d.Title)
	}
	year := 0
	if len(d.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(d.ReleaseDate[:4])
	}
	locks := []string{}
	for _, f := range d.Locked {
		if LockableFields[f] {
			locks = append(locks, f)
		}
	}
	genres, _ := json.Marshal(nonNil(d.Genres))
	labels, _ := json.Marshal(nonNil(d.Labels))
	lockJSON, _ := json.Marshal(locks)
	_, err := s.db.Exec(`UPDATE items SET title = ?, sort_title = ?, original_title = ?, edition = ?, release_date = ?, year = ?,
		content_rating = ?, rating = ?, studio = ?, tagline = ?, overview = ?, genres = ?, labels = ?, episode_order = ?,
		locked = ?, updated_at = ? WHERE id = ?`,
		d.Title, d.SortTitle, d.OriginalTitle, d.Edition, d.ReleaseDate, year,
		d.ContentRating, d.Rating, d.Studio, d.Tagline, d.Overview, string(genres), string(labels), d.EpisodeOrder,
		string(lockJSON), now(), id)
	return err
}

// SetArtwork replaces poster, backdrop or logo and locks it.
func (s *Store) SetArtwork(id int64, kind, ref string) error {
	if kind != "poster" && kind != "backdrop" && kind != "logo" {
		return fmt.Errorf("unknown artwork type %q", kind)
	}
	_, err := s.db.Exec(`UPDATE items SET `+kind+` = ?, updated_at = ?,
		locked = CASE WHEN EXISTS (SELECT 1 FROM json_each(locked) WHERE value = ?) THEN locked ELSE json_insert(locked, '$[#]', ?) END
		WHERE id = ?`, ref, now(), kind, kind, id)
	return err
}

// SetMatch points an item at a different TMDB id; the next refresh uses it.
func (s *Store) SetMatch(id, tmdbID int64) error {
	_, err := s.db.Exec(`UPDATE items SET tmdb_id = ?, metadata_at = 0, updated_at = ? WHERE id = ?`, tmdbID, now(), id)
	return err
}

// ShowTVDB returns a show's TheTVDB id (0 when it uses TMDB) and episode order.
func (s *Store) ShowTVDB(id int64) (tvdbID int64, order string, err error) {
	err = s.db.QueryRow(`SELECT tvdb_id, tvdb_order FROM items WHERE id = ?`, id).Scan(&tvdbID, &order)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// SetTVDBMatch points a show at TheTVDB (or back at TMDB with tvdbID 0) and
// queues it, with its seasons and episodes, for a metadata refresh.
func (s *Store) SetTVDBMatch(id, tvdbID int64, order string) error {
	_, err := s.db.Exec(`UPDATE items SET tvdb_id = CASE WHEN id = ?1 THEN ?2 ELSE tvdb_id END,
		tvdb_order = CASE WHEN id = ?1 THEN ?3 ELSE tvdb_order END, metadata_at = 0, updated_at = ?4
		WHERE id = ?1 OR show_id = ?1`, id, tvdbID, order, now())
	return err
}

// RawArtwork returns the stored (not URL-mapped) image reference, used to
// clean up uploaded files when they are replaced.
func (s *Store) RawArtwork(id int64, kind string) (string, error) {
	if kind != "poster" && kind != "backdrop" && kind != "logo" {
		return "", fmt.Errorf("unknown artwork type %q", kind)
	}
	var ref string
	err := s.db.QueryRow(`SELECT `+kind+` FROM items WHERE id = ?`, id).Scan(&ref)
	return ref, err
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
	Movies     int     `json:"movies"`
	Shows      int     `json:"shows"`
	Seasons    int     `json:"seasons"`
	Episodes   int     `json:"episodes"`
	Files      int     `json:"files"`
	TotalSize  int64   `json:"totalSize"`
	TotalHours float64 `json:"totalHours"`
	Users      int     `json:"users"`
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
