package media

import "testing"

func TestDolbyVision(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		side []sideData
		want bool
	}{
		{"dvh1", nil, true},
		{"[0][0][0][0]", []sideData{{"DOVI configuration record"}}, true},
		{"hev1", []sideData{{"Mastering display metadata"}}, false},
		{"", nil, false},
	} {
		if got := dolbyVision(tc.tag, tc.side); got != tc.want {
			t.Errorf("%q %v: %v", tc.tag, tc.side, got)
		}
	}
}
