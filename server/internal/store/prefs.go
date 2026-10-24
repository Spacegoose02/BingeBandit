package store

import (
	"encoding/json"
	"fmt"
)

// Prefs are a user's playback preferences, applied by every client.
type Prefs struct {
	// AudioLanguage is an ISO 639-2 code ("eng"); empty keeps the file's default track.
	AudioLanguage string `json:"audioLanguage"`
	// SubtitleMode: "off"; "auto" (only when the audio isn't SubtitleLanguage);
	// "forced" (forced/foreign-parts subtitles only); "always".
	SubtitleMode       string `json:"subtitleMode"`
	SubtitleLanguage   string `json:"subtitleLanguage"`
	SubtitleSize       string `json:"subtitleSize"` // small | medium | large
	SubtitleBackground bool   `json:"subtitleBackground"`
	AutoQuality        bool   `json:"autoQuality"`  // step quality down when playback keeps buffering
	AutoPlayNext       bool   `json:"autoPlayNext"` // count down into the next episode
}

var DefaultPrefs = Prefs{
	AudioLanguage: "eng", SubtitleMode: "auto", SubtitleLanguage: "eng", SubtitleSize: "medium",
	AutoQuality: true, AutoPlayNext: true,
}

func (p *Prefs) Validate() error {
	switch p.SubtitleMode {
	case "off", "auto", "forced", "always":
	default:
		return fmt.Errorf("subtitle mode must be off, auto, forced or always")
	}
	switch p.SubtitleSize {
	case "small", "medium", "large":
	default:
		return fmt.Errorf("subtitle size must be small, medium or large")
	}
	if len(p.AudioLanguage) > 8 || len(p.SubtitleLanguage) > 8 {
		return fmt.Errorf("language codes are at most 8 characters")
	}
	return nil
}

// UserPrefs returns saved preferences layered over the defaults, so new
// fields get sensible values for existing users.
func (s *Store) UserPrefs(userID int64) (Prefs, error) {
	p := DefaultPrefs
	var raw string
	if err := s.db.QueryRow(`SELECT prefs FROM users WHERE id = ?`, userID).Scan(&raw); err != nil {
		return p, err
	}
	json.Unmarshal([]byte(raw), &p)
	return p, nil
}

func (s *Store) SetUserPrefs(userID int64, p Prefs) error {
	b, _ := json.Marshal(p)
	_, err := s.db.Exec(`UPDATE users SET prefs = ? WHERE id = ?`, string(b), userID)
	return err
}
