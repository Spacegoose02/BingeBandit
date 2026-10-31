package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// Admin alerts tell whoever runs the server that something needs looking at:
// a new version, a failed backup or recording, a drive gone offline or nearly
// full. Each goes to every admin's notifications and to the email, Discord
// and ntfy set up in Settings → Admin alerts.

// alertKinds maps each kind to the setting that turns it on.
var alertKinds = map[string]string{
	"update":    store.SettingAlertUpdates,
	"backup":    store.SettingAlertBackups,
	"drive":     store.SettingAlertDrives,
	"recording": store.SettingAlertRecordings,
	"disk":      store.SettingAlertDiskSpace,
}

type alert struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Link  string `json:"link,omitempty"` // a path in the web app, e.g. /admin/health
	// Problem alerts are sent with a higher priority than good news.
	Problem bool `json:"problem"`
}

// alertResult is one alert as it was sent, for the health page.
type alertResult struct {
	alert
	Time    int64             `json:"time"`
	Results map[string]string `json:"results,omitempty"` // channel → "sent" or the error
}

// alertChannels is where alerts go outside the app, read from settings.
type alertChannels struct {
	EmailTo    []string
	SMTPHost   string
	SMTPPort   int
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
	DiscordURL string
	NtfyURL    string
	NtfyToken  string
}

func alertChannelsFrom(get func(string) string) alertChannels {
	c := alertChannels{
		SMTPHost: strings.TrimSpace(get(store.SettingAlertSMTPHost)), SMTPUser: strings.TrimSpace(get(store.SettingAlertSMTPUser)),
		SMTPPass: get(store.SettingAlertSMTPPassword), SMTPFrom: strings.TrimSpace(get(store.SettingAlertSMTPFrom)),
		DiscordURL: strings.TrimSpace(get(store.SettingAlertDiscordURL)),
		NtfyURL:    strings.TrimSpace(get(store.SettingAlertNtfyURL)), NtfyToken: strings.TrimSpace(get(store.SettingAlertNtfyToken)),
	}
	for _, a := range strings.Split(get(store.SettingAlertEmailTo), ",") {
		if a = strings.TrimSpace(a); a != "" {
			c.EmailTo = append(c.EmailTo, a)
		}
	}
	c.SMTPPort, _ = strconv.Atoi(strings.TrimSpace(get(store.SettingAlertSMTPPort)))
	if c.SMTPPort == 0 {
		c.SMTPPort = 587
	}
	if c.SMTPFrom == "" {
		c.SMTPFrom = c.SMTPUser
	}
	return c
}

func (c alertChannels) any() bool {
	return len(c.EmailTo) > 0 || c.DiscordURL != "" || c.NtfyURL != ""
}

// alertHistory keeps the newest alerts for the health page.
type alertHistory struct {
	mu   sync.Mutex
	list []alertResult
}

const alertHistoryKeep = 20

func (h *alertHistory) add(a alertResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.list = append([]alertResult{a}, h.list...)
	if len(h.list) > alertHistoryKeep {
		h.list = h.list[:alertHistoryKeep]
	}
}

func (h *alertHistory) recent() []alertResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]alertResult{}, h.list...)
}

var alertClient = &http.Client{Timeout: 15 * time.Second}

// sendAlert tells admins about a, unless its kind is turned off. It returns
// at once; delivery happens in the background.
func (s *Server) sendAlert(a alert) {
	if key := alertKinds[a.Kind]; key != "" && s.Store.Setting(key) == "false" {
		return
	}
	s.notifyAdmins(a)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		results := s.deliverAlert(ctx, alertChannelsFrom(s.Store.Setting), a)
		for ch, res := range results {
			if res != "sent" {
				slog.Warn("admin alert", "channel", ch, "title", a.Title, "err", res)
			}
		}
		s.alerts.add(alertResult{alert: a, Time: time.Now().Unix(), Results: results})
	}()
}

