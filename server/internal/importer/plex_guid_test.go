package importer

import (
	"encoding/json"
	"testing"
)

// Plex sends every item's own guid as a string, and the new agents' ids as a
// Guid array. Libraries on the old agents only have the string.
func TestPlexGuids(t *testing.T) {
	var c plexContainer
	err := json.Unmarshal([]byte(`{"MediaContainer":{"Metadata":[
		{"ratingKey":"1","type":"movie","guid":"plex://movie/5d776","Guid":[{"id":"imdb://tt0111161"},{"id":"tmdb://278"},{"id":"tvdb://190"}]},
		{"ratingKey":"2","type":"movie","guid":"com.plexapp.agents.imdb://tt0068646?lang=en"},
		{"ratingKey":"3","type":"movie","guid":"com.plexapp.agents.themoviedb://238?lang=en"},
		{"ratingKey":"4","type":"show","guid":"com.plexapp.agents.thetvdb://81189?lang=en"},
		{"ratingKey":"5","type":"episode","guid":"com.plexapp.agents.thetvdb://81189/1/2?lang=en"},
		{"ratingKey":"6","type":"movie","guid":"local://6"},
		{"ratingKey":"7","type":"movie","guid":"com.plexapp.agents.none://abc"}]}}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	want := []plexIDs{
		{TMDB: 278, IMDB: "tt0111161", TVDB: 190},
		{IMDB: "tt0068646"},
		{TMDB: 238},
		{TVDB: 81189},
		{TVDB: 81189},
		{},
		{},
	}
	for i, m := range c.MediaContainer.Metadata {
		if got := m.ids(); got != want[i] {
			t.Errorf("item %s: ids %+v, want %+v", m.RatingKey, got, want[i])
		}
	}
}
