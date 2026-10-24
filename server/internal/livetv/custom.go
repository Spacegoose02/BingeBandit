package livetv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// playoutHorizon is how far ahead custom channel schedules are built.
const playoutHorizon = 48 * time.Hour

// activeBlock picks the timed block covering t, or the all-day default.
func activeBlock(blocks []*store.Block, t time.Time) *store.Block {
	m := t.Hour()*60 + t.Minute()
	var fallback *store.Block
	for _, b := range blocks {
		if b.StartMinute < 0 {
			if fallback == nil {
				fallback = b
			}
			continue
		}
		in := m >= b.StartMinute && m < b.EndMinute
		if b.EndMinute <= b.StartMinute { // crosses midnight, e.g. 22:00–02:00
			in = m >= b.StartMinute || m < b.EndMinute
		}
		if in {
			return b
		}
	}
	if fallback == nil && len(blocks) > 0 {
		fallback = blocks[0]
	}
	return fallback
}

// BuildPlayout extends a custom channel's schedule to playoutHorizon from now.
// Shuffle plays every item once (in a fresh random order each cycle) before
// repeating; order walks the sources in sequence. Cursors persist, so a
// series continues where it left off. Returns how many entries were added.
func BuildPlayout(st *store.Store, channelID int64, now time.Time) (int, error) {
	blocks, err := st.CustomBlocks(channelID)
	if err != nil {
		return 0, err
	}
	if len(blocks) == 0 {
		return 0, errors.New("this channel has no content")
	}
	pools := map[int64][]store.Playable{}
	for _, b := range blocks {
		seen := map[int64]bool{}
		for _, src := range b.Sources {
			list, err := st.ResolveSource(src)
			if err != nil {
				return 0, err
			}
			for _, p := range list {
				if !seen[p.ItemID] {
					seen[p.ItemID] = true
					pools[b.ID] = append(pools[b.ID], p)
				}
			}
		}
	}

	end, err := st.PlayoutEnd(channelID)
	if err != nil {
		return 0, err
	}
	t := time.Unix(end, 0)
	if end < now.Unix() {
		t = now.Truncate(time.Minute)
	}
	horizon := now.Add(playoutHorizon)
	cursors := map[int64]int{}
	for _, b := range blocks {
		cursors[b.ID] = b.Cursor
	}
	perms := map[string][]int{}
	var items []store.PlayoutItem
	for t.Before(horizon) {
		b := activeBlock(blocks, t)
		pool := pools[b.ID]
		if len(pool) == 0 {
			// An empty timed block falls back to the default block's content.
			if d := activeBlock(onlyDefault(blocks), t); d != nil && len(pools[d.ID]) > 0 {
				b, pool = d, pools[d.ID]
			} else {
				break
			}
		}
		c := cursors[b.ID]
		idx := c % len(pool)
		if b.Mode == "shuffle" {
			key := fmt.Sprintf("%d/%d", b.ID, c/len(pool))
			perm, ok := perms[key]
			if !ok {
				perm = rand.New(rand.NewSource(channelID*1_000_003 + b.ID*7919 + int64(c/len(pool)))).Perm(len(pool))
				perms[key] = perm
			}
			idx = perm[idx]
		}
		p := pool[idx]
		dur := time.Duration(math.Ceil(max(p.Duration, 1))) * time.Second
		items = append(items, store.PlayoutItem{
			ChannelID: channelID, ItemID: p.ItemID, FileID: p.FileID, Start: t.Unix(), Stop: t.Add(dur).Unix(),
			Title: p.Title, Subtitle: p.Subtitle, Description: p.Description, Category: p.Category, Icon: p.Icon,
		})
		cursors[b.ID] = c + 1
		t = t.Add(dur)
	}
	if len(items) == 0 && end < now.Unix() {
		return 0, errors.New("the channel's sources have no playable videos")
	}
	if err := st.AddPlayout(items); err != nil {
		return 0, err
	}
	for id, c := range cursors {
		st.SetBlockCursor(id, c)
	}
	return len(items), nil
}

func onlyDefault(blocks []*store.Block) []*store.Block {
	for _, b := range blocks {
		if b.StartMinute < 0 {
			return []*store.Block{b}
		}
	}
	return nil
}

