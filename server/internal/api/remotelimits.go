package api

import (
	"fmt"
	"strconv"

	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// remoteCapKbps is the most video a viewer at ip may be sent: the admin's
// cap outside the home network, 0 at home or with no cap.
func (s *Server) remoteCapKbps(ip string) int {
	if s.isLocal(ip) {
		return 0
	}
	n, _ := strconv.Atoi(s.Store.Setting(store.SettingRemoteMaxKbps))
	return max(n, 0)
}

// overRemoteCap reports whether playing mf as it is would send a viewer
// at ip more than the remote cap. Files of unknown bitrate are let through.
func (s *Server) overRemoteCap(mf *store.MediaFile, ip string) bool {
	limit := s.remoteCapKbps(ip)
	kbps := stream.FileKbps(mf)
	return limit > 0 && kbps > limit
}

// streamLimitHit checks the admin's limit on streams outside the home
// network before u starts one from ip. It counts the whole account (an
// account and its profiles share the limit): players, conversions that
// haven't reported in yet and live TV. Playing file fileID again from the
// same address (seeking, changing quality, falling back from direct play)
// is the same stream and isn't counted. Admin accounts have no limit.
// It returns the message to show, or "".
func (s *Server) streamLimitHit(u *store.User, ip string, fileID int64) string {
	if s.isLocal(ip) {
		return ""
	}
	limit, _ := strconv.Atoi(s.Store.Setting(store.SettingRemoteMaxStreams))
	if limit <= 0 {
		return ""
	}
	household, err := s.Store.Household(u.HouseholdID())
	if err != nil || len(household) == 0 || household[0].IsAdmin {
		return ""
	}
	users := map[int64]bool{}
	for _, m := range household {
		users[m.ID] = true
	}
	n := 0
	seen := map[string]bool{} // transcodes already counted through their player
	if s.Activity != nil {
		for _, np := range s.Activity.List() {
			if !users[np.UserID] || np.Local {
				continue
			}
			if np.TranscodeID != "" {
				seen[np.TranscodeID] = true
			}
			if fileID == 0 || np.FileID != fileID || np.IP != ip {
				n++
			}
		}
	}
	if s.Transcoder != nil {
		for _, t := range s.Transcoder.List() {
			if users[t.UserID] && t.Remote && !seen[t.ID] && t.FileID != fileID {
				n++
			}
		}
	}
	if s.Live != nil {
		for _, vip := range s.Live.ViewerIPs(users) {
			if !s.isLocal(vip) {
				n++
			}
		}
	}
	if n < limit {
		return ""
	}
	if limit == 1 {
		return "This account is already watching something away from home, and this server allows one stream at a time from outside. Stop the other one first."
	}
	return fmt.Sprintf("This account is already watching %d things away from home, the most this server allows at once from outside. Stop one first.", n)
}
