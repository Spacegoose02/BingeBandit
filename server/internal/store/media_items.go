package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Music, audiobook and photo items. They reuse the item tree movies and
// shows use: parent_id is the album (or audiobook, or photo album) and
// show_id the artist (or the audiobook itself), season_number is a track's
// disc and episode_number its track number (or a chapter's number). Studio
// holds a track's or album's artist and an audiobook's author, since those
// kinds have no studio. They never get TMDB metadata: their details come
// from the files' own tags, set by SetTaggedDetails.

// AudioKinds are the item kinds that play as audio.
var AudioKinds = []string{"track", "chapter"}

// IsAudioKind says whether an item kind plays as audio (a track or chapter).
func IsAudioKind(kind string) bool { return kind == "track" || kind == "chapter" }

// HasCover says whether an item kind's artwork comes from its files (the
// cover endpoint) rather than a metadata provider.
func HasCover(kind string) bool {
	switch kind {
	case "artist", "album", "track", "audiobook", "chapter", "photoalbum", "photo":
		return true
	}
	return false
}

// CoverURL is where the apps load the artwork of an item that has a cover.
func CoverURL(id int64) string { return fmt.Sprintf("/api/items/%d/cover", id) }

// TaggedDetails are what a file's tags say about its item.
type TaggedDetails struct {
	Title       string
	Year        int
	ReleaseDate string // YYYY-MM-DD, or ""
	Genres      []string
	Artist      string // stored in studio
	Runtime     int    // minutes
}

// SetTaggedDetails saves an item's details from its files' tags and marks it
// as having metadata, so nothing looks it up elsewhere. Fields an admin
// locked keep their value; empty ones keep what's there.
func (s *Store) SetTaggedDetails(id int64, d TaggedDetails) error {
	genres := ""
	if len(d.Genres) > 0 {
		b, _ := json.Marshal(d.Genres)
		genres = string(b)
	}
	keep := func(col string, fields ...string) string {
		return fmt.Sprintf(`%s = CASE WHEN ? = '' OR ? = '0' OR EXISTS (SELECT 1 FROM json_each(locked) WHERE value IN ('%s')) THEN %s ELSE ? END`,
			col, strings.Join(fields, "','"), col)
	}
	var sets []string
	var args []any
	add := func(col string, v any, fields ...string) {
		sets = append(sets, keep(col, fields...))
		str := fmt.Sprint(v)
		args = append(args, str, str, v)
	}
	add("title", d.Title, "title")
	add("sort_title", SortTitle(d.Title), "title", "sortTitle")
	add("year", d.Year, "releaseDate")
	add("release_date", d.ReleaseDate, "releaseDate")
	add("genres", genres, "genres")
	add("studio", d.Artist, "studio")
	add("runtime", d.Runtime, "runtime")
	args = append(args, now(), id)
	_, err := s.db.Exec(`UPDATE items SET `+strings.Join(sets, ", ")+`, metadata_at = ? WHERE id = ?`, args...)
	return err
}

