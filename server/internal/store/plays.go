package store

import (
	"strings"
	"time"
)

// Play is one viewing session in the history.
type Play struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"userId"`
	Username  string `json:"username,omitempty"`
	ItemID    int64  `json:"itemId,omitempty"` // 0 once the item left the library
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Kind      string `json:"kind"`
	LibraryID int64  `json:"libraryId"`
	ShowID    int64  `json:"showId,omitempty"`
	StartedAt int64  `json:"startedAt"`
	StoppedAt int64  `json:"stoppedAt"`
	Watched   int64  `json:"watched"` // seconds
	Completed bool   `json:"completed"`
	Mode      string `json:"mode"`
	Client    string `json:"client"`
	IP        string `json:"ip,omitempty"`
	Poster    string `json:"poster,omitempty"`
}

func (s *Store) StartPlay(p *Play) (int64, error) {
	var item any
	if p.ItemID != 0 {
		item = p.ItemID
	}
	res, err := s.db.Exec(`INSERT INTO plays (user_id, item_id, title, subtitle, kind, library_id, show_id, started_at, stopped_at,
			watched, completed, mode, client, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, item, p.Title, p.Subtitle, p.Kind, p.LibraryID, p.ShowID, p.StartedAt, p.StoppedAt,
		p.Watched, boolInt(p.Completed), p.Mode, p.Client, p.IP)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePlay(id, stoppedAt, watched int64, completed bool, mode string) error {
	_, err := s.db.Exec(`UPDATE plays SET stopped_at = ?, watched = ?, completed = MAX(completed, ?), mode = ? WHERE id = ?`,
		stoppedAt, watched, boolInt(completed), mode, id)
	return err
}

// History lists plays, newest first; userID 0 means everyone.
func (s *Store) History(userID int64, limit, offset int) ([]Play, error) {
	rows, err := s.db.Query(`SELECT p.id, p.user_id, COALESCE(u.username, ''), COALESCE(p.item_id, 0), p.title, p.subtitle, p.kind,
			p.library_id, p.show_id, p.started_at, p.stopped_at, p.watched, p.completed, p.mode, p.client, p.ip,
			COALESCE(NULLIF(sh.poster, ''), it.poster, '')
		FROM plays p LEFT JOIN users u ON u.id = p.user_id
		LEFT JOIN items it ON it.id = p.item_id LEFT JOIN items sh ON sh.id = NULLIF(p.show_id, 0)
		WHERE (? = 0 OR p.user_id = ?) ORDER BY p.started_at DESC LIMIT ? OFFSET ?`, userID, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Play{}
	for rows.Next() {
		var p Play
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.ItemID, &p.Title, &p.Subtitle, &p.Kind, &p.LibraryID, &p.ShowID,
			&p.StartedAt, &p.StoppedAt, &p.Watched, &p.Completed, &p.Mode, &p.Client, &p.IP, &p.Poster); err != nil {
			return nil, err
		}
		p.Poster = ImageURL(p.Poster)
		out = append(out, p)
	}
	return out, rows.Err()
}

type Count struct {
	Key    string  `json:"key"`
	ItemID int64   `json:"itemId,omitempty"`
	Poster string  `json:"poster,omitempty"`
	Plays  int     `json:"plays"`
	Hours  float64 `json:"hours"`
}

func (s *Store) counts(q string, args ...any) ([]Count, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Count{}
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Key, &c.ItemID, &c.Poster, &c.Plays, &c.Hours); err != nil {
			return nil, err
		}
		c.Poster = ImageURL(c.Poster)
		out = append(out, c)
	}
	return out, rows.Err()
}

type UserStats struct {
	Hours        float64 `json:"hours"`
	Plays        int     `json:"plays"`
	Movies       int     `json:"movies"`   // distinct movies finished
	Episodes     int     `json:"episodes"` // distinct episodes finished
	Shows        int     `json:"shows"`    // distinct shows watched
	TopGenres    []Count `json:"topGenres"`
	TopShows     []Count `json:"topShows"`
	MonthlyHours []Count `json:"monthlyHours"` // last 12 months, oldest first
	FavoriteHour int     `json:"favoriteHour"` // hour of day with most plays, -1 if none
}

