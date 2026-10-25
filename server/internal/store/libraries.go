package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

type Library struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"` // movies | shows
	Paths     []string `json:"paths"`
	CreatedAt int64    `json:"createdAt"`
	LastScan  int64    `json:"lastScan"`
	ItemCount int      `json:"itemCount"`
	// SkipMarkers turns off intro and credits detection for this library.
	SkipMarkers bool `json:"skipMarkers"`
	// CollectionMode is how the Library tab shows collections: "show" them
	// alongside all titles, "hide" them, or "collapse" titles into them.
	CollectionMode string `json:"collectionMode"`
	// Pinned is per user: the library is in their sidebar and feeds Home.
	Pinned bool `json:"pinned"`
}

const libraryCols = `l.id, l.name, l.kind, l.paths, l.created_at, l.last_scan,
	(SELECT COUNT(*) FROM items i WHERE i.library_id = l.id AND i.kind IN ('movie','show')), l.skip_markers, l.collection_mode`

func scanLibrary(r interface{ Scan(...any) error }) (*Library, error) {
	var l Library
	var paths string
	if err := r.Scan(&l.ID, &l.Name, &l.Kind, &paths, &l.CreatedAt, &l.LastScan, &l.ItemCount, &l.SkipMarkers, &l.CollectionMode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	json.Unmarshal([]byte(paths), &l.Paths)
	if l.Paths == nil {
		l.Paths = []string{}
	}
	return &l, nil
}

func (s *Store) Libraries() ([]*Library, error) {
	rows, err := s.db.Query(`SELECT ` + libraryCols + ` FROM libraries l ORDER BY l.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Library{}
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) Library(id int64) (*Library, error) {
	return scanLibrary(s.db.QueryRow(`SELECT `+libraryCols+` FROM libraries l WHERE l.id = ?`, id))
}

func (s *Store) CreateLibrary(l *Library) error {
	paths, _ := json.Marshal(l.Paths)
	l.CreatedAt = now()
	if l.CollectionMode == "" {
		l.CollectionMode = "show"
	}
	res, err := s.db.Exec(`INSERT INTO libraries (name, kind, paths, created_at, skip_markers, collection_mode) VALUES (?, ?, ?, ?, ?, ?)`,
		l.Name, l.Kind, string(paths), l.CreatedAt, l.SkipMarkers, l.CollectionMode)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateLibrary(l *Library) error {
	paths, _ := json.Marshal(l.Paths)
	_, err := s.db.Exec(`UPDATE libraries SET name = ?, paths = ?, skip_markers = ?, collection_mode = ? WHERE id = ?`,
		l.Name, string(paths), l.SkipMarkers, l.CollectionMode, l.ID)
	return err
}

func (s *Store) DeleteLibrary(id int64) error {
	_, err := s.db.Exec(`DELETE FROM libraries WHERE id = ?`, id)
	return err
}

func (s *Store) SetLibraryScanned(id int64) error {
	_, err := s.db.Exec(`UPDATE libraries SET last_scan = ? WHERE id = ?`, now(), id)
	return err
}
