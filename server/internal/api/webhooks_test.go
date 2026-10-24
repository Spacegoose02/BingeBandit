package api

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

func TestPlexWebhookBody(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{Store: st}
	body, ct := s.plexBody("playback.finished", map[string]any{
		"user": "ann", "kind": "episode", "title": "The Office", "episodeTitle": "Diversity Day", "season": 1, "episode": 2,
		"showTmdbId": int64(2316), "year": 2005, "client": "Web", "local": true,
	})
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Event    string
		Account  struct{ Title string }
		Metadata struct {
			Type             string
			Title            string
			GrandparentTitle string
			ParentIndex      int
			Index            int
			GrandparentGuid  []struct{ ID string }
		}
	}
	if err := json.Unmarshal([]byte(form.Value["payload"][0]), &p); err != nil {
		t.Fatal(err)
	}
	m := p.Metadata
	if p.Event != "media.scrobble" || p.Account.Title != "ann" || m.Type != "episode" || m.Title != "Diversity Day" ||
		m.GrandparentTitle != "The Office" || m.ParentIndex != 1 || m.Index != 2 || len(m.GrandparentGuid) != 1 || m.GrandparentGuid[0].ID != "tmdb://2316" {
		t.Errorf("payload: %+v", p)
	}
}
