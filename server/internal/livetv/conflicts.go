package livetv

import (
	"fmt"
	"sort"
	"strings"

	"bingebandit/internal/store"
)

// Conflicts reports upcoming recordings that may not get a provider
// connection because other recordings at the same time already use up the
// provider's limit. Recordings on the same channel share one connection, and
// custom channels need none. Earlier recordings win; live viewers aren't
// counted (they come and go), so this is a lower bound.
func (rec *Recorder) Conflicts() (map[int64]string, error) {
	recs, err := rec.st.UpcomingRecordings()
	if err != nil {
		return nil, err
	}
	chans, err := rec.st.Channels(0, true)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*store.Channel, len(chans))
	for _, c := range chans {
		byID[c.ID] = c
	}
	return findConflicts(recs, byID), nil
}

// findConflicts walks recordings in start order, giving each one a
// connection from the first of its channel's providers with one free.
func findConflicts(recs []*store.Recording, channels map[int64]*store.Channel) map[int64]string {
	sorted := append([]*store.Recording(nil), recs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartAt != sorted[j].StartAt {
			return sorted[i].StartAt < sorted[j].StartAt
		}
		return sorted[i].ID < sorted[j].ID
	})
	type slot struct {
		r      *store.Recording
		source int64 // 0 = needs no provider connection
	}
	var active []slot
	out := map[int64]string{}
	for _, r := range sorted {
		// Recordings that end before this one starts free their connection.
		still := active[:0]
		for _, a := range active {
			if a.r.StopAt > r.StartAt {
				still = append(still, a)
			}
		}
		active = still

		ch := channels[r.ChannelID]
		switch {
		case ch == nil:
			out[r.ID] = "the channel was deleted"
			continue
		case ch.Custom:
			active = append(active, slot{r, 0})
			continue
		case len(ch.Streams) == 0:
			out[r.ID] = ErrNoStreams.Error()
			continue
		}
		shared := false
		for _, a := range active {
			if a.r.ChannelID == r.ChannelID {
				active = append(active, slot{r, a.source})
				shared = true
				break
			}
		}
		if shared {
			continue
		}
		// Connections in use per provider: one per channel being recorded.
		inUse := map[int64]map[int64]bool{}
		for _, a := range active {
			if a.source == 0 {
				continue
			}
			if inUse[a.source] == nil {
				inUse[a.source] = map[int64]bool{}
			}
			inUse[a.source][a.r.ChannelID] = true
		}
		assigned := false
		for _, s := range ch.Streams {
			if s.MaxStreams <= 0 || len(inUse[s.SourceID]) < s.MaxStreams {
				active = append(active, slot{r, s.SourceID})
				assigned = true
				break
			}
		}
		if assigned {
			continue
		}
		s := ch.Streams[0]
		var others []string
		seen := map[string]bool{}
		for _, a := range active {
			for _, cs := range ch.Streams {
				if a.source == cs.SourceID && !seen[a.r.Title] {
					seen[a.r.Title] = true
					others = append(others, a.r.Title)
				}
			}
		}
		sources := map[int64]bool{}
		for _, cs := range ch.Streams {
			sources[cs.SourceID] = true
		}
		if len(sources) > 1 {
			out[r.ID] = "every provider for this channel is at its connection limit then: " + strings.Join(others, ", ")
		} else {
			out[r.ID] = fmt.Sprintf("%s allows %d connection%s and other recordings use %s then: %s",
				s.SourceName, s.MaxStreams, plural(s.MaxStreams), pronoun(s.MaxStreams), strings.Join(others, ", "))
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}
