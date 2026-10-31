package store

import (
	"database/sql"
	"errors"
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
	DecidedBy   int64  `json:"-"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	Mine        bool   `json:"mine"` // the viewer asked for it (filled by the API)
}

// Request statuses.
const (
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
	r.note, r.status, r.reason, r.link, r.decided_by, r.created_at, r.updated_at
	FROM title_requests r LEFT JOIN users u ON u.id = r.user_id`

func scanRequest(row interface{ Scan(...any) error }) (*TitleRequest, error) {
	var t TitleRequest
	err := row.Scan(&t.ID, &t.Kind, &t.TMDBID, &t.Title, &t.Year, &t.Poster, &t.Overview, &t.UserID, &t.RequestedBy,
		&t.People, &t.Note, &t.Status, &t.Reason, &t.Link, &t.DecidedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.Poster = ImageURL(t.Poster)
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

// RequestTitle asks for a title for user uid. t carries the title's kind,
// TMDB id, title, year, poster reference (tmdb:/…), overview and note. When
// the title was already asked for, uid joins that request; created says
// whether this one is new.
func (s *Store) RequestTitle(uid int64, t *TitleRequest, status string) (req *TitleRequest, created bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`SELECT id FROM title_requests WHERE kind = ? AND tmdb_id = ?`, t.Kind, t.TMDBID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec(`INSERT INTO title_requests (kind, tmdb_id, title, year, poster, overview, user_id, note, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.Kind, t.TMDBID, t.Title, t.Year, t.Poster, t.Overview, uid, t.Note, status, now(), now())
		if err != nil {
			return nil, false, err
		}
		id, _ = res.LastInsertId()
		created = true
	case err != nil:
		return nil, false, err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO title_request_people (request_id, user_id, created_at) VALUES (?, ?, ?)`, id, uid, now()); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	req, err = s.TitleRequest(id)
	return req, created, err
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
	res, err := s.db.Exec(`UPDATE title_requests SET status = ?, reason = ?, link = CASE WHEN ? != '' THEN ? ELSE link END,
		decided_by = CASE WHEN ? != 0 THEN ? ELSE decided_by END, updated_at = ? WHERE id = ?`,
		status, reason, link, link, decidedBy, decidedBy, now(), id)
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

// ArrivedRequests are the open requests (not declined or already
// available) whose title is now in a library.
func (s *Store) ArrivedRequests() ([]*TitleRequest, error) {
	return s.queryRequests(requestSelect + ` WHERE r.status NOT IN ('declined', 'available')
		AND EXISTS (SELECT 1 FROM items i WHERE i.kind = r.kind AND i.tmdb_id = r.tmdb_id)`)
}
