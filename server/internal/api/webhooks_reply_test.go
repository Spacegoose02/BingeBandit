package api

import (
	"strings"
	"testing"
)

func TestReplySnippet(t *testing.T) {
	for in, want := range map[string]string{
		"Invalid JSON": " (Invalid JSON)",
		"":             "",
		"<html><head><style>body{}</style></head><body><h1>400 Bad Request</h1><hr><center>openresty</center></body></html>": " (400 Bad Request openresty)",
	} {
		if got := replySnippet(strings.NewReader(in)); got != want {
			t.Errorf("replySnippet(%q) = %q, want %q", in, got, want)
		}
	}
	if got := replySnippet(strings.NewReader(strings.Repeat("é", 200))); !strings.HasSuffix(got, "…)") || len(got) > 170 {
		t.Errorf("long reply not cut: %q", got)
	}
}
