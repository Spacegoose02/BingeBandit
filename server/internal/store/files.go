package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

type Stream struct {
	Index           int    `json:"index"`
	Type            string `json:"type"` // video | audio | subtitle
	Codec           string `json:"codec"`
	Language        string `json:"language,omitempty"`
	Title           string `json:"title,omitempty"`
	Channels        int    `json:"channels,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	Default         bool   `json:"default,omitempty"`
	Forced          bool   `json:"forced,omitempty"`
	HearingImpaired bool   `json:"hearingImpaired,omitempty"`
	// Range is a video stream's dynamic range: sdr, hdr10, hlg or dv (Dolby
	// Vision). Empty: probed before BingeBandit looked for it.
	Range string `json:"range,omitempty"`
	// Profile is ffprobe's codec profile, which names DTS-HD MA, DTS:X
	// and Atmos (e.g. "Dolby TrueHD + Dolby Atmos").
	Profile string `json:"profile,omitempty"`
	// External is the file name of a subtitle stored next to the video
	// (e.g. "Movie.en.srt"); empty for streams inside the file. External
	// streams get indexes from ExternalIndexBase up.
	External string `json:"external,omitempty"`
}

// ExternalIndexBase keeps sidecar subtitle indexes clear of embedded ones.
const ExternalIndexBase = 1000

type MediaFile struct {
	ID            int64    `json:"id"`
	ItemID        int64    `json:"itemId"`
	Path          string   `json:"path"`
	Size          int64    `json:"size"`
	MTime         int64    `json:"-"`
	Container     string   `json:"container"`
	Duration      float64  `json:"duration"`
	Bitrate       int64    `json:"bitrate"`
	VideoCodec    string   `json:"videoCodec"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	BitDepth      int      `json:"bitDepth"`
	HDR           bool     `json:"hdr"`
	AudioCodec    string   `json:"audioCodec"`
	AudioChannels int      `json:"audioChannels"`
	Streams       []Stream `json:"streams"`
	ProbedAt      int64    `json:"-"`
}

const fileCols = `id, item_id, path, size, mtime, container, duration, bitrate, video_codec, width, height, bit_depth, hdr,
	audio_codec, audio_channels, streams, probed_at`

func scanFile(r interface{ Scan(...any) error }) (*MediaFile, error) {
	var f MediaFile
	var streams string
	err := r.Scan(&f.ID, &f.ItemID, &f.Path, &f.Size, &f.MTime, &f.Container, &f.Duration, &f.Bitrate, &f.VideoCodec,
		&f.Width, &f.Height, &f.BitDepth, &f.HDR, &f.AudioCodec, &f.AudioChannels, &streams, &f.ProbedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(streams), &f.Streams)
	if f.Streams == nil {
		f.Streams = []Stream{}
	}
	return &f, nil
}

func (s *Store) File(id int64) (*MediaFile, error) {
	return scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM media_files WHERE id = ?`, id))
}

func (s *Store) FileByPath(path string) (*MediaFile, error) {
	return scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM media_files WHERE path = ?`, path))
}

// itemFiles matches the files of item ?1: its own, and a multi-episode file
// it shares with an earlier episode.
const itemFiles = `(item_id = ?1 OR id IN (SELECT file_id FROM media_file_episodes WHERE item_id = ?1))`

