package s3

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The "GET Object" example from the AWS Signature Version 4 documentation.
func TestSignMatchesAWSExample(t *testing.T) {
	c := &Client{
		Endpoint: "https://s3.amazonaws.com", Bucket: "examplebucket",
		AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		now: func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) },
	}
	req, err := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	c.sign(req, emptySHA256)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestAddressing(t *testing.T) {
	aws := &Client{Endpoint: "https://s3.eu-west-1.amazonaws.com", Bucket: "b", Region: "eu-west-1", AccessKey: "k", SecretKey: "s"}
	req, _ := aws.request(context.Background(), "GET", "bingebandit/x y.db", nil, nil, emptySHA256)
	if req.URL.String() != "https://b.s3.eu-west-1.amazonaws.com/bingebandit/x%20y.db" {
		t.Errorf("aws: %s", req.URL)
	}
	b2 := &Client{Endpoint: "https://s3.us-west-002.backblazeb2.com/", Bucket: "b", AccessKey: "k", SecretKey: "s"}
	req, _ = b2.request(context.Background(), "GET", "", map[string][]string{"prefix": {"bb/"}, "list-type": {"2"}}, nil, emptySHA256)
	if req.URL.String() != "https://s3.us-west-002.backblazeb2.com/b?list-type=2&prefix=bb%2F" {
		t.Errorf("b2: %s", req.URL)
	}
	if _, err := (&Client{Endpoint: "s3.example.com", Bucket: "b"}).request(context.Background(), "GET", "", nil, nil, emptySHA256); err == nil {
		t.Error("endpoint without a scheme accepted")
	}
}

// Against real S3-compatible storage, when BB_S3_TEST_ENDPOINT is set
// (e.g. a local MinIO), with BB_S3_TEST_BUCKET, _ACCESS_KEY and _SECRET_KEY.
func TestRoundTripLive(t *testing.T) {
	endpoint := os.Getenv("BB_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("BB_S3_TEST_ENDPOINT not set")
	}
	c := &Client{Endpoint: endpoint, Bucket: os.Getenv("BB_S3_TEST_BUCKET"), Prefix: "/bb test/",
		AccessKey: os.Getenv("BB_S3_TEST_ACCESS_KEY"), SecretKey: os.Getenv("BB_S3_TEST_SECRET_KEY")}
	ctx := context.Background()
	// Make the bucket if it's missing (a fresh MinIO); an error just means it exists.
	if req, err := c.request(ctx, http.MethodPut, "", nil, nil, emptySHA256); err == nil {
		c.do(req, nil)
	}
	path := filepath.Join(t.TempDir(), "f.db")
	data := bytes.Repeat([]byte("bingebandit "), 100000)
	os.WriteFile(path, data, 0o644)
	if err := c.Put(ctx, "bingebandit-20261024-120000.db", path); err != nil {
		t.Fatal(err)
	}
	if err := c.PutBytes(ctx, "second.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	list, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "bingebandit-20261024-120000.db" || list[0].Size != int64(len(data)) {
		t.Fatalf("list: %+v", list)
	}
	var got bytes.Buffer
	if err := c.Get(ctx, list[0].Name, &got); err != nil || !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("get: %v (%d bytes)", err, got.Len())
	}
	for _, o := range list {
		if err := c.Delete(ctx, o.Name); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := c.List(ctx); len(list) != 0 {
		t.Fatalf("left after delete: %+v", list)
	}
	bad := *c
	bad.SecretKey = "wrong"
	if err := bad.PutBytes(ctx, "x", []byte("x")); err == nil || !strings.Contains(err.Error(), "Signature") {
		t.Fatalf("wrong secret: %v", err)
	}
}
