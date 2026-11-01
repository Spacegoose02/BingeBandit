package store

import (
	"fmt"
	"strings"
)

// Rating limits: a user or profile can be kept to titles rated up to a
// level (MaxRating). Levels follow the US movie and TV ratings the metadata
// providers give; anything else (unrated, NR, a foreign rating) counts as
// unknown, and unknown movies and shows are hidden under a limit. Seasons and
// episodes go by their own rating, else the show's. Titles a parent approved
// for a kid one by one or through a collection are shown whatever their
// rating: the parent picked them. Music, audiobooks and photos aren't rated
// and aren't affected.

// RatingLevels lists the limits, lowest first; a level's number is its
// index + 1.
var RatingLevels = []struct {
	Name    string   `json:"name"`
	Ratings []string `json:"ratings"`
}{
	{"G / TV-Y", []string{"G", "TV-Y", "TV-G"}},
	{"TV-Y7", []string{"TV-Y7", "TV-Y7-FV"}},
	{"PG / TV-PG", []string{"PG", "TV-PG"}},
	{"PG-13 / TV-14", []string{"PG-13", "TV-14"}},
	{"R / TV-MA", []string{"R", "TV-MA"}},
	{"NC-17", []string{"NC-17"}},
}

// RatingLevel is the level of a rating as stored on an item, 0 if unknown.
func RatingLevel(rating string) int {
	r := normRating(rating)
	for i, l := range RatingLevels {
		for _, x := range l.Ratings {
			if r == x {
				return i + 1
			}
		}
	}
	return 0
}

// normRating drops the prefixes NFO files and some agents add
// ("Rated PG-13", "US:PG-13", "us/TV-14").
func normRating(r string) string {
	r = strings.ToUpper(strings.TrimSpace(r))
	r = strings.TrimPrefix(r, "RATED ")
	for _, p := range []string{"US:", "US/", "US-"} {
		r = strings.TrimPrefix(r, p)
	}
	return strings.TrimSpace(r)
}

// ratingLevelSQL is RatingLevel in SQL, for the rating expression r.
func ratingLevelSQL(r string) string {
	n := `TRIM(UPPER(TRIM(` + r + `)))`
	n = `CASE WHEN ` + n + ` LIKE 'RATED %' THEN TRIM(SUBSTR(` + n + `, 7)) ELSE ` + n + ` END`
	n = `CASE WHEN SUBSTR(` + n + `, 1, 3) IN ('US:', 'US/', 'US-') THEN TRIM(SUBSTR(` + n + `, 4)) ELSE ` + n + ` END`
	var b strings.Builder
	b.WriteString(`(CASE ` + n)
	for i, l := range RatingLevels {
		for _, x := range l.Ratings {
			fmt.Fprintf(&b, ` WHEN '%s' THEN %d`, x, i+1)
		}
	}
	b.WriteString(` ELSE 0 END)`)
	return b.String()
}

// ratingAllowed is a condition on items i for the user bound to @uid: true
// when the item's rating is within their limit. A profile is also held to
// its account's limit, so an account an admin limited can't get around it
// with a new profile. Admins have no limit.
var ratingAllowed = `(i.kind NOT IN ('movie','show','season','episode')
	OR COALESCE((SELECT MIN(ru.max_rating) FROM users ru WHERE ru.max_rating > 0 AND ru.is_admin = 0
		AND ru.id IN (@uid, (SELECT COALESCE(ou.owner_id, 0) FROM users ou WHERE ou.id = @uid))), 0) = 0
	OR ` + ratingLevelSQL(`COALESCE(NULLIF(i.content_rating, ''), (SELECT rs.content_rating FROM items rs WHERE rs.id = i.show_id), '')`) + `
		BETWEEN 1 AND (SELECT MIN(ru.max_rating) FROM users ru WHERE ru.max_rating > 0 AND ru.is_admin = 0
		AND ru.id IN (@uid, (SELECT COALESCE(ou.owner_id, 0) FROM users ou WHERE ou.id = @uid))))`

// SetMaxRating sets a user's rating limit; 0 removes it.
func (s *Store) SetMaxRating(userID int64, level int) error {
	if level < 0 || level > len(RatingLevels) {
		return fmt.Errorf("rating level %d out of range", level)
	}
	res, err := s.db.Exec(`UPDATE users SET max_rating = ? WHERE id = ?`, level, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
