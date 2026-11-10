package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App updates: TV apps ask their server what the newest app release is, so
// devices update without anyone opening a browser on them. The server looks
// at the project's GitHub releases (cached for an hour).

type appRelease struct {
	Version     string `json:"version"`     // e.g. 0.4.0
	VersionCode int    `json:"versionCode"` // major*10000 + minor*100 + patch, like the APK
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	PublishedAt string `json:"publishedAt"`
	Notes       string `json:"notes,omitempty"`
}

type appUpdates struct {
	mu      sync.Mutex
	fetched time.Time
	latest  *appRelease
	err     error
}

var updateClient = &http.Client{Timeout: 15 * time.Second}

// versionCode turns "v1.2.3" into 10203, matching apps/tv/android/app/build.gradle.
func versionCode(v string) int {
	parts := strings.FieldsFunc(strings.TrimPrefix(v, "v"), func(r rune) bool { return r == '.' || r == '-' })
	code, mult := 0, 10000
	for i := 0; i < 3; i++ {
		n := 0
		if i < len(parts) {
			n, _ = strconv.Atoi(parts[i])
		}
		code += n * mult
		mult /= 100
	}
	return max(code, 1)
}

func (u *appUpdates) get(ctx context.Context, repo string) (*appRelease, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if time.Since(u.fetched) < time.Hour && (u.latest != nil || u.err != nil) {
		return u.latest, u.err
	}
	u.latest, u.err = fetchAppRelease(ctx, repo)
	u.fetched = time.Now()
	if u.err != nil {
		u.fetched = time.Now().Add(-55 * time.Minute) // retry errors after 5 minutes
	}
	return u.latest, u.err
}

// fetchAppRelease finds the newest published release that has a TV APK. It
// looks past the very latest release, so a release whose TV build failed (or
// is still uploading) doesn't leave /tv and TV updates with nothing.
func fetchAppRelease(ctx context.Context, repo string) (*appRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL+repo+"/releases?per_page=10", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BingeBandit")
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no releases yet
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rels []struct {
		TagName     string `json:"tag_name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, err
	}
	// GitHub lists releases newest first.
	for _, rel := range rels {
		if rel.Draft || rel.Prerelease {
			continue
		}
		for _, a := range rel.Assets {
			if strings.HasPrefix(a.Name, "BingeBandit-TV-") && strings.HasSuffix(a.Name, ".apk") {
				notes := rel.Body
				if len(notes) > 2000 {
					notes = notes[:2000]
				}
				return &appRelease{Version: strings.TrimPrefix(rel.TagName, "v"), VersionCode: versionCode(rel.TagName),
					URL: a.URL, Size: a.Size, PublishedAt: rel.PublishedAt, Notes: notes}, nil
			}
		}
	}
	return nil, nil // no recent release has a TV app
}

// androidApp tells the TV app about the newest release (204 when there is none).
func (s *Server) androidApp(w http.ResponseWriter, r *http.Request) {
	rel, err := s.updates.get(r.Context(), s.UpdateRepo)
	switch {
	case err != nil:
		writeErr(w, http.StatusBadGateway, err.Error())
	case rel == nil:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, http.StatusOK, rel)
	}
}
