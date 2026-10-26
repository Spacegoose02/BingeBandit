package store

import (
	"bingebandit/internal/sports"
)

// MaxSportsTeams is how many teams one person can follow.
const MaxSportsTeams = 40

// SportsTeams are the teams a user follows, in their order.
func (s *Store) SportsTeams(userID int64) ([]sports.Team, error) {
	rows, err := s.db.Query(`SELECT league, team_id, name, short, abbr, logo, color, alt_color FROM sports_teams
		WHERE user_id = ? ORDER BY position, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sports.Team{}
	for rows.Next() {
		var t sports.Team
		if err := rows.Scan(&t.League, &t.ID, &t.Name, &t.Short, &t.Abbr, &t.Logo, &t.Color, &t.AltColor); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetSportsTeams replaces the teams a user follows.
func (s *Store) SetSportsTeams(userID int64, teams []sports.Team) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sports_teams WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for i, t := range teams {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO sports_teams (user_id, league, team_id, name, short, abbr, logo, color, alt_color, position)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, t.League, t.ID, t.Name, t.Short, t.Abbr, t.Logo, t.Color, t.AltColor, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SportsFan is someone who follows a team.
type SportsFan struct {
	UserID   int64  `json:"userId"`
	Username string `json:"user"`
	Name     string `json:"name"` // the profile name, else the username
	League   string `json:"league"`
	TeamID   string `json:"teamId"`
}

// SportsFans lists who follows which team, for every team anyone follows.
func (s *Store) SportsFans() ([]SportsFan, error) {
	rows, err := s.db.Query(`SELECT t.user_id, u.username, COALESCE(NULLIF(u.profile_name, ''), u.username), t.league, t.team_id
		FROM sports_teams t JOIN users u ON u.id = t.user_id ORDER BY t.user_id, t.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportsFan
	for rows.Next() {
		var f SportsFan
		if err := rows.Scan(&f.UserID, &f.Username, &f.Name, &f.League, &f.TeamID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MarkSportsAlert records an alert about a game, and reports false when it
// was already sent.
func (s *Store) MarkSportsAlert(userID int64, gameID, kind string) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO sports_alerts (user_id, game_id, kind, created_at) VALUES (?, ?, ?, ?)`,
		userID, gameID, kind, now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PruneSportsAlerts forgets alerts older than two weeks.
func (s *Store) PruneSportsAlerts() error {
	_, err := s.db.Exec(`DELETE FROM sports_alerts WHERE created_at < ?`, now()-14*24*3600)
	return err
}
