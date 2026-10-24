package stream

import (
	"testing"
	"time"
)

func TestActivityExpiry(t *testing.T) {
	a := NewActivity()
	var expired []string
	a.OnExpire = func(np NowPlaying) { expired = append(expired, np.ID) }
	a.Update(NowPlaying{ID: "old", ItemID: 1})
	a.Update(NowPlaying{ID: "new", ItemID: 2})
	a.m["old"].UpdatedAt = time.Now().Unix() - activityTTL - 1
	if list := a.List(); len(list) != 1 || list[0].ID != "new" {
		t.Errorf("list: %+v", list)
	}
	if len(expired) != 1 || expired[0] != "old" {
		t.Errorf("expired: %v", expired)
	}
	if _, ok := a.Get("old"); ok {
		t.Error("an expired player should be gone")
	}
	a.List()
	if len(expired) != 1 {
		t.Errorf("expired twice: %v", expired)
	}
}
