package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

type Stream struct {
	Index    int    `json:"index"`
	Type     string `json:"type"` // video | audio | subtitle
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
}

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

func (s *Store) FilesForItem(itemID int64) ([]*MediaFile, error) {
	rows, err := s.db.Query(`SELECT `+fileCols+` FROM media_files WHERE item_id = ? ORDER BY height DESC, size DESC`, itemID)
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
	for _, q := range []string{
		`DELETE FROM items WHERE library_id = ? AND kind IN ('movie','episode') AND NOT EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = items.id)`,
		`DELETE FROM items WHERE library_id = ? AND kind = 'season' AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = items.id)`,
		`DELETE FROM items WHERE library_id = ? AND kind = 'show' AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = items.id)`,
	} {
		if _, err := tx.Exec(q, libraryID); err != nil {
			return 0, err
		}
	}
	return removed, tx.Commit()
}