// RefillPlayouts tops up every custom channel and drops aired entries.
// Used by the scheduled task.
func RefillPlayouts(st *store.Store) (string, error) {
	ids, err := st.CustomChannelIDs()
	if err != nil {
		return "", err
	}
	added, failed := 0, 0
	for _, id := range ids {
		n, err := BuildPlayout(st, id, time.Now())
		if err != nil {
			log.Printf("custom channel %d: %v", id, err)
			failed++
		}
		added += n
	}
	if err := st.PrunePlayout(time.Now().Add(-guideBehind).Unix()); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("%d channels, %d new schedule entries", len(ids), added)
	if failed > 0 {
		return "", fmt.Errorf("%s; %d channels have no playable content", msg, failed)
	}
	return msg, nil
}

// customVideo normalizes every item to the same 720p30 H.264 + AAC stereo so
// one continuous stream can be built from files with different formats.
// The returned slices go before and after the input.
func customVideo(hw string) (in, out []string) {
	vf := "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2,fps=30,setsar=1,format=yuv420p"
	in, out = encodeArgs(hw, vf, "4M", "5M")
	return in, append(out, "-g", "60", "-force_key_frames", "expr:gte(t,n_forced*2)")
}

// pullCustom plays a custom channel: for whatever airs now, ffmpeg reads the
// file from the right offset at real-time speed. Timestamps are offset from
// the relay's start so consecutive items form one continuous stream.
func (r *Relay) pullCustom() error {
	for r.ctx.Err() == nil {
		now := time.Now()
		it, err := r.m.st.PlayoutAt(r.channel.ID, now.Unix())
		if errors.Is(err, store.ErrNotFound) {
			if _, berr := BuildPlayout(r.m.st, r.channel.ID, now); berr != nil {
				return berr
			}
			it, err = r.m.st.PlayoutAt(r.channel.ID, now.Unix())
		}
		if err != nil {
			return fmt.Errorf("nothing is scheduled: %w", err)
		}
		f, err := r.m.st.File(it.FileID)
		if err != nil {
			return fmt.Errorf("%q is no longer in the library", it.Title)
		}
		offset := float64(now.Unix() - it.Start)
		remaining := float64(it.Stop - now.Unix())
		r.mu.Lock()
		r.stream = store.ChannelStream{Name: strings.TrimSpace(it.Title + " " + it.Subtitle), SourceName: "Library"}
		r.head.Reset()
		r.mu.Unlock()

		hw := r.m.hw.pick(r.m.st.Setting(store.SettingHWEncoder))
		vin, vout := customVideo(hw)
		args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, vin...)
		args = append(args, "-re", "-ss", fmt.Sprintf("%.3f", offset), "-i", f.Path, "-map", "0:v:0", "-map", "0:a:0?")
		args = append(args, vout...)
		args = append(args, "-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "160k",
			"-t", fmt.Sprintf("%.3f", remaining),
			"-output_ts_offset", fmt.Sprintf("%.3f", time.Since(r.started).Seconds()),
			"-f", "mpegts", "pipe:1")
		started := time.Now()
		if err := r.runCustomItem(args); err != nil {
			if r.ctx.Err() != nil {
				return nil
			}
			if time.Since(started) < 3*time.Second {
				if hw != "none" {
					r.m.hw.markFailed(hw, lastLines(err.Error(), 4)) // the retry uses the CPU
				}
				return fmt.Errorf("%q could not be played: %w", it.Title, err)
			}
			log.Printf("custom channel %q: %q ended early: %v", r.channel.Name, it.Title, err)
		}
	}
	return nil
}

func (r *Relay) runCustomItem(args []string) error {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.m.ffmpeg, args...)
	media.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	stall := r.m.stallTimeout()
	go func() { // stall watchdog, as for provider streams
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if time.Since(time.Unix(0, last.Load())) > stall {
					cancel()
					return
				}
			}
		}
	}()
	for {
		buf := make([]byte, 64*1024)
		n, rerr := out.Read(buf)
		if n > 0 {
			last.Store(time.Now().UnixNano())
			r.broadcast(buf[:n])
		}
		if rerr != nil {
			werr := cmd.Wait()
			if rerr != io.EOF {
				return rerr
			}
			if werr != nil && ctx.Err() == nil {
				return fmt.Errorf("%v %s", werr, strings.TrimSpace(stderr.String()))
			}
			return nil
		}
	}
}
