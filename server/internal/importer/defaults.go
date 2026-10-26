package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// Defaults builds Kometa-style default collections in every movie and show
// library: one per studio (network for shows), decade and genre with enough
// titles, and award winners. Studios, decades and genres are smart
// collections, so they stay current on their own; award winners come from
// Wikidata and are refreshed each run. Each group has its own switch in
// Admin → Overlays, and turning one off removes its collections (only
// the ones BingeBandit made).
type Defaults struct {
	st     *store.Store
	client *http.Client
	// wikidata is the SPARQL endpoint; tests point it elsewhere.
	wikidata string
}

func NewDefaults(st *store.Store) *Defaults {
	return &Defaults{st: st, client: &http.Client{Timeout: 60 * time.Second}, wikidata: "https://query.wikidata.org/sparql"}
}

// maxPerGroup keeps a big library from getting hundreds of studio collections.
const maxPerGroup = 40

// Award is a prize whose winners become a collection. Labels are the
// award's English names on Wikidata (some were renamed over the years).
type Award struct {
	ID     string
	Name   string
	Kind   string // movies | shows
	Labels []string
}

var Awards = []Award{
	{"oscar-best-picture", "Oscar Best Picture Winners", "movies", []string{"Academy Award for Best Picture"}},
	{"oscar-animated", "Oscar Best Animated Feature Winners", "movies", []string{"Academy Award for Best Animated Feature"}},
	{"oscar-international", "Oscar Best International Feature Winners", "movies", []string{"Academy Award for Best International Feature Film"}},
	{"golden-globe-drama", "Golden Globe Best Drama Winners", "movies", []string{"Golden Globe Award for Best Motion Picture – Drama"}},
	{"golden-globe-comedy", "Golden Globe Best Musical or Comedy Winners", "movies", []string{"Golden Globe Award for Best Motion Picture – Musical or Comedy"}},
	{"bafta-best-film", "BAFTA Best Film Winners", "movies", []string{"BAFTA Award for Best Film"}},
	{"palme-dor", "Palme d'Or Winners", "movies", []string{"Palme d'Or"}},
	{"golden-lion", "Golden Lion Winners", "movies", []string{"Golden Lion"}},
	{"golden-bear", "Golden Bear Winners", "movies", []string{"Golden Bear"}},
	{"emmy-drama", "Emmy Outstanding Drama Series Winners", "shows", []string{"Primetime Emmy Award for Outstanding Drama Series"}},
	{"emmy-comedy", "Emmy Outstanding Comedy Series Winners", "shows", []string{"Primetime Emmy Award for Outstanding Comedy Series"}},
	{"emmy-limited", "Emmy Outstanding Limited Series Winners", "shows", []string{
		"Primetime Emmy Award for Outstanding Limited or Anthology Series",
		"Primetime Emmy Award for Outstanding Limited Series",
		"Primetime Emmy Award for Outstanding Miniseries",
	}},
	{"golden-globe-tv-drama", "Golden Globe Best TV Drama Winners", "shows", []string{"Golden Globe Award for Best Television Series – Drama"}},
	{"golden-globe-tv-comedy", "Golden Globe Best TV Musical or Comedy Winners", "shows", []string{"Golden Globe Award for Best Television Series – Musical or Comedy"}},
}

type defaultWanted struct {
	name, overview string
	rules          store.SmartRules
	members        []int64 // award winners; nil for smart collections
}

