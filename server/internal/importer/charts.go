package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"bingebandit/internal/store"
)

// Chart sources, the way Kometa's tmdb_*, trakt_*, imdb_chart and
// yamtrack_list builders read them. Each one is named by a link to the page
// a person would open on that site, so a collection's list is still a URL:
//
//	https://www.themoviedb.org/movie                 TMDB popular (also /top-rated, /now-playing, /upcoming)
//	https://www.themoviedb.org/tv                    (also /top-rated, /on-the-air, /airing-today)
//	https://www.themoviedb.org/trending/movie/week   TMDB trending (movie or tv, day or week)
//	https://www.themoviedb.org/list/<id>             a TMDB list
//	https://trakt.tv/movies/trending                 (also popular, anticipated, boxoffice, watched/weekly, …; shows/…)
//	https://trakt.tv/users/<user>/lists/<list>       a Trakt list
//	https://www.imdb.com/chart/top                   (also moviemeter, toptv, tvmeter, bottom, top-english-movies)
//	https://www.imdb.com/list/ls<id>                 an IMDb list
//	https://<yamtrack>/list/<id>                     a Yamtrack list
//
// "?limit=40" keeps the first 40 titles of any of them.

// defaultChartLimit is how many titles a TMDB or Trakt chart keeps.
const defaultChartLimit = 100

// listLimit reads ?limit= from a list link; 0 means the source's default.
func listLimit(u *url.URL) int {
	n, _ := strconv.Atoi(u.Query().Get("limit"))
	return max(n, 0)
}

func (l *Lists) getJSON(ctx context.Context, raw string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header = header.Clone()
	req.Header.Set("User-Agent", "BingeBandit")
	req.Header.Set("Accept", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't read the list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{resp.StatusCode, resp.Status}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out); err != nil {
		return fmt.Errorf("the list site sent something that isn't a list")
	}
	return nil
}

type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string { return "the list site answered " + e.status }

func refused(err error) bool {
	se, ok := err.(*statusError)
	return ok && (se.code == http.StatusUnauthorized || se.code == http.StatusForbidden)
}

// ---- TMDB ----

var tmdbCharts = map[string]string{
	"/movie": "/movie/popular", "/movie/top-rated": "/movie/top_rated", "/movie/now-playing": "/movie/now_playing", "/movie/upcoming": "/movie/upcoming",
	"/tv": "/tv/popular", "/tv/top-rated": "/tv/top_rated", "/tv/on-the-air": "/tv/on_the_air", "/tv/airing-today": "/tv/airing_today",
}

var (
	tmdbTrending = regexp.MustCompile(`^/trending/(movie|tv|all)/(day|week)$`)
	tmdbList     = regexp.MustCompile(`^/list/(\d+)`)
)

type tmdbEntry struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	MediaType    string `json:"media_type"`
}

func (e tmdbEntry) record(kind string) (Record, bool) {
	switch e.MediaType {
	case "movie":
		kind = "movie"
	case "tv":
		kind = "show"
	case "":
	default:
		return Record{}, false // people
	}
	r := Record{Kind: kind, TMDB: e.ID, Title: e.Title}
	date := e.ReleaseDate
	if kind == "show" {
		r.Title, date = e.Name, e.FirstAirDate
	}
	if len(date) >= 4 {
		r.Year, _ = strconv.Atoi(date[:4])
	}
	return r, e.ID > 0
}

func (l *Lists) tmdb(ctx context.Context, u *url.URL) ([]Record, error) {
	key := strings.TrimSpace(l.st.Setting(store.SettingTMDBKey))
	if key == "" {
		return nil, fmt.Errorf("TMDB charts need the TMDB API key in Settings → Metadata")
	}
	path := strings.TrimSuffix(u.Path, "/")
	limit := listLimit(u)
	var api, kind string
	switch {
	case tmdbCharts[path] != "":
		api, kind = tmdbCharts[path], "movie"
		if strings.HasPrefix(path, "/tv") {
			kind = "show"
		}
	case tmdbTrending.MatchString(path):
		m := tmdbTrending.FindStringSubmatch(path)
		api, kind = path, map[string]string{"movie": "movie", "tv": "show"}[m[1]]
	case tmdbList.MatchString(path):
		api, kind = "/list/"+tmdbList.FindStringSubmatch(path)[1], "movie"
		if limit == 0 {
			limit = 5000
		}
	default:
		return nil, fmt.Errorf("TMDB links look like https://www.themoviedb.org/movie/top-rated, /trending/movie/week or /list/<id>")
	}
	if limit == 0 {
		limit = defaultChartLimit
	}
	header := http.Header{}
	q := url.Values{"language": {l.st.Setting(store.SettingMetadataLanguage)}}
	if strings.HasPrefix(key, "eyJ") { // a v4 read-access token
		header.Set("Authorization", "Bearer "+key)
	} else {
		q.Set("api_key", key)
	}
	var out []Record
	for page := 1; len(out) < limit && page <= 50; page++ {
		q.Set("page", strconv.Itoa(page))
		var res struct {
			Results    []tmdbEntry `json:"results"`
			Items      []tmdbEntry `json:"items"` // lists
			TotalPages int         `json:"total_pages"`
		}
		if err := l.getJSON(ctx, l.tmdbBase+api+"?"+q.Encode(), header, &res); err != nil {
			if refused(err) {
				return nil, fmt.Errorf("TMDB refused the request; check the TMDB API key in Settings → Metadata")
			}
			return nil, err
		}
		for _, e := range append(res.Results, res.Items...) {
			if r, ok := e.record(kind); ok && len(out) < limit {
				out = append(out, r)
			}
		}
		if page >= res.TotalPages {
			break
		}
	}
	return out, nil
}

