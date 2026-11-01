package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bingebandit/pkg/bbplugin"
)

func TestSendRequests(t *testing.T) {
	cm := &fakeCinemask{profile: 2}
	cs := httptest.NewServer(cm)
	defer cs.Close()
	a := &app{p: &bbplugin.Plugin{Manifest: manifest}}
	a.settings = map[string]string{"url": cs.URL, "api_key": "cmk_test"}
	waiting := []request{
		{ID: 1, Kind: "movie", TMDBID: 949, Title: "Heat"},       // already monitored: searched
		{ID: 2, Kind: "movie", TMDBID: 329865, Title: "Arrival"}, // new: monitored and searched
	}
	a.listRequests = func(context.Context) ([]request, error) { return waiting, nil }
	got := map[int64]string{}
	a.setRequest = func(_ context.Context, id int64, status, reason, link string) error {
		got[id] = status + " " + reason + " " + link
		return nil
	}
	a.sendRequests(context.Background())
	calls := strings.Join(cm.calls, "\n")
	if !strings.Contains(calls, "POST /api/monitored/4/search") {
		t.Errorf("Heat wasn't searched: %s", calls)
	}
	if !strings.Contains(calls, `POST /api/monitored {"kind":"movie","search":true,"tmdbId":329865}`) {
		t.Errorf("Arrival wasn't added with a search: %s", calls)
	}
	if got[1] != "sent  "+cs.URL+"/movie/949" || got[2] != "sent  "+cs.URL+"/movie/329865" {
		t.Errorf("reports: %v", got)
	}

	// Cinemask refusing is reported, so an admin can try again.
	a.settings["api_key"] = "nope"
	got = map[int64]string{}
	a.sendRequests(context.Background())
	if !strings.HasPrefix(got[1], "failed Cinemask didn't accept the API key") {
		t.Errorf("bad key: %v", got)
	}
	// Not set up: left waiting, nothing reported.
	a.settings["url"] = ""
	got = map[int64]string{}
	a.sendRequests(context.Background())
	if len(got) != 0 {
		t.Errorf("not set up: %v", got)
	}
	// Turned off: BingeBandit isn't even asked.
	a.settings = map[string]string{"url": cs.URL, "api_key": "cmk_test", "requests": "false"}
	a.listRequests = func(context.Context) ([]request, error) {
		t.Error("asked for requests while turned off")
		return nil, errors.New("off")
	}
	a.sendRequests(context.Background())
}

func TestRequestSeasonsAnd4K(t *testing.T) {
	cm := &fakeCinemask{profile: 2}
	cs := httptest.NewServer(cm)
	defer cs.Close()
	c, _ := newCinemask(cs.URL, "cmk_test")
	ctx := context.Background()
	// A new show: just the seasons asked for.
	if err := sendRequest(ctx, c, request{Kind: "show", TMDBID: 95396, Title: "Severance", Seasons: []int{2, 3}}, ""); err != nil {
		t.Fatal(err)
	}
	// 4K without a profile set fails with what to do; with one, Heat moves to it and is searched.
	if err := sendRequest(ctx, c, request{Kind: "movie", TMDBID: 949, Title: "Heat", FourK: true}, ""); err == nil || !strings.Contains(err.Error(), "4K quality profile") {
		t.Errorf("4K without a profile: %v", err)
	}
	if err := sendRequest(ctx, c, request{Kind: "movie", TMDBID: 949, Title: "Heat", FourK: true}, "hd-720p"); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(cm.calls, "\n")
	if !strings.Contains(calls, `POST /api/monitored {"kind":"show","search":true,"seasons":[2,3],"tmdbId":95396}`) {
		t.Errorf("seasons: %s", calls)
	}
	if !strings.Contains(calls, `PATCH /api/monitored/4 {"profileId":1}`) || cm.profile != 1 {
		t.Errorf("4K profile: %s", calls)
	}
}

func TestProgressOf(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	eta := 600
	downloads := []Download{
		{MonitoredID: 4, Status: "downloading", Progress: 0.5, ETA: &eta},
		{MonitoredID: 4, Status: "importing", Progress: 1},
		{MonitoredID: 5, Status: "downloading", Progress: 0.1},
	}
	if p := progressOf(request{}, 4, downloads, now); p.Stage != "downloading" || p.Progress != 0.75 || p.ETA != now.Unix()+600 {
		t.Errorf("two downloads: %+v", p)
	}
	if p := progressOf(request{}, 9, downloads, now); p.Stage != "searching" {
		t.Errorf("none: %+v", p)
	}
	if p := progressOf(request{ReleaseDate: "2030-01-01"}, 9, downloads, now); p.Stage != "waiting" {
		t.Errorf("not out yet: %+v", p)
	}
}

func TestRefetch(t *testing.T) {
	cm := &fakeCinemask{profile: 2}
	cs := httptest.NewServer(cm)
	defer cs.Close()
	c, _ := newCinemask(cs.URL, "cmk_test")
	reply, err := refetch(context.Background(), c, problem{ID: 1, ItemKind: "movie", TMDBID: 949, Title: "Heat"})
	if err != nil || !strings.Contains(reply, "Heat.1995.1080p.BluRay") {
		t.Fatalf("refetch: %q %v", reply, err)
	}
	if !strings.Contains(strings.Join(cm.calls, "\n"), `POST /api/monitored/4/grab {"guid":"g1"}`) {
		t.Errorf("not grabbed: %v", cm.calls)
	}
}
