package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionCode(t *testing.T) {
	for in, want := range map[string]int{"v0.4.0": 400, "1.2.3": 10203, "v2.10.7": 21007, "v1.0": 10000, "v0.0.0": 1, "v3.1.4-rc1": 30104} {
		if got := versionCode(in); got != want {
			t.Errorf("versionCode(%q) = %d, want %d", in, got, want)
		}
	}
}

// The newest release's TV build failed: /tv falls back to the one before it.
func TestFetchAppReleaseSkipsReleaseWithoutTVApp(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/o/r/releases" {
			http.NotFound(w, r)
			return
		}
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.51.0-rc1", "prerelease": true, "assets": []asset{{"BingeBandit-TV-0.51.0-rc1.apk", "rc"}}},
			{"tag_name": "v0.50.0", "assets": []asset{{"BingeBandit-Roku-0.50.0.zip", "roku"}}},
			{"tag_name": "v0.49.0", "assets": []asset{{"BingeBandit-TV-0.49.0.apk", "tv49"}}},
		})
	}))
	defer gh.Close()
	old := releasesURL
	releasesURL = gh.URL + "/"
	defer func() { releasesURL = old }()

	rel, err := fetchAppRelease(context.Background(), "o/r")
	if err != nil || rel == nil || rel.Version != "0.49.0" || rel.URL != "tv49" {
		t.Fatalf("got %+v, %v", rel, err)
	}
}
