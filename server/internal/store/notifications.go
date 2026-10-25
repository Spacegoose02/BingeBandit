package store

import (
	"strings"
)

// Notification is a message for one user in the app.
type Notification struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Kind      string `json:"kind"` // episodes | recording
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Link      string `json:"link,omitempty"`
	Image     string `json:"image,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	ReadAt    int64  `json:"readAt,omitempty"`
}

const notificationKeep = 60 * 24 * 3600 // seconds

func (s *Store) AddNotification(n *Notification) error {
	n.CreatedAt = now()
	res, err := s.db.Exec(`INSERT INTO notifications (user_id, kind, title, body, link, image, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.UserID, n.Kind, n.Title, n.Body, n.Link, n.Image, n.CreatedAt)
	if err != nil {
		return err
	}
	n.ID, _ = res.LastInsertId()
	return nil
}

// Notifications are a user's newest notifications and how many are unread.
func (s *Store) Notifications(userID int64, limit int) ([]*Notification, int, error) {
	var unread int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at = 0`, userID).Scan(&unread); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, user_id, kind, title, body, link, image, created_at, read_at FROM notifications
		WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.Image, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &n)
	}
	return out, unread, rows.Err()
}

func (s *Store) UnreadNotifications(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at = 0`, userID).Scan(&n)
	return n, err
}

// ReadNotifications marks the given notifications read, or all when ids is empty.
func (s *Store) ReadNotifications(userID int64, ids []int64) error {
	if len(ids) == 0 {
		_, err := s.db.Exec(`UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at = 0`, now(), userID)
		return err
	}
	args := []any{now(), userID}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Exec(`UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at = 0 AND id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args...)
	return err
}

// ClearNotifications deletes all of a user's notifications.
func (s *Store) ClearNotifications(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM notifications WHERE user_id = ?`, userID)
	return err
}

// PruneNotifications drops notifications older than two months.
func (s *Store) PruneNotifications() error {
	_, err := s.db.Exec(`DELETE FROM notifications WHERE created_at < ?`, now()-notificationKeep)
	return err
}

// NewEpisode is an episode added to a library.
type NewEpisode struct {
	ID        int64
	ShowID    int64
	ShowTitle string
	Season    int
	Episode   int
	Title     string
	AddedAt   int64
}

// EpisodesAddedSince lists episodes added after since (oldest first), at most limit.
func (s *Store) EpisodesAddedSince(since int64, limit int) ([]NewEpisode, error) {
	rows, err := s.db.Query(`SELECT e.id, e.show_id, sh.title, COALESCE(e.season_number, 0), COALESCE(e.episode_number, 0), e.title, e.added_at
		FROM items e JOIN items sh ON sh.id = e.show_id
		WHERE e.kind = 'episode' AND e.added_at > ? ORDER BY e.added_at, e.id LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NewEpisode
	for rows.Next() {
		var e NewEpisode
		if err := rows.Scan(&e.ID, &e.ShowID, &e.ShowTitle, &e.Season, &e.Episode, &e.Title, &e.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ShowFollowers are users who have started or finished an episode of the
// show, or have it on their watchlist.
func (s *Store) ShowFollowers(showID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT w.user_id FROM watch_state w JOIN items e ON e.id = w.item_id
			WHERE e.show_id = ? AND (w.watched = 1 OR w.position > 0)
		UNION SELECT user_id FROM watchlist WHERE item_id = ?`, showID, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// WatchedItem reports whether the user has finished the item.
func (s *Store) WatchedItem(userID, itemID int64) bool {
	var w int
	s.db.QueryRow(`SELECT watched FROM watch_state WHERE user_id = ? AND item_id = ?`, userID, itemID).Scan(&w)
	return w == 1
}
