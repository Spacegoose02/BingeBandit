package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"bingebandit/internal/store"
)

// commonNetworks are offered on Admin → Live TV → Networks before ESPN has
// listed any games, spelled the way ESPN lists them.
var commonNetworks = []string{"ABC", "CBS", "FOX", "NBC", "The CW", "ESPN", "ESPN2", "ESPNU", "FS1", "FS2", "TNT", "TBS", "truTV",
	"USA Net", "CBS Sports Network", "NFL Network", "NBA TV", "MLB Network", "NHL Network", "Big Ten Network", "SEC Network", "ACC Network", "Golf Channel"}

type networksOut struct {
	Networks []store.NetworkChannels `json:"networks"`
	// Known are network names to offer: ones ESPN has listed for games since
	// the server started, plus the usual ones.
	Known []string `json:"known"`
}

func (s *Server) adminNetworks(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Networks()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, networksOut{list, s.knownNetworks()})
}

func (s *Server) knownNetworks() []string {
	seen := map[string]string{}
	for _, n := range commonNetworks {
		seen[strings.ToLower(n)] = n
	}
	s.networksSeen.Range(func(k, v any) bool {
		if k.(string) != "" {
			seen[k.(string)] = v.(string)
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// saveNetworks replaces the network → channel mapping.
func (s *Server) saveNetworks(w http.ResponseWriter, r *http.Request) {
	var in []store.NetworkChannels
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "send a list of {network, channels}")
		return
	}
	if err := s.Store.SetNetworks(in); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.adminNetworks(w, r)
}
