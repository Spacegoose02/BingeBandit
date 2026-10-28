package store

import "testing"

func TestServerIDStaysTheSame(t *testing.T) {
	s := openTest(t)
	id := s.ServerID()
	if len(id) != 32 {
		t.Fatalf("id = %q, want 32 hex characters", id)
	}
	if again := s.ServerID(); again != id {
		t.Fatalf("second call = %q, want %q", again, id)
	}
	all, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := all[settingServerID]; ok {
		t.Fatal("the server id must not be listed with the admin settings")
	}
}
