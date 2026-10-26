package importer

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// KometaCollection is one collection from a Kometa collection file, mapped
// onto what a BingeBandit collection has.
type KometaCollection struct {
	Name      string `json:"name"`
	Summary   string `json:"summary,omitempty"`
	SortTitle string `json:"sortTitle,omitempty"`
	// Lists are the links the collection follows (see Lists), in builder order.
	Lists []string `json:"lists"`
	// Sort is a collection sort (store.CollectionSorts).
	Sort     string `json:"sort"`
	Mode     string `json:"mode,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
	// Visible on the library's Recommended tab, Home, and friends' Home;
	// nil where the file doesn't say.
	VisibleLibrary *bool `json:"visibleLibrary,omitempty"`
	VisibleHome    *bool `json:"visibleHome,omitempty"`
	VisibleShared  *bool `json:"visibleShared,omitempty"`
	// Skipped says what of the file's settings BingeBandit can't use.
	Skipped []string `json:"skipped,omitempty"`
}

// kometaIgnored are settings that only matter to Kometa or the *arrs.
var kometaIgnored = regexp.MustCompile(`^(radarr_|sonarr_|item_radarr|item_sonarr|item_|build_collection$|sync_mode$|schedule$|delete_|run_again$|missing_|show_missing|save_report$|only_filter_missing$|cache_builders$|validate_builders$|minimum_items$|server_preroll$|ignore_ids$|ignore_imdb_ids$|tmdb_region$|blank_collection$|changes_webhooks$|delete_below_minimum$|delete_not_scheduled$|tvdb_language$|limit$)`)

var traktPeriodic = regexp.MustCompile(`^trakt_(watched|collected|recommended)_(daily|weekly|monthly|yearly|all)$`)

// ParseKometa reads a Kometa collection file (the YAML with a "collections:"
// section) for a library of libraryKind ("movies" or "shows"). Collections
// come back in the file's order. Ones with no list BingeBandit can follow
// still come back, with Skipped saying why.
func ParseKometa(data []byte, libraryKind string) ([]KometaCollection, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("that isn't a YAML file: %v", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("that isn't a Kometa collection file")
	}
	cols := mapValue(doc.Content[0], "collections")
	if cols == nil || cols.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(`no "collections:" section in that file`)
	}
	shows := libraryKind == "shows"
	var out []KometaCollection
	for i := 0; i+1 < len(cols.Content); i += 2 {
		name := strings.TrimSpace(cols.Content[i].Value)
		body := cols.Content[i+1]
		if name == "" || body.Kind != yaml.MappingNode {
			continue
		}
		c := KometaCollection{Name: name}
		var order, smart, listSort string
		for j := 0; j+1 < len(body.Content); j += 2 {
			key := strings.ToLower(strings.TrimSpace(body.Content[j].Value))
			v := body.Content[j+1]
			lists, sortBy, err := kometaBuilder(key, v, shows)
			if err != nil {
				c.Skipped = append(c.Skipped, err.Error())
				continue
			}
			if lists != nil {
				c.Lists = append(c.Lists, lists...)
				if listSort == "" {
					listSort = sortBy
				}
				continue
			}
			switch key {
			case "summary":
				c.Summary = strings.TrimSpace(v.Value)
			case "sort_title":
				c.SortTitle = strings.TrimSpace(v.Value)
			case "url_poster":
				c.Poster = kometaImage(v.Value, &c.Skipped, key)
			case "url_background":
				c.Backdrop = kometaImage(v.Value, &c.Skipped, key)
			case "visible_library":
				c.VisibleLibrary = kometaBool(v)
			case "visible_home":
				c.VisibleHome = kometaBool(v)
			case "visible_shared":
				c.VisibleShared = kometaBool(v)
			case "collection_order":
				order = strings.ToLower(strings.TrimSpace(v.Value))
			case "smart_label":
				// A smart collection of the titles Kometa labels; the value is its sort.
				smart = strings.ToLower(strings.TrimSpace(v.Value))
			case "collection_mode":
				c.Mode = map[string]string{"hide": "hide", "hide_items": "collapse", "show_items": "show"}[strings.ToLower(v.Value)]
			case "file_poster", "file_background":
				c.Skipped = append(c.Skipped, fmt.Sprintf("%s %s is a file on the Kometa computer; upload it in the collection editor", key, v.Value))
			default:
				if !kometaIgnored.MatchString(key) {
					c.Skipped = append(c.Skipped, key+" isn't supported")
				}
			}
		}
		switch {
		case smart != "":
			c.Sort = kometaSort(smart)
		case order == "custom" && listSort != "":
			// The list's own order, sorted on the list's site: "added" there
			// means added to the list, which is the list's order.
			c.Sort = kometaSort(listSort)
			if c.Sort == "added" || (c.Sort == "" && !strings.HasPrefix(listSort, "release")) {
				c.Sort = "list"
			}
		case order != "":
			c.Sort = kometaSort(order)
		}
		if len(c.Lists) == 0 {
			c.Skipped = append(c.Skipped, "no list BingeBandit can follow, so the collection starts empty")
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no collections in that file")
	}
	return out, nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// kometaSort maps a Kometa sort (collection_order, smart_label, an MDBList
// sort_by) onto a collection sort.
func kometaSort(s string) string {
	field, dir, _ := strings.Cut(s, ".")
	desc := dir == "desc"
	switch field {
	case "custom":
		return "list"
	case "release", "released", "year", "originally_available", "releasedigital":
		if desc {
			return "release_desc"
		}
		return ""
	case "title", "alpha":
		return "title"
	case "added", "added_at":
		return "added"
	case "audience_rating", "critic_rating", "user_rating", "rating", "score", "imdbrating", "rtomatoes", "rtaudience", "metacritic", "tmdbrating":
		return "rating"
	case "rank", "usort":
		return "list"
	}
	return ""
}

func kometaBool(v *yaml.Node) *bool {
	b, err := strconv.ParseBool(strings.TrimSpace(v.Value))
	if err != nil {
		return nil
	}
	return &b
}

func kometaImage(v string, skipped *[]string, key string) string {
	v = strings.TrimSpace(v)
	if u, err := url.Parse(v); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		return v
	}
	*skipped = append(*skipped, key+" isn't a link")
	return ""
}

// kometaValues reads a builder's value: one value, a list, or a dictionary
// (or a list of them), each as a map with "value" holding a lone scalar.
func kometaValues(v *yaml.Node) []map[string]string {
	var out []map[string]string
	var one func(n *yaml.Node)
	one = func(n *yaml.Node) {
		switch n.Kind {
		case yaml.ScalarNode:
			for _, part := range strings.Split(n.Value, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, map[string]string{"value": part})
				}
			}
		case yaml.SequenceNode:
			for _, c := range n.Content {
				one(c)
			}
		case yaml.MappingNode:
			m := map[string]string{}
			for i := 0; i+1 < len(n.Content); i += 2 {
				m[strings.ToLower(n.Content[i].Value)] = strings.TrimSpace(n.Content[i+1].Value)
			}
			out = append(out, m)
		}
	}
	one(v)
	return out
}

func withLimit(link string, limit string) string {
	if n, err := strconv.Atoi(limit); err == nil && n > 0 {
		sep := "?"
		if strings.Contains(link, "?") {
			sep = "&"
		}
		return link + sep + "limit=" + strconv.Itoa(n)
	}
	return link
}

var kometaIMDbCharts = map[string]string{
	"top_movies": "/chart/top", "popular_movies": "/chart/moviemeter", "top_shows": "/chart/toptv",
	"popular_shows": "/chart/tvmeter", "lowest_rated": "/chart/bottom", "top_english": "/chart/top-english-movies",
}

// kometaBuilder turns one builder into list links. It returns nil lists for
// keys that aren't builders, and an error for builders it can't follow.
func kometaBuilder(key string, v *yaml.Node, shows bool) (lists []string, sortBy string, err error) {
	media, tv := "movies", "movie"
	if shows {
		media, tv = "shows", "tv"
	}
	vals := kometaValues(v)
	link := func(m map[string]string) string {
		if u := m["url"]; u != "" {
			return u
		}
		if u := m["list_id"]; u != "" {
			return u
		}
		return m["value"]
	}
	add := func(l string) { lists = append(lists, l) }
	switch {
	case key == "mdblist_list" || key == "trakt_list" || key == "yamtrack_list" || key == "imdb_list":
		for _, m := range vals {
			l := link(m)
			if !strings.HasPrefix(l, "http") {
				if key == "imdb_list" && strings.HasPrefix(l, "ls") {
					l = "https://www.imdb.com/list/" + l + "/"
				} else {
					return nil, "", fmt.Errorf("%s %q isn't a link", key, l)
				}
			}
			add(withLimit(l, m["limit"]))
			if sortBy == "" {
				sortBy = strings.ToLower(m["sort_by"])
			}
		}
	case key == "tmdb_list":
		for _, m := range vals {
			l := link(m)
			if !strings.HasPrefix(l, "http") {
				l = "https://www.themoviedb.org/list/" + l
			}
			add(l)
		}
	case key == "trakt_trending" || key == "trakt_popular" || key == "trakt_anticipated" || key == "trakt_boxoffice":
		if key == "trakt_boxoffice" && shows {
			return nil, "", fmt.Errorf("trakt_boxoffice is for movies")
		}
		add(withLimit("https://trakt.tv/"+media+"/"+strings.TrimPrefix(key, "trakt_"), firstValue(vals)))
	case traktPeriodic.MatchString(key):
		m := traktPeriodic.FindStringSubmatch(key)
		add(withLimit("https://trakt.tv/"+media+"/"+m[1]+"/"+m[2], firstValue(vals)))
	case key == "trakt_watchlist":
		for _, m := range vals {
			add("https://trakt.tv/users/" + url.PathEscape(link(m)) + "/watchlist")
		}
	case key == "tmdb_trending_daily" || key == "tmdb_trending_weekly":
		period := map[string]string{"tmdb_trending_daily": "day", "tmdb_trending_weekly": "week"}[key]
		add(withLimit("https://www.themoviedb.org/trending/"+tv+"/"+period, firstValue(vals)))
	case key == "tmdb_popular" || key == "tmdb_top_rated" || key == "tmdb_now_playing" || key == "tmdb_upcoming" || key == "tmdb_airing_today" || key == "tmdb_on_the_air":
		page := strings.ReplaceAll(strings.TrimPrefix(key, "tmdb_"), "_", "-")
		if page == "popular" {
			page = ""
		}
		movieOnly := page == "now-playing" || page == "upcoming"
		tvOnly := page == "airing-today" || page == "on-the-air"
		if (movieOnly && shows) || (tvOnly && !shows) {
			return nil, "", fmt.Errorf("%s doesn't fit this library", key)
		}
		add(withLimit(strings.TrimSuffix("https://www.themoviedb.org/"+tv+"/"+page, "/"), firstValue(vals)))
	case key == "imdb_chart":
		for _, m := range vals {
			name := m["chart"]
			if name == "" {
				name = m["value"]
			}
			path, ok := kometaIMDbCharts[strings.ToLower(name)]
			if !ok {
				return nil, "", fmt.Errorf("IMDb chart %q isn't supported", name)
			}
			add(withLimit("https://www.imdb.com"+path+"/", m["limit"]))
		}
	case strings.HasPrefix(key, "tmdb_") || strings.HasPrefix(key, "trakt_") || strings.HasPrefix(key, "imdb_") ||
		strings.HasPrefix(key, "tvdb_") || strings.HasPrefix(key, "plex_") || strings.HasPrefix(key, "smart_filter") ||
		strings.HasPrefix(key, "letterboxd_") || strings.HasPrefix(key, "mdblist_") || strings.HasPrefix(key, "anidb_") ||
		strings.HasPrefix(key, "mal_") || strings.HasPrefix(key, "anilist_") || strings.HasPrefix(key, "reciperr_") ||
		strings.HasPrefix(key, "stevenlu_") || strings.HasPrefix(key, "icheckmovies_") || strings.HasPrefix(key, "flixpatrol_"):
		return nil, "", fmt.Errorf("the %s builder isn't supported", key)
	default:
		return nil, "", nil
	}
	return lists, sortBy, nil
}

func firstValue(vals []map[string]string) string {
	if len(vals) == 0 {
		return ""
	}
	if v := vals[0]["limit"]; v != "" {
		return v
	}
	return vals[0]["value"]
}
