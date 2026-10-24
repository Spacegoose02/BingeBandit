package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestLogoLibraryAndCategories(t *testing.T) {
	s := openTest(t)
	src := &LiveSource{Kind: "m3u", Name: "IPTV", URL: "http://x", RefreshHours: 24, Enabled: true}
	if err := s.SaveLiveSource(src); err != nil {
		t.Fatal(err)
	}
	importStreams(t, s, src.ID,
		StreamEntry{Name: "CNN", URL: "http://x/1", Logo: "http://logos/cnn.png"},
		StreamEntry{Name: "TSN", URL: "http://x/2"},
	)
	streams, _, _ := s.LiveStreams(StreamQuery{Limit: 10})
	ids := []int64{}
	for _, st := range streams {
		ids = append(ids, st.ID)
	}
	if _, err := s.CreateChannelsFromStreams(ids, 1); err != nil {
		t.Fatal(err)
	}
	chans, _ := s.Channels(0, true)
	cnn, tsn := chans[0], chans[1]
	if cnn.Name != "CNN" {
		cnn, tsn = tsn, cnn
	}

	// Logos: the same file twice is one entry; deleting puts the provider logo back.
	l, err := s.AddChannelLogo("CNN", "aa.png")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.AddChannelLogo("CNN copy", "aa.png"); again.ID != l.ID {
		t.Fatalf("re-upload made a new logo %d, want %d", again.ID, l.ID)
	}
	if err := s.SetChannelsLogo([]int64{cnn.ID, tsn.ID}, LibraryLogoPrefix+"aa.png"); err != nil {
		t.Fatal(err)
	}
	if l, _ = s.ChannelLogo(l.ID); l.Channels != 2 {
		t.Fatalf("logo used by %d channels, want 2", l.Channels)
	}
	if err := s.SetLogoChannels("aa.png", []int64{tsn.ID}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Channel(0, cnn.ID); c.Logo != "http://logos/cnn.png" {
		t.Fatalf("CNN logo = %q after it was unassigned, want the provider's", c.Logo)
	}
	s.SetChannelsLogo([]int64{cnn.ID}, LibraryLogoPrefix+"aa.png")
	if _, err := s.DeleteChannelLogo(l.ID); err != nil {
		t.Fatal(err)
	}
	c1, _ := s.Channel(0, cnn.ID)
	c2, _ := s.Channel(0, tsn.ID)
	if c1.Logo != "http://logos/cnn.png" || c2.Logo != "" {
		t.Fatalf("after delete logos = %q, %q; want the provider's back", c1.Logo, c2.Logo)
	}
	if s.HasChannelLogoFile("aa.png") {
		t.Fatal("deleted logo still in the library")
	}

	// Categories belong to one user.
	ann, _ := s.CreateUser("ann", "x", false)
	bob, _ := s.CreateUser("bob", "x", false)
	cat := &ChannelCategory{Name: " Sports ", ChannelIDs: []int64{tsn.ID, 9999}}
	if err := s.SaveChannelCategory(ann.ID, cat); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveChannelCategory(ann.ID, &ChannelCategory{Name: "News", ChannelIDs: []int64{cnn.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChannelInCategory(bob.ID, cat.ID, cnn.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob changed ann's category: %v", err)
	}
	if err := s.SetChannelInCategory(ann.ID, cat.ID, cnn.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ChannelCategories(ann.ID)
	if len(got) != 2 || got[0].Name != "Sports" || !reflect.DeepEqual(got[0].ChannelIDs, []int64{cnn.ID, tsn.ID}) && !reflect.DeepEqual(got[0].ChannelIDs, []int64{tsn.ID, cnn.ID}) {
		t.Fatalf("ann's categories = %+v", got)
	}
	if len(got[0].ChannelIDs) != 2 {
		t.Fatalf("unknown channel 9999 was kept: %v", got[0].ChannelIDs)
	}
	if err := s.OrderChannelCategories(ann.ID, []int64{got[1].ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.ChannelCategories(ann.ID); got[0].Name != "News" {
		t.Fatalf("order = %s first, want News", got[0].Name)
	}
	if b, _ := s.ChannelCategories(bob.ID); len(b) != 0 {
		t.Fatalf("bob sees %d categories", len(b))
	}
	if err := s.DeleteChannelCategory(bob.ID, cat.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob deleted ann's category: %v", err)
	}
	if err := s.DeleteChannel(cnn.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.ChannelCategories(ann.ID); len(got[1].ChannelIDs) != 1 {
		t.Fatalf("deleted channel still in a category: %+v", got[1])
	}
}
