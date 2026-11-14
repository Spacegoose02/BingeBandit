package store

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// TitleRequest is a movie or show someone asked for. There's one per title;
// later askers join it.
//
// Status goes pending → approved (an admin said yes; waiting for Cinemask)
// → sent (Cinemask has it) → available (it's in a library). An admin can
// also decline it, and Cinemask can fail it (approving again retries).
type TitleRequest struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"` // movie | show
	TMDBID      int64  `json:"tmdbId"`
	Title       string `json:"title"`
	Year        int    `json:"year,omitempty"`
	Poster      string `json:"poster,omitempty"` // URL
	Overview    string `json:"overview,omitempty"`
	UserID      int64  `json:"userId"`      // who asked first
	RequestedBy string `json:"requestedBy"` // their name
	People      int    `json:"people"`      // how many asked
	Note        string `json:"note,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"` // why it was declined, or what went wrong
	Link        string `json:"link,omitempty"`   // the title's page in Cinemask
	// Seasons a show request is for; empty is the whole show.
	Seasons []int `json:"seasons,omitempty"`
	FourK   bool  `json:"fourK,omitempty"` // the 4K copy is wanted too
	// While it's fetched, what the fetching plugin last said: searching,
	// downloading, importing or waiting (not out yet), how far along (0-1)
	// and when it should finish (unix seconds; 0 when unknown).
	Stage       string  `json:"stage,omitempty"`
	Progress    float64 `json:"progress,omitempty"`
	ETA         int64   `json:"eta,omitempty"`
	ReleaseDate string  `json:"releaseDate,omitempty"`
	AvailableAt int64   `json:"availableAt,omitempty"`
	DecidedBy   int64   `json:"-"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
	Mine        bool    `json:"mine"` // the viewer asked for it (filled by the API)
}

// Request statuses.
const (
	RequestParent    = "parent" // a kid asked; waiting for their parent
	RequestPending   = "pending"
	RequestApproved  = "approved"
	RequestSent      = "sent"
	RequestFailed    = "failed"
	RequestDeclined  = "declined"
	RequestAvailable = "available"
)

const requestSelect = `SELECT r.id, r.kind, r.tmdb_id, r.title, r.year, r.poster, r.overview, r.user_id,
	COALESCE(CASE WHEN u.profile_name != '' THEN u.profile_name ELSE u.username END, ''),
	(SELECT COUNT(*) FROM title_request_people p WHERE p.request_id = r.id),
	r.note, r.status, r.reason, r.link, r.seasons, r.four_k, r.stage, r.progress, r.eta, r.release_date, r.available_at,
	r.decided_by, r.created_at, r.updated_at
	FROM title_requests r LEFT JOIN users u ON u.id = r.user_id`

func scanRequest(row interface{ Scan(...any) error }) (*TitleRequest, error) {
	var t TitleRequest
	var seasons string
	err := row.Scan(&t.ID, &t.Kind, &t.TMDBID, &t.Title, &t.Year, &t.Poster, &t.Overview, &t.UserID, &t.RequestedBy,
		&t.People, &t.Note, &t.Status, &t.Reason, &t.Link, &seasons, &t.FourK, &t.Stage, &t.Progress, &t.ETA, &t.ReleaseDate,
		&t.AvailableAt, &t.DecidedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.Poster = ImageURL(t.Poster)
	t.Seasons = ParseSeasons(seasons)
	return &t, err
}

func (s *Store) queryRequests(q string, args ...any) ([]*TitleRequest, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TitleRequest{}
	for rows.Next() {
		t, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ParseSeasons reads a seasons column ("1,2,5").
func ParseSeasons(v string) []int {
	var out []int
	for _, p := range strings.Split(v, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// FormatSeasons writes seasons sorted, without repeats.
func FormatSeasons(list []int) string {
	seen := map[int]bool{}
	var keep []int
	for _, n := range list {
		if n > 0 && !seen[n] {
			seen[n] = true
			keep = append(keep, n)
		}
	}
	sort.Ints(keep)
	parts := make([]string, len(keep))
	for i, n := range keep {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// mergeSeasons is what a show request covers after another ask: the whole
// show when either was, else both lists.
func mergeSeasons(have, add string) string {
	if have == "" || add == "" {
		return ""
	}
	return FormatSeasons(append(ParseSeasons(have), ParseSeasons(add)...))
}

// What RequestTitle did.
const (
	RequestCreated  = "created"  // a new request
	RequestJoined   = "joined"   // added uid to an existing one
	RequestReopened = "reopened" // more seasons or the 4K copy: the request starts over at status
)

// RequestTitle asks for a title for user uid. t carries the title's kind,
// TMDB id, title, year, poster reference (tmdb:/…), overview, note, seasons,
// 4K and release date; source is "" when they asked and "watchlist" when
// their watchlist did. When the title was already asked for, uid joins that
// request, and when they want more of it (seasons, 4K) than it covers it
// starts over at status, unless status is RequestParent (a kid's ask waits
// for a parent first).
func (s *Store) RequestTitle(uid int64, t *TitleRequest, status, source string) (req *TitleRequest, outcome string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var id int64
	var seasons, oldStatus string
	var fourK bool
	want := FormatSeasons(t.Seasons)
	err = tx.QueryRow(`SELECT id, seasons, four_k, status FROM title_requests WHERE kind = ? AND tmdb_id = ?`, t.Kind, t.TMDBID).
		Scan(&id, &seasons, &fourK, &oldStatus)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec(`INSERT INTO title_requests (kind, tmdb_id, title, year, poster, overview, user_id, note, status,
			seasons, four_k, release_date, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.Kind, t.TMDBID, t.Title, t.Year, t.Poster, t.Overview, uid, t.Note, status,
			want, t.FourK, t.ReleaseDate, now(), now())
		if err != nil {
			return nil, "", err
		}
		id, _ = res.LastInsertId()
		outcome = RequestCreated
	case err != nil:
		return nil, "", err
	default:
		outcome = RequestJoined
		merged := seasons
		if t.Kind == "show" {
			merged = mergeSeasons(seasons, want)
		}
		more := merged != seasons || (t.FourK && !fourK)
		if more && status != RequestParent {
			// They want more than it covers: it has to be fetched again.
			reopen := oldStatus == RequestAvailable || oldStatus == RequestSent || oldStatus == RequestDeclined || oldStatus == RequestFailed
			q := `UPDATE title_requests SET seasons = ?, four_k = four_k OR ?, updated_at = ? WHERE id = ?`
			args := []any{merged, t.FourK, now(), id}
			if reopen {
				q = `UPDATE title_requests SET seasons = ?, four_k = four_k OR ?, status = ?, reason = '', stage = '', progress = 0, eta = 0,
					available_at = 0, updated_at = ? WHERE id = ?`
				args = []any{merged, t.FourK, status, now(), id}
				outcome = RequestReopened
			}
			if _, err := tx.Exec(q, args...); err != nil {
				return nil, "", err
			}
		}
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO title_request_people (request_id, user_id, created_at, source) VALUES (?, ?, ?, ?)`,
		id, uid, now(), source); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	req, err = s.TitleRequest(id)
	return req, outcome, err
}

// RequestsSince counts the kind of requests uid asked for themselves (not
// their watchlist) since a unix time, for weekly limits.
func (s *Store) RequestsSince(uid int64, kind string, since int64) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM title_request_people p JOIN title_requests r ON r.id = p.request_id
		WHERE p.user_id = ? AND r.kind = ? AND p.created_at >= ? AND p.source = ''`, uid, kind, since).Scan(&n)
	return n
}