// ---- Trakt ----

var (
	traktChart = regexp.MustCompile(`^/(movies|shows)/(trending|popular|anticipated|boxoffice|recommended(?:/\w+)?|(?:watched|played|collected)(?:/\w+)?)$`)
	traktList  = regexp.MustCompile(`^/users/([^/]+)/(?:lists/([^/]+)|watchlist)`)
)

type traktTitle struct {
	Title string `json:"title"`
	Year  int    `json:"year"`
	IDs   struct {
		TMDB int64  `json:"tmdb"`
		IMDB string `json:"imdb"`
		TVDB int64  `json:"tvdb"`
	} `json:"ids"`
}

func (t *traktTitle) record(kind string) Record {
	return Record{Kind: kind, Title: t.Title, Year: t.Year, TMDB: t.IDs.TMDB, IMDB: t.IDs.IMDB, TVDB: t.IDs.TVDB}
}

func (l *Lists) trakt(ctx context.Context, u *url.URL) ([]Record, error) {
	id := strings.TrimSpace(l.st.Setting(store.SettingTraktClientID))
	if id == "" {
		return nil, fmt.Errorf("Trakt lists need a Trakt client ID; add one under Settings → Trakt")
	}
	path := strings.TrimSuffix(u.Path, "/")
	limit := listLimit(u)
	var api, kind string
	switch {
	case traktChart.MatchString(path):
		m := traktChart.FindStringSubmatch(path)
		kind = map[string]string{"movies": "movie", "shows": "show"}[m[1]]
		if limit == 0 {
			limit = defaultChartLimit
		}
		api = path + "?extended=min&limit=" + strconv.Itoa(limit)
	case traktList.MatchString(path):
		m := traktList.FindStringSubmatch(path)
		api = "/users/" + url.PathEscape(m[1]) + "/watchlist"
		if m[2] != "" {
			api = "/users/" + url.PathEscape(m[1]) + "/lists/" + url.PathEscape(m[2]) + "/items"
		}
	default:
		return nil, fmt.Errorf("Trakt links look like https://trakt.tv/movies/trending or https://trakt.tv/users/<user>/lists/<list>")
	}
	header := http.Header{"Trakt-Api-Version": {"2"}, "Trakt-Api-Key": {id}, "Content-Type": {"application/json"}}
	var rows []struct {
		Type  string      `json:"type"`
		Movie *traktTitle `json:"movie"`
		Show  *traktTitle `json:"show"`
		traktTitle
	}
	if err := l.getJSON(ctx, l.traktBase+api, header, &rows); err != nil {
		if refused(err) {
			return nil, fmt.Errorf("Trakt refused the request; check the client ID under Settings → Trakt, and that the list is public")
		}
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		switch {
		case r.Movie != nil:
			out = append(out, r.Movie.record("movie"))
		case r.Show != nil:
			out = append(out, r.Show.record("show"))
		case kind != "" && r.Title != "":
			out = append(out, r.traktTitle.record(kind))
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ---- IMDb ----

// imdbCharts maps IMDb's chart pages to its GraphQL chart types.
var imdbCharts = map[string]struct {
	chart string
	first int
}{
	"/chart/top":                {"TOP_RATED_MOVIES", 250},
	"/chart/toptv":              {"TOP_RATED_TV_SHOWS", 250},
	"/chart/moviemeter":         {"MOST_POPULAR_MOVIES", 100},
	"/chart/tvmeter":            {"MOST_POPULAR_TV_SHOWS", 100},
	"/chart/bottom":             {"LOWEST_RATED_MOVIES", 100},
	"/chart/top-english-movies": {"TOP_RATED_ENGLISH_MOVIES", 250},
}

var (
	imdbListPath = regexp.MustCompile(`^/list/(ls\d+)`)
	imdbTitleID  = regexp.MustCompile(`/title/(tt\d+)`)
)

type imdbTitle struct {
	ID        string `json:"id"`
	TitleText struct {
		Text string `json:"text"`
	} `json:"titleText"`
	ReleaseYear *struct {
		Year int `json:"year"`
	} `json:"releaseYear"`
	TitleType *struct {
		ID string `json:"id"`
	} `json:"titleType"`
}

func (t *imdbTitle) record() Record {
	r := Record{Kind: "movie", IMDB: t.ID, Title: t.TitleText.Text}
	if t.TitleType != nil && strings.HasPrefix(t.TitleType.ID, "tv") && t.TitleType.ID != "tvMovie" && t.TitleType.ID != "tvSpecial" {
		r.Kind = "show"
	}
	if t.ReleaseYear != nil {
		r.Year = t.ReleaseYear.Year
	}
	return r
}

const imdbTitleFields = `id titleText { text } releaseYear { year } titleType { id }`

// imdb reads a chart or list through IMDb's GraphQL API, which the site
// itself uses, and falls back to the title links on the page.
func (l *Lists) imdb(ctx context.Context, u *url.URL, libraryKind string) ([]Record, error) {
	path := strings.TrimSuffix(u.Path, "/")
	limit := listLimit(u)
	var out []Record
	var err error
	switch {
	case imdbCharts[path].chart != "":
		c := imdbCharts[path]
		var res struct {
			ChartTitles struct {
				Edges []struct {
					Node imdbTitle `json:"node"`
				} `json:"edges"`
			} `json:"chartTitles"`
		}
		err = l.imdbQuery(ctx, fmt.Sprintf(`{ chartTitles(chart: { chartType: %s }, first: %d) { edges { node { %s } } } }`, c.chart, c.first, imdbTitleFields), &res)
		for _, e := range res.ChartTitles.Edges {
			out = append(out, e.Node.record())
		}
	case imdbListPath.MatchString(path):
		id := imdbListPath.FindStringSubmatch(path)[1]
		after := ""
		for page := 0; page < 40; page++ {
			var res struct {
				List struct {
					Items struct {
						Edges []struct {
							ListItem imdbTitle `json:"listItem"`
						} `json:"edges"`
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"titleListItemSearch"`
				} `json:"list"`
			}
			cursor := ""
			if after != "" {
				cursor = fmt.Sprintf(`, after: %q`, after)
			}
			if err = l.imdbQuery(ctx, fmt.Sprintf(`{ list(id: %q) { titleListItemSearch(first: 250%s) { edges { listItem { ... on Title { %s } } } pageInfo { hasNextPage endCursor } } } }`, id, cursor, imdbTitleFields), &res); err != nil {
				break
			}
			for _, e := range res.List.Items.Edges {
				out = append(out, e.ListItem.record())
			}
			after = res.List.Items.PageInfo.EndCursor
			if !res.List.Items.PageInfo.HasNextPage || after == "" {
				break
			}
		}
	default:
		return nil, fmt.Errorf("IMDb links look like https://www.imdb.com/chart/top or https://www.imdb.com/list/ls…")
	}
	if err != nil || len(out) == 0 {
		if out, err = l.imdbPage(ctx, u, path, libraryKind); err != nil {
			return nil, err
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (l *Lists) imdbQuery(ctx context.Context, query string, out any) error {
	body, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.imdbGraphQL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-imdb-client-name", "imdb-web-next")
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IMDb answered %s", resp.Status)
	}
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&res); err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("IMDb: %s", res.Errors[0].Message)
	}
	return json.Unmarshal(res.Data, out)
}

// imdbPage reads the title links on a chart or list page, in page order.
func (l *Lists) imdbPage(ctx context.Context, u *url.URL, path, libraryKind string) ([]Record, error) {
	body, err := l.get(ctx, l.imdbSite+path+"/")
	if err != nil {
		return nil, fmt.Errorf("couldn't read IMDb: %w", err)
	}
	defer body.Close()
	page, err := io.ReadAll(io.LimitReader(body, 16<<20))
	if err != nil {
		return nil, err
	}
	kind := "movie"
	if path == "/chart/toptv" || path == "/chart/tvmeter" || (libraryKind == "shows" && !strings.HasPrefix(path, "/chart/")) {
		kind = "show"
	}
	seen := map[string]bool{}
	var out []Record
	for _, m := range imdbTitleID.FindAllStringSubmatch(string(page), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, Record{Kind: kind, IMDB: m[1]})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("IMDb sent a page with no titles on it")
	}
	return out, nil
}

// ---- Yamtrack ----

var (
	yamtrackTMDB = regexp.MustCompile(`/details/tmdb/(movie|tv)/(\d+)(?:/|$)`)
	yamtrackTVDB = regexp.MustCompile(`/details/tvdb/tv/(\d+)(?:/[^/]+)?/?$`)
	hrefs        = regexp.MustCompile(`href="([^"]+)"`)
	csrfInput    = regexp.MustCompile(`name="csrfmiddlewaretoken"\s+value="([^"]+)"|value="([^"]+)"\s+name="csrfmiddlewaretoken"`)
)

// isYamtrack is true for links to the Yamtrack server in Settings, or that
// look like a Yamtrack list when none is set.
func (l *Lists) isYamtrack(u *url.URL) bool {
	if base, err := url.Parse(strings.TrimSpace(l.st.Setting(store.SettingYamtrackURL))); err == nil && base.Host != "" {
		return strings.EqualFold(base.Host, u.Host)
	}
	return regexp.MustCompile(`^/list/[^/]+/?$`).MatchString(u.Path)
}

// yamtrack reads a Yamtrack list's pages in its own order, the way Kometa's
// yamtrack_list does, signing in when the list isn't public.
func (l *Lists) yamtrack(ctx context.Context, u *url.URL) ([]Record, error) {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Timeout: l.client.Timeout, Transport: l.client.Transport, Jar: jar}
	limit := listLimit(u)
	q := u.Query()
	q.Del("limit")
	q.Set("sort", "custom")
	signedIn := false
	seen := map[string]bool{}
	var out []Record
	for page := 1; page <= 50; page++ {
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		pu := *u
		pu.RawQuery = q.Encode()
		html, final, err := yamtrackGet(ctx, c, pu.String(), "")
		if err != nil {
			if page > 1 {
				break // past the last page
			}
			return nil, err
		}
		if strings.HasPrefix(final.Path, "/accounts/login") {
			if signedIn || page > 1 {
				return nil, fmt.Errorf("Yamtrack didn't accept the sign-in in Settings → Metadata")
			}
			if err := l.yamtrackLogin(ctx, c, final, u.Path); err != nil {
				return nil, err
			}
			signedIn = true
			page--
			continue
		}
		added := 0
		for _, h := range hrefs.FindAllStringSubmatch(html, -1) {
			p := h[1]
			if pu, err := url.Parse(p); err == nil {
				p = pu.Path
			}
			var r Record
			if m := yamtrackTMDB.FindStringSubmatch(p); m != nil {
				r.TMDB, _ = strconv.ParseInt(m[2], 10, 64)
				r.Kind = map[string]string{"movie": "movie", "tv": "show"}[m[1]]
			} else if m := yamtrackTVDB.FindStringSubmatch(p); m != nil {
				r.TVDB, _ = strconv.ParseInt(m[1], 10, 64)
				r.Kind = "show"
			} else {
				continue
			}
			key := fmt.Sprint(r.Kind, r.TMDB, r.TVDB)
			if !seen[key] {
				seen[key] = true
				out = append(out, r)
				added++
			}
		}
		if added == 0 || (limit > 0 && len(out) >= limit) {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("that Yamtrack list has no movies or shows on it")
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func yamtrackGet(ctx context.Context, c *http.Client, raw, referer string) (string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "BingeBandit")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("couldn't reach Yamtrack: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("Yamtrack answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return string(b), resp.Request.URL, err
}

func (l *Lists) yamtrackLogin(ctx context.Context, c *http.Client, login *url.URL, next string) error {
	user, pass := strings.TrimSpace(l.st.Setting(store.SettingYamtrackUser)), l.st.Setting(store.SettingYamtrackPassword)
	if user == "" || pass == "" {
		return fmt.Errorf("this Yamtrack list isn't public; add your Yamtrack username and password in Settings → Metadata")
	}
	page, _, err := yamtrackGet(ctx, c, login.String(), "")
	if err != nil {
		return err
	}
	form := url.Values{"login": {user}, "username": {user}, "password": {pass}, "next": {next}}
	token := ""
	if m := csrfInput.FindStringSubmatch(page); m != nil {
		token = m[1] + m[2]
		form.Set("csrfmiddlewaretoken", token)
	}
	post := *login
	post.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, post.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", login.String())
	req.Header.Set("User-Agent", "BingeBandit")
	if token != "" {
		req.Header.Set("X-CSRFToken", token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't sign in to Yamtrack: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 || strings.HasPrefix(resp.Request.URL.Path, "/accounts/login") {
		return fmt.Errorf("Yamtrack didn't accept the username and password in Settings → Metadata")
	}
	return nil
}
