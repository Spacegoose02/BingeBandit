package livetv

import (
	"encoding/xml"
	"io"
	"strings"
	"time"
)

type GuideChannel struct {
	ID          string
	DisplayName string
	Icon        string
}

type Program struct {
	ChannelID   string
	Start, Stop time.Time
	Title       string
	Subtitle    string
	Description string
	Category    string
	Icon        string
	Episode     string
}

type xmlChannel struct {
	ID           string   `xml:"id,attr"`
	DisplayNames []string `xml:"display-name"`
	Icon         struct {
		Src string `xml:"src,attr"`
	} `xml:"icon"`
}

type xmlProgramme struct {
	Start      string   `xml:"start,attr"`
	Stop       string   `xml:"stop,attr"`
	Channel    string   `xml:"channel,attr"`
	Titles     []string `xml:"title"`
	SubTitles  []string `xml:"sub-title"`
	Descs      []string `xml:"desc"`
	Categories []string `xml:"category"`
	Icon       struct {
		Src string `xml:"src,attr"`
	} `xml:"icon"`
	EpisodeNums []struct {
		System string `xml:"system,attr"`
		Value  string `xml:",chardata"`
	} `xml:"episode-num"`
}

// ParseXMLTV streams an XMLTV guide, calling onChannel and onProgram for
// each element. Programs outside [from, to) are skipped to keep the guide small.
func ParseXMLTV(r io.Reader, from, to time.Time, onChannel func(GuideChannel) error, onProgram func(Program) error) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "channel":
			var c xmlChannel
			if err := dec.DecodeElement(&c, &se); err != nil {
				return err
			}
			name := c.ID
			if len(c.DisplayNames) > 0 {
				name = strings.TrimSpace(c.DisplayNames[0])
			}
			if err := onChannel(GuideChannel{ID: c.ID, DisplayName: name, Icon: c.Icon.Src}); err != nil {
				return err
			}
		case "programme":
			var p xmlProgramme
			if err := dec.DecodeElement(&p, &se); err != nil {
				return err
			}
			start, err1 := parseXMLTVTime(p.Start)
			stop, err2 := parseXMLTVTime(p.Stop)
			if err1 != nil || err2 != nil || !stop.After(from) || !start.Before(to) {
				continue
			}
			prog := Program{ChannelID: p.Channel, Start: start, Stop: stop, Icon: p.Icon.Src,
				Title: first(p.Titles), Subtitle: first(p.SubTitles), Description: first(p.Descs), Category: first(p.Categories)}
			for _, e := range p.EpisodeNums {
				if e.System == "onscreen" || prog.Episode == "" {
					prog.Episode = strings.TrimSpace(e.Value)
				}
			}
			if err := onProgram(prog); err != nil {
				return err
			}
		}
	}
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.TrimSpace(s[0])
}

// parseXMLTVTime reads "20261023180000 +0000" (offset optional, then UTC).
func parseXMLTVTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 20 {
		return time.Parse("20060102150405 -0700", s[:20])
	}
	if len(s) >= 14 {
		return time.Parse("20060102150405", s[:14])
	}
	return time.Parse("200601021504", s)
}