func (s *Store) FilesForItem(itemID int64) ([]*MediaFile, error) {
	rows, err := s.db.Query(`SELECT `+fileCols+` FROM media_files WHERE `+itemFiles+` ORDER BY height DESC, size DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*MediaFile{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpsertFile stores a (re)probed file and stamps it with the scan generation.
func (s *Store) UpsertFile(f *MediaFile, gen int64) error {
	streams, _ := json.Marshal(f.Streams)
	if f.Streams == nil {
		streams = []byte("[]")
	}
	_, err := s.db.Exec(`INSERT INTO media_files (item_id, path, size, mtime, container, duration, bitrate, video_codec, width, height,
			bit_depth, hdr, audio_codec, audio_channels, streams, probed_at, scan_gen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET item_id = excluded.item_id, size = excluded.size, mtime = excluded.mtime,
			container = excluded.container, duration = excluded.duration, bitrate = excluded.bitrate,
			video_codec = excluded.video_codec, width = excluded.width, height = excluded.height, bit_depth = excluded.bit_depth,
			hdr = excluded.hdr, audio_codec = excluded.audio_codec, audio_channels = excluded.audio_channels,
			streams = excluded.streams, probed_at = excluded.probed_at, scan_gen = excluded.scan_gen`,
		f.ItemID, f.Path, f.Size, f.MTime, f.Container, f.Duration, f.Bitrate, f.VideoCodec, f.Width, f.Height,
		f.BitDepth, boolInt(f.HDR), f.AudioCodec, f.AudioChannels, string(streams), f.ProbedAt, gen)
	return err
}

// SetFileEpisodes links a multi-episode file (by id, or by path when id is
// 0) to its later episodes, replacing the links it had.
func (s *Store) SetFileEpisodes(fileID int64, path string, itemIDs []int64) error {
	if fileID == 0 {
		if err := s.db.QueryRow(`SELECT id FROM media_files WHERE path = ?`, path).Scan(&fileID); err != nil {
			return err
		}
	}
	if len(itemIDs) == 0 {
		// The common case: nothing to link, and usually nothing to unlink.
		_, err := s.db.Exec(`DELETE FROM media_file_episodes WHERE file_id = ?`, fileID)
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM media_file_episodes WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	for _, id := range itemIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO media_file_episodes (file_id, item_id) VALUES (?, ?)`, fileID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SharedFileEpisodes lists the other episodes that play the same
// multi-episode file as itemID (none for an ordinary file).
func (s *Store) SharedFileEpisodes(itemID int64) ([]int64, error) {
	rows, err := s.db.Query(`WITH f AS (SELECT id FROM media_files WHERE `+itemFiles+`)
		SELECT item_id FROM media_files WHERE id IN f AND item_id != ?1
		UNION SELECT item_id FROM media_file_episodes WHERE file_id IN f AND item_id != ?1`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TouchFile marks an unchanged file as seen in this scan.
func (s *Store) TouchFile(id, itemID, gen int64) error {
	_, err := s.db.Exec(`UPDATE media_files SET item_id = ?, scan_gen = ? WHERE id = ?`, itemID, gen, id)
	return err
}

// PruneLibrary removes files not seen in scan generation gen, then any items
// left without media. Only call it when every library root was reachable.
func (s *Store) PruneLibrary(libraryID, gen int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM media_files WHERE scan_gen != ? AND item_id IN (SELECT id FROM items WHERE library_id = ?)`, gen, libraryID)
	if err != nil {
		return 0, err
	}
	removed, _ := res.RowsAffected()
	if err := deleteOrphans(tx, libraryID); err != nil {
		return 0, err
	}
	return removed, tx.Commit()
}

// PruneUnder is PruneLibrary limited to files below the given folders
// (each ending in a path separator), for partial scans.
func (s *Store) PruneUnder(libraryID, gen int64, prefixes []string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var removed int64
	for _, p := range prefixes {
		res, err := tx.Exec(`DELETE FROM media_files WHERE scan_gen != ? AND substr(path, 1, ?) = ?
			AND item_id IN (SELECT id FROM items WHERE library_id = ?)`, gen, utf8.RuneCountInString(p), p, libraryID) // substr counts characters
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		removed += n
	}
	if err := deleteOrphans(tx, libraryID); err != nil {
		return 0, err
	}
	return removed, tx.Commit()
}

// deleteOrphans removes movies/episodes without files, then empty seasons and shows.
func deleteOrphans(tx *sql.Tx, libraryID int64) error {
	for _, q := range []string{
		`DELETE FROM items WHERE library_id = ? AND kind IN ('movie','episode') AND NOT EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = items.id)
			AND NOT EXISTS (SELECT 1 FROM media_file_episodes l WHERE l.item_id = items.id)`,
		`DELETE FROM items WHERE library_id = ? AND kind = 'season' AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = items.id)`,
		`DELETE FROM items WHERE library_id = ? AND kind = 'show' AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = items.id)`,
	} {
		if _, err := tx.Exec(q, libraryID); err != nil {
			return err
		}
	}
	return nil
}

// FileSummary is a video file with the movie or episode it belongs to, for
// plugins that look over the whole library (such as upgrade finders).
type FileSummary struct {
	FileID     int64   `json:"fileId"`
	ItemID     int64   `json:"itemId"`
	LibraryID  int64   `json:"libraryId"`
	Kind       string  `json:"kind"` // movie | episode
	Title      string  `json:"title"`
	Year       int     `json:"year,omitempty"`
	TMDBID     int64   `json:"tmdbId,omitempty"`
	ShowID     int64   `json:"showId,omitempty"`
	ShowTitle  string  `json:"showTitle,omitempty"`
	ShowTMDBID int64   `json:"showTmdbId,omitempty"`
	Season     *int    `json:"season,omitempty"`
	Episode    *int    `json:"episode,omitempty"`
	Path       string  `json:"path"`
	Size       int64   `json:"size"`
	Duration   float64 `json:"duration"`
	Bitrate    int64   `json:"bitrate"`
	VideoCodec string  `json:"videoCodec"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	BitDepth   int     `json:"bitDepth"`
	HDR        bool    `json:"hdr"`
	AudioCodec string  `json:"audioCodec"`
}

// FileSummaries pages through every movie and episode file, by file id.
func (s *Store) FileSummaries(offset, limit int) ([]FileSummary, int, error) {
	const from = ` FROM media_files f JOIN items i ON i.id = f.item_id LEFT JOIN items sh ON sh.id = i.show_id
		WHERE i.kind IN ('movie', 'episode')`
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*)` + from).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT f.id, i.id, i.library_id, i.kind, i.title, i.year, i.tmdb_id,
		COALESCE(i.show_id, 0), COALESCE(sh.title, ''), COALESCE(sh.tmdb_id, 0), i.season_number, i.episode_number,
		f.path, f.size, f.duration, f.bitrate, f.video_codec, f.width, f.height, f.bit_depth, f.hdr, f.audio_codec`+from+`
		ORDER BY f.id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []FileSummary{}
	for rows.Next() {
		var f FileSummary
		var season, episode sql.NullInt64
		if err := rows.Scan(&f.FileID, &f.ItemID, &f.LibraryID, &f.Kind, &f.Title, &f.Year, &f.TMDBID,
			&f.ShowID, &f.ShowTitle, &f.ShowTMDBID, &season, &episode,
			&f.Path, &f.Size, &f.Duration, &f.Bitrate, &f.VideoCodec, &f.Width, &f.Height, &f.BitDepth, &f.HDR, &f.AudioCodec); err != nil {
			return nil, 0, err
		}
		if season.Valid {
			n := int(season.Int64)
			f.Season = &n
		}
		if episode.Valid {
			n := int(episode.Int64)
			f.Episode = &n
		}
		out = append(out, f)
	}
	return out, total, rows.Err()
}
