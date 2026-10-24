package api

import "testing"

func TestVersionCode(t *testing.T) {
	for in, want := range map[string]int{"v0.4.0": 400, "1.2.3": 10203, "v2.10.7": 21007, "v1.0": 10000, "v0.0.0": 1, "v3.1.4-rc1": 30104} {
		if got := versionCode(in); got != want {
			t.Errorf("versionCode(%q) = %d, want %d", in, got, want)
		}
	}
}
