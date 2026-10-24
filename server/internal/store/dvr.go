package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

type Recording struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"userId"`
	Username     string `json:"username,omitempty"`
	RuleID       int64  `json:"ruleId,omitempty"`
	ChannelID    int64  `json:"channelId,omitempty"`
	ChannelName  string `json:"channelName"`
	EPGID        string `json:"epgId,omitempty"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle,omitempty"`
	Description  string `json:"description,omitempty"`
	Episode      string `json:"episode,omitempty"`
	ProgramStart int64  `json:"programStart"`
	ProgramStop  int64  `json:"programStop"`
	StartAt      int64  `json:"startAt"`
	StopAt       int64  `json:"stopAt"`
	Status       string `json:"status"`
	Path         string `json:"path,omitempty"`
	Size         int64  `json:"size"`
	Error        string `json:"error,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
	ItemID       int64  `json:"itemId,omitempty"` // set when a library has picked the file up
}

const recordingCols = `r.id, r.user_id, COALESCE(u.username, ''), COALESCE(r.rule_id, 0), COALESCE(r.channel_id, 0), r.channel_name, r.epg_id,
	r.title, r.subtitle, r.description, r.episode, r.program_start, r.program_stop, r.start_at, r.stop_at, r.status, r.path, r.size,
	r.error, r.created_at, COALESCE((SELECT f.item_id FROM media_files f WHERE f.path = r.path AND r.path != ''), 0)`

const recordingFrom = ` FROM recordings r LEFT JOIN users u ON u.id = r.user_id`

func scanRecording(row interface{ Scan(...any) error }) (*Recording, error) {
	var r Recording
	err := row.Scan(&r.ID, &r.UserID, &r.Username, &r.RuleID, &r.ChannelID, &r.ChannelName, &r.EPGID, &r.Title, &r.Subtitle,
		&r.Description, &r.Episode, &r.ProgramStart, &r.ProgramStop, &r.StartAt, &r.StopAt, &r.Status, &r.Path, &r.Size,
		&r.Error, &r.CreatedAt, &r.ItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) queryRecordings(where string, args ...any) ([]*Recording, error) {
	rows, err := s.db.Query(`SELECT `+recordingCols+recordingFrom+` `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Recording{}
	for rows.Next() {
		r, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRecording schedules a program; a program already scheduled on the
// same channel returns the existing recording instead of a duplicate.
func (s *Store) CreateRecording(r *Recording) (*Recording, error) {
	var rule any
	if r.RuleID != 0 {
		rule = r.RuleID
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO recordings (user_id, rule_id, channel_id, channel_name, epg_id, title, subtitle, description,
			episode, program_start, program_stop, start_at, stop_at, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UserID, rule, r.ChannelID, r.ChannelName, r.EPGID, r.Title, r.Subtitle, r.Description, r.Episode,
		r.ProgramStart, r.ProgramStop, r.StartAt, r.StopAt, r.Status, now())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return scanRecording(s.db.QueryRow(`SELECT `+recordingCols+recordingFrom+` WHERE r.channel_id = ? AND r.program_start = ?`, r.ChannelID, r.ProgramStart))
	}
	id, _ := res.LastInsertId()
	return s.Recording(id)
}

func (s *Store) Recording(id int64) (*Recording, error) {
	return scanRecording(s.db.QueryRow(`SELECT `+recordingCols+recordingFrom+` WHERE r.id = ?`, id))
}

// Recordings lists recordings, newest first for finished ones and soonest
// first for upcoming ones; userID 0 means everyone's.
func (s *Store) Recordings(userID int64) ([]*Recording, error) {
	return s.queryRecordings(`WHERE (? = 0 OR r.user_id = ?)
		ORDER BY r.status IN ('completed','failed','cancelled'), CASE WHEN r.status IN ('completed','failed','cancelled') THEN -r.start_at ELSE r.start_at END`,
		userID, userID)
}

// DueRecordings are scheduled recordings whose window has started.
func (s *Store) DueRecordings(now int64) ([]*Recording, error) {
	return s.queryRecordings(`WHERE r.status = 'scheduled' AND r.start_at <= ? AND r.stop_at > ?`, now, now)
}

// MissRecordings fails scheduled recordings whose window passed (server was off).
func (s *Store) MissRecordings(now int64) error {
	_, err := s.db.Exec(`UPDATE recordings SET status = 'failed', error = 'missed: the server was not running' WHERE status = 'scheduled' AND stop_at <= ?`, now)
	return err
}

// ResetInterrupted marks recordings left "recording" by a restart as failed.
func (s *Store) ResetInterrupted() error {
	_, err := s.db.Exec(`UPDATE recordings SET status = 'failed', error = 'interrupted: the server restarted' WHERE status = 'recording'`)
	return err
}

func (s *Store) SetRecordingState(id int64, status, path string, size int64, errMsg string) error {
	_, err := s.db.Exec(`UPDATE recordings SET status = ?, path = ?, size = ?, error = ? WHERE id = ?`, status, path, size, errMsg, id)
	return err
}

func (s *Store) SetRecordingStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE recordings SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) DeleteRecording(id int64) error {
	_, err := s.db.Exec(`DELETE FROM recordings WHERE id = ?`, id)
	return err
}

// RecordingStatuses maps "epgID:start" → status for programs in a window, so
// the guide can mark what is set to record.
func (s *Store) RecordingStatuses(from, to int64) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT epg_id, program_start, status FROM recordings
		WHERE program_start < ? AND program_stop > ? AND status IN ('pending','scheduled','recording','completed')`, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var epg, status string
		var start int64
		if err := rows.Scan(&epg, &start, &status); err != nil {
			return nil, err
		}
		out[epg+":"+strconv.FormatInt(start, 10)] = status
	}
	return out, rows.Err()
}