// Build makes and removes default collections to match the settings.
func (d *Defaults) Build(ctx context.Context) (string, error) {
	on := func(k string) bool { return d.st.Setting(k) == "true" }
	studios, decades, genres, awards := on(store.SettingDefaultStudios), on(store.SettingDefaultDecades), on(store.SettingDefaultGenres), on(store.SettingDefaultAwards)
	minimum, _ := strconv.Atoi(d.st.Setting(store.SettingDefaultMinimum))
	if minimum < 1 {
		minimum = 1
	}
	libs, err := d.st.Libraries()
	if err != nil {
		return "", err
	}
	winners := map[string][]store.ExternalRef{} // award id → winners, fetched once a run
	failedAwards := map[string]error{}
	created, removed := 0, 0
	for _, lib := range libs {
		if lib.Kind != "movies" && lib.Kind != "shows" {
			continue
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		have, err := d.st.BuiltinCollections(lib.ID)
		if err != nil {
			return "", err
		}
		want := map[string]defaultWanted{}
		keep := map[string]bool{} // award collections whose list couldn't be read this time
		studioFacets, genreFacets, decadeFacets, err := d.st.LibraryFacetCounts(lib.ID)
		if err != nil {
			return "", err
		}
		things := "movies"
		if lib.Kind == "shows" {
			things = "shows"
		}
		if studios {
			for _, f := range top(studioFacets, minimum) {
				want["studio:"+strings.ToLower(f.Value)] = defaultWanted{name: f.Value, rules: store.SmartRules{StudioRule: f.Value},
					overview: fmt.Sprintf("Every %s from %s.", strings.TrimSuffix(things, "s"), f.Value)}
			}
		}
		if genres {
			for _, f := range top(genreFacets, minimum) {
				want["genre:"+strings.ToLower(f.Value)] = defaultWanted{name: f.Value, rules: store.SmartRules{GenreRule: f.Value}}
			}
		}
		if decades {
			for _, f := range decadeFacets {
				y, _ := strconv.Atoi(f.Value)
				if f.Count < minimum || y < 1800 {
					continue
				}
				want["decade:"+f.Value] = defaultWanted{name: fmt.Sprintf("%ds", y), rules: store.SmartRules{YearFrom: y, YearTo: y + 9},
					overview: fmt.Sprintf("%s released from %d to %d.", strings.ToUpper(things[:1])+things[1:], y, y+9)}
			}
		}
		if awards {
			kind := "movie"
			if lib.Kind == "shows" {
				kind = "show"
			}
			for _, a := range Awards {
				if a.Kind != lib.Kind {
					continue
				}
				key := "award:" + a.ID
				refs, ok := winners[a.ID]
				if !ok && failedAwards[a.ID] == nil {
					if refs, err = d.AwardWinners(ctx, a); err != nil {
						failedAwards[a.ID] = err
					} else {
						winners[a.ID] = refs
					}
				}
				if failedAwards[a.ID] != nil {
					keep[key] = true
					continue
				}
				ids, err := d.st.ItemsByExternalIDs(lib.ID, kind, refs)
				if err != nil {
					return "", err
				}
				if len(ids) > 0 {
					want[key] = defaultWanted{name: a.Name, members: ids,
						overview: fmt.Sprintf("Winners of the %s in this library, from Wikidata.", a.Labels[0])}
				}
			}
		}
		for key, w := range want {
			id, ok := have[key]
			if !ok {
				id, err = d.st.CreateBuiltinCollection(lib.ID, key, w.name, w.overview, w.rules)
				if errors.Is(err, store.ErrExists) {
					continue // the admin has their own collection by that name
				}
				if err != nil {
					return "", err
				}
				created++
			}
			if w.members != nil {
				if err := d.st.SetBuiltinMembers(id, w.members); err != nil {
					return "", err
				}
			}
		}
		for key, id := range have {
			if _, ok := want[key]; ok || keep[key] {
				continue
			}
			if err := d.st.DeleteCollection(id); err != nil {
				return "", err
			}
			removed++
		}
	}
	msg := fmt.Sprintf("%d default collections made, %d removed", created, removed)
	if len(failedAwards) > 0 {
		var names []string
		for id := range failedAwards {
			names = append(names, id)
		}
		sort.Strings(names)
		msg += fmt.Sprintf("; couldn't read award winners from Wikidata (%s): %v", strings.Join(names, ", "), failedAwards[names[0]])
	}
	return msg, nil
}

// top keeps facets with at least min titles, the biggest first.
func top(facets []store.Facet, min int) []store.Facet {
	var out []store.Facet
	for _, f := range facets {
		if f.Count >= min && len(out) < maxPerGroup {
			out = append(out, f)
		}
	}
	return out
}

// AwardWinners asks Wikidata for the films or shows that received an award
// ("award received", P166), with their IMDb and TMDB ids, oldest win first.
func (d *Defaults) AwardWinners(ctx context.Context, a Award) ([]store.ExternalRef, error) {
	var labels []string
	for _, l := range a.Labels {
		labels = append(labels, sparqlString(l))
		if strings.Contains(l, "–") { // Wikidata labels use an en dash; aliases sometimes a hyphen
			labels = append(labels, sparqlString(strings.ReplaceAll(l, "–", "-")))
		}
		if strings.Contains(l, "'") {
			labels = append(labels, sparqlString(strings.ReplaceAll(l, "'", "’")))
		}
	}
	query := `SELECT ?imdb ?tmdbMovie ?tmdbShow (MIN(?y) AS ?year) WHERE {
  VALUES ?label { ` + strings.Join(labels, " ") + ` }
  ?award rdfs:label ?label .
  ?work p:P166 ?st . ?st ps:P166 ?award .
  OPTIONAL { ?st pq:P585 ?t . BIND(YEAR(?t) AS ?y) }
  OPTIONAL { ?work wdt:P345 ?imdb }
  OPTIONAL { ?work wdt:P4947 ?tmdbMovie }
  OPTIONAL { ?work wdt:P4983 ?tmdbShow }
} GROUP BY ?imdb ?tmdbMovie ?tmdbShow ORDER BY ?year`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.wikidata+"?"+url.Values{"query": {query}, "format": {"json"}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	// Wikidata asks every client to say who it is.
	req.Header.Set("User-Agent", "BingeBandit/1.0 (https://github.com/Spacegoose02/BingeBandit)")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Wikidata answered %s", resp.Status)
	}
	var out struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("Wikidata's answer: %w", err)
	}
	tmdbKey := "tmdbMovie"
	if a.Kind == "shows" {
		tmdbKey = "tmdbShow"
	}
	var refs []store.ExternalRef
	for _, b := range out.Results.Bindings {
		r := store.ExternalRef{IMDb: b["imdb"].Value}
		r.TMDB, _ = strconv.ParseInt(b[tmdbKey].Value, 10, 64)
		if r.IMDb != "" || r.TMDB > 0 {
			refs = append(refs, r)
		}
	}
	return refs, nil
}

func sparqlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"@en`
}