func (s *Store) UserStats(uid int64) (*UserStats, error) {
	var st UserStats
	err := s.db.QueryRow(`SELECT COALESCE(SUM(watched), 0) / 3600.0, COUNT(*),
		COUNT(DISTINCT CASE WHEN kind = 'movie' AND completed = 1 THEN item_id END),
		COUNT(DISTINCT CASE WHEN kind = 'episode' AND completed = 1 THEN item_id END),
		COUNT(DISTINCT NULLIF(show_id, 0))
		FROM plays WHERE user_id = ?`, uid).Scan(&st.Hours, &st.Plays, &st.Movies, &st.Episodes, &st.Shows)
	if err != nil {
		return nil, err
	}
	if st.TopGenres, err = s.counts(`SELECT j.value, 0, '', COUNT(*), SUM(p.watched) / 3600.0
		FROM plays p JOIN items i ON i.id = COALESCE(NULLIF(p.show_id, 0), p.item_id), json_each(i.genres) j
		WHERE p.user_id = ? GROUP BY j.value ORDER BY 4 DESC LIMIT 6`, uid); err != nil {
		return nil, err
	}
	if st.TopShows, err = s.counts(`SELECT sh.title, sh.id, sh.poster, COUNT(*), SUM(p.watched) / 3600.0
		FROM plays p JOIN items sh ON sh.id = p.show_id WHERE p.user_id = ? GROUP BY sh.id ORDER BY 5 DESC LIMIT 6`, uid); err != nil {
		return nil, err
	}
	since := time.Now().AddDate(0, -11, 0)
	since = time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.Local)
	monthly, err := s.counts(`SELECT strftime('%Y-%m', started_at, 'unixepoch', 'localtime'), 0, '', COUNT(*), SUM(watched) / 3600.0
		FROM plays WHERE user_id = ? AND started_at >= ? GROUP BY 1 ORDER BY 1`, uid, since.Unix())
	if err != nil {
		return nil, err
	}
	st.MonthlyHours = fillMonths(monthly, since)
	st.FavoriteHour = -1
	s.db.QueryRow(`SELECT CAST(strftime('%H', started_at, 'unixepoch', 'localtime') AS INTEGER) FROM plays WHERE user_id = ?
		GROUP BY 1 ORDER BY COUNT(*) DESC LIMIT 1`, uid).Scan(&st.FavoriteHour)
	return &st, nil
}

// fillMonths returns 12 months starting at since, with zeros where there were no plays.
func fillMonths(rows []Count, since time.Time) []Count {
	by := map[string]Count{}
	for _, r := range rows {
		by[r.Key] = r
	}
	out := make([]Count, 0, 12)
	for i := 0; i < 12; i++ {
		k := since.AddDate(0, i, 0).Format("2006-01")
		c := by[k]
		c.Key = k
		out = append(out, c)
	}
	return out
}

type ServerStats struct {
	Days       int     `json:"days"`
	Daily      []Count `json:"daily"` // one per day, oldest first
	TopMovies  []Count `json:"topMovies"`
	TopShows   []Count `json:"topShows"`
	TopUsers   []Count `json:"topUsers"`
	Modes      []Count `json:"modes"`
	Clients    []Count `json:"clients"`
	TotalPlays int     `json:"totalPlays"`
	TotalHours float64 `json:"totalHours"`
}

func (s *Store) ServerStats(days int) (*ServerStats, error) {
	today := time.Now()
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -(days - 1))
	since := start.Unix()
	st := &ServerStats{Days: days}
	var err error
	daily, err := s.counts(`SELECT strftime('%Y-%m-%d', started_at, 'unixepoch', 'localtime'), 0, '', COUNT(*), SUM(watched) / 3600.0
		FROM plays WHERE started_at >= ? GROUP BY 1 ORDER BY 1`, since)
	if err != nil {
		return nil, err
	}
	by := map[string]Count{}
	for _, d := range daily {
		by[d.Key] = d
		st.TotalPlays += d.Plays
		st.TotalHours += d.Hours
	}
	for i := 0; i < days; i++ {
		k := start.AddDate(0, 0, i).Format("2006-01-02")
		c := by[k]
		c.Key = k
		st.Daily = append(st.Daily, c)
	}
	queries := []struct {
		dst *[]Count
		q   string
	}{
		{&st.TopMovies, `SELECT p.title, COALESCE(p.item_id, 0), COALESCE(it.poster, ''), COUNT(*), SUM(p.watched) / 3600.0
			FROM plays p LEFT JOIN items it ON it.id = p.item_id WHERE p.kind = 'movie' AND p.started_at >= ?
			GROUP BY COALESCE(p.item_id, p.title) ORDER BY 4 DESC, 5 DESC LIMIT 8`},
		{&st.TopShows, `SELECT sh.title, sh.id, sh.poster, COUNT(*), SUM(p.watched) / 3600.0
			FROM plays p JOIN items sh ON sh.id = p.show_id WHERE p.started_at >= ? GROUP BY sh.id ORDER BY 4 DESC, 5 DESC LIMIT 8`},
		{&st.TopUsers, `SELECT COALESCE(u.username, '(deleted)'), 0, '', COUNT(*), SUM(p.watched) / 3600.0
			FROM plays p LEFT JOIN users u ON u.id = p.user_id WHERE p.started_at >= ? GROUP BY p.user_id ORDER BY 5 DESC LIMIT 8`},
		{&st.Modes, `SELECT CASE WHEN mode = 'direct' THEN 'Direct Play' ELSE 'Transcode' END, 0, '', COUNT(*), SUM(watched) / 3600.0
			FROM plays WHERE started_at >= ? GROUP BY 1 ORDER BY 4 DESC`},
		{&st.Clients, `SELECT COALESCE(NULLIF(client, ''), 'Unknown'), 0, '', COUNT(*), SUM(watched) / 3600.0
			FROM plays WHERE started_at >= ? GROUP BY 1 ORDER BY 4 DESC LIMIT 8`},
	}
	for _, q := range queries {
		if *q.dst, err = s.counts(q.q, since); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// NormalizeName trims a user-supplied name (playlists etc.).
func NormalizeName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}