// TrackFile is the file an audio item or photo plays or shows.
type TrackFile struct {
	FileID     int64   `json:"fileId"`
	Duration   float64 `json:"duration"`
	Container  string  `json:"container"`
	AudioCodec string  `json:"audioCodec,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	Size       int64   `json:"size"`
}

// Track is a playable item with its file, for the web's audio player queue
// and photo viewer.
type Track struct {
	Item *Item     `json:"item"`
	File TrackFile `json:"file"`
	// Album is a track's album (or a chapter's book, a photo's album).
	Album string `json:"album,omitempty"`
}

// trackOrder plays an artist's albums oldest first, then each album by disc
// and track (chapters by number), then by title.
const trackOrder = `COALESCE(alb.year, 0), COALESCE(alb.sort_title, '') COLLATE NOCASE, alb.id,
	COALESCE(i.season_number, 0), COALESCE(i.episode_number, 0), i.sort_title COLLATE NOCASE, i.id`

// Tracks lists what playing an item plays, in order: an artist's tracks
// album by album, an album's or audiobook's tracks or chapters, a photo
// album's photos, or a track, chapter or photo itself.
func (s *Store) Tracks(uid, id int64) ([]Track, error) {
	rows, err := s.db.Query(`SELECT i.id, f.id, f.duration, f.container, f.audio_codec, f.width, f.height, f.size, COALESCE(alb.title, '')
		FROM items i
		JOIN media_files f ON f.id = (SELECT MIN(mf.id) FROM media_files mf WHERE mf.item_id = i.id)
		LEFT JOIN items alb ON alb.id = i.parent_id
		WHERE i.kind IN ('track','chapter','photo') AND (i.id = ?1 OR i.parent_id = ?1 OR i.show_id = ?1)
		ORDER BY `+trackOrder, id)
	if err != nil {
		return nil, err
	}
	var ids []int64
	files := map[int64]TrackFile{}
	albums := map[int64]string{}
	for rows.Next() {
		var itemID int64
		var f TrackFile
		var album string
		if err := rows.Scan(&itemID, &f.FileID, &f.Duration, &f.Container, &f.AudioCodec, &f.Width, &f.Height, &f.Size, &album); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, itemID)
		files[itemID] = f
		albums[itemID] = album
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Track{}
	if len(ids) == 0 {
		return out, nil
	}
	list, _ := json.Marshal(ids)
	items, err := s.queryItems(listSelect+` WHERE i.id IN (SELECT value FROM json_each(@ids)) AND `+visible, uidArg(uid), sql.Named("ids", string(list)))
	if err != nil {
		return nil, err
	}
	byID := map[int64]*Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, id := range ids {
		if it := byID[id]; it != nil {
			out = append(out, Track{Item: it, File: files[id], Album: albums[id]})
		}
	}
	return out, nil
}

// CoverSources are the files an item's cover can come from, best first: its
// own file (a track, chapter or photo), then the first few files of its
// album, book or artist (their folder may have a cover.jpg; any of them may
// have a picture inside), and whether they are photos.
func (s *Store) CoverSources(id int64) (paths []string, photo bool, err error) {
	var kind string
	var parent sql.NullInt64
	if err := s.db.QueryRow(`SELECT kind, parent_id FROM items WHERE id = ?`, id).Scan(&kind, &parent); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	group := id
	if (kind == "track" || kind == "chapter" || kind == "photo") && parent.Valid {
		group = parent.Int64
	}
	rows, err := s.db.Query(`SELECT f.path, i.kind FROM items i JOIN media_files f ON f.item_id = i.id
		LEFT JOIN items alb ON alb.id = i.parent_id
		WHERE i.kind IN ('track','chapter','photo') AND (i.parent_id = ?1 OR i.show_id = ?1 OR i.id = ?2)
		ORDER BY i.id = ?2 DESC, `+trackOrder+` LIMIT 8`, group, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path, &kind); err != nil {
			return nil, false, err
		}
		paths = append(paths, path)
		photo = kind == "photo"
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(paths) == 0 {
		return nil, false, ErrNotFound
	}
	return paths, photo, nil
}

// ContinueListening lists audiobooks the user is partway through, latest
// first: some chapter started or finished, and some chapter not finished.
func (s *Store) ContinueListening(uid int64, limit int) ([]*Item, error) {
	last := `(SELECT MAX(x.updated_at) FROM watch_state x JOIN items c ON c.id = x.item_id
		WHERE c.parent_id = i.id AND x.user_id = @uid AND (x.watched = 1 OR x.position > 15))`
	return s.queryItems(itemSelect+` WHERE i.kind = 'audiobook' AND `+visible+` AND `+pinned+`
		AND `+last+` IS NOT NULL
		AND EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id
			AND NOT EXISTS (SELECT 1 FROM watch_state x WHERE x.item_id = c.id AND x.user_id = @uid AND x.watched = 1))
		ORDER BY `+last+` DESC LIMIT @limit`, uidArg(uid), sql.Named("limit", limit))
}

// SearchKinds is Search over the given item kinds.
func (s *Store) SearchKinds(uid int64, q string, kinds []string, limit int) ([]*Item, error) {
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	list, _ := json.Marshal(kinds)
	return s.queryItems(itemSelect+` WHERE i.kind IN (SELECT value FROM json_each(@kinds)) AND `+visible+`
		AND (i.title LIKE @q ESCAPE '\' OR i.original_title LIKE @q ESCAPE '\' OR (i.kind IN ('album','audiobook') AND i.studio LIKE @q ESCAPE '\'))
		ORDER BY i.sort_title COLLATE NOCASE LIMIT @limit`, uidArg(uid), sql.Named("q", like), sql.Named("kinds", string(list)), sql.Named("limit", limit))
}

// SetPhotoAlbumYear raises a photo album's year to a newer photo's.
func (s *Store) SetPhotoAlbumYear(id int64, year int) error {
	_, err := s.db.Exec(`UPDATE items SET year = MAX(year, ?), metadata_at = ? WHERE id = ?`, year, now(), id)
	return err
}
