package store

import (
	"database/sql"
	"slices"
	"strings"
)

// LibraryFilter narrows a library's titles. The zero value matches everything.
type LibraryFilter struct {
	Genre     string  // one of the title's genres, any case
	YearFrom  int     // first year, 0 = any
	YearTo    int     // last year, 0 = any
	Unwatched bool    // movies not watched; shows with an episode left
	MinRating float64 // TMDB (or TheTVDB) score out of 10, 0 = any
}

// Active reports whether the filter leaves anything out.
func (f LibraryFilter) Active() bool { return f != LibraryFilter{} }

func (f LibraryFilter) where() string {
	var b strings.Builder
	if f.Genre != "" {
		b.WriteString(` AND EXISTS (SELECT 1 FROM json_each(i.genres) g WHERE g.value = @genre COLLATE NOCASE)`)
	}
	if f.YearFrom > 0 {
		b.WriteString(` AND i.year >= @yearfrom`)
	}
	if f.YearTo > 0 {
		b.WriteString(` AND i.year > 0 AND i.year <= @yearto`)
	}
	if f.MinRating > 0 {
		b.WriteString(` AND i.rating >= @minrating`)
	}
	if f.Unwatched {
		b.WriteString(` AND CASE i.kind
			WHEN 'show' THEN EXISTS (SELECT 1 FROM items e WHERE e.show_id = i.id AND e.kind = 'episode'
				AND NOT EXISTS (SELECT 1 FROM watch_state x WHERE x.item_id = e.id AND x.user_id = @uid AND x.watched = 1))
			ELSE NOT EXISTS (SELECT 1 FROM watch_state x WHERE x.item_id = i.id AND x.user_id = @uid AND x.watched = 1) END`)
	}
	return b.String()
}

func (f LibraryFilter) args() []any {
	return []any{sql.Named("genre", f.Genre), sql.Named("yearfrom", f.YearFrom), sql.Named("yearto", f.YearTo), sql.Named("minrating", f.MinRating)}
}

// FilterOptions are the choices a library's filter bar offers.
type FilterOptions struct {
	Genres  []string `json:"genres"`
	Decades []int    `json:"decades"` // newest first, e.g. 2020, 2010
}

// LibraryFilterOptions lists the genres and decades a library's movies and
// shows have.
func (s *Store) LibraryFilterOptions(libraryID int64) (*FilterOptions, error) {
	genres, err := s.queryStrings(`SELECT DISTINCT j.value FROM items i, json_each(i.genres) j
		WHERE i.library_id = ? AND i.kind IN ('movie','show') AND j.value != '' ORDER BY 1 COLLATE NOCASE`, libraryID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT DISTINCT (year / 10) * 10 FROM items
		WHERE library_id = ? AND kind IN ('movie','show') AND year > 0 ORDER BY 1 DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &FilterOptions{Genres: genres, Decades: []int{}}
	for rows.Next() {
		var d int
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out.Decades = append(out.Decades, d)
	}
	return out, rows.Err()
}

// Person is someone in the cast or crew of titles the user can see.
type Person struct {
	Name   string         `json:"name"`
	Photo  string         `json:"photo,omitempty"`
	Titles []PersonCredit `json:"titles"`
}

// PersonCredit is one of a person's titles and what they did in it.
type PersonCredit struct {
	Item *Item  `json:"item"`
	Role string `json:"role"` // their characters if they acted in it, else their jobs, joined with ", "
	Kind string `json:"kind"` // cast if they acted in it at all, else crew
}

// PersonByName finds the movies and shows the user can see that credit
// someone by name (credits carry no ids), newest first. ErrNotFound when
// there are none.
func (s *Store) PersonByName(uid int64, name string) (*Person, error) {
	items, err := s.queryItems(itemSelect+` WHERE i.kind IN ('movie','show') AND `+visible+`
		AND EXISTS (SELECT 1 FROM json_each(i.credits) c WHERE json_extract(c.value, '$.name') = @name)
		ORDER BY i.year DESC, i.release_date DESC, i.sort_title COLLATE NOCASE`,
		uidArg(uid), sql.Named("name", name))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	p := &Person{Name: name, Titles: []PersonCredit{}}
	for _, it := range items {
		pc := PersonCredit{Item: it, Kind: "crew"}
		var parts, jobs []string
		for _, c := range it.Credits {
			if c.Name != name {
				continue
			}
			if p.Photo == "" {
				p.Photo = c.Photo
			}
			if c.Kind == "cast" {
				pc.Kind = "cast"
				if c.Role != "" && !slices.Contains(parts, c.Role) {
					parts = append(parts, c.Role)
				}
			} else if c.Role != "" && !slices.Contains(jobs, c.Role) {
				jobs = append(jobs, c.Role)
			}
		}
		// Someone who acted in it and produced it is listed for the part.
		if pc.Kind == "cast" {
			pc.Role = strings.Join(parts, ", ")
		} else {
			pc.Role = strings.Join(jobs, ", ")
		}
		it.Credits = nil
		p.Titles = append(p.Titles, pc)
	}
	return p, nil
}
