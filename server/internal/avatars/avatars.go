// Package avatars holds the built-in profile pictures. Someone without a
// picture of their own gets one of these, picked from their user id, and can
// roll for another.
package avatars

import (
	"embed"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"path"
	"slices"
	"strings"
)

//go:embed images/*.webp
var files embed.FS

// Ref prefix stored in users.avatar for a built-in picture: "avatar:/<name>".
const RefPrefix = "avatar:/"

var names = func() []string {
	entries, _ := fs.ReadDir(files, "images")
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".webp"))
	}
	return out
}()

// Names lists the built-in pictures, sorted.
func Names() []string { return slices.Clone(names) }

// URL is where a built-in picture is served.
func URL(name string) string { return "/api/avatars/" + name + ".webp" }

// ForUser is the picture someone without one gets. It's fixed per user, and
// neighbouring ids land far apart in the list.
func ForUser(id int64) string {
	if len(names) == 0 {
		return ""
	}
	return names[(uint64(id)*2654435761)%uint64(len(names))]
}

// Roll picks a picture other than the current one.
func Roll(current string) string {
	if len(names) < 2 {
		return ForUser(0)
	}
	for {
		if n := names[rand.IntN(len(names))]; n != current {
			return n
		}
	}
}

// Valid tells whether name is a built-in picture.
func Valid(name string) bool {
	_, ok := slices.BinarySearch(names, name)
	return ok
}

// Handler serves /api/avatars/<name>.webp. The pictures never change, and
// aren't private, so they're cached for a long time and need no sign-in.
func Handler() http.Handler {
	sub, _ := fs.Sub(files, "images")
	fsrv := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.URL.Path)
		if !Valid(strings.TrimSuffix(name, ".webp")) || !strings.HasSuffix(name, ".webp") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=604800")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + name
		fsrv.ServeHTTP(w, r2)
	})
}
