package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Tautulli's history covers every account and keeps working after Plex is
// gone. It has titles and season/episode numbers but no TMDB ids, so matching
// is by title and year.

// watchedPercent is Tautulli's default "watched" threshold.
const watchedPercent = 85

// TautulliAPI reads history through Tautulli's HTTP API.
type TautulliAPI struct {
	BaseURL string
	APIKey  string
	client  *http.Client
	names   map[string]string // user_id → username
}

func NewTautulliAPI(baseURL, apiKey string) *TautulliAPI {
	return &TautulliAPI{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, client: &http.Client{Timeout: 60 * time.Second}}
}

func (t *TautulliAPI) Kind() string { return "tautulli" }

func (t *TautulliAPI) call(ctx context.Context, cmd string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("apikey", t.APIKey)
	q.Set("cmd", cmd)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.BaseURL+"/api/v2?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Tautulli at %s: %w", t.BaseURL, err)
	}
	defer resp.Body.Close()
	var env struct {
		Response struct {
			Result  string          `json:"result"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("Tautulli answered %s with something that isn't its API (check the URL): %w", resp.Status, err)
	}
	if env.Response.Result != "success" {
		msg := env.Response.Message
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("Tautulli: %s", msg)
	}
	return json.Unmarshal(env.Response.Data, out)
}

// flexInt reads numbers Tautulli sometimes sends as strings.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil // ignore junk rather than failing the whole page
	}
	*f = flexInt(n)
	return nil
}

func (t *TautulliAPI) loadUsers(ctx context.Context) ([]SourceUser, error) {
	var users []struct {
		UserID   flexInt `json:"user_id"`
		Username string  `json:"username"`
		Friendly string  `json:"friendly_name"`
		Email    string  `json:"email"`
	}
	if err := t.call(ctx, "get_users", nil, &users); err != nil {
		return nil, err
	}
	t.names = map[string]string{}
	var out []SourceUser
	for _, u := range users {
		name := u.Username
		if name == "" {
			name = u.Friendly
		}
		if name == "" || name == "Local" {
			continue
		}
		t.names[strconv.FormatInt(int64(u.UserID), 10)] = name
		out = append(out, SourceUser{Name: name, Email: u.Email})
	}
	return out, nil
}

func (t *TautulliAPI) Users(ctx context.Context) ([]SourceUser, error) { return t.loadUsers(ctx) }

type tautulliRow struct {
	ID               flexInt `json:"id"`
	RowID            flexInt `json:"row_id"`
	Date             flexInt `json:"date"`
	Started          flexInt `json:"started"`
	Stopped          flexInt `json:"stopped"`
	PlayDuration     flexInt `json:"play_duration"`
	Duration         flexInt `json:"duration"`
	UserID           flexInt `json:"user_id"`
	User             string  `json:"user"`
	MediaType        string  `json:"media_type"`
	Title            string  `json:"title"`
	GrandparentTitle string  `json:"grandparent_title"`
	Year             flexInt `json:"year"`
	MediaIndex       flexInt `json:"media_index"`
	ParentMediaIndex flexInt `json:"parent_media_index"`
	Percent          flexInt `json:"percent_complete"`
	Watched          float64 `json:"watched_status"`
	Player           string  `json:"player"`
	Product          string  `json:"product"`
}

const tautulliPage = 1000

func (t *TautulliAPI) Records(ctx context.Context, _ bool, emit func(Record) error, progress func(string, int, int)) error {
	if t.names == nil {
		if _, err := t.loadUsers(ctx); err != nil {
			return err
		}
	}
	for start := 0; ; start += tautulliPage {
		var page struct {
			Total flexInt       `json:"recordsFiltered"`
			Data  []tautulliRow `json:"data"`
		}
		q := url.Values{"start": {strconv.Itoa(start)}, "length": {strconv.Itoa(tautulliPage)}, "grouping": {"0"},
			"order_column": {"date"}, "order_dir": {"desc"}}
		if err := t.call(ctx, "get_history", q, &page); err != nil {
			return err
		}
		for i, row := range page.Data {
			progress("Reading Tautulli history", start+i+1, int(page.Total))
			if row.MediaType != "movie" && row.MediaType != "episode" {
				continue
			}
			id := row.ID
			if id == 0 {
				id = row.RowID
			}
			user := t.names[strconv.FormatInt(int64(row.UserID), 10)]
			if user == "" {
				user = row.User
			}
			played := int64(row.PlayDuration)
			if played == 0 {
				played = int64(row.Duration)
			}
			r := Record{
				Key: "tautulli:" + strconv.FormatInt(int64(id), 10), User: user, Kind: row.MediaType, Title: row.Title,
				StartedAt: int64(row.Started), ViewedAt: int64(row.Stopped), Played: played,
				Completed: row.Watched >= 1 || row.Percent >= watchedPercent, Client: firstNonEmpty(row.Player, row.Product),
			}
			if r.ViewedAt == 0 {
				r.ViewedAt = int64(row.Date)
			}
			if row.MediaType == "episode" {
				r.ShowTitle, r.Season, r.Episode = row.GrandparentTitle, int(row.ParentMediaIndex), int(row.MediaIndex)
			} else {
				r.Year = int(row.Year)
			}
			if err := emit(r); err != nil {
				return err
			}
		}
		if len(page.Data) < tautulliPage {
			return nil
		}
	}
}

func (t *TautulliAPI) Collections(context.Context, func(Collection) error) error { return nil }

// TautulliDB reads a copy of tautulli.db directly — the way to bring history
// over once Plex and Tautulli are switched off.
type TautulliDB struct {
	Path string
}

func (t *TautulliDB) Kind() string { return "tautulli-db" }

func (t *TautulliDB) open() (*sql.DB, error) {
	if _, err := os.Stat(t.Path); err != nil {
		return nil, fmt.Errorf("can't read %s: %w", t.Path, err)
	}
	db, err := sql.Open("sqlite", "file:"+filepathToURI(t.Path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	for table, cols := range map[string][]string{
		"session_history":          {"id", "started", "stopped", "user_id", "user", "media_type", "paused_counter", "view_offset", "player"},
		"session_history_metadata": {"id", "title", "grandparent_title", "media_index", "parent_media_index", "year", "duration"},
		"users":                    {"user_id", "username"},
	} {
		have := map[string]bool{}
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("%s doesn't look like a Tautulli database: %w", t.Path, err)
		}
		for rows.Next() {
			var n string
			rows.Scan(&n)
			have[n] = true
		}
		rows.Close()
		for _, c := range cols {
			if !have[c] {
				db.Close()
				return nil, fmt.Errorf("%s doesn't look like a Tautulli database (table %s has no %s column)", t.Path, table, c)
			}
		}
	}
	return db, nil
}

// filepathToURI keeps Windows paths working inside a file: URI.
func filepathToURI(p string) string {
	return strings.NewReplacer("?", "%3f", "#", "%23").Replace(strings.ReplaceAll(p, `\`, "/"))
}

func (t *TautulliDB) Users(ctx context.Context) ([]SourceUser, error) {
	db, err := t.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(NULLIF(u.username, ''), MAX(h.user)) AS name, COALESCE(u.email, ''), COUNT(*) FROM session_history h
		LEFT JOIN users u ON u.user_id = h.user_id
		WHERE h.media_type IN ('movie', 'episode') GROUP BY h.user_id ORDER BY 3 DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceUser
	for rows.Next() {
		var u SourceUser
		if err := rows.Scan(&u.Name, &u.Email, &u.Plays); err != nil {
			return nil, err
		}
		if u.Name != "" {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}

func (t *TautulliDB) Records(ctx context.Context, _ bool, emit func(Record) error, progress func(string, int, int)) error {
	db, err := t.open()
	if err != nil {
		return err
	}
	defer db.Close()
	var total int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_history WHERE media_type IN ('movie', 'episode')`).Scan(&total)
	rows, err := db.QueryContext(ctx, `SELECT h.id, h.started, h.stopped, COALESCE(NULLIF(u.username, ''), h.user), h.media_type,
			COALESCE(h.paused_counter, 0), COALESCE(h.view_offset, 0), COALESCE(h.player, ''),
			COALESCE(m.title, ''), COALESCE(m.grandparent_title, ''), COALESCE(m.media_index, 0), COALESCE(m.parent_media_index, 0),
			COALESCE(m.year, 0), COALESCE(m.duration, 0)
		FROM session_history h JOIN session_history_metadata m ON m.id = h.id
		LEFT JOIN users u ON u.user_id = h.user_id
		WHERE h.media_type IN ('movie', 'episode') ORDER BY h.stopped DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var (
			id, started, stopped, paused, offsetMS, durationMS int64
			user, kind, player, title, show                    string
			ep, season, year                                   int
		)
		if err := rows.Scan(&id, &started, &stopped, &user, &kind, &paused, &offsetMS, &player, &title, &show, &ep, &season, &year, &durationMS); err != nil {
			return err
		}
		n++
		progress("Reading the Tautulli database", n, total)
		r := Record{
			Key: "tautulli:" + strconv.FormatInt(id, 10), User: user, Kind: kind, Title: title,
			StartedAt: started, ViewedAt: stopped, Played: max(stopped-started-paused, 0),
			Duration: float64(durationMS) / 1000, Client: player,
			Completed: durationMS > 0 && offsetMS*100 >= durationMS*watchedPercent,
		}
		if kind == "episode" {
			r.ShowTitle, r.Season, r.Episode = show, season, ep
		} else {
			r.Year = year
		}
		if err := emit(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (t *TautulliDB) Collections(context.Context, func(Collection) error) error { return nil }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
