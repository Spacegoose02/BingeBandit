package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bingebandit/internal/store"
	"bingebandit/internal/tasks"
)

func healthServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.CreateUser("admin", "x", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("viewer", "x", false); err != nil {
		t.Fatal(err)
	}
	return &Server{Store: st, DataDir: dir, Version: "0.36.0"}, st
}

// waitAlerts waits for n alerts to finish sending.
func waitAlerts(t *testing.T, s *Server, n int) []alertResult {
	t.Helper()
	for i := 0; i < 200; i++ {
		if list := s.alerts.recent(); len(list) >= n {
			return list
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wanted %d alerts, got %+v", n, s.alerts.recent())
	return nil
}

// adminNotes returns the titles of the admin's server notifications.
func adminNotes(t *testing.T, st *store.Store) []string {
	t.Helper()
	list, _, err := st.Notifications(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range list {
		if n.Kind == "server" {
			out = append(out, n.Title)
		}
	}
	return out
}

func TestAlertChannels(t *testing.T) {
	s, st := healthServer(t)
	var mu sync.Mutex
	got := map[string]*http.Request{}
	bodies := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path], bodies[r.URL.Path] = r, string(b)
		mu.Unlock()
		if r.URL.Path == "/broken" {
			http.Error(w, "topic is read-only", http.StatusForbidden)
		}
	}))
	defer srv.Close()
	st.SetSettings(map[string]string{
		store.SettingServerName: "Den", store.SettingPublicURL: "https://tv.example.com",
		store.SettingAlertDiscordURL: srv.URL + "/discord", store.SettingAlertNtfyURL: srv.URL + "/ntfy", store.SettingAlertNtfyToken: "tk_abc",
	})

	s.sendAlert(alert{Kind: "backup", Title: "The database backup failed", Body: "disk full", Link: "/admin/tasks", Problem: true})
	res := waitAlerts(t, s, 1)[0]
	if res.Results["discord"] != "sent" || res.Results["ntfy"] != "sent" || len(res.Results) != 2 {
		t.Fatalf("results: %v", res.Results)
	}
	var d struct {
		Embeds []struct {
			Title, Description, URL string
			Color                   int
		}
	}
	json.Unmarshal([]byte(bodies["/discord"]), &d)
	if len(d.Embeds) != 1 || d.Embeds[0].Title != "The database backup failed" || d.Embeds[0].URL != "https://tv.example.com/admin/tasks" || d.Embeds[0].Color != 0xd9534f {
		t.Errorf("discord: %s", bodies["/discord"])
	}
	n := got["/ntfy"]
	if n.Header.Get("Title") != "Den: The database backup failed" || n.Header.Get("Priority") != "high" ||
		n.Header.Get("Authorization") != "Bearer tk_abc" || n.Header.Get("Click") != "https://tv.example.com/admin/tasks" || bodies["/ntfy"] != "disk full" {
		t.Errorf("ntfy: %v %q", n.Header, bodies["/ntfy"])
	}
	if notes := adminNotes(t, st); len(notes) != 1 || notes[0] != "The database backup failed" {
		t.Errorf("admin notifications: %v", notes)
	}
	if list, _, _ := st.Notifications(2, 50); len(list) != 0 {
		t.Errorf("a viewer was told: %v", list)
	}

	// A kind turned off sends nothing, not even to the app.
	st.SetSettings(map[string]string{store.SettingAlertBackups: "false"})
	s.sendAlert(alert{Kind: "backup", Title: "again"})
	time.Sleep(50 * time.Millisecond)
	if len(s.alerts.recent()) != 1 || len(adminNotes(t, st)) != 1 {
		t.Error("a turned-off alert was sent")
	}

	// A receiver's refusal is reported with its reason.
	c := alertChannels{NtfyURL: srv.URL + "/broken", NtfyToken: "me:pw"}
	out := s.deliverAlert(context.Background(), c, alert{Title: "x"})
	if !strings.Contains(out["ntfy"], "403") || !strings.Contains(out["ntfy"], "topic is read-only") {
		t.Errorf("refusal: %v", out)
	}
	if got["/broken"].Header.Get("Authorization") != "Basic bWU6cHc=" {
		t.Errorf("basic auth: %v", got["/broken"].Header)
	}
}

// fakeSMTP accepts one message without TLS or sign-in and returns it.
func fakeSMTP(t *testing.T) (port string, msg <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { conn.Write([]byte(s + "\r\n")) }
		say("220 fake")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "DATA"):
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				say("250 queued")
			case strings.HasPrefix(cmd, "QUIT"):
				say("221 bye")
				out <- data.String()
				return
			default:
				say("250 ok")
			}
		}
	}()
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	return port, out
}

func TestAlertEmail(t *testing.T) {
	s, st := healthServer(t)
	port, msg := fakeSMTP(t)
	st.SetSettings(map[string]string{store.SettingServerName: "Den"})
	c := alertChannelsFrom(func(k string) string {
		return map[string]string{
			store.SettingAlertEmailTo: "ann@example.com, Bob <bob@example.com>", store.SettingAlertSMTPHost: "127.0.0.1",
			store.SettingAlertSMTPPort: port, store.SettingAlertSMTPFrom: "server@example.com",
		}[k]
	})
	out := s.deliverAlert(context.Background(), c, alert{Title: "Recording failed: News", Body: "BBC One\nno signal"})
	if out["email"] != "sent" {
		t.Fatalf("email: %v", out)
	}
	m := <-msg
	for _, want := range []string{"From: <server@example.com>", "To: ann@example.com, Bob <bob@example.com>", "Subject: Den: Recording failed: News", "BBC One\r\nno signal"} {
		if !strings.Contains(m, want) {
			t.Errorf("message lacks %q:\n%s", want, m)
		}
	}
}

