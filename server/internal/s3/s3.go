// Package s3 is a small client for S3-compatible storage (AWS S3, Backblaze
// B2, MinIO, Wasabi, Cloudflare R2): just enough to keep database backups
// off the machine. Requests are signed with AWS Signature Version 4.
package s3

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Client talks to one bucket. Objects live under Prefix (e.g. "bingebandit/").
type Client struct {
	Endpoint  string // e.g. https://s3.us-west-002.backblazeb2.com
	Region    string // e.g. us-west-002; "us-east-1" when empty
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	HTTP      *http.Client

	now func() time.Time // for tests
}

// Object is one stored file, named without the prefix.
type Object struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Configured reports whether enough is set to use the bucket.
func (c *Client) Configured() bool {
	return c.Endpoint != "" && c.Bucket != "" && c.AccessKey != "" && c.SecretKey != ""
}

func (c *Client) prefix() string {
	p := strings.Trim(c.Prefix, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// Put uploads the file at path as name.
func (c *Client) Put(ctx context.Context, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	req, err := c.request(ctx, http.MethodPut, c.prefix()+name, nil, io.NopCloser(f), hex.EncodeToString(h.Sum(nil)))
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	return c.do(req, nil)
}

// PutBytes uploads a small object (used to test the settings).
func (c *Client) PutBytes(ctx context.Context, name string, b []byte) error {
	sum := sha256.Sum256(b)
	req, err := c.request(ctx, http.MethodPut, c.prefix()+name, nil, io.NopCloser(strings.NewReader(string(b))), hex.EncodeToString(sum[:]))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(b))
	return c.do(req, nil)
}

// Get writes the object name to w.
func (c *Client) Get(ctx context.Context, name string, w io.Writer) error {
	req, err := c.request(ctx, http.MethodGet, c.prefix()+name, nil, nil, emptySHA256)
	if err != nil {
		return err
	}
	return c.do(req, func(body io.Reader) error {
		_, err := io.Copy(w, body)
		return err
	})
}

// Delete removes the object name.
func (c *Client) Delete(ctx context.Context, name string) error {
	req, err := c.request(ctx, http.MethodDelete, c.prefix()+name, nil, nil, emptySHA256)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// List returns the objects under the prefix, sorted by name.
func (c *Client) List(ctx context.Context) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {c.prefix()}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		req, err := c.request(ctx, http.MethodGet, "", q, nil, emptySHA256)
		if err != nil {
			return nil, err
		}
		var res struct {
			Contents []struct {
				Key          string
				Size         int64
				LastModified time.Time
			}
			IsTruncated           bool
			NextContinuationToken string
		}
		if err := c.do(req, func(body io.Reader) error { return xml.NewDecoder(body).Decode(&res) }); err != nil {
			return nil, err
		}
		for _, o := range res.Contents {
			name := strings.TrimPrefix(o.Key, c.prefix())
			if name != "" && !strings.Contains(name, "/") {
				out = append(out, Object{name, o.Size, o.LastModified})
			}
		}
		if !res.IsTruncated || res.NextContinuationToken == "" {
			break
		}
		token = res.NextContinuationToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// request builds a signed request for key (empty for the bucket itself).
// AWS hosts use virtual-hosted buckets (bucket.s3…amazonaws.com); everything
// else uses path-style addressing (endpoint/bucket/key), which every
// S3-compatible service supports.
func (c *Client) request(ctx context.Context, method, key string, query url.Values, body io.ReadCloser, payloadHash string) (*http.Request, error) {
	base, err := url.Parse(strings.TrimRight(c.Endpoint, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, errors.New("the endpoint must be a URL like https://s3.us-west-002.backblazeb2.com")
	}
	u := *base
	if strings.HasSuffix(base.Hostname(), "amazonaws.com") {
		u.Host = c.Bucket + "." + base.Host
		u.Path = "/" + key
	} else {
		u.Path = "/" + c.Bucket + "/" + key
		if key == "" {
			u.Path = "/" + c.Bucket
		}
	}
	u.RawPath = uriEncode(u.Path, false)
	u.RawQuery = canonicalQuery(query)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	c.sign(req, payloadHash)
	return req, nil
}

func (c *Client) do(req *http.Request, read func(io.Reader) error) error {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e struct {
			Code    string
			Message string
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if xml.Unmarshal(b, &e) == nil && e.Code != "" {
			return fmt.Errorf("storage said %s: %s", e.Code, e.Message)
		}
		return fmt.Errorf("storage answered %s", resp.Status)
	}
	if read != nil {
		return read(resp.Body)
	}
	return nil
}

// sign adds AWS Signature Version 4 headers to req.
func (c *Client) sign(req *http.Request, payloadHash string) {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	t := now().UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	region := c.Region
	if region == "" {
		region = "us-east-1"
	}
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// Every x-amz-* header plus host and range are signed.
	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") || lk == "range" || lk == "content-type" {
			headers[lk] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{
		req.Method,
		uriEncode(req.URL.Path, false),
		req.URL.RawQuery,
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	key := hmacSHA256([]byte("AWS4"+c.SecretKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.AccessKey, scope, signed, sig))
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// canonicalQuery sorts and encodes query parameters the way SigV4 wants.
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// uriEncode percent-encodes everything but unreserved characters (and "/"
// unless encodeSlash).
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case 'A' <= ch && ch <= 'Z', 'a' <= ch && ch <= 'z', '0' <= ch && ch <= '9', ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == '/' && !encodeSlash:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}
