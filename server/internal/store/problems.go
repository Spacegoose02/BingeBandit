package store

import (
	"database/sql"
	"errors"
)

// ProblemReport is someone saying something is wrong with a movie or
// episode: bad picture or sound, missing or wrong subtitles, the wrong file.
type ProblemReport struct {
	ID       int64   `json:"id"`
	ItemID   int64   `json:"itemId"`
	UserID   int64   `json:"userId"`
	User     string  `json:"user"` // their name
	Kind     string  `json:"kind"` // video | audio | subtitles | wrong | other
	Note     string  `json:"note,omitempty"`
	Position float64 `json:"position,omitempty"` // seconds into the file when they reported it
	// Status is open, fixing (sent off to be fetched again) or resolved.
	Status    string `json:"status"`
	Reply     string `json:"reply,omitempty"` // the admin's answer
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	// Filled from the item.
	Title     string `json:"title"`
	ShowTitle string `json:"showTitle,omitempty"`
	ItemKind  string `json:"itemKind"`
	Season    *int   `json:"seasonNumber,omitempty"`
	Episode   *int   `json:"episodeNumber,omitempty"`
	Poster    string `json:"poster,omitempty"`
	TMDBID    int64  `json:"tmdbId,omitempty"` // the movie's, or the show's for an episode
}

// Problem report kinds and statuses.
var ProblemKinds = []string{"video", "audio", "subtitles", "wrong", "other"}

const (
	ProblemOpen     = "open"
	ProblemFixing   = "fixing"
	ProblemResolved = "resolved"
)

const problemSelect = `SELECT p.id, p.item_id, p.user_id,
	COALESCE(CASE WHEN u.profile_name != '' THEN u.profile_name ELSE u.username END, ''),
	p.kind, p.note, p.position, p.status, p.reply, p.created_at, p.updated_at,
	i.title, COALESCE(sh.title, ''), i.kind, i.season_number, i.episode_number,
	CASE WHEN i.poster != '' THEN i.poster ELSE COALESCE(sh.poster, '') END,
	CASE WHEN i.kind = 'episode' THEN COALESCE(sh.tmdb_id, 0) ELSE i.tmdb_id END
	FROM problem_reports p JOIN items i ON i.id = p.item_id
	LEFT JOIN items sh ON sh.id = i.show_id
	LEFT JOIN users u ON u.id = p.user_id`

func scanProblem(row interface{ Scan(...any) error }) (*ProblemReport, error) {
	var p ProblemReport
	var season, episode sql.NullInt64
	err := row.Scan(&p.ID, &p.ItemID, &p.UserID, &p.User, &p.Kind, &p.Note, &p.Position, &p.Status, &p.Reply, &p.CreatedAt, &p.UpdatedAt,
		&p.Title, &p.ShowTitle, &p.ItemKind, &season, &episode, &p.Poster, &p.TMDBID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if season.Valid {
		n := int(season.Int64)
		p.Season = &n
	}
	if episode.Valid {
		n := int(episode.Int64)
		p.Episode = &n
	}
	p.Poster = ImageURL(p.Poster)
	return &p, err
}

// ReportProblem saves a report, or adds to the reporter's open one about the
// same item and kind.
func (s *Store) ReportProblem(p *ProblemReport) (*ProblemReport, bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM problem_reports WHERE item_id = ? AND user_id = ? AND kind = ? AND status != 'resolved'`,
		p.ItemID, p.UserID, p.Kind).Scan(&id)
	created := false
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := s.db.Exec(`INSERT INTO problem_reports (item_id, user_id, kind, note, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, p.ItemID, p.UserID, p.Kind, p.Note, p.Position, now(), now())
		if err != nil {
			return nil, false, err
		}
		id, _ = res.LastInsertId()
		created = true
	case err != nil:
		return nil, false, err
	default:
		if _, err := s.db.Exec(`UPDATE problem_reports SET note = CASE WHEN ? != '' THEN ? ELSE note END, position = ?, updated_at = ? WHERE id = ?`,
			p.Note, p.Note, p.Position, now(), id); err != nil {
			return nil, false, err
		}
	}
	out, err := s.ProblemReport(id)
	return out, created, err
}

func (s *Store) ProblemReport(id int64) (*ProblemReport, error) {
	return scanProblem(s.db.QueryRow(problemSelect+` WHERE p.id = ?`, id))
}

// ProblemReports lists reports, newest first: everyone's when uid is 0,
// with resolved ones only when all is set.
func (s *Store) ProblemReports(uid int64, all bool) ([]*ProblemReport, error) {
	q := problemSelect + ` WHERE (? = 0 OR p.user_id = ?) AND (? OR p.status != 'resolved') ORDER BY p.created_at DESC LIMIT 300`
	rows, err := s.db.Query(q, uid, uid, all)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ProblemReport{}
	for rows.Next() {
		p, err := scanProblem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProblemStatus moves a report on, with the admin's reply (kept when empty).
func (s *Store) SetProblemStatus(id int64, status, reply string) error {
	res, err := s.db.Exec(`UPDATE problem_reports SET status = ?, reply = CASE WHEN ? != '' THEN ? ELSE reply END, updated_at = ? WHERE id = ?`,
		status, reply, reply, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteProblemReport(id int64) error {
	_, err := s.db.Exec(`DELETE FROM problem_reports WHERE id = ?`, id)
	return err
}

// OpenProblems counts the reports an admin hasn't closed.
func (s *Store) OpenProblems() int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM problem_reports WHERE status != 'resolved'`).Scan(&n)
	return n
}
