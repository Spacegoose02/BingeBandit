package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// LiveSource is an M3U playlist or XMLTV guide provider.
type LiveSource struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"` // m3u | epg
	Name         string `json:"name"`
	URL          string `json:"url"`
	UserAgent    string `json:"userAgent"`
	MaxStreams   int    `json:"maxStreams"`
	RefreshHours int    `json:"refreshHours"`
	Enabled      bool   `json:"enabled"`
	LastRefresh  int64  `json:"lastRefresh"`
	LastError    string `json:"lastError,omitempty"`
	EntryCount   int    `json:"entryCount"`
	CreatedAt    int64  `json:"createdAt"`
}

const liveSourceCols = `id, kind, name, url, user_agent, max_streams, refresh_hours, enabled, last_refresh, last_error, entry_count, created_at`

func scanLiveSource(r interface{ Scan(...any) error }) (*LiveSource, error) {
	var s LiveSource
	err := r.Scan(&s.ID, &s.Kind, &s.Name, &s.URL, &s.UserAgent, &s.MaxStreams, &s.RefreshHours, &s.Enabled,
		&s.LastRefresh, &s.LastError, &s.EntryCount, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (s *Store) LiveSources(kind string) ([]*LiveSource, error) {
	rows, err := s.db.Query(`SELECT `+liveSourceCols+` FROM live_sources WHERE (? = '' OR kind = ?) ORDER BY kind, name COLLATE NOCASE`, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LiveSource{}
	for rows.Next() {
		src, err := scanLiveSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) LiveSource(id int64) (*LiveSource, error) {
	return scanLiveSource(s.db.QueryRow(`SELECT `+liveSourceCols+` FROM live_sources WHERE id = ?`, id))
}

func (s *Store) SaveLiveSource(src *LiveSource) error {
	if src.ID == 0 {
		src.CreatedAt = now()
		res, err := s.db.Exec(`INSERT INTO live_sources (kind, name, url, user_agent, max_streams, refresh_hours, enabled, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, src.Kind, src.Name, src.URL, src.UserAgent, src.MaxStreams, src.RefreshHours, boolInt(src.Enabled), src.CreatedAt)
		if err != nil {
			return err
		}
		src.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.Exec(`UPDATE live_sources SET name = ?, url = ?, user_agent = ?, max_streams = ?, refresh_hours = ?, enabled = ? WHERE id = ?`,
		src.Name, src.URL, src.UserAgent, src.MaxStreams, src.RefreshHours, boolInt(src.Enabled), src.ID)
	return err
}

func (s *Store) DeleteLiveSource(id int64) error {
	_, err := s.db.Exec(`DELETE FROM live_sources WHERE id = ?`, id)
	return err
}

func (s *Store) SetSourceRefreshed(id int64, count int, refreshErr error) error {
	msg := ""
	if refreshErr != nil {
		msg = refreshErr.Error()
		_, err := s.db.Exec(`UPDATE live_sources SET last_refresh = ?, last_error = ? WHERE id = ?`, now(), msg, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE live_sources SET last_refresh = ?, last_error = '', entry_count = ? WHERE id = ?`, now(), count, id)
	return err
}

// StreamEntry is one stream from a playlist, as imported.
type StreamEntry struct {
	Name, URL, Logo, Group, TvgID, TvgChno string
}

// StreamImport upserts a playlist's streams in one transaction. Streams the
// playlist no longer lists are removed on Finish (and drop out of channels).
type StreamImport struct {
	tx       *sql.Tx
	stmt     *sql.Stmt
	sourceID int64
	gen      int64
	count    int
}

func (s *Store) BeginStreamImport(sourceID int64) (*StreamImport, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	stmt, err := tx.Prepare(`INSERT INTO live_streams (source_id, name, url, logo, grp, tvg_id, tvg_chno, seen_gen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source_id, url) DO UPDATE SET name = excluded.name, logo = excluded.logo, grp = excluded.grp,
			tvg_id = excluded.tvg_id, tvg_chno = excluded.tvg_chno, seen_gen = excluded.seen_gen`)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &StreamImport{tx: tx, stmt: stmt, sourceID: sourceID, gen: now()}, nil
}

func (im *StreamImport) Add(e StreamEntry) error {
	im.count++
	_, err := im.stmt.Exec(im.sourceID, e.Name, e.URL, e.Logo, e.Group, e.TvgID, e.TvgChno, im.gen)
	return err
}

func (im *StreamImport) Abort() { im.tx.Rollback() }

func (im *StreamImport) Finish() (int, error) {
	defer im.tx.Rollback()
	if _, err := im.tx.Exec(`DELETE FROM live_streams WHERE source_id = ? AND seen_gen != ?`, im.sourceID, im.gen); err != nil {
		return 0, err
	}
	return im.count, im.tx.Commit()
}

type LiveStream struct {
	ID         int64   `json:"id"`
	SourceID   int64   `json:"sourceId"`
	SourceName string  `json:"sourceName"`
	Name       string  `json:"name"`
	URL        string  `json:"url"`
	Logo       string  `json:"logo,omitempty"`
	Group      string  `json:"group,omitempty"`
	TvgID      string  `json:"tvgId,omitempty"`
	TvgChno    string  `json:"tvgChno,omitempty"`
	ChannelIDs []int64 `json:"channelIds,omitempty"` // channels already using it
	UserAgent  string  `json:"-"`
	MaxStreams int     `json:"-"`
}

type StreamQuery struct {
	SourceID int64
	Group    string
	Search   string
	Offset   int
	Limit    int
}

func (s *Store) LiveStreams(q StreamQuery) ([]*LiveStream, int, error) {
	where := `WHERE (? = 0 OR ls.source_id = ?) AND (? = '' OR ls.grp = ?) AND (? = '' OR ls.name LIKE ? ESCAPE '\')`
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q.Search) + "%"
	args := []any{q.SourceID, q.SourceID, q.Group, q.Group, q.Search, like}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM live_streams ls `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT ls.id, ls.source_id, src.name, ls.name, ls.url, ls.logo, ls.grp, ls.tvg_id, ls.tvg_chno,
			COALESCE((SELECT group_concat(cs.channel_id) FROM channel_streams cs WHERE cs.stream_id = ls.id), '')
		FROM live_streams ls JOIN live_sources src ON src.id = ls.source_id `+where+`
		ORDER BY ls.grp COLLATE NOCASE, ls.name COLLATE NOCASE LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*LiveStream{}
	for rows.Next() {
		var ls LiveStream
		var chans string
		if err := rows.Scan(&ls.ID, &ls.SourceID, &ls.SourceName, &ls.Name, &ls.URL, &ls.Logo, &ls.Group, &ls.TvgID, &ls.TvgChno, &chans); err != nil {
			return nil, 0, err
		}
		for _, c := range strings.Split(chans, ",") {
			if id, err := strconv.ParseInt(c, 10, 64); err == nil {
				ls.ChannelIDs = append(ls.ChannelIDs, id)
			}
		}
		out = append(out, &ls)
	}
	return out, total, rows.Err()
}

type GroupCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (s *Store) StreamGroups(sourceID int64) ([]GroupCount, error) {
	rows, err := s.db.Query(`SELECT grp, COUNT(*) FROM live_streams WHERE (? = 0 OR source_id = ?) GROUP BY grp ORDER BY grp COLLATE NOCASE`, sourceID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GroupCount{}
	for rows.Next() {
		var g GroupCount
		if err := rows.Scan(&g.Name, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GuideImport replaces one guide source's channels and programs atomically.
type GuideImport struct {
	tx            *sql.Tx
	chStmt, pStmt *sql.Stmt
	sourceID      int64
	count         int
}

func (s *Store) BeginGuideImport(sourceID int64) (*GuideImport, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	for _, q := range []string{`DELETE FROM epg_channels WHERE source_id = ?`, `DELETE FROM epg_programs WHERE source_id = ?`} {
		if _, err := tx.Exec(q, sourceID); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	ch, err := tx.Prepare(`INSERT OR REPLACE INTO epg_channels (source_id, xmltv_id, display_name, icon) VALUES (?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	p, err := tx.Prepare(`INSERT INTO epg_programs (source_id, xmltv_id, start_at, stop_at, title, subtitle, description, category, icon, episode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &GuideImport{tx: tx, chStmt: ch, pStmt: p, sourceID: sourceID}, nil
}

func (g *GuideImport) Channel(id, name, icon string) error {
	_, err := g.chStmt.Exec(g.sourceID, id, name, icon)
	return err
}

func (g *GuideImport) Program(p *GuideProgram) error {
	g.count++
	_, err := g.pStmt.Exec(g.sourceID, p.EPGID, p.Start, p.Stop, p.Title, p.Subtitle, p.Description, p.Category, p.Icon, p.Episode)
	return err
}

func (g *GuideImport) Abort() { g.tx.Rollback() }

func (g *GuideImport) Finish() (int, error) {
	defer g.tx.Rollback()
	return g.count, g.tx.Commit()
}

type EPGChannel struct {
	SourceID    int64  `json:"sourceId"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon,omitempty"`
}

func (s *Store) EPGChannels(search string, limit int) ([]EPGChannel, error) {
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search) + "%"
	rows, err := s.db.Query(`SELECT source_id, xmltv_id, display_name, icon FROM epg_channels
		WHERE ? = '' OR display_name LIKE ? ESCAPE '\' OR xmltv_id LIKE ? ESCAPE '\'
		ORDER BY display_name COLLATE NOCASE LIMIT ?`, search, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EPGChannel{}
	for rows.Next() {
		var c EPGChannel
		if err := rows.Scan(&c.SourceID, &c.ID, &c.DisplayName, &c.Icon); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type GuideProgram struct {
	ID          int64  `json:"id"`
	EPGID       string `json:"epgId"`
	Start       int64  `json:"start"`
	Stop        int64  `json:"stop"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Episode     string `json:"episode,omitempty"`
}

// Programs lists guide entries overlapping [from, to) for the given XMLTV ids.
func (s *Store) Programs(epgIDs []string, from, to int64) ([]GuideProgram, error) {
	if len(epgIDs) == 0 {
		return []GuideProgram{}, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(epgIDs)), ",")
	args := []any{}
	for _, id := range epgIDs {
		args = append(args, id)
	}
	args = append(args, to, from)
	rows, err := s.db.Query(`SELECT MIN(id), xmltv_id, start_at, MAX(stop_at), title, subtitle, description, category, icon, episode
		FROM epg_programs WHERE xmltv_id IN (`+ph+`) AND start_at < ? AND stop_at > ?
		GROUP BY xmltv_id, start_at ORDER BY xmltv_id, start_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GuideProgram{}
	for rows.Next() {
		var p GuideProgram
		if err := rows.Scan(&p.ID, &p.EPGID, &p.Start, &p.Stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Icon, &p.Episode); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PruneProgramsBefore(t int64) error {
	_, err := s.db.Exec(`DELETE FROM epg_programs WHERE stop_at < ?`, t)
	return err
}

// ChannelStream is one stream behind a channel, in fallback order.
type ChannelStream struct {
	StreamID   int64  `json:"streamId"`
	Name       string `json:"name"`
	SourceID   int64  `json:"sourceId"`
	SourceName string `json:"sourceName"`
	URL        string `json:"url,omitempty"`
	UserAgent  string `json:"-"`
	MaxStreams int    `json:"-"`
}

type Channel struct {
	ID       int64           `json:"id"`
	Number   float64         `json:"number"`
	Name     string          `json:"name"`
	Logo     string          `json:"logo,omitempty"`
	Group    string          `json:"group,omitempty"`
	EPGID    string          `json:"epgId,omitempty"`
	Enabled  bool            `json:"enabled"`
	Favorite bool            `json:"favorite"`
	Streams  []ChannelStream `json:"streams,omitempty"`
}

// Channels lists channels in number order, with each one's fallback streams
// and the user's favourites. Disabled channels are only included for admins.
func (s *Store) Channels(uid int64, includeDisabled bool) ([]*Channel, error) {
	rows, err := s.db.Query(`SELECT c.id, c.number, c.name, c.logo, c.grp, c.epg_id, c.enabled,
			EXISTS (SELECT 1 FROM channel_favorites f WHERE f.channel_id = c.id AND f.user_id = ?)
		FROM channels c WHERE ? OR c.enabled = 1 ORDER BY c.number, c.name COLLATE NOCASE`, uid, includeDisabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Channel{}
	byID := map[int64]*Channel{}
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Number, &c.Name, &c.Logo, &c.Group, &c.EPGID, &c.Enabled, &c.Favorite); err != nil {
			return nil, err
		}
		out = append(out, &c)
		byID[c.ID] = &c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	all, err := s.channelStreams(0)
	if err != nil {
		return nil, err
	}
	for chID, list := range all {
		if c := byID[chID]; c != nil {
			c.Streams = list
		}
	}
	return out, nil
}

func (s *Store) Channel(uid, id int64) (*Channel, error) {
	var c Channel
	err := s.db.QueryRow(`SELECT c.id, c.number, c.name, c.logo, c.grp, c.epg_id, c.enabled,
			EXISTS (SELECT 1 FROM channel_favorites f WHERE f.channel_id = c.id AND f.user_id = ?)
		FROM channels c WHERE c.id = ?`, uid, id).
		Scan(&c.ID, &c.Number, &c.Name, &c.Logo, &c.Group, &c.EPGID, &c.Enabled, &c.Favorite)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	streams, err := s.channelStreams(id)
	c.Streams = streams[id]
	return &c, err
}

// channelStreams returns channel id → streams by priority (all channels when id is 0).
func (s *Store) channelStreams(channelID int64) (map[int64][]ChannelStream, error) {
	rows, err := s.db.Query(`SELECT cs.channel_id, ls.id, ls.name, src.id, src.name, ls.url, src.user_agent, src.max_streams
		FROM channel_streams cs JOIN live_streams ls ON ls.id = cs.stream_id JOIN live_sources src ON src.id = ls.source_id
		WHERE (? = 0 OR cs.channel_id = ?) ORDER BY cs.channel_id, cs.priority`, channelID, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]ChannelStream{}
	for rows.Next() {
		var ch int64
		var cs ChannelStream
		if err := rows.Scan(&ch, &cs.StreamID, &cs.Name, &cs.SourceID, &cs.SourceName, &cs.URL, &cs.UserAgent, &cs.MaxStreams); err != nil {
			return nil, err
		}
		out[ch] = append(out[ch], cs)
	}
	return out, rows.Err()
}

// CreateChannelsFromStreams makes one channel per stream, numbered from
// startNumber (or the playlist's tvg-chno when it is free), mapping the
// guide by tvg-id. Returns how many channels were created.
func (s *Store) CreateChannelsFromStreams(streamIDs []int64, startNumber float64) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	taken := map[float64]bool{}
	rows, err := tx.Query(`SELECT number FROM channels`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var n float64
		rows.Scan(&n)
		taken[n] = true
	}
	rows.Close()
	next := startNumber
	created := 0
	for _, sid := range streamIDs {
		var name, logo, grp, tvgID, chno string
		if err := tx.QueryRow(`SELECT name, logo, grp, tvg_id, tvg_chno FROM live_streams WHERE id = ?`, sid).
			Scan(&name, &logo, &grp, &tvgID, &chno); err != nil {
			return created, err
		}
		number, err := strconv.ParseFloat(chno, 64)
		if err != nil || number <= 0 || taken[number] {
			for taken[next] {
				next++
			}
			number = next
		}
		taken[number] = true
		res, err := tx.Exec(`INSERT INTO channels (number, name, logo, grp, epg_id, enabled, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
			number, name, logo, grp, tvgID, now())
		if err != nil {
			return created, err
		}
		chID, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO channel_streams (channel_id, stream_id, priority) VALUES (?, ?, 0)`, chID, sid); err != nil {
			return created, err
		}
		created++
	}
	return created, tx.Commit()
}

// UpdateChannel saves a channel's details and its fallback order.
func (s *Store) UpdateChannel(c *Channel, streamIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE channels SET number = ?, name = ?, logo = ?, grp = ?, epg_id = ?, enabled = ? WHERE id = ?`,
		c.Number, c.Name, c.Logo, c.Group, c.EPGID, boolInt(c.Enabled), c.ID); err != nil {
		return err
	}
	if streamIDs != nil {
		if _, err := tx.Exec(`DELETE FROM channel_streams WHERE channel_id = ?`, c.ID); err != nil {
			return err
		}
		for i, sid := range streamIDs {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO channel_streams (channel_id, stream_id, priority) VALUES (?, ?, ?)`, c.ID, sid, i); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteChannel(id int64) error {
	_, err := s.db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	return err
}

func (s *Store) SetChannelFavorite(uid, channelID int64, fav bool) error {
	var err error
	if fav {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO channel_favorites (user_id, channel_id) VALUES (?, ?)`, uid, channelID)
	} else {
		_, err = s.db.Exec(`DELETE FROM channel_favorites WHERE user_id = ? AND channel_id = ?`, uid, channelID)
	}
	return err
}
