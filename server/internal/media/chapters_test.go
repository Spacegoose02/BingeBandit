package media

import (
	"reflect"
	"testing"
)

func TestParseChapters(t *testing.T) {
	out := []byte(`{"chapters":[
		{"start_time":"0.000000","end_time":"300.500000","tags":{"title":"Opening"}},
		{"start_time":"300.500000","end_time":"600.000000","tags":{"title":"00:05:00.500"}},
		{"start_time":"600.000000","end_time":"600.000000","tags":{"title":"Empty"}},
		{"start_time":"600.000000","end_time":"900.000000","tags":{"TITLE":"The Heist"}},
		{"start_time":"900.000000","end_time":"1200.000000"}
	]}`)
	got, err := parseChapters(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []Chapter{
		{Start: 0, End: 300.5, Title: "Opening"},
		{Start: 300.5, End: 600},
		{Start: 600, End: 900, Title: "The Heist"},
		{Start: 900, End: 1200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	one, _ := parseChapters([]byte(`{"chapters":[{"start_time":"0","end_time":"5400","tags":{"title":"Movie"}}]}`))
	if len(one) != 0 {
		t.Errorf("a single chapter should be dropped, got %+v", one)
	}
	none, _ := parseChapters([]byte(`{}`))
	if none == nil || len(none) != 0 {
		t.Errorf("no chapters should be an empty list, got %#v", none)
	}
}
