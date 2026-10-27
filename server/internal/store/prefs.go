package store

import (
	"encoding/json"
	"fmt"
	"slices"
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
	// SkipIntro and SkipCredits: "button" (offer it), "auto" (just do it) or "off".
	SkipIntro   string `json:"skipIntro"`
	SkipCredits string `json:"skipCredits"`
	// Before a movie that starts from the beginning: up to this many trailers
	// of other movies, then the server's intro video when PreRollIntro is on
	// and an admin has set one.
	PreRollTrailers int  `json:"preRollTrailers"`
	PreRollIntro    bool `json:"preRollIntro"`
	// Notifications in the app.
	NotifyEpisodes   bool `json:"notifyEpisodes"`   // new episodes of shows you watch or have on your watchlist
	NotifyRecordings bool `json:"notifyRecordings"` // your recordings finished or failed
	// Games of the sports teams you follow: an alert this many minutes
	// before each starts (0 for none), and the final score when one ends.
	GameAlertMinutes int  `json:"gameAlertMinutes"`
	NotifyGameScores bool `json:"notifyGameScores"`
	// ScoreBug shows the score of your teams' games in a corner of the player
	// while they're on (not on the channel showing the game).
	ScoreBug bool `json:"scoreBug"`
	// NotifyTogether: people may invite you to watch together.
	NotifyTogether bool `json:"notifyTogether"`
	// LiveFollows are Live TV show titles to be told about LiveAlertMinutes
	// before they air (0 for no alerts), on any channel you can watch.
	LiveFollows      []string `json:"liveFollows"`
	LiveAlertMinutes int      `json:"liveAlertMinutes"`
	// QuietWhilePlaying keeps alerts from popping up over a movie or show
	// (they still wait under the bell).
	QuietWhilePlaying bool `json:"quietWhilePlaying"`
	// HiddenTabs are the TV and phone app menu tabs this person turned off.
	// Home, Search and Settings always show; libraries come and go by pinning.
	// Never set (null) means DefaultHiddenTabs; an empty list shows them all.
	HiddenTabs []string `json:"hiddenTabs"`
	// PinnedTabs are app menu tabs that stay under More until pinned
	// (OptInTabs), so turning one on never surprises anyone's saved menu.
	PinnedTabs []string `json:"pinnedTabs"`
}

// OptInTabs are the app menu tabs PinnedTabs may name.
var OptInTabs = []string{"discover"}

// DefaultHiddenTabs start out under More instead of in the menu, until the
// person pins them. Kept out of DefaultPrefs: decoding into a shared slice
// would write over it.
var DefaultHiddenTabs = []string{"sports"}

// HideableTabs are the app menu tabs that HiddenTabs may name.
var HideableTabs = []string{"sports", "live", "more"}

var DefaultPrefs = Prefs{
	AudioLanguage: "eng", SubtitleMode: "auto", SubtitleLanguage: "eng", SubtitleSize: "medium",
	AutoQuality: true, AutoPlayNext: true, SkipIntro: "button", SkipCredits: "button",
	PreRollIntro: true, NotifyEpisodes: true, NotifyRecordings: true, GameAlertMinutes: 15,
	NotifyTogether: true, LiveAlertMinutes: 10, ScoreBug: true,
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
	for _, v := range []string{p.SkipIntro, p.SkipCredits} {
		if v != "button" && v != "auto" && v != "off" {
			return fmt.Errorf("skip settings must be button, auto or off")
		}
	}
	if p.PreRollTrailers < 0 || p.PreRollTrailers > 5 {
		return fmt.Errorf("trailers before a movie must be from 0 to 5")
	}
	if p.GameAlertMinutes < 0 || p.GameAlertMinutes > 180 {
		return fmt.Errorf("game alerts must be from 0 to 180 minutes before")
	}
	for _, t := range p.HiddenTabs {
		if !slices.Contains(HideableTabs, t) {
			return fmt.Errorf("only the sports, live and more tabs can be hidden")
		}
	}
	for _, t := range p.PinnedTabs {
		if !slices.Contains(OptInTabs, t) {
			return fmt.Errorf("only the discover tab can be pinned this way")
		}
	}
	if p.LiveAlertMinutes < 0 || p.LiveAlertMinutes > 180 {
		return fmt.Errorf("show alerts must be from 0 to 180 minutes before")
	}
	if len(p.LiveFollows) > 200 {
		return fmt.Errorf("follow at most 200 shows")
	}
	for _, t := range p.LiveFollows {
		if len(t) == 0 || len(t) > 200 {
			return fmt.Errorf("followed show titles are 1 to 200 characters")
		}
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
	if p.HiddenTabs == nil {
		p.HiddenTabs = slices.Clone(DefaultHiddenTabs)
	}
	return p, nil
}

func (s *Store) SetUserPrefs(userID int64, p Prefs) error {
	b, _ := json.Marshal(p)
	_, err := s.db.Exec(`UPDATE users SET prefs = ? WHERE id = ?`, string(b), userID)
	return err
}
