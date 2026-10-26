package store

import (
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync/atomic"
)

// PosterOverlay is an image an admin uploaded to draw on posters: their own
// resolution badge, a frame, a "New" ribbon. Condition says which titles get
// it (see overlay.Conditions).
type PosterOverlay struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Image     string `json:"image"` // local:/<file>; URL when sent to clients
	Position  string `json:"position"`
	Size      int    `json:"size"`   // % of the poster's width
	Margin    int    `json:"margin"` // % of the poster's width
	Condition string `json:"condition"`
	Kind      string `json:"kind"` // "" (movies and shows), movie or show
	LibraryID int64  `json:"libraryId"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt int64  `json:"updatedAt"`
}

const overlayCols = `id, name, image, position, size, margin, condition, kind, library_id, enabled, updated_at`

func scanOverlay(r interface{ Scan(...any) error }) (*PosterOverlay, error) {
	var o PosterOverlay
	err := r.Scan(&o.ID, &o.Name, &o.Image, &o.Position, &o.Size, &o.Margin, &o.Condition, &o.Kind, &o.LibraryID, &o.Enabled, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

// PosterOverlays lists uploaded overlays in drawing order (later ones on top).
func (s *Store) PosterOverlays() ([]*PosterOverlay, error) {
	rows, err := s.db.Query(`SELECT ` + overlayCols + ` FROM poster_overlays ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PosterOverlay{}
	for rows.Next() {
		o, err := scanOverlay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) PosterOverlay(id int64) (*PosterOverlay, error) {
	return scanOverlay(s.db.QueryRow(`SELECT `+overlayCols+` FROM poster_overlays WHERE id = ?`, id))
}

// SavePosterOverlay creates (ID 0) or updates an overlay. New ones go on top.
func (s *Store) SavePosterOverlay(o *PosterOverlay) error {
	o.UpdatedAt = now()
	if o.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO poster_overlays (name, image, position, size, margin, condition, kind, library_id, enabled, sort, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort), 0) + 1 FROM poster_overlays), ?)`,
			o.Name, o.Image, o.Position, o.Size, o.Margin, o.Condition, o.Kind, o.LibraryID, o.Enabled, o.UpdatedAt)
		if err != nil {
			return err
		}
		o.ID, _ = res.LastInsertId()
	} else {
		res, err := s.db.Exec(`UPDATE poster_overlays SET name = ?, image = ?, position = ?, size = ?, margin = ?, condition = ?, kind = ?,
			library_id = ?, enabled = ?, updated_at = ? WHERE id = ?`,
			o.Name, o.Image, o.Position, o.Size, o.Margin, o.Condition, o.Kind, o.LibraryID, o.Enabled, o.UpdatedAt, o.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	s.loadOverlayTag()
	return nil
}

func (s *Store) DeletePosterOverlay(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM poster_overlays WHERE id = ?`, id); err != nil {
		return err
	}
	s.loadOverlayTag()
	return nil
}

// OrderPosterOverlays saves the drawing order.
func (s *Store) OrderPosterOverlays(ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE poster_overlays SET sort = ?, updated_at = ? WHERE id = ?`, i, now(), id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.loadOverlayTag()
	return nil
}

// overlayTag is "" while overlays are off, else a short hash of everything
// that changes how posters are drawn. It goes into poster URLs, so clients
// fetch new posters when an admin changes the overlays. One store per
// process, so it lives at package level, where scanItem can see it.
var overlayTag atomic.Pointer[string]

func (s *Store) loadOverlayTag() {
	tag := ""
	if s.Setting(SettingOverlays) == "true" {
		h := fnv.New32a()
		for _, k := range []string{SettingOverlayResolution, SettingOverlayHDR, SettingOverlayAudio, SettingOverlayRatings} {
			fmt.Fprintf(h, "%s=%s;", k, s.Setting(k))
		}
		var n, latest int64
		s.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(updated_at), 0) FROM poster_overlays`).Scan(&n, &latest)
		fmt.Fprintf(h, "%d/%d", n, latest)
		tag = strconv.FormatUint(uint64(h.Sum32()), 36)
	}
	overlayTag.Store(&tag)
}

// OverlaysOn reports whether posters are served with overlays.
func OverlaysOn() bool {
	t := overlayTag.Load()
	return t != nil && *t != ""
}

// overlayPosterURL is where a movie's or show's poster is served with its
// overlays: /api/images/poster/<id>-<tag>.jpg. It stays under /api/images/
// so the apps still add ?w= for the size they want.
func overlayPosterURL(id int64, ref string) string {
	t := overlayTag.Load()
	if t == nil || *t == "" || ref == "" {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(ref))
	return fmt.Sprintf("/api/images/poster/%d-%s%s.jpg", id, *t, strconv.FormatUint(uint64(h.Sum32()), 36))
}

// OverlayFacts is what a title's overlays are worked out from: its best
// copy (for shows, across every episode), its scores and its labels.
type OverlayFacts struct {
	Item    *Item
	Ref     string       // stored poster reference (tmdb:/…, local:/…)
	Files   []*MediaFile // a movie's versions, or a show's episodes' files
	Ratings ExternalRatings
}

// OverlayFacts loads what OverlayFacts needs for a movie or show the user
// can see.
func (s *Store) OverlayFacts(uid, id int64) (*OverlayFacts, error) {
	it, err := s.Item(uid, id)
	if err != nil {
		return nil, err
	}
	if it.Kind != "movie" && it.Kind != "show" {
		return nil, ErrNotFound
	}
	f := &OverlayFacts{Item: it}
	if err := s.db.QueryRow(`SELECT poster FROM items WHERE id = ?`, id).Scan(&f.Ref); err != nil {
		return nil, err
	}
	q := `SELECT ` + fileCols + ` FROM media_files WHERE item_id = ?`
	if it.Kind == "show" {
		q = `SELECT ` + prefixCols("f.", fileCols) + ` FROM media_files f JOIN items e ON e.id = f.item_id WHERE e.show_id = ?`
	}
	rows, err := s.db.Query(q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		mf, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		f.Files = append(f.Files, mf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if r, err := s.ExternalRatings(id); err == nil {
		f.Ratings = *r
	}
	return f, nil
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// SamplePosterItem picks a movie (else a show) with a poster for previews,
// preferring one with the most files and scores to show off.
func (s *Store) SamplePosterItem() (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT i.id FROM items i WHERE i.kind IN ('movie', 'show') AND i.poster != ''
		ORDER BY i.kind = 'movie' DESC, EXISTS (SELECT 1 FROM external_ratings r WHERE r.item_id = i.id) DESC, i.rating DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// FilesNeedingRange lists analyzed video files whose video stream has no
// dynamic range yet: probed before BingeBandit looked for Dolby Vision,
// HDR10 and Atmos.
func (s *Store) FilesNeedingRange(limit int) ([]*MediaFile, error) {
	rows, err := s.db.Query(`SELECT `+fileCols+` FROM media_files WHERE probed_at > 0 AND video_codec != ''
		AND streams NOT LIKE '%"range":%' ORDER BY probed_at, id LIMIT ?`, limit)
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
