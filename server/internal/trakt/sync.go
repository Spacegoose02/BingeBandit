package trakt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"bingebandit/internal/importer"
	"bingebandit/internal/store"
)

// ErrNotConfigured means the admin hasn't added a Trakt app yet.
var ErrNotConfigured = errors.New("Trakt isn't set up on this server yet; an admin can add it under Settings → Trakt")

// Syncer connects users to Trakt and moves history both ways.
type Syncer struct {
	st  *store.Store
	api string

	mu      sync.Mutex
	devices map[int64]*DeviceCode // users mid-connection
	pushing map[int64]*sync.Mutex // one push per user at a time
}

func NewSyncer(st *store.Store, api string) *Syncer {
	return &Syncer{st: st, api: api, devices: map[int64]*DeviceCode{}, pushing: map[int64]*sync.Mutex{}}
}

func (s *Syncer) client() (*Client, error) {
	id, secret := s.st.Setting(store.SettingTraktClientID), s.st.Setting(store.SettingTraktSecret)
	if id == "" || secret == "" {
		return nil, ErrNotConfigured
	}
	return NewClient(s.api, id, secret), nil
}

func (s *Syncer) Configured() bool {
	_, err := s.client()
	return err == nil
}

// Start begins connecting: show the returned code to the person.
func (s *Syncer) Start(ctx context.Context, uid int64) (*DeviceCode, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	d, err := c.DeviceCode(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.devices[uid] = d
	s.mu.Unlock()
	return d, nil
}

// Poll finishes connecting once the person approved the code; until then it
// returns ErrPending.
func (s *Syncer) Poll(ctx context.Context, uid int64) (*store.TraktAccount, error) {
	c, err := s.client()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	d := s.devices[uid]
	s.mu.Unlock()
	if d == nil {
		return nil, ErrExpired
	}
	tok, err := c.PollToken(ctx, d.DeviceCode)
	if err != nil {
		if errors.Is(err, ErrExpired) || errors.Is(err, ErrDenied) {
			s.mu.Lock()
			delete(s.devices, uid)
			s.mu.Unlock()
		}
		return nil, err
	}
	s.mu.Lock()
	delete(s.devices, uid)
	s.mu.Unlock()
	name, err := c.Username(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	acc := &store.TraktAccount{UserID: uid, Username: name, AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken,
		ExpiresAt: tok.ExpiresAt(), AutoSync: true}
	if err := s.st.SaveTraktAccount(acc); err != nil {
		return nil, err
	}
	log.Printf("trakt: user %d connected as %s", uid, name)
	return s.st.TraktAccount(uid)
}

func (s *Syncer) Disconnect(ctx context.Context, uid int64) error {
	if acc, err := s.st.TraktAccount(uid); err == nil {
		if c, err := s.client(); err == nil {
			c.Revoke(ctx, acc.AccessToken)
		}
	}
	return s.st.DeleteTraktAccount(uid)
}

// token returns a usable access token, refreshing it when it's close to expiring.
func (s *Syncer) token(ctx context.Context, uid int64) (*Client, *store.TraktAccount, error) {
	c, err := s.client()
	if err != nil {
		return nil, nil, err
	}
	acc, err := s.st.TraktAccount(uid)
	if err != nil {
		return nil, nil, err
	}
	if acc.ExpiresAt-time.Now().Unix() < 3600 {
		tok, err := c.Refresh(ctx, acc.RefreshToken)
		if err != nil {
			s.st.SetTraktResult(uid, err.Error())
			return nil, nil, err
		}
		acc.AccessToken, acc.RefreshToken, acc.ExpiresAt = tok.AccessToken, tok.RefreshToken, tok.ExpiresAt()
		if err := s.st.SaveTraktAccount(acc); err != nil {
			return nil, nil, err
		}
	}
	return c, acc, nil
}

// PushResult is what a push sent.
type PushResult struct {
	Movies   int `json:"movies"`
	Episodes int `json:"episodes"`
	NotFound int `json:"notFound"`
}

// Push sends finished plays (started at or after since) that Trakt doesn't have yet.
func (s *Syncer) Push(ctx context.Context, uid, since int64) (PushResult, error) {
	s.mu.Lock()
	lock := s.pushing[uid]
	if lock == nil {
		lock = &sync.Mutex{}
		s.pushing[uid] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()

	var res PushResult
	c, acc, err := s.token(ctx, uid)
	if err != nil {
		return res, err
	}
	for {
		plays, err := s.st.PendingTraktPlays(uid, since, 100)
		if err != nil || len(plays) == 0 {
			if err == nil {
				s.st.SetTraktResult(uid, "")
			}
			return res, err
		}
		r, err := c.AddHistory(ctx, acc.AccessToken, historyBody(plays))
		if err != nil {
			s.st.SetTraktResult(uid, err.Error())
			return res, err
		}
		res.Movies += r.Added.Movies
		res.Episodes += r.Added.Episodes
		res.NotFound += len(r.NotFound.Movies) + len(r.NotFound.Shows) + len(r.NotFound.Episodes)
		// Plays Trakt couldn't place are marked too, so they aren't retried forever.
		ids := make([]int64, len(plays))
		for i, p := range plays {
			ids[i] = p.PlayID
		}
		if err := s.st.MarkTraktSent(ids); err != nil {
			return res, err
		}
	}
}

// PushNew sends a user's new plays in the background if they turned on automatic sync.
func (s *Syncer) PushNew(uid int64) {
	acc, err := s.st.TraktAccount(uid)
	if err != nil || !acc.AutoSync || !s.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if r, err := s.Push(ctx, uid, acc.ConnectedAt); err != nil {
			log.Printf("trakt: sync for user %d: %v", uid, err)
		} else if r.Movies+r.Episodes > 0 {
			log.Printf("trakt: sent %d movies, %d episodes for %s", r.Movies, r.Episodes, acc.Username)
		}
	}()
}

func ids(tmdb int64) IDs { return IDs{TMDB: tmdb} }

// historyBody groups plays into Trakt's movies/shows → seasons → episodes shape.
// Items without a TMDB id are sent by title and year.
func historyBody(plays []store.WatchedPlay) *AddHistory {
	body := &AddHistory{}
	shows := map[string]int{} // show key → index in body.Shows
	for _, p := range plays {
		at := time.Unix(p.WatchedAt, 0).UTC().Format(time.RFC3339)
		if p.Kind == "movie" {
			m := HistoryMovie{WatchedAt: at, IDs: ids(p.TMDBID)}
			if p.TMDBID == 0 {
				m.Title, m.Year = p.Title, p.Year
			}
			body.Movies = append(body.Movies, m)
			continue
		}
		key := "tmdb:" + strconv.FormatInt(p.TMDBID, 10)
		if p.TMDBID == 0 {
			key = fmt.Sprintf("title:%s:%d", p.Title, p.Year)
		}
		i, ok := shows[key]
		if !ok {
			sh := HistoryShow{IDs: ids(p.TMDBID)}
			if p.TMDBID == 0 {
				sh.Title, sh.Year = p.Title, p.Year
			}
			body.Shows = append(body.Shows, sh)
			i = len(body.Shows) - 1
			shows[key] = i
		}
		sh := &body.Shows[i]
		si := -1
		for j := range sh.Seasons {
			if sh.Seasons[j].Number == p.Season {
				si = j
			}
		}
		if si < 0 {
			sh.Seasons = append(sh.Seasons, HistorySeason{Number: p.Season})
			si = len(sh.Seasons) - 1
		}
		sh.Seasons[si].Episodes = append(sh.Seasons[si].Episodes, HistoryEpisode{Number: p.Episode, WatchedAt: at})
	}
	return body
}

// Import brings the person's Trakt history into their BingeBandit history and watched marks.
func (s *Syncer) Import(ctx context.Context, uid int64) (importer.Status, error) {
	c, acc, err := s.token(ctx, uid)
	if err != nil {
		return importer.Status{}, err
	}
	src := &source{c: c, token: acc.AccessToken}
	st, err := importer.RunOnce(ctx, s.st, src, importer.Options{History: true, Watched: true, Users: map[string]int64{"trakt": uid}})
	if err == nil {
		s.st.SetTraktResult(uid, "")
	}
	return st, err
}

// source reads one person's Trakt history for the importer.
type source struct {
	c     *Client
	token string
}

func (t *source) Kind() string { return "trakt" }

func (t *source) Users(context.Context) ([]importer.SourceUser, error) {
	return []importer.SourceUser{{Name: "trakt"}}, nil
}

func (t *source) Collections(context.Context, func(importer.Collection) error) error { return nil }

func (t *source) Records(ctx context.Context, _ bool, emit func(importer.Record) error, progress func(string, int, int)) error {
	for _, kind := range []string{"movies", "episodes"} {
		for page := 1; ; page++ {
			entries, pages, err := t.c.History(ctx, t.token, kind, page)
			if err != nil {
				return err
			}
			progress("Reading Trakt "+kind, page, pages)
			for _, e := range entries {
				r := importer.Record{Key: "trakt:" + strconv.FormatInt(e.ID, 10), User: "trakt", ViewedAt: e.WatchedAt.Unix(),
					Completed: true, Client: "Trakt"}
				switch {
				case e.Movie != nil:
					r.Kind, r.Title, r.Year, r.TMDB = "movie", e.Movie.Title, e.Movie.Year, e.Movie.IDs.TMDB
				case e.Show != nil && e.Episode != nil:
					r.Kind, r.Title = "episode", e.Episode.Title
					r.ShowTitle, r.ShowYear, r.TMDB = e.Show.Title, e.Show.Year, e.Show.IDs.TMDB
					r.Season, r.Episode = e.Episode.Season, e.Episode.Number
				default:
					continue
				}
				if err := emit(r); err != nil {
					return err
				}
			}
			if page >= pages || len(entries) == 0 {
				break
			}
		}
	}
	return nil
}
