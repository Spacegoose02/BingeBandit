package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Config is the bridge and which part of the picture each light follows.
// It lives in bridge.json in the plugin's data folder (the bridge username
// is a credential, so it isn't a plugin setting the web app would show).
type Config struct {
	Bridge   string            `json:"bridge"`
	Username string            `json:"username"`
	Lights   map[string]string `json:"lights"` // light id → zone
}

type configStore struct {
	dir func() string
	mu  sync.Mutex
	c   *Config
}

func (s *configStore) file() string { return filepath.Join(s.dir(), "bridge.json") }

func (s *configStore) get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c == nil {
		s.c = &Config{}
		if b, err := os.ReadFile(s.file()); err == nil {
			json.Unmarshal(b, s.c)
		}
	}
	c := *s.c
	c.Lights = map[string]string{}
	for k, v := range s.c.Lights {
		c.Lights[k] = v
	}
	return c
}

func (s *configStore) set(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.c = &c
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}

// assigned lists the lights that follow a zone, in a stable order.
func (c Config) assigned() [][2]string {
	out := [][2]string{}
	for id, z := range c.Lights {
		if z != "" {
			out = append(out, [2]string{id, z})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func sortLights(ls []Light) {
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].Room != ls[j].Room {
			return ls[i].Room < ls[j].Room
		}
		return ls[i].Name < ls[j].Name
	})
}
