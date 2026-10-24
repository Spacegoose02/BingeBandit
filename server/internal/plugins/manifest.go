// Package plugins runs BingeBandit's out-of-process plugins. A plugin is any
// program that speaks HTTP: BingeBandit either starts it (a "managed" plugin
// in a plugins folder) or reaches it at a URL (a "remote" plugin, such as its
// own Docker container). The protocol is in docs/PLUGINS.md.
package plugins

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Manifest describes a plugin. Plugins serve it at GET /bingebandit/manifest;
// managed plugins also ship it as plugin.json, with Command set.
type Manifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	Events      []string  `json:"events,omitempty"` // event names, or "*" for all
	Settings    []Setting `json:"settings,omitempty"`
	Pages       []Page    `json:"pages,omitempty"`
	// Command starts a managed plugin, run in its folder. The first element
	// may be relative to the folder; ".exe" is tried on Windows.
	Command []string `json:"command,omitempty"`
}

// Setting is one field on the plugin's settings form.
type Setting struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type,omitempty"` // text (default), password, number, bool, select
	Default string   `json:"default,omitempty"`
	Help    string   `json:"help,omitempty"`
	Options []string `json:"options,omitempty"` // select
}

// Page is a link the web UI shows for the plugin. Pages are served by the
// plugin through /api/plugins/{id}/{path}.
type Page struct {
	Title string `json:"title"`
	Path  string `json:"path"`
	Admin bool   `json:"admin,omitempty"` // only shown to admins (the plugin still checks)
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

func parseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if !idPattern.MatchString(m.ID) {
		return nil, fmt.Errorf("manifest: id %q must be lowercase letters, digits and dashes", m.ID)
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	for i, s := range m.Settings {
		if s.Key == "" {
			return nil, fmt.Errorf("manifest: setting %d has no key", i)
		}
		if s.Type == "" {
			m.Settings[i].Type = "text"
		}
	}
	return &m, nil
}

// Wants reports whether the plugin subscribed to an event.
func (m *Manifest) Wants(event string) bool {
	for _, e := range m.Events {
		if e == event || e == "*" {
			return true
		}
	}
	return false
}

// Values fills in defaults for settings the admin hasn't set.
func (m *Manifest) Values(saved map[string]string) map[string]string {
	out := map[string]string{}
	for _, s := range m.Settings {
		out[s.Key] = s.Default
	}
	for k, v := range saved {
		out[k] = v
	}
	return out
}
