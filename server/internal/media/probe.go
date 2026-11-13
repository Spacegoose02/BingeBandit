// Package media inspects video, audio and image files with ffprobe.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
		AvgFrameRate  string            `json:"avg_frame_rate"`
		RFrameRate    string            `json:"r_frame_rate"`
		ColorTransfer string            `json:"color_transfer"`
		CodecTag      string            `json:"codec_tag_string"`
		Profile       string            `json:"profile"`
		SideData      []sideData        `json:"side_data_list"`
		Channels      int               `json:"channels"`
		Tags          map[string]string `json:"tags"`
		Disposition   map[string]int    `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
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
	// Tag names vary in case by container (ID3 "artist", Vorbis "ARTIST").
	f.Tags = map[string]string{}
	for k, v := range p.Format.Tags {
		f.Tags[strings.ToLower(k)] = strings.TrimSpace(v)
	}
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
			// Ogg and Opus keep their tags on the audio stream.
			for k, v := range s.Tags {
				if k = strings.ToLower(k); f.Tags[k] == "" {
					f.Tags[k] = strings.TrimSpace(v)
				}
			}
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
			st.FrameRate = frameRate(s.AvgFrameRate, s.RFrameRate)
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

// frameRate reads ffprobe's "24000/1001" rates: the average when it's
// usable, else the stream's base rate. 0 when neither is.
func frameRate(rates ...string) float64 {
	for _, r := range rates {
		n, d, ok := strings.Cut(r, "/")
		num, err1 := strconv.ParseFloat(n, 64)
		den := 1.0
		var err2 error
		if ok {
			den, err2 = strconv.ParseFloat(d, 64)
		}
		if err1 == nil && err2 == nil && den > 0 && num > 0 {
			if fps := num / den; fps >= 1 && fps <= 300 {
				return math.Round(fps*1000) / 1000
			}
		}
	}
	return 0
}

// VideoFrameRate asks ffprobe for the frame rate of a file's first video
// stream, for files probed before frame rates were kept.
func VideoFrameRate(ctx context.Context, ffprobe, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "quiet", "-select_streams", "v:0", "-print_format", "json",
		"-show_entries", "stream=avg_frame_rate,r_frame_rate", path)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil || len(p.Streams) == 0 {
		return 0, fmt.Errorf("ffprobe %s: no video stream", path)
	}
	return frameRate(p.Streams[0].AvgFrameRate, p.Streams[0].RFrameRate), nil
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
