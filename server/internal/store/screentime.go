package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ScreenTime is a profile's viewing allowance: minutes a day Monday to
// Friday and at weekends (0: no limit), and a bedtime ("20:30") to a wake
// time ("07:00") with no watching in between. Times are the server's local
// time.
type ScreenTime struct {
	WeekdayMinutes int    `json:"weekdayMinutes"`
	WeekendMinutes int    `json:"weekendMinutes"`
	Bedtime        string `json:"bedtime"`
	Wake           string `json:"wake"`
}

// Any reports whether the rules limit anything.
func (t ScreenTime) Any() bool {
	return t.WeekdayMinutes > 0 || t.WeekendMinutes > 0 || (t.Bedtime != "" && t.Wake != "")
}

// ParseClock reads "HH:MM" as minutes after midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("use a time such as 20:30")
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Validate checks the rules.
func (t ScreenTime) Validate() error {
	if t.WeekdayMinutes < 0 || t.WeekdayMinutes > 24*60 || t.WeekendMinutes < 0 || t.WeekendMinutes > 24*60 {
		return errors.New("minutes a day must be between 0 and 1440")
	}
	if (t.Bedtime == "") != (t.Wake == "") {
		return errors.New("give both a bedtime and a wake time, or neither")
	}
	if t.Bedtime != "" {
		if _, err := ParseClock(t.Bedtime); err != nil {
			return err
		}
		if _, err := ParseClock(t.Wake); err != nil {
			return err
		}
		if t.Bedtime == t.Wake {
			return errors.New("bedtime and wake time can't be the same")
		}
	}
	return nil
}

// Allowance is the minutes allowed on the day of now (0: no limit).
func (t ScreenTime) Allowance(now time.Time) int {
	if wd := now.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return t.WeekendMinutes
	}
	return t.WeekdayMinutes
}

// InBedtime reports whether now falls between bedtime and wake time.
func (t ScreenTime) InBedtime(now time.Time) bool {
	if t.Bedtime == "" || t.Wake == "" {
		return false
	}
	bed, err1 := ParseClock(t.Bedtime)
	wake, err2 := ParseClock(t.Wake)
	if err1 != nil || err2 != nil {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if bed < wake {
		return m >= bed && m < wake
	}
	return m >= bed || m < wake // over midnight
}

// ScreenTimeDay names the day a moment counts toward.
func ScreenTimeDay(now time.Time) string { return now.Format("2006-01-02") }

// ScreenTimeRules returns a user's rules (all zero when none are set).
func (s *Store) ScreenTimeRules(userID int64) (ScreenTime, error) {
	var t ScreenTime
	err := s.db.QueryRow(`SELECT weekday_minutes, weekend_minutes, bedtime, wake FROM screen_time WHERE user_id = ?`, userID).
		Scan(&t.WeekdayMinutes, &t.WeekendMinutes, &t.Bedtime, &t.Wake)
	if errors.Is(err, sql.ErrNoRows) {
		return ScreenTime{}, nil
	}
	return t, err
}

// SetScreenTimeRules sets (or, with nothing limited, removes) a user's rules.
func (s *Store) SetScreenTimeRules(userID int64, t ScreenTime) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if !t.Any() {
		_, err := s.db.Exec(`DELETE FROM screen_time WHERE user_id = ?`, userID)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO screen_time (user_id, weekday_minutes, weekend_minutes, bedtime, wake) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET weekday_minutes = excluded.weekday_minutes, weekend_minutes = excluded.weekend_minutes,
		bedtime = excluded.bedtime, wake = excluded.wake`, userID, t.WeekdayMinutes, t.WeekendMinutes, t.Bedtime, t.Wake)
	return err
}

// AddScreenTime counts seconds of watching toward a user's day, and drops
// days older than a week.
func (s *Store) AddScreenTime(userID int64, day string, seconds int64) error {
	if seconds <= 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO screen_time_used (user_id, day, seconds) VALUES (?, ?, ?)
		ON CONFLICT (user_id, day) DO UPDATE SET seconds = seconds + excluded.seconds`, userID, day, seconds)
	if err == nil {
		s.db.Exec(`DELETE FROM screen_time_used WHERE user_id = ? AND day < ?`, userID, ScreenTimeDay(time.Now().AddDate(0, 0, -7)))
	}
	return err
}

// ScreenTimeUsed is the seconds a user has watched on a day.
func (s *Store) ScreenTimeUsed(userID int64, day string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT seconds FROM screen_time_used WHERE user_id = ? AND day = ?`, userID, day).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// ResetScreenTime clears what a user has used today (a parent granting more time).
func (s *Store) ResetScreenTime(userID int64, day string) error {
	_, err := s.db.Exec(`DELETE FROM screen_time_used WHERE user_id = ? AND day = ?`, userID, day)
	return err
}
