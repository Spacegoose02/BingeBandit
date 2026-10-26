package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

func TestKidChannels(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	parent, err := st.CreateUser("parent", "", false)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.CreateProfile(parent, "Kids")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetKid(kid.ID, true); err != nil {
		t.Fatal(err)
	}
	adminKey, _, _ := st.CreateAPIKey(parent.ID, "parent") // a plain account, so it sees everything
	kidKey, _, err := st.CreateAPIKey(kid.ID, "Plex")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*store.Channel{
		{Number: 1, Name: "News", Group: "News", Enabled: true},
		{Number: 2, Name: "Cartoons", Group: "Kids", Enabled: true},
		{Number: 3, Name: "PBS", Group: "Local", Enabled: true},
		{Number: 4, Name: "Toons West", Group: "Kids", Enabled: true},
	} {
		if err := st.CreateChannel(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Store: st, Live: livetv.NewManager(st, "ffmpeg", "ffprobe", t.TempDir())}
	h := s.Router(context.Background())
	names := func(key string) []string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/live/channels", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("channels: %d %s", rec.Code, rec.Body)
		}
		var list []liveChannel
		json.Unmarshal(rec.Body.Bytes(), &list)
		out := []string{}
		for _, c := range list {
			out = append(out, c.Name)
		}
		return out
	}

	if got := names(kidKey); len(got) != 0 {
		t.Fatalf("kid sees channels before any are approved: %v", got)
	}
	if err := st.SetKidGroup("Kids", true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetKidChannels([]int64{3}, true); err != nil {
		t.Fatal(err)
	}
	if got := names(kidKey); len(got) != 3 || got[0] != "Cartoons" || got[1] != "PBS" || got[2] != "Toons West" {
		t.Fatalf("kid sees %v", got)
	}
	if got := names(adminKey); len(got) != 4 {
		t.Fatalf("parent sees %v", got)
	}
	// Unapproved channels can't be opened directly either, and the Plex
	// tuner lineup follows the same rule.
	if _, err := st.Channel(kid.ID, 1); err != store.ErrNotFound {
		t.Fatalf("kid opened an unapproved channel: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/live/tuner/"+kidKey+"/lineup.json", nil))
	var lineup []struct{ GuideName string }
	json.Unmarshal(rec.Body.Bytes(), &lineup)
	if len(lineup) != 3 {
		t.Fatalf("kid's tuner lineup: %s", rec.Body)
	}

	groups, ids, err := st.KidApprovals()
	if err != nil || len(groups) != 3 || len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("approvals: %+v %v %v", groups, ids, err)
	}
	for _, g := range groups {
		if g.Approved != (g.Name == "Kids") {
			t.Errorf("group %+v", g)
		}
	}
	if err := st.SetKid(kid.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := names(kidKey); len(got) != 4 {
		t.Fatalf("after kid mode is off: %v", got)
	}
}
