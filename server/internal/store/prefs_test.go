package store

import "testing"

func TestPrefsHiddenTabs(t *testing.T) {
	p := DefaultPrefs
	p.HiddenTabs = []string{"sports", "live", "more"}
	if err := p.Validate(); err != nil {
		t.Fatalf("hiding sports, live and more: %v", err)
	}
	for _, tab := range []string{"home", "search", "settings", "lib:1"} {
		p.HiddenTabs = []string{tab}
		if p.Validate() == nil {
			t.Errorf("hiding %q was allowed", tab)
		}
	}
}

func TestPrefsSportsHiddenByDefault(t *testing.T) {
	s := openTest(t)
	u, err := s.CreateUser("ann", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.UserPrefs(u.ID); len(p.HiddenTabs) != 1 || p.HiddenTabs[0] != "sports" {
		t.Fatalf("new user: hidden %v, want [sports]", p.HiddenTabs)
	}
	// Prefs saved before this default (null) get it too.
	p := DefaultPrefs
	if err := s.SetUserPrefs(u.ID, p); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.UserPrefs(u.ID); len(p.HiddenTabs) != 1 {
		t.Fatalf("saved null: hidden %v, want [sports]", p.HiddenTabs)
	}
	// Turning Sports back on sticks.
	p.HiddenTabs = []string{}
	if err := s.SetUserPrefs(u.ID, p); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.UserPrefs(u.ID); len(p.HiddenTabs) != 0 {
		t.Fatalf("shown again: hidden %v", p.HiddenTabs)
	}
}
