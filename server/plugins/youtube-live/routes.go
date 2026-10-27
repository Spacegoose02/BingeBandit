package main

import (
	"html/template"
	"net/http"
	"time"

	"bingebandit/pkg/bbplugin"
)

var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"ago": func(t time.Time) string {
		if t.IsZero() {
			return "not yet"
		}
		return time.Since(t).Round(time.Second).String() + " ago"
	},
}).Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>YouTube Live</title>
<style>
body { font: 14px/1.5 system-ui, sans-serif; margin: 0; padding: 20px 16px; background: #111418; color: #e8eaed; }
h1 { font-size: 20px; margin: 0 0 4px; } p { color: #9aa0a6; margin: 0 0 16px; }
table { border-collapse: collapse; width: 100%; max-width: 900px; }
td, th { text-align: left; padding: 8px 10px; border-bottom: 1px solid #2a2f36; vertical-align: middle; }
img { width: 32px; height: 32px; border-radius: 50%; vertical-align: middle; margin-right: 8px; }
.live { color: #ff4e45; font-weight: 700; } .err { color: #f28b82; } .muted { color: #9aa0a6; }
form { margin-top: 16px; } button { font: inherit; padding: 6px 14px; border-radius: 8px; border: 0; background: #8ab4f8; color: #111; cursor: pointer; }
</style></head><body>
<h1>YouTube Live</h1>
<p>Each YouTuber gets a channel in the {{.Group}} group of Live TV, shown while they stream. Add or remove YouTubers in this plugin's settings. Last sent to Live TV {{ago .Sent}}{{if .SendErr}} <span class="err">({{.SendErr}})</span>{{end}}.</p>
{{if .People}}<table><tr><th>YouTuber</th><th>Now</th><th>Checked</th></tr>
{{range .People}}<tr><td>{{if .Avatar}}<img src="{{.Avatar}}" alt="">{{end}}{{if .Name}}{{.Name}}{{else}}{{.Input}}{{end}}</td>
<td>{{if .Error}}<span class="err">{{.Error}}</span>{{else if .Live.Live}}<span class="live">● Live</span> {{.Live.Title}}{{else}}<span class="muted">Not live</span>{{end}}</td>
<td class="muted">{{ago .Checked}}</td></tr>{{end}}</table>
{{else}}<p>No YouTubers yet: list them in the plugin's settings.</p>{{end}}
<form method="post" action="check"><button>Check now</button></form>
</body></html>`))

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	admin := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, ok := bbplugin.UserOf(r); !ok || !u.Admin {
				http.Error(w, "admins only", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /{$}", admin(func(w http.ResponseWriter, r *http.Request) {
		s := a.conf(r.Context())
		a.mu.Lock()
		var people []youtuber
		for _, in := range splitList(s["youtubers"]) {
			if y := a.people[in]; y != nil {
				people = append(people, *y)
			} else {
				people = append(people, youtuber{Input: in})
			}
		}
		data := map[string]any{"People": people, "Sent": a.lastSent, "SendErr": a.sendErr, "Group": s["group"]}
		a.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		page.Execute(w, data)
	}))
	mux.HandleFunc("POST /check", admin(func(w http.ResponseWriter, r *http.Request) {
		a.kick()
		time.Sleep(2 * time.Second) // usually enough for a short list
		http.Redirect(w, r, "./", http.StatusSeeOther)
	}))
	return mux
}
