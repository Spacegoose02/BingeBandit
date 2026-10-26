// Package media inspects video files with ffprobe.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

type probeOutput struct {
	Streams []struct {
		Index         int               `json:"index"`
		CodecType     string            `json:"codec_type"`
		CodecName     string            `json:"codec_name"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		PixFmt        string            `json:"pix_fmt"`
		ColorTransfer string            `json:"color_transfer"`
		CodecTag      string            `json:"codec_tag_string"`
		Profile       string            `json:"profile"`
		SideData      []sideData        `json:"side_data_list"`
		Channels      int               `json:"channels"`
		Tags          map[string]string `json:"tags"`
		Disposition   map[string]int    `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

type sideData struct {
	Type string `json:"side_data_type"`
}

// Probe fills the technical fields of f from ffprobe.
func Probe(ctx context.Context, ffprobe string, f *store.MediaFile) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", f.Path)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ffprobe %s: %w", f.Path, err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return fmt.Errorf("ffprobe output: %w", err)
	}

	f.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), ".")
	f.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	f.Bitrate, _ = strconv.ParseInt(p.Format.BitRate, 10, 64)
	f.Streams = f.Streams[:0]
	for _, s := range p.Streams {
		st := store.Stream{
			Index:    s.Index,
			Type:     s.CodecType,
			Codec:    s.CodecName,
			Language: s.Tags["language"],
			Title:    s.Tags["title"],
			Channels: s.Channels,
			Width:    s.Width,
			Height:   s.Height,
			Default:  s.Disposition["default"] == 1,
			Forced:   s.Disposition["forced"] == 1,

			HearingImpaired: s.Disposition["hearing_impaired"] == 1,
		}
		if s.CodecType == "audio" {
			st.Profile = s.Profile
		}
		switch s.CodecType {
		case "video":
			// Cover art is stored as a video stream; skip it.
			if s.Disposition["attached_pic"] == 1 {
				continue
			}
			if f.VideoCodec == "" {
				f.VideoCodec, f.Width, f.Height = s.CodecName, s.Width, s.Height
				f.BitDepth = 8
				if strings.Contains(s.PixFmt, "10") {
					f.BitDepth = 10
				} else if strings.Contains(s.PixFmt, "12") {
					f.BitDepth = 12
				}
				f.HDR = s.ColorTransfer == "smpte2084" || s.ColorTransfer == "arib-std-b67"
			}
			st.Range = "sdr"
			switch {
			case dolbyVision(s.CodecTag, s.SideData):
				st.Range = "dv"
			case s.ColorTransfer == "smpte2084":
				st.Range = "hdr10"
			case s.ColorTransfer == "arib-std-b67":
				st.Range = "hlg"
			}
		case "audio", "subtitle":
		default:
			continue
		}
		f.Streams = append(f.Streams, st)
	}
	if a := DefaultAudio(f.Streams); a != nil {
		f.AudioCodec, f.AudioChannels = a.Codec, a.Channels
	}
	f.ProbedAt = time.Now().Unix()
	return nil
}

// dolbyVision spots Dolby Vision from the stream's DOVI configuration
// record (MKV and MP4) or its dvhe/dvh1/dav1 tag (MP4).
func dolbyVision(tag string, side []sideData) bool {
	switch strings.ToLower(tag) {
	case "dvhe", "dvh1", "dva1", "dvav", "dav1":
		return true
	}
	for _, d := range side {
		if strings.Contains(strings.ToLower(d.Type), "dovi") {
			return true
		}
	}
	return false
}

// DefaultAudio is the stream flagged default, else the first audio stream.
func DefaultAudio(streams []store.Stream) *store.Stream {
	var first *store.Stream
	for i := range streams {
		s := &streams[i]
		if s.Type != "audio" {
			continue
		}
		if s.Default {
			return s
		}
		if first == nil {
			first = s
		}
	}
	return first
}

// Version returns the first line of `<bin> -version`, or "" if it cannot run.
func Version(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-version")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}
