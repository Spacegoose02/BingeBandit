package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

func TestPersonFilmography(t *testing.T) {
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/person":
			if r.URL.Query().Get("query") != "Ada Actor" {
				w.Write([]byte(`{"results":[]}`))
				return
			}
			w.Write([]byte(`{"results":[{"id":7,"name":"Ada Actress"},{"id":8,"name":"Ada Actor"}]}`))
		case "/person/8":
			if r.URL.Query().Get("append_to_response") != "combined_credits" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`{"id":8,"name":"Ada Actor","biography":"Born somewhere.","profile_path":"/ada.jpg","known_for_department":"Acting",
				"combined_credits":{
					"cast":[
						{"id":101,"media_type":"movie","title":"Owned Movie","release_date":"2010-05-01","character":"Hero","popularity":5},
						{"id":102,"media_type":"movie","title":"Missing Movie","release_date":"2020-01-01","character":"Villain","popularity":9},
						{"id":201,"media_type":"tv","name":"Long Show","first_air_date":"2015-03-03","character":"Doctor","episode_count":40,"popularity":3},
						{"id":202,"media_type":"tv","name":"Late Night","first_air_date":"2001-01-01","character":"Self","genre_ids":[10767]},
						{"id":103,"media_type":"movie","title":"Announced","character":"Lead"},
						{"id":101,"media_type":"movie","title":"Owned Movie","release_date":"2010-05-01","character":"Hero's Twin"}
					],
					"crew":[
						{"id":101,"media_type":"movie","title":"Owned Movie","release_date":"2010-05-01","job":"Producer"},
						{"id":104,"media_type":"movie","title":"Directed One","release_date":"2018-01-01","job":"Director"}
					]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer tmdb.Close()
	defer metadata.SetTMDBBase(tmdb.URL)()

	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingTMDBKey: "fake-key"})
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), false)
	st.CreateSession("dana", dana.ID, "Web", "")
	kid, _ := st.CreateUser("kid", string(hash), false)
	st.SetKid(kid.ID, true)
	st.CreateSession("kid", kid.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	owned, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Owned Movie", MatchKey: "movie:owned"})
	st.UpdateMetadata(owned, &store.Item{Title: "Owned Movie", TMDBID: 101})

	h := (&Server{Store: st, Metadata: metadata.NewService(st)}).Router(context.Background())
	type credit struct {
		Kind     string `json:"kind"`
		TMDBID   int64  `json:"tmdbId"`
		Title    string `json:"title"`
		Role     string `json:"role"`
		ItemID   int64  `json:"itemId"`
		Crew     bool   `json:"crew"`
		Episodes int    `json:"episodes"`
	}
	type film struct {
		Name       string   `json:"name"`
		Photo      string   `json:"photo"`
		Biography  string   `json:"biography"`
		CanRequest bool     `json:"canRequest"`
		Credits    []credit `json:"credits"`
	}
	get := func(token, name string) (film, int) {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/people/filmography?name="+strings.ReplaceAll(name, " ", "+"), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var f film
		json.Unmarshal(rec.Body.Bytes(), &f)
		return f, rec.Code
	}

	f, code := get("dana", "Ada Actor")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if f.Name != "Ada Actor" || f.Biography != "Born somewhere." || f.Photo == "" || !f.CanRequest {
		t.Fatalf("person: %+v", f)
	}
	var got []string
	for _, c := range f.Credits {
		got = append(got, c.Title)
	}
	// Announced first, then newest; the talk show is left out.
	if strings.Join(got, ",") != "Announced,Missing Movie,Directed One,Long Show,Owned Movie" {
		t.Fatalf("order: %v", got)
	}
	byTitle := map[string]credit{}
	for _, c := range f.Credits {
		byTitle[c.Title] = c
	}
	if c := byTitle["Owned Movie"]; c.ItemID != owned || c.Role != "Hero, Hero's Twin" || c.Crew {
		t.Fatalf("owned: %+v", c)
	}
	if c := byTitle["Directed One"]; !c.Crew || c.Role != "Director" || c.ItemID != 0 {
		t.Fatalf("directed: %+v", c)
	}
	if c := byTitle["Long Show"]; c.Kind != "show" || c.Episodes != 40 {
		t.Fatalf("show: %+v", c)
	}

	// A kid (requests off for kids) still sees it, without asking.
	if f, code := get("kid", "Ada Actor"); code != http.StatusOK || f.CanRequest || len(f.Credits) != 5 {
		t.Fatalf("kid: %d %+v", code, f)
	}

	// Nobody by that name.
	if _, code := get("dana", "Nobody Here"); code != http.StatusNotFound {
		t.Fatalf("unknown person: %d", code)
	}
}
