package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Sub is one person's subscription to a feed.
type Sub struct {
	ID        string `json:"id"`
	UserID    int64  `json:"userId"`
	URL       string `json:"url"`
	Name      string `json:"name"`
	AddedAt   int64  `json:"addedAt"`
	LastCheck int64  `json:"lastCheck"`
	LastError string `json:"lastError,omitempty"`
	Added     int    `json:"added"`   // items added to the watchlist so far
	Private   bool   `json:"private"` // added by an admin, so it may be on the local network
	// Missing are the newest feed entries that aren't in the library (yet);
	// they're tried again on every check.
	Missing []string `json:"missing"`
	// Seen are entries already added, so one the person removes from their
	// watchlist isn't added back.
	Seen []string `json:"seen,omitempty"`
}

const maxSeen = 5000

// subStore keeps subscriptions in subscriptions.json in the plugin's data folder.
type subStore struct {
	dir    func() string
	mu     sync.Mutex
	loaded bool
	subs   []*Sub
}

func (s *subStore) file() string { return filepath.Join(s.dir(), "subscriptions.json") }

func (s *subStore) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	if b, err := os.ReadFile(s.file()); err == nil {
		json.Unmarshal(b, &s.subs)
	}
}

func (s *subStore) saveLocked() error {
	b, _ := json.MarshalIndent(s.subs, "", "  ")
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}

func clone(x *Sub) *Sub {
	c := *x
	c.Missing = append([]string(nil), x.Missing...)
	c.Seen = append([]string(nil), x.Seen...)
	return &c
}

// forUser lists a person's subscriptions, without the seen lists.
func (s *subStore) forUser(uid int64) []*Sub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	out := []*Sub{}
	for _, x := range s.subs {
		if x.UserID == uid {
			c := clone(x)
			c.Seen = nil
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AddedAt < out[j].AddedAt })
	return out
}

func (s *subStore) get(uid int64, id string) *Sub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	for _, x := range s.subs {
		if x.ID == id && x.UserID == uid {
			return clone(x)
		}
	}
	return nil
}

func (s *subStore) add(uid int64, url, name string, private bool) (*Sub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	b := make([]byte, 6)
	rand.Read(b)
	x := &Sub{ID: hex.EncodeToString(b), UserID: uid, URL: url, Name: name, Private: private, AddedAt: time.Now().Unix(), Missing: []string{}}
	s.subs = append(s.subs, x)
	return clone(x), s.saveLocked()
}

func (s *subStore) remove(uid int64, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	for i, x := range s.subs {
		if x.ID == id && x.UserID == uid {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			s.saveLocked()
			return true
		}
	}
	return false
}

// update saves the result of a check, unless the subscription was removed meanwhile.
func (s *subStore) update(n *Sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	for i, x := range s.subs {
		if x.ID == n.ID {
			if len(n.Seen) > maxSeen {
				n.Seen = n.Seen[len(n.Seen)-maxSeen:]
			}
			s.subs[i] = n
			s.saveLocked()
			return
		}
	}
}

// due lists subscriptions not checked within every.
func (s *subStore) due(every time.Duration) []*Sub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	cutoff := time.Now().Add(-every).Unix()
	out := []*Sub{}
	for _, x := range s.subs {
		if x.LastCheck <= cutoff {
			out = append(out, clone(x))
		}
	}
	return out
}
