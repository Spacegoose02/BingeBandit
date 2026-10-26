package overlay

import (
	"image"
	"image/color"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestFactsAndConditions(t *testing.T) {
	f := FactsFrom(&store.OverlayFacts{
		Item: &store.Item{Kind: "movie", LibraryID: 1, Labels: []string{"Kids"}, AddedAt: time.Now().Unix(), Rating: 7.84},
		Files: []*store.MediaFile{
			{Width: 1920, Height: 800, Streams: []store.Stream{{Type: "video", Range: "sdr"}, {Type: "audio", Codec: "ac3", Channels: 6}}},
			{Width: 3840, Height: 1600, Streams: []store.Stream{{Type: "video", Range: "dv"},
				{Type: "audio", Codec: "truehd", Channels: 8, Profile: "Dolby TrueHD + Dolby Atmos"}}},
		},
		Ratings: store.ExternalRatings{IMDb: 8.1, RT: 93},
	})
	if f.Resolution != "4k" || f.Range != "dv" || f.Audio != "atmos" {
		t.Fatalf("facts %+v", f)
	}
	// A file probed before ranges were recorded still counts as HDR.
	old := FactsFrom(&store.OverlayFacts{Item: &store.Item{Kind: "show"}, Files: []*store.MediaFile{
		{Width: 1280, Height: 720, HDR: true, Streams: []store.Stream{{Type: "video"}, {Type: "audio", Codec: "dts", Title: "DTS-HD Master Audio 5.1", Channels: 6}}},
	}})
	if old.Resolution != "720" || old.Range != "hdr" || old.Audio != "dtshd" {
		t.Fatalf("old facts %+v", old)
	}
	if !NeedsProbe(&store.MediaFile{Streams: []store.Stream{{Type: "video"}}}) || NeedsProbe(&store.MediaFile{Streams: []store.Stream{{Type: "video", Range: "sdr"}}}) {
		t.Error("NeedsProbe")
	}

	for cond, want := range map[string]bool{
		"always": true, "res:4k": true, "res:1080": false, "hdr": true, "dv": true, "hdr10": false,
		"atmos": true, "dtsx": false, "surround": true, "label:kids": true, "label:Adults": false,
		"imdb:8": true, "imdb:8.5": false, "rt:90": true, "new:7": true,
	} {
		o := &store.PosterOverlay{Condition: cond, Enabled: true}
		if got := Matches(o, f); got != want {
			t.Errorf("%s: %v, want %v", cond, got, want)
		}
	}
	if Matches(&store.PosterOverlay{Condition: "always", Enabled: true, Kind: "show"}, f) {
		t.Error("a shows-only overlay matched a movie")
	}
	if Matches(&store.PosterOverlay{Condition: "always", Enabled: true, LibraryID: 2}, f) {
		t.Error("another library's overlay matched")
	}
	if Matches(&store.PosterOverlay{Condition: "always"}, f) {
		t.Error("a disabled overlay matched")
	}
	for _, c := range []string{"always", "res:4k", "label:Kids", "imdb:7.5", "new:30"} {
		if err := ValidCondition(c); err != nil {
			t.Errorf("%s: %v", c, err)
		}
	}
	for _, c := range []string{"", "nope", "label", "label:", "imdb:x", "new:-3", "res:8k:x"} {
		if ValidCondition(c) == nil {
			t.Errorf("%q accepted", c)
		}
	}

	p := PlanFor(f, Badges{Resolution: true, HDR: true, Audio: true, Ratings: true}, nil)
	if p.Resolution != "4K" || p.Range != "DOLBY VISION" || p.Audio != "ATMOS" || len(p.Ratings) != 3 || p.Ratings[2].Text != "78%" {
		t.Errorf("plan %+v", p)
	}
	if !PlanFor(f, Badges{}, nil).Empty() {
		t.Error("plan with every badge off isn't empty")
	}
	if p.Key("tmdb:/a.jpg", 342) == p.Key("tmdb:/b.jpg", 342) || p.Key("tmdb:/a.jpg", 342) == p.Key("tmdb:/a.jpg", 500) {
		t.Error("keys don't change with the poster or size")
	}
}

func TestDraw(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 680, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 680; x++ {
			base.Set(x, y, color.RGBA{200, 200, 200, 255})
		}
	}
	frame := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for i := range frame.Pix {
		frame.Pix[i] = 255 // opaque white
	}
	plan := Plan{Resolution: "4K", Audio: "ATMOS", Ratings: []Rating{{"imdb", "8.1"}},
		Custom: []*store.PosterOverlay{{ID: 1, Position: "center", Size: 20}}}
	out := Draw(base, 342, plan, Images{1: frame})
	if b := out.Bounds(); b.Dx() != 342 || b.Dy() != 503 {
		t.Fatalf("size %v", b)
	}
	dark := func(x, y int) bool { r, _, _, _ := out.At(x, y).RGBA(); return r>>8 < 120 }
	if !dark(14, 14) { // top-left badge
		t.Error("no resolution badge")
	}
	if !dark(342-14, 14) { // top-right badge
		t.Error("no audio badge")
	}
	if dark(171, 100) { // plain poster between them
		t.Error("badge where there shouldn't be one")
	}
	if r, _, _, _ := out.At(171, 251).RGBA(); r>>8 < 250 { // the uploaded overlay, centred
		t.Error("no uploaded overlay in the middle")
	}
	yellow := false // the IMDb pill along the bottom
	for x := 0; x < 342 && !yellow; x++ {
		r, g, b, _ := out.At(x, 503-15).RGBA()
		yellow = r>>8 > 200 && g>>8 > 150 && b>>8 < 80
	}
	if !yellow {
		t.Error("no IMDb rating at the bottom")
	}
}