// OldestRequestSince is when the first of those requests was made, so the
// limit can say when the next one frees up.
func (s *Store) OldestRequestSince(uid int64, kind string, since int64) int64 {
	var at sql.NullInt64
	s.db.QueryRow(`SELECT MIN(p.created_at) FROM title_request_people p JOIN title_requests r ON r.id = p.request_id
		WHERE p.user_id = ? AND r.kind = ? AND p.created_at >= ? AND p.source = ''`, uid, kind, since).Scan(&at)
	return at.Int64
}

func (s *Store) TitleRequest(id int64) (*TitleRequest, error) {
	return scanRequest(s.db.QueryRow(requestSelect+` WHERE r.id = ?`, id))
}

// TitleRequests lists requests, newest first: all of them when uid is 0,
// otherwise the ones uid asked for. statuses narrows them when given.
func (s *Store) TitleRequests(uid int64, statuses ...string) ([]*TitleRequest, error) {
	where, args := []string{"1"}, []any{}
	if uid != 0 {
		where = append(where, `r.id IN (SELECT request_id FROM title_request_people WHERE user_id = ?)`)
		args = append(args, uid)
	}
	if len(statuses) > 0 {
		where = append(where, `r.status IN (?`+strings.Repeat(",?", len(statuses)-1)+`)`)
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	return s.queryRequests(requestSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY r.created_at DESC, r.id DESC`, args...)
}

// TitleRequestFor finds the request for a title, or nil.
func (s *Store) TitleRequestFor(kind string, tmdbID int64) *TitleRequest {
	t, err := scanRequest(s.db.QueryRow(requestSelect+` WHERE r.kind = ? AND r.tmdb_id = ?`, kind, tmdbID))
	if err != nil {
		return nil
	}
	return t
}

// TitleRequestsFor finds the requests for these titles ("movie:603"), by key.
func (s *Store) TitleRequestsFor(keys []string) (map[string]*TitleRequest, error) {
	out := map[string]*TitleRequest{}
	if len(keys) == 0 {
		return out, nil
	}
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	list, err := s.queryRequests(requestSelect+` WHERE (r.kind || ':' || r.tmdb_id) IN (?`+strings.Repeat(",?", len(keys)-1)+`)`, args...)
	for _, t := range list {
		out[t.Kind+":"+strconv.FormatInt(t.TMDBID, 10)] = t
	}
	return out, err
}

// RequestedBy tells whether uid is one of the people who asked for a request.
func (s *Store) RequestedBy(id, uid int64) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM title_request_people WHERE request_id = ? AND user_id = ?`, id, uid).Scan(&n)
	return n > 0
}

// RequestPeople are the users who asked for a request.
func (s *Store) RequestPeople(id int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT user_id FROM title_request_people WHERE request_id = ? ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// SetRequestStatus moves a request on. link is kept when empty; reason
// always replaces the old one.
func (s *Store) SetRequestStatus(id int64, status, reason, link string, decidedBy int64) error {
	availableAt := int64(0)
	if status == RequestAvailable {
		availableAt = now()
	}
	res, err := s.db.Exec(`UPDATE title_requests SET status = ?, reason = ?, link = CASE WHEN ? != '' THEN ? ELSE link END,
		decided_by = CASE WHEN ? != 0 THEN ? ELSE decided_by END,
		stage = CASE WHEN status = ? THEN stage ELSE '' END, progress = CASE WHEN status = ? THEN progress ELSE 0 END,
		eta = CASE WHEN status = ? THEN eta ELSE 0 END, available_at = ?, updated_at = ? WHERE id = ?`,
		status, reason, link, link, decidedBy, decidedBy, status, status, status, availableAt, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// WithdrawRequest takes uid off a request, deleting the request when nobody
// else asked for it. It reports whether the request is gone.
func (s *Store) WithdrawRequest(id, uid int64) (bool, error) {
	if _, err := s.db.Exec(`DELETE FROM title_request_people WHERE request_id = ? AND user_id = ?`, id, uid); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM title_requests WHERE id = ? AND NOT EXISTS (SELECT 1 FROM title_request_people WHERE request_id = ?)`, id, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) DeleteTitleRequest(id int64) error {
	_, err := s.db.Exec(`DELETE FROM title_requests WHERE id = ?`, id)
	return err
}

// SetRequestProgress records what the fetching plugin says about a sent
// request: its stage, progress (0-1) and ETA (unix seconds).
func (s *Store) SetRequestProgress(id int64, stage string, progress float64, eta int64) error {
	_, err := s.db.Exec(`UPDATE title_requests SET stage = ?, progress = ?, eta = ?, updated_at = ? WHERE id = ?`,
		stage, min(max(progress, 0), 1), max(eta, 0), now(), id)
	return err
}

// ArrivedRequests are the open requests (not declined or already
// available) that are now in a library: the movie, the show with every
// season asked for, and with a 4K file when the 4K copy was wanted.
func (s *Store) ArrivedRequests() ([]*TitleRequest, error) {
	list, err := s.queryRequests(requestSelect + ` WHERE r.status NOT IN ('declined', 'available', 'parent')
		AND EXISTS (SELECT 1 FROM items i WHERE i.kind = r.kind AND i.tmdb_id = r.tmdb_id)`)
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, t := range list {
		if s.RequestCovered(t) {
			out = append(out, t)
		}
	}
	return out, nil
}

// RequestCovered tells whether the server has everything t asks for.
func (s *Store) RequestCovered(t *TitleRequest) bool {
	if t.Kind == "show" {
		have, err := s.ShowSeasons(t.TMDBID)
		if err != nil || len(have) == 0 {
			return false
		}
		for _, n := range t.Seasons {
			if have[n] == 0 {
				return false
			}
		}
	}
	return !t.FourK || s.Has4K(t.Kind, t.TMDBID)
}

// ShowSeasons counts the episodes the server has of a show (by TMDB id) in
// each season, across libraries. An error means the counts couldn't be read
// (a damaged database, say), not that the server has none.
func (s *Store) ShowSeasons(tmdbID int64) (map[int]int, error) {
	out := map[int]int{}
	rows, err := s.db.Query(`SELECT e.season_number, COUNT(*) FROM items e JOIN items sh ON sh.id = e.show_id
		WHERE sh.kind = 'show' AND sh.tmdb_id = ? AND e.kind = 'episode' AND e.season_number IS NOT NULL
		GROUP BY e.season_number`, tmdbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var season, n int
		if err := rows.Scan(&season, &n); err != nil {
			return nil, err
		}
		out[season] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Has4K tells whether a movie, or any episode of a show, has a file 2160
// lines or taller (or 3840 wide, for scope).
func (s *Store) Has4K(kind string, tmdbID int64) bool {
	var n int
	if kind == "movie" {
		s.db.QueryRow(`SELECT COUNT(*) FROM media_files f JOIN items i ON i.id = f.item_id
			WHERE i.kind = 'movie' AND i.tmdb_id = ? AND (f.height >= 2000 OR f.width >= 3800)`, tmdbID).Scan(&n)
	} else {
		s.db.QueryRow(`SELECT COUNT(*) FROM media_files f JOIN items e ON e.id = f.item_id JOIN items sh ON sh.id = e.show_id
			WHERE sh.kind = 'show' AND sh.tmdb_id = ? AND (f.height >= 2000 OR f.width >= 3800)`, tmdbID).Scan(&n)
	}
	return n > 0
}

// UnwatchedRequests are titles that came in through requests at least
// days ago that none of the people who asked has played since, oldest first.
func (s *Store) UnwatchedRequests(days int) ([]*TitleRequest, error) {
	cut := now() - int64(days)*86400
	return s.queryRequests(requestSelect+` WHERE r.status = 'available' AND r.available_at > 0 AND r.available_at <= ?
		AND NOT EXISTS (
			SELECT 1 FROM plays p JOIN title_request_people rp ON rp.user_id = p.user_id AND rp.request_id = r.id
			JOIN items i ON i.id = p.item_id OR i.id = p.show_id
			WHERE i.kind = r.kind AND i.tmdb_id = r.tmdb_id AND p.started_at >= r.available_at - 86400)
		ORDER BY r.available_at`, cut)
}

// ItemsForRequest finds the movie or show items a request brought in.
func (s *Store) ItemsForRequest(t *TitleRequest) []int64 {
	rows, err := s.db.Query(`SELECT id FROM items WHERE kind = ? AND tmdb_id = ?`, t.Kind, t.TMDBID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// RequestRules are what an admin set for one person's requests.
type RequestRules struct {
	AutoApprove bool `json:"autoApprove"`
	MovieLimit  int  `json:"movieLimit"` // per 7 days; -1 follows the server's setting, 0 is no limit
	ShowLimit   int  `json:"showLimit"`
	Watchlist   bool `json:"watchlist"` // the person's own choice: request what's on their Trakt watchlist
}

func (s *Store) RequestRules(uid int64) RequestRules {
	r := RequestRules{MovieLimit: -1, ShowLimit: -1}
	s.db.QueryRow(`SELECT auto_approve, movie_limit, show_limit, watchlist FROM request_rules WHERE user_id = ?`, uid).
		Scan(&r.AutoApprove, &r.MovieLimit, &r.ShowLimit, &r.Watchlist)
	return r
}

func (s *Store) SetRequestRules(uid int64, r RequestRules) error {
	_, err := s.db.Exec(`INSERT INTO request_rules (user_id, auto_approve, movie_limit, show_limit, watchlist) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET auto_approve = excluded.auto_approve, movie_limit = excluded.movie_limit,
		show_limit = excluded.show_limit, watchlist = excluded.watchlist`, uid, r.AutoApprove, r.MovieLimit, r.ShowLimit, r.Watchlist)
	return err
}

// AllRequestRules lists everyone's rules that differ from the defaults, by user.
func (s *Store) AllRequestRules() map[int64]RequestRules {
	out := map[int64]RequestRules{}
	rows, err := s.db.Query(`SELECT user_id, auto_approve, movie_limit, show_limit, watchlist FROM request_rules`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var r RequestRules
		if rows.Scan(&uid, &r.AutoApprove, &r.MovieLimit, &r.ShowLimit, &r.Watchlist) == nil {
			out[uid] = r
		}
	}
	return out
}

// WatchlistRequesters are the people who want their Trakt watchlist requested.
func (s *Store) WatchlistRequesters() []int64 {
	rows, err := s.db.Query(`SELECT user_id FROM request_rules WHERE watchlist = 1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// OfferSeason records that uid was offered a show's season, reporting
// false when they already were.
func (s *Store) OfferSeason(uid, tmdbID int64, season int) bool {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO request_offers (user_id, tmdb_id, season, created_at) VALUES (?, ?, ?, ?)`,
		uid, tmdbID, season, now())
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// FinishedTitle is a movie or show someone finished lately.
type FinishedTitle struct {
	Kind   string
	TMDBID int64
	Title  string
}

// RecentlyFinished lists the movies and shows uid most recently finished
// something of (a movie, or an episode of the show), newest first.
func (s *Store) RecentlyFinished(uid int64, n int) []FinishedTitle {
	rows, err := s.db.Query(`SELECT t.kind, t.tmdb_id, t.title FROM plays p
		JOIN items t ON t.id = CASE WHEN p.show_id > 0 THEN p.show_id ELSE p.item_id END
		WHERE p.user_id = ? AND p.completed = 1 AND t.kind IN ('movie', 'show') AND t.tmdb_id > 0
		GROUP BY t.kind, t.tmdb_id ORDER BY MAX(p.stopped_at) DESC LIMIT ?`, uid, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []FinishedTitle
	for rows.Next() {
		var f FinishedTitle
		if rows.Scan(&f.Kind, &f.TMDBID, &f.Title) == nil {
			out = append(out, f)
		}
	}
	return out
}

// SetRequestAvailableAt changes when a request's title came in (for the
// cleanup list's tests, and imports).
func (s *Store) SetRequestAvailableAt(id, at int64) error {
	_, err := s.db.Exec(`UPDATE title_requests SET available_at = ? WHERE id = ?`, at, id)
	return err
}