// notifyAdmins puts a in each admin account's notifications.
func (s *Server) notifyAdmins(a alert) {
	users, err := s.Store.ListUsers()
	if err != nil {
		return
	}
	for _, u := range users {
		if !u.IsAdmin {
			continue
		}
		n := &store.Notification{UserID: u.ID, Kind: "server", Title: a.Title, Body: a.Body, Link: a.Link}
		if err := s.Store.AddNotification(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

// deliverAlert sends a to every channel that is set up, at the same time,
// and returns "sent" or the error for each.
func (s *Server) deliverAlert(ctx context.Context, c alertChannels, a alert) map[string]string {
	name := s.Store.Setting(store.SettingServerName)
	link := ""
	if a.Link != "" {
		if base := s.Store.Setting(store.SettingPublicURL); base != "" {
			link = strings.TrimRight(base, "/") + a.Link
		}
	}
	senders := map[string]func() error{}
	if len(c.EmailTo) > 0 {
		senders["email"] = func() error { return sendAlertEmail(c, name, link, a) }
	}
	if c.DiscordURL != "" {
		senders["discord"] = func() error { return sendAlertDiscord(ctx, c.DiscordURL, name, link, a) }
	}
	if c.NtfyURL != "" {
		senders["ntfy"] = func() error { return sendAlertNtfy(ctx, c.NtfyURL, c.NtfyToken, name, link, a) }
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[string]string{}
	for ch, send := range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := "sent"
			if err := send(); err != nil {
				res = err.Error()
			}
			mu.Lock()
			results[ch] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func alertText(a alert, link string) string {
	text := a.Body
	if link != "" {
		if text != "" {
			text += "\n\n"
		}
		text += link
	}
	return text
}

func sendAlertDiscord(ctx context.Context, hook, server, link string, a alert) error {
	color := 0x2e9e5b
	if a.Problem {
		color = 0xd9534f
	}
	embed := map[string]any{"title": a.Title, "description": a.Body, "color": color, "footer": map[string]string{"text": server}}
	if link != "" {
		embed["url"] = link
	}
	body, _ := json.Marshal(map[string]any{"username": "BingeBandit", "embeds": []any{embed}})
	return postAlert(ctx, hook, "application/json", body, nil)
}

func sendAlertNtfy(ctx context.Context, topic, token, server, link string, a alert) error {
	headers := map[string]string{"Title": server + ": " + a.Title, "Tags": "tv"}
	if a.Problem {
		headers["Priority"], headers["Tags"] = "high", "warning"
	}
	if link != "" {
		headers["Click"] = link
	}
	if token != "" {
		if user, pass, ok := strings.Cut(token, ":"); ok {
			headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
		} else {
			headers["Authorization"] = "Bearer " + token
		}
	}
	body := a.Body
	if body == "" {
		body = a.Title
	}
	return postAlert(ctx, topic, "text/plain; charset=utf-8", []byte(body), headers)
}

func postAlert(ctx context.Context, url, contentType string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "BingeBandit")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := alertClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New(resp.Status + replySnippet(resp.Body))
	}
	return nil
}

// sendAlertEmail sends a by SMTP: implicit TLS on port 465, STARTTLS when the
// server offers it on other ports.
func sendAlertEmail(c alertChannels, server, link string, a alert) error {
	if c.SMTPHost == "" {
		return errors.New("no SMTP server set")
	}
	if c.SMTPFrom == "" {
		return errors.New("fill in a From address or an SMTP username")
	}
	from, err := mail.ParseAddress(c.SMTPFrom)
	if err != nil {
		return fmt.Errorf("the From address %q isn't an email address", c.SMTPFrom)
	}
	var to []string
	for _, t := range c.EmailTo {
		addr, err := mail.ParseAddress(t)
		if err != nil {
			return fmt.Errorf("%q isn't an email address", t)
		}
		to = append(to, addr.Address)
	}
	msg := emailMessage(from, c.EmailTo, server+": "+a.Title, alertText(a, link))

	addr := net.JoinHostPort(c.SMTPHost, strconv.Itoa(c.SMTPPort))
	tlsConfig := &tls.Config{ServerName: c.SMTPHost}
	var conn net.Conn
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if c.SMTPPort == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	client, err := smtp.NewClient(conn, c.SMTPHost)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok && c.SMTPPort != 465 {
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if c.SMTPUser != "" {
		if err := client.Auth(smtp.PlainAuth("", c.SMTPUser, c.SMTPPass, c.SMTPHost)); err != nil {
			return fmt.Errorf("sign-in: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	for _, t := range to {
		if err := client.Rcpt(t); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func emailMessage(from *mail.Address, to []string, subject, text string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.Bytes()
}

// testAlerts sends a test alert with the settings in the body (not yet saved)
// and reports each channel's result.
func (s *Server) testAlerts(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	c := alertChannelsFrom(func(k string) string {
		if v, ok := in[k]; ok {
			return v
		}
		return s.Store.Setting(k)
	})
	if !c.any() {
		writeErr(w, http.StatusBadRequest, "fill in an email address, a Discord webhook or an ntfy topic first")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	a := alert{Kind: "test", Title: "Test alert", Body: "Admin alerts from this server will arrive here.", Link: "/admin/health"}
	writeJSON(w, http.StatusOK, map[string]any{"results": s.deliverAlert(ctx, c, a)})
}

// validAlertSettings checks the alert settings in a settings update.
func validAlertSettings(in map[string]string) error {
	for _, key := range []string{store.SettingAlertDiscordURL, store.SettingAlertNtfyURL} {
		if v := strings.TrimSpace(in[key]); v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			return fmt.Errorf("%s must be an http:// or https:// address", key)
		}
	}
	if v, ok := in[store.SettingAlertSMTPPort]; ok && v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 65535 {
			return errors.New("the SMTP port must be a number from 1 to 65535")
		}
	}
	if v := in[store.SettingAlertEmailTo]; strings.TrimSpace(v) != "" {
		if _, err := mail.ParseAddressList(v); err != nil {
			return errors.New("alert email addresses must be separated by commas")
		}
	}
	for key := range alertKinds {
		if v, ok := in[alertKinds[key]]; ok && v != "true" && v != "false" {
			return fmt.Errorf("%s must be true or false", alertKinds[key])
		}
	}
	if v, ok := in[store.SettingUpdateCheck]; ok && v != "true" && v != "false" {
		return errors.New("update_check must be true or false")
	}
	return nil
}
