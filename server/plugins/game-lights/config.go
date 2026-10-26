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
	}
	c := *s.c
	c.Lights = slices.Clone(c.Lights)
	c.Off = slices.Clone(c.Off)
	c.Colours = maps.Clone(c.Colours)
	c.Teams = maps.Clone(c.Teams)
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
	var custom *[2]string
	if p, ok := c.Colours[t.key()]; ok {
		custom = &p
	}
	return palette(teamHex(t.Abbr, t.Color, t.AltColor, custom))
}
