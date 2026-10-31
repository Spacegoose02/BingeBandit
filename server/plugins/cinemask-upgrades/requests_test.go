package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

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
