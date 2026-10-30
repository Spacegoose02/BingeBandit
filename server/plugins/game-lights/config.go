package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Team is a team the plugin has followed, with ESPN's colours.
type Team struct {
	League   string `json:"league"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Short    string `json:"shortName"`
	Abbr     string `json:"abbr"`
	Color    string `json:"color,omitempty"`
	AltColor string `json:"altColor,omitempty"`
}

func (t Team) key() string { return t.League + "/" + t.ID }

// Config lives in config.json in the plugin's data folder: the bridge (its
// username is a credential, so it isn't a plugin setting the web app would
// show), the lights that flash, which effects are on, and team colours.
type Config struct {
	Bridge   string               `json:"bridge"`
	Username string               `json:"username"`
	Lights   []string             `json:"lights"`
	Off      []string             `json:"off,omitempty"`     // effects turned off
	Colours  map[string][2]string `json:"colours,omitempty"` // team key → two hex colours
	Teams    map[string]Team      `json:"teams,omitempty"`   // teams followed so far, for the setup page
	People   map[string]Person    `json:"people,omitempty"`  // each person's own choices, by user id
	// Version 2: Game Lights is opt-in per person (Person.Enabled).
	Version int `json:"version,omitempty"`
}

// Person is what each person sets for themselves on My Game Lights (web) or
// in the TV app's Sports tab.
type Person struct {
	// Enabled is the person's own switch for Game Lights (Account on the
	// web); nobody's lights flash until they turn it on.
	Enabled bool `json:"enabled,omitempty"`
	// Off is the TV app's Start / Stop: Game Lights stays on for the person,
	// but nothing flashes until they start it again.
	Off    bool     `json:"off,omitempty"`
	Skip   []string `json:"skip,omitempty"`   // team keys (league/id) the lights leave out
	Area   string   `json:"area,omitempty"`   // Hue room or zone (group id); "" = the lights the admin ticked
	Source string   `json:"source,omitempty"` // how they're watching, which picks the delay
	// Delays are seconds behind ESPN per source ("Cable", "YouTube TV"…), so
	// switching providers is one choice; missing = the admin's delay.
	Delays   map[string]int       `json:"delays,omitempty"`
	Opponent string               `json:"opponent,omitempty"` // other team scores: "red" (default), "colour" (dim, in their colours) or "off"
	Colours  map[string][2]string `json:"colours,omitempty"`  // team key → two hex colours, over the admin's
	// Local means the person's TV paired its own Hue hub and plays their
	// effects there (the server's bridge may be in another home), so the
	// server queues cues for it instead of flashing its own lights.
	Local bool `json:"local,omitempty"`
}

// lightsFor reports whether this person's lights flash for a team.
func (p Person) lightsFor(team string) bool {
	return p.Enabled && !p.Off && !slices.Contains(p.Skip, team)
}

// Sources are offered for the delay; people can name their own too.
var Sources = []string{"Cable or antenna", "YouTube TV", "Streaming app", "BingeBandit Live TV"}

// delay is how long to hold this person's effects, given the admin's default.
func (p Person) delay(def int) int {
	if d, ok := p.Delays[p.Source]; ok {
		return d
	}
	return def
}

type configStore struct {
	dir func() string
	mu  sync.Mutex
	c   *Config
}

func (s *configStore) file() string { return filepath.Join(s.dir(), "config.json") }

func (s *configStore) get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// load reads the file the first time and returns a copy; s.mu is held.
func (s *configStore) load() Config {
	if s.c == nil {
		s.c = &Config{}
		if b, err := os.ReadFile(s.file()); err == nil {
			json.Unmarshal(b, s.c)
		}
		if s.c.Version < 2 {
			// Before it was opt-in, everyone who'd set their own choices used it.
			for id, p := range s.c.People {
				p.Enabled = true
				s.c.People[id] = p
			}
			s.c.Version = 2
		}
	}
	c := *s.c
	c.Lights = slices.Clone(c.Lights)
	c.Off = slices.Clone(c.Off)
	c.Colours = maps.Clone(c.Colours)
	c.Teams = maps.Clone(c.Teams)
	c.People = maps.Clone(c.People) // a Person's maps are replaced, never changed in place
	return c
}

func (s *configStore) set(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(c)
}

func (s *configStore) save(c Config) error {
	s.c = &c
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}

// update changes the config in one step.
func (s *configStore) update(f func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.load()
	f(&c)
	return s.save(c)
}

// palette is the team's light colours, with the admin's colours if set.
func (c Config) palette(t Team) Palette {
	return c.paletteFor(t, Person{})
}

// paletteFor is the team's colours for one person: theirs, else the
// admin's, else ESPN's.
func (c Config) paletteFor(t Team, p Person) Palette {
	var custom *[2]string
	if pc, ok := p.Colours[t.key()]; ok {
		custom = &pc
	} else if ac, ok := c.Colours[t.key()]; ok {
		custom = &ac
	}
	return palette(teamHex(t.Abbr, t.Color, t.AltColor, custom))
}
