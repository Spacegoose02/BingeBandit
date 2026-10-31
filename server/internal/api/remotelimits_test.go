package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// Away from home, files over the quality cap must be converted and each
// account (with its profiles) gets the admin's number of streams. At home,
// and for admins, neither applies.
func TestRemoteStreamLimits(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	sam, _ := st.CreateUser("sam", "x", false)
	kid, err := st.CreateProfile(sam, "Kid")
	if err != nil {
		t.Fatal(err)
	}
	st.CreateSession("admin-token", admin.ID, "Web", "")
	st.CreateSession("sam-token", sam.ID, "Web", "")
	st.CreateSession("kid-token", kid.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	files := map[string]int64{}
	for _, name := range []string{"a", "b", "big"} {
		path := filepath.Join(dir, name+".mp4")
		os.WriteFile(path, []byte("not really a video"), 0o644)
		itemID, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: name, Year: 2026, MatchKey: "movie:" + name})
		bitrate := int64(3_000_000)
		if name == "big" {
			bitrate = 40_000_000
		}
		st.UpsertFile(&store.MediaFile{ItemID: itemID, Path: path, Size: 18, Container: "mp4", Duration: 60, Bitrate: bitrate, VideoCodec: "h264", Height: 1080}, 1)
		mf, _ := st.FileByPath(path)
		files[name] = mf.ID
	}
	st.SetSettings(map[string]string{store.SettingRemoteMaxStreams: "1", store.SettingRemoteMaxKbps: "8000"})

	h := (&Server{Store: st, Activity: stream.NewActivity(), DataDir: dir}).Router(context.Background())
	call := func(token, ip, method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = ip + ":5000"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	const away, away2, home = "203.0.113.7", "198.51.100.9", "192.168.1.20"
	direct := func(name string) string { return "/api/files/" + itoa(files[name]) + "/direct" }
	itemOf := func(name string) int64 { mf, _ := st.File(files[name]); return mf.ItemID }
	ping := func(token, ip, session, name string) int {
		return call(token, ip, "POST", "/api/progress",
			`{"playSessionId":"`+session+`","itemId":`+itoa(itemOf(name))+`,"fileId":`+itoa(files[name])+`,"position":5,"duration":60,"mode":"direct"}`)
	}

	// Quality cap: refused away from home (players then convert), fine at home.
	if c := call("sam-token", away, "GET", direct("big"), ""); c != http.StatusForbidden {
		t.Errorf("big file away from home: %d", c)
	}
	if c := call("sam-token", home, "GET", direct("big"), ""); c != http.StatusOK {
		t.Errorf("big file at home: %d", c)
	}

	// Sam starts a stream away from home.
	if c := call("sam-token", away, "GET", direct("a"), ""); c != http.StatusOK {
		t.Fatalf("first stream: %d", c)
	}
	if c := ping("sam-token", away, "p1", "a"); c != http.StatusOK {
		t.Fatalf("first ping: %d", c)
	}
	// More of the same file from the same place is the same stream.
	if c := call("sam-token", away, "GET", direct("a"), ""); c != http.StatusOK {
		t.Errorf("same stream again: %d", c)
	}
	// A second one, from elsewhere or by a profile, is over the limit.
	if c := call("sam-token", away2, "GET", direct("a"), ""); c != http.StatusTooManyRequests {
		t.Errorf("second place: %d", c)
	}
	if c := call("kid-token", away, "GET", direct("b"), ""); c != http.StatusTooManyRequests {
		t.Errorf("profile's stream: %d", c)
	}
	if c := ping("kid-token", away2, "p2", "b"); c != http.StatusTooManyRequests {
		t.Errorf("profile's first ping: %d", c)
	}
	// At home there's no limit, and admins have none.
	if c := call("kid-token", home, "GET", direct("b"), ""); c != http.StatusOK {
		t.Errorf("profile at home: %d", c)
	}
	if c := call("admin-token", away2, "GET", direct("b"), ""); c != http.StatusOK {
		t.Errorf("admin away: %d", c)
	}

	// Once Sam stops, the profile can watch.
	call("sam-token", away, "POST", "/api/progress",
		`{"playSessionId":"p1","itemId":`+itoa(itemOf("a"))+`,"fileId":`+itoa(files["a"])+`,"position":9,"duration":60,"stopped":true}`)
	if c := call("kid-token", away2, "GET", direct("b"), ""); c != http.StatusOK {
		t.Errorf("after the other stream stopped: %d", c)
	}

	// Settings are checked.
	if c := call("admin-token", home, "PUT", "/api/admin/settings", `{"remote_max_streams":"-1"}`); c != http.StatusBadRequest {
		t.Errorf("negative limit saved: %d", c)
	}
}
