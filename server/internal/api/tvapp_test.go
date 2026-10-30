package api

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// fakeAPK builds a zip with an APK Signing Block holding a stand-in v2
// signature pair and, if padTo is set, apksigner-style padding.
func fakeAPK(t *testing.T, padTo int) []byte {
	t.Helper()
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	f, _ := zw.Create("AndroidManifest.xml")
	f.Write([]byte("manifest"))
	zw.Close()
	b := z.Bytes()
	eocd := findEOCD(b)
	cd := int(binary.LittleEndian.Uint32(b[eocd+16:]))

	var pairs []byte
	sig := []byte("signature-goes-here")
	pairs = binary.LittleEndian.AppendUint64(pairs, uint64(len(sig)+4))
	pairs = binary.LittleEndian.AppendUint32(pairs, 0x7109871a)
	pairs = append(pairs, sig...)
	if padTo > 0 {
		pad := padTo - 8 - len(pairs) - 12 - 24
		pairs = binary.LittleEndian.AppendUint64(pairs, uint64(pad+4))
		pairs = binary.LittleEndian.AppendUint32(pairs, verityPaddingID)
		pairs = append(pairs, make([]byte, pad)...)
	}
	size := len(pairs) + 24
	var out []byte
	out = append(out, b[:cd]...)
	out = binary.LittleEndian.AppendUint64(out, uint64(size))
	out = append(out, pairs...)
	out = binary.LittleEndian.AppendUint64(out, uint64(size))
	out = append(out, apkSigBlockMagic...)
	newCD := len(out)
	out = append(out, b[cd:]...)
	binary.LittleEndian.PutUint32(out[newCD+(eocd-cd)+16:], uint32(newCD))
	return out
}

func readsAsZip(t *testing.T, b []byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	if string(got) != "manifest" {
		t.Fatalf("entry = %q", got)
	}
}

func TestStampAPK(t *testing.T) {
	for _, padTo := range []int{0, 4096} {
		apk := fakeAPK(t, padTo)
		if readAPKStamp(apk) != nil {
			t.Fatal("unstamped APK has a stamp")
		}
		one, err := stampAPK(apk, []byte(`{"servers":["https://tv.example.com"]}`))
		if err != nil {
			t.Fatal(err)
		}
		readsAsZip(t, one)
		if got := string(readAPKStamp(one)); got != `{"servers":["https://tv.example.com"]}` {
			t.Fatalf("stamp = %q", got)
		}
		// Stamping again replaces the old stamp.
		two, err := stampAPK(one, []byte(`{"servers":["http://203.0.113.7:8484"]}`))
		if err != nil {
			t.Fatal(err)
		}
		readsAsZip(t, two)
		if got := string(readAPKStamp(two)); got != `{"servers":["http://203.0.113.7:8484"]}` {
			t.Fatalf("restamp = %q", got)
		}
		if padTo > 0 && (len(one) != len(apk) || len(two) != len(apk)) {
			t.Errorf("padded APK changed size: %d → %d, %d", len(apk), len(one), len(two))
		}
		// Everything outside the signing block is untouched apart from the
		// central directory offset.
		if !bytes.Contains(two, []byte("signature-goes-here")) {
			t.Error("signature pair was lost")
		}
	}
}

func TestStampAPKNeedsSignature(t *testing.T) {
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	zw.Create("a")
	zw.Close()
	if _, err := stampAPK(z.Bytes(), []byte("x")); err == nil {
		t.Error("stamped a zip without a signing block")
	}
	if _, err := stampAPK([]byte("not a zip"), []byte("x")); err == nil {
		t.Error("stamped junk")
	}
}