func TestFolderAlerts(t *testing.T) {
	s, st := healthServer(t)
	movies := filepath.Join(t.TempDir(), "movies")
	os.MkdirAll(movies, 0o755)
	os.WriteFile(filepath.Join(movies, "Film (2020).mkv"), []byte("x"), 0o644)
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{movies}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}

	folders := s.checkFolders()
	if len(folders) != 2 || !folders[0].Online || !folders[1].Online || folders[1].Label != "Movies" || folders[1].Total == 0 {
		t.Fatalf("folders: %+v %+v", folders[0], folders[1])
	}
	if len(adminNotes(t, st)) != 0 {
		t.Fatal("alerted with everything fine")
	}

	// The drive goes away: one alert, not one per check.
	os.RemoveAll(movies)
	s.checkFolders()
	s.checkFolders()
	if notes := adminNotes(t, st); len(notes) != 1 || notes[0] != "A storage folder is offline" {
		t.Fatalf("offline: %v", notes)
	}
	// It's back, but empty: an unmounted drive's mount point. Only counts once
	// the library has items, so give it one.
	os.MkdirAll(movies, 0o755)
	if _, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Film", MatchKey: "movie:film"}); err != nil {
		t.Fatal(err)
	}
	s.checkFolders()
	if notes := adminNotes(t, st); len(notes) != 1 {
		t.Fatalf("an empty mount point counted as back: %v", notes)
	}
	os.WriteFile(filepath.Join(movies, "Film (2020).mkv"), []byte("x"), 0o644)
	s.checkFolders()
	if notes := adminNotes(t, st); len(notes) != 2 || notes[0] != "A storage folder is back online" {
		t.Fatalf("back: %v", notes)
	}

	// Nearly full: the threshold is 50% at most, so fake it with the check.
	f := &storageFolder{Path: movies}
	f.check(50)
	if !f.Online || f.Total == 0 || f.Low != (f.Free*2 < f.Total) {
		t.Errorf("space: %+v", f)
	}
}

func TestUpdateCheck(t *testing.T) {
	s, st := healthServer(t)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Spacegoose02/BingeBandit/releases/latest" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.37.0", "html_url": "https://github.com/x/releases/v0.37.0", "body": "- Health page\n- Alerts"})
	}))
	defer gh.Close()
	old := releasesURL
	releasesURL = gh.URL + "/"
	defer func() { releasesURL = old }()
	s.UpdateRepo = "Spacegoose02/BingeBandit"

	s.checkUpdate(context.Background())
	s.checkUpdate(context.Background())
	notes := adminNotes(t, st)
	if len(notes) != 1 || notes[0] != "BingeBandit 0.37.0 is available" {
		t.Fatalf("update notes: %v", notes)
	}
	if rel := s.cachedRelease(); rel == nil || rel.Version != "0.37.0" {
		t.Errorf("cached: %+v", rel)
	}

	// Up to date, or a dev build: nothing to say.
	for _, v := range []string{"0.37.0", "dev", "0.38.1"} {
		s.Version = v
		if s.isNewer("0.37.0") {
			t.Errorf("%s: told to update to 0.37.0", v)
		}
	}
}

func TestBackupAndRecordingAlerts(t *testing.T) {
	s, st := healthServer(t)
	s.TaskDone(&tasks.Task{ID: "scan_libraries"}, "", io.EOF)
	s.TaskDone(&tasks.Task{ID: "backup_database"}, "saved", nil)
	s.TaskDone(&tasks.Task{ID: "backup_database"}, "", io.ErrShortWrite)
	s.Emit("recording.completed", &store.Recording{Title: "News"})
	s.Emit("recording.failed", &store.Recording{Title: "Match of the Day", ChannelName: "BBC One", Error: "no signal"})
	notes := adminNotes(t, st)
	if len(notes) != 2 || notes[1] != "The database backup failed" || notes[0] != "Recording failed: Match of the Day" {
		t.Errorf("alerts: %v", notes)
	}
}

func TestAlertSettings(t *testing.T) {
	for in, ok := range map[[2]string]bool{
		{store.SettingAlertDiscordURL, "https://discord.com/api/webhooks/1/x"}: true,
		{store.SettingAlertDiscordURL, "discord.com/api/webhooks"}:             false,
		{store.SettingAlertNtfyURL, "ftp://ntfy.sh/x"}:                         false,
		{store.SettingAlertSMTPPort, "465"}:                                    true,
		{store.SettingAlertSMTPPort, "smtp"}:                                   false,
		{store.SettingAlertEmailTo, "a@example.com, b@example.com"}:            true,
		{store.SettingAlertEmailTo, "a@example.com; b@example.com"}:            false,
		{store.SettingAlertDrives, "yes"}:                                      false,
	} {
		if err := validAlertSettings(map[string]string{in[0]: in[1]}); (err == nil) != ok {
			t.Errorf("%s=%q: %v", in[0], in[1], err)
		}
	}
}
