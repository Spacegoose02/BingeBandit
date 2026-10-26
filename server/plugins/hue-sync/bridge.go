package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Bridge talks to a Hue bridge's local API (v1, plain HTTP on the LAN).
type Bridge struct {
	Addr     string // host or host:port; "http://" is added when missing
	Username string
	client   *http.Client
}

func newBridge(addr, username string) *Bridge {
	return &Bridge{Addr: addr, Username: username, client: &http.Client{Timeout: 4 * time.Second}}
}

func (b *Bridge) base() string {
	a := strings.TrimRight(b.Addr, "/")
	if !strings.HasPrefix(a, "http://") && !strings.HasPrefix(a, "https://") {
		a = "http://" + a
	}
	return a + "/api"
}

// hueError is one entry of the bridge's error list.
type hueError struct {
	Type        int    `json:"type"`
	Description string `json:"description"`
}

var errLinkButton = errors.New("press the round link button on the bridge, then try again within 30 seconds")

func (b *Bridge) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base()+path, rd)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bridge: %s", resp.Status)
	}
	// Errors come back as 200 with [{"error": {...}}].
	var errs []struct {
		Error *hueError `json:"error"`
	}
	if json.Unmarshal(raw, &errs) == nil {
		for _, e := range errs {
			if e.Error == nil {
				continue
			}
			switch e.Error.Type {
			case 101:
				return errLinkButton
			case 1:
				return errors.New("the bridge doesn't know this app any more; pair again")
			}
			return fmt.Errorf("bridge: %s", e.Error.Description)
		}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Pair asks the bridge for a username; the link button must have been pressed.
func (b *Bridge) Pair(ctx context.Context) (string, error) {
	var res []struct {
		Success struct {
			Username string `json:"username"`
		} `json:"success"`
	}
	if err := b.do(ctx, http.MethodPost, "", map[string]string{"devicetype": "bingebandit#hue-sync"}, &res); err != nil {
		return "", err
	}
	if len(res) == 0 || res[0].Success.Username == "" {
		return "", errors.New("the bridge didn't answer with a username")
	}
	return res[0].Success.Username, nil
}

// LightState is what the bridge reports, and what restoring puts back.
type LightState struct {
	On        bool      `json:"on"`
	Bri       int       `json:"bri,omitempty"`
	XY        []float64 `json:"xy,omitempty"`
	CT        int       `json:"ct,omitempty"`
	ColorMode string    `json:"colormode,omitempty"`
	Reachable *bool     `json:"reachable,omitempty"`
}

type Light struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Type   string     `json:"type"`
	State  LightState `json:"state"`
	Colour bool       `json:"colour"` // takes xy colours
	Room   string     `json:"room,omitempty"`
}

// Lights lists the bridge's lights, with the room each is in.
func (b *Bridge) Lights(ctx context.Context) ([]Light, error) {
	var raw map[string]Light
	if err := b.do(ctx, http.MethodGet, "/"+b.Username+"/lights", nil, &raw); err != nil {
		return nil, err
	}
	var groups map[string]struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		Lights []string `json:"lights"`
	}
	b.do(ctx, http.MethodGet, "/"+b.Username+"/groups", nil, &groups)
	room := map[string]string{}
	for _, g := range groups {
		if g.Type == "Room" {
			for _, id := range g.Lights {
				if room[id] == "" {
					room[id] = g.Name
				}
			}
		}
	}
	out := make([]Light, 0, len(raw))
	for id, l := range raw {
		l.ID = id
		l.Colour = l.State.XY != nil
		l.Room = room[id]
		out = append(out, l)
	}
	sortLights(out)
	return out, nil
}

// Area is one of the Hue app's rooms or zones.
type Area struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Type   string   `json:"type"` // Room or Zone
	Lights []string `json:"lights"`
}

// Areas lists the bridge's rooms, then its zones, by name.
func (b *Bridge) Areas(ctx context.Context) ([]Area, error) {
	var groups map[string]Area
	if err := b.do(ctx, http.MethodGet, "/"+b.Username+"/groups", nil, &groups); err != nil {
		return nil, err
	}
	out := []Area{}
	for id, g := range groups {
		if g.Type == "Room" || g.Type == "Zone" {
			g.ID = id
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == "Room"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// SetState changes one light. transition is in tenths of a second.
func (b *Bridge) SetState(ctx context.Context, id string, state map[string]any) error {
	return b.do(ctx, http.MethodPut, "/"+b.Username+"/lights/"+id+"/state", state, nil)
}

// Restore puts a light back the way it was before syncing started.
func (b *Bridge) Restore(ctx context.Context, id string, s LightState) error {
	state := map[string]any{"on": s.On, "transitiontime": 10}
	if s.On {
		if s.Bri > 0 {
			state["bri"] = s.Bri
		}
		switch {
		case s.ColorMode == "ct" && s.CT > 0:
			state["ct"] = s.CT
		case len(s.XY) == 2:
			state["xy"] = s.XY
		}
	}
	return b.SetState(ctx, id, state)
}

// discover asks Philips' discovery service for bridges on this network.
func discover(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://discovery.meethue.com/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery: %s", resp.Status)
	}
	var list []struct {
		IP string `json:"internalipaddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := []string{}
	for _, b := range list {
		if b.IP != "" {
			out = append(out, b.IP)
		}
	}
	return out, nil
}
