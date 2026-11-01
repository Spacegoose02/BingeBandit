package store

import "strings"

// NetworkChannels are the channels an admin says carry a TV network, such as
// CBS on WDBJ 7. The Sports page uses them to find a game's local channel.
type NetworkChannels struct {
	Network  string  `json:"network"`
	Channels []int64 `json:"channels"`
}

// Networks lists every mapped network with its channels in the admin's order.
func (s *Store) Networks() ([]NetworkChannels, error) {
	rows, err := s.db.Query(`SELECT network, channel_id FROM network_channels ORDER BY network COLLATE NOCASE, position, channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NetworkChannels{}
	for rows.Next() {
		var n string
		var id int64
		if err := rows.Scan(&n, &id); err != nil {
			return nil, err
		}
		if len(out) == 0 || !strings.EqualFold(out[len(out)-1].Network, n) {
			out = append(out, NetworkChannels{Network: n})
		}
		last := &out[len(out)-1]
		last.Channels = append(last.Channels, id)
	}
	return out, rows.Err()
}

// SetNetworks replaces the whole mapping. Blank names, unknown channels and
// networks with no channels are dropped.
func (s *Store) SetNetworks(list []NetworkChannels) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM network_channels`); err != nil {
		return err
	}
	for _, n := range list {
		name := strings.TrimSpace(n.Network)
		if name == "" {
			continue
		}
		for i, id := range n.Channels {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO network_channels (network, channel_id, position)
				SELECT ?, id, ? FROM channels WHERE id = ?`, name, i, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
