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