type RecordingRule struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId"`
	Username    string `json:"username,omitempty"`
	ChannelID   int64  `json:"channelId,omitempty"`
	ChannelName string `json:"channelName,omitempty"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"createdAt"`
	Upcoming    int    `json:"upcoming"`
}

func (s *Store) CreateRule(r *RecordingRule) (*RecordingRule, error) {
	var ch any
	if r.ChannelID != 0 {
		ch = r.ChannelID
	}
	// One rule per title and channel per user.
	var existing int64
	err := s.db.QueryRow(`SELECT id FROM recording_rules WHERE user_id = ? AND title = ? COLLATE NOCASE AND COALESCE(channel_id, 0) = ?`,
		r.UserID, r.Title, r.ChannelID).Scan(&existing)
	if err == nil {
		return s.Rule(existing)
	}
	res, err := s.db.Exec(`INSERT INTO recording_rules (user_id, channel_id, title, status, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.UserID, ch, r.Title, r.Status, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Rule(id)
}

const ruleSelect = `SELECT rr.id, rr.user_id, COALESCE(u.username, ''), COALESCE(rr.channel_id, 0), COALESCE(c.name, ''), rr.title, rr.status, rr.created_at,
	(SELECT COUNT(*) FROM recordings r WHERE r.rule_id = rr.id AND r.status IN ('pending','scheduled','recording'))
	FROM recording_rules rr LEFT JOIN users u ON u.id = rr.user_id LEFT JOIN channels c ON c.id = rr.channel_id`

func scanRule(row interface{ Scan(...any) error }) (*RecordingRule, error) {
	var r RecordingRule
	err := row.Scan(&r.ID, &r.UserID, &r.Username, &r.ChannelID, &r.ChannelName, &r.Title, &r.Status, &r.CreatedAt, &r.Upcoming)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) Rule(id int64) (*RecordingRule, error) {
	return scanRule(s.db.QueryRow(ruleSelect+` WHERE rr.id = ?`, id))
}

// Rules lists series rules; userID 0 means everyone's, status "" means any.
func (s *Store) Rules(userID int64, status string) ([]*RecordingRule, error) {
	rows, err := s.db.Query(ruleSelect+` WHERE (? = 0 OR rr.user_id = ?) AND (? = '' OR rr.status = ?) ORDER BY rr.title COLLATE NOCASE`,
		userID, userID, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*RecordingRule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SetRuleStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE recording_rules SET status = ? WHERE id = ?`, status, id)
	return err
}

// DeleteRule removes a rule and cancels its recordings that haven't started.
func (s *Store) DeleteRule(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM recordings WHERE rule_id = ? AND status IN ('pending','scheduled')`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recording_rules WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ProgramByID returns one guide entry.
func (s *Store) ProgramByID(id int64) (*GuideProgram, error) {
	var p GuideProgram
	err := s.db.QueryRow(`SELECT id, xmltv_id, start_at, stop_at, title, subtitle, description, category, icon, episode FROM epg_programs WHERE id = ?`, id).
		Scan(&p.ID, &p.EPGID, &p.Start, &p.Stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Icon, &p.Episode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// RuleMatch is an upcoming airing of a rule's title on a channel.
type RuleMatch struct {
	Program     GuideProgram
	ChannelID   int64
	ChannelName string
}

// UpcomingAirings finds programs titled title (any case) starting in
// [from, to), on channelID or, when 0, on every enabled channel with a guide.
func (s *Store) UpcomingAirings(title string, channelID, from, to int64) ([]RuleMatch, error) {
	rows, err := s.db.Query(`SELECT p.id, p.xmltv_id, p.start_at, p.stop_at, p.title, p.subtitle, p.description, p.category, p.icon, p.episode,
			c.id, c.name
		FROM epg_programs p JOIN channels c ON c.epg_id = p.xmltv_id AND c.enabled = 1
		WHERE p.title = ? COLLATE NOCASE AND p.start_at >= ? AND p.start_at < ? AND (? = 0 OR c.id = ?)
		GROUP BY p.xmltv_id, p.start_at ORDER BY p.start_at`, strings.TrimSpace(title), from, to, channelID, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuleMatch
	for rows.Next() {
		var m RuleMatch
		p := &m.Program
		if err := rows.Scan(&p.ID, &p.EPGID, &p.Start, &p.Stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Icon, &p.Episode,
			&m.ChannelID, &m.ChannelName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
