package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Plugin is an installed plugin. The manifest is kept as raw JSON so the store
// doesn't need to know its shape; package plugins owns that.
type Plugin struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"` // managed | remote
	URL       string            `json:"url,omitempty"`
	ServerURL string            `json:"serverUrl,omitempty"`
	Enabled   bool              `json:"enabled"`
	Key       string            `json:"-"`
	Settings  map[string]string `json:"-"`
	Manifest  json.RawMessage   `json:"-"`
	AddedAt   int64             `json:"addedAt"`
}

const pluginCols = `id, kind, url, server_url, enabled, plugin_key, settings, manifest, added_at`

func scanPlugin(r interface{ Scan(...any) error }) (*Plugin, error) {
	var p Plugin
	var settings, manifest string
	err := r.Scan(&p.ID, &p.Kind, &p.URL, &p.ServerURL, &p.Enabled, &p.Key, &settings, &manifest, &p.AddedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Settings = map[string]string{}
	json.Unmarshal([]byte(settings), &p.Settings)
	p.Manifest = json.RawMessage(manifest)
	return &p, nil
}

// NewPluginKey makes the shared secret a plugin and BingeBandit use to call each other.
func NewPluginKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "bbp_" + hex.EncodeToString(b)
}

func (s *Store) Plugins() ([]*Plugin, error) {
	rows, err := s.db.Query(`SELECT ` + pluginCols + ` FROM plugins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Plugin{}
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Plugin(id string) (*Plugin, error) {
	return scanPlugin(s.db.QueryRow(`SELECT `+pluginCols+` FROM plugins WHERE id = ?`, id))
}

// PluginByKey finds the plugin calling the plugin API. Keys start with bbp_ so
// a user's token never matches.
func (s *Store) PluginByKey(key string) (*Plugin, error) {
	if !strings.HasPrefix(key, "bbp_") {
		return nil, ErrNotFound
	}
	return scanPlugin(s.db.QueryRow(`SELECT `+pluginCols+` FROM plugins WHERE plugin_key = ?`, key))
}

// AddPlugin inserts a plugin, or for a managed plugin found again on disk,
// leaves the existing row alone. New plugins start disabled unless enabled is set.
func (s *Store) AddPlugin(p *Plugin) error {
	if p.Key == "" {
		p.Key = NewPluginKey()
	}
	if p.Settings == nil {
		p.Settings = map[string]string{}
	}
	if len(p.Manifest) == 0 {
		p.Manifest = json.RawMessage("{}")
	}
	settings, _ := json.Marshal(p.Settings)
	p.AddedAt = now()
	_, err := s.db.Exec(`INSERT INTO plugins (`+pluginCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`,
		p.ID, p.Kind, p.URL, p.ServerURL, p.Enabled, p.Key, string(settings), string(p.Manifest), p.AddedAt)
	return err
}

// UpdateRemotePlugin re-points a remote plugin (re-adding it by URL) and rotates its key.
func (s *Store) UpdateRemotePlugin(id, url, serverURL, key string, manifest json.RawMessage) error {
	_, err := s.db.Exec(`UPDATE plugins SET url = ?, server_url = ?, plugin_key = ?, manifest = ? WHERE id = ? AND kind = 'remote'`,
		url, serverURL, key, string(manifest), id)
	return err
}

func (s *Store) SetPluginEnabled(id string, enabled bool) error {
	return s.execOne(`UPDATE plugins SET enabled = ? WHERE id = ?`, enabled, id)
}

func (s *Store) SetPluginSettings(id string, values map[string]string) error {
	b, _ := json.Marshal(values)
	return s.execOne(`UPDATE plugins SET settings = ? WHERE id = ?`, string(b), id)
}

func (s *Store) SetPluginManifest(id string, manifest json.RawMessage) error {
	_, err := s.db.Exec(`UPDATE plugins SET manifest = ? WHERE id = ?`, string(manifest), id)
	return err
}

func (s *Store) DeletePlugin(id string) error {
	return s.execOne(`DELETE FROM plugins WHERE id = ?`, id)
}

func (s *Store) execOne(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FindItems looks up movies and shows the way plugins know them: by TMDB id,
// or else by title (and year, when given). kind is "movie", "show" or "" for both.
func (s *Store) FindItems(uid int64, kind string, tmdbID int64, title string, year int) ([]*Item, error) {
	kinds := `i.kind IN ('movie','show')`
	if kind == "movie" || kind == "show" {
		kinds = `i.kind = @kind`
	}
	if tmdbID > 0 {
		return s.queryItems(itemSelect+` WHERE `+kinds+` AND `+visible+` AND i.tmdb_id = @tmdb ORDER BY i.id`,
			uidArg(uid), sql.Named("kind", kind), sql.Named("tmdb", tmdbID))
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return []*Item{}, nil
	}
	return s.queryItems(itemSelect+` WHERE `+kinds+` AND `+visible+`
		AND (i.title = @title COLLATE NOCASE OR i.original_title = @title COLLATE NOCASE)
		AND (@year = 0 OR i.year BETWEEN @year - 1 AND @year + 1)
		ORDER BY ABS(i.year - @year), i.id`,
		uidArg(uid), sql.Named("kind", kind), sql.Named("title", title), sql.Named("year", year))
}
