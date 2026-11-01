package store

import (
	"strings"
)

// PushDevice is a browser (or an installed web app on a phone) that gets a
// user's notifications as push messages, even with BingeBandit closed.
type PushDevice struct {
	ID       int64    `json:"id"`
	UserID   int64    `json:"-"`
	Endpoint string   `json:"endpoint"` // the push service address; only ever shown to its owner
	P256dh   string   `json:"-"`
	Auth     string   `json:"-"`
	Device   string   `json:"device"`   // "Chrome on Android", as the browser described itself
	KindsOff []string `json:"kindsOff"` // notification kinds this device doesn't want
	Created  int64    `json:"createdAt"`
	LastUsed int64    `json:"lastUsed,omitempty"`
}

// PushKinds are the kinds of notification a device can turn off: the
// Notification kinds, plus "together" for watch together invitations.
var PushKinds = []string{"request", "sports", "together", "live", "episodes", "recording", "server"}

func cleanKinds(kinds []string) string {
	var out []string
	for _, k := range PushKinds {
		for _, want := range kinds {
			if want == k {
				out = append(out, k)
				break
			}
		}
	}
	return strings.Join(out, ",")
}

func splitKinds(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// SavePushDevice adds d, or updates it when the browser already has a
// subscription (its endpoint), moving it to d.UserID: the browser belongs to
// whoever signed in on it last.
func (s *Store) SavePushDevice(d *PushDevice) error {
	t := now()
	err := s.db.QueryRow(`INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, device, kinds_off, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth,
			device = excluded.device, kinds_off = excluded.kinds_off
		RETURNING id, created_at`,
		d.UserID, d.Endpoint, d.P256dh, d.Auth, d.Device, cleanKinds(d.KindsOff), t).Scan(&d.ID, &d.Created)
	if err != nil {
		return err
	}
	d.KindsOff = splitKinds(cleanKinds(d.KindsOff))
	return nil
}

// PushDevices are a user's devices, newest first.
func (s *Store) PushDevices(userID int64) ([]*PushDevice, error) {
	rows, err := s.db.Query(`SELECT id, user_id, endpoint, p256dh, auth, device, kinds_off, created_at, last_used
		FROM push_subscriptions WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PushDevice{}
	for rows.Next() {
		var d PushDevice
		var off string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Endpoint, &d.P256dh, &d.Auth, &d.Device, &off, &d.Created, &d.LastUsed); err != nil {
			return nil, err
		}
		d.KindsOff = splitKinds(off)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// SetPushKindsOff changes which kinds one of the user's devices skips.
func (s *Store) SetPushKindsOff(userID, id int64, kinds []string) error {
	res, err := s.db.Exec(`UPDATE push_subscriptions SET kinds_off = ? WHERE id = ? AND user_id = ?`, cleanKinds(kinds), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePushDevice forgets one of the user's devices.
func (s *Store) DeletePushDevice(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DropPushEndpoint forgets a subscription the push service says is gone.
func (s *Store) DropPushEndpoint(endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

// TouchPushDevice records a delivered message.
func (s *Store) TouchPushDevice(id int64) {
	s.db.Exec(`UPDATE push_subscriptions SET last_used = ? WHERE id = ?`, now(), id)
}
