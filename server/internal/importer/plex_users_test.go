package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexUserEmails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts":
			w.Write([]byte(`{"MediaContainer":{"Account":[{"id":0,"name":""},{"id":1,"name":"mom"},{"id":5,"name":"timmy"},{"id":9,"name":"Kids"}]}}`))
		case "/api/v2/user":
			w.Write([]byte(`{"id":123,"email":"mom@example.com"}`))
		case "/api/v2/friends":
			w.Write([]byte(`[{"id":5,"email":"timmy@example.com"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := NewPlex(srv.URL, "tok")
	p.plexTV = srv.URL
	users, err := p.Users(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []SourceUser{{Name: "mom", Email: "mom@example.com"}, {Name: "timmy", Email: "timmy@example.com"}, {Name: "Kids"}}
	if len(users) != len(want) {
		t.Fatalf("users = %+v", users)
	}
	for i := range want {
		if users[i] != want[i] {
			t.Fatalf("users = %+v", users)
		}
	}

	// plex.tv being unreachable just means no emails.
	p = NewPlex(srv.URL, "tok")
	p.plexTV = "http://127.0.0.1:1"
	if users, err := p.Users(context.Background()); err != nil || len(users) != 3 || users[0].Email != "" {
		t.Fatalf("offline users = %+v, %v", users, err)
	}
}
