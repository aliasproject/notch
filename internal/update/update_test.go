package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.7.1", "v0.7.2", true},
		{"0.7.1", "v0.8.0", true},
		{"v0.7.1", "v1.0.0", true},
		{"v0.7.1", "v0.7.1", false},
		{"v0.7.2", "v0.7.1", false},
		{"v0.7", "v0.7.0", false},
		{"v0.7.0", "v0.7.0.1", true},
		{"v0.8.0-rc1", "v0.8.0", true},
		{"v0.8.0", "v0.8.0-rc1", false},
		{"v0.8.0-rc1", "v0.8.0-rc2", true},
		{"v0.9.0", "v0.10.0", true},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestComparable(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.7.1": true, "0.7.1": true, "v1": true,
		"dev": false, "(devel)": false, "": false, "v0.x": false,
	} {
		if got := Comparable(v); got != want {
			t.Errorf("Comparable(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("linux", "amd64"); got != "notch_linux_amd64.tar.gz" {
		t.Errorf("linux asset = %q", got)
	}
	if got := AssetName("windows", "arm64"); got != "notch_windows_arm64.zip" {
		t.Errorf("windows asset = %q", got)
	}
}

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// fakeRelease serves a GitHub-shaped latest-release endpoint plus the
// assets it lists. checksums is the checksums.txt body; assets maps asset
// name → content.
func fakeRelease(t *testing.T, tag string, assets map[string][]byte) *Client {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var list []string
		for name := range assets {
			list = append(list, fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, name, srv.URL+"/download/"+name))
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, tag, strings.Join(list, ","))
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		data, ok := assets[strings.TrimPrefix(r.URL.Path, "/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	})
	return &Client{APIBase: srv.URL, HTTP: srv.Client()}
}

func TestLatest(t *testing.T) {
	c := fakeRelease(t, "v0.9.0", map[string][]byte{"checksums.txt": nil, "notch_linux_amd64.tar.gz": nil})
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.9.0" || rel.Version() != "0.9.0" {
		t.Errorf("tag = %q, version = %q", rel.Tag, rel.Version())
	}
	if len(rel.Assets) != 2 || !strings.HasSuffix(rel.Assets["checksums.txt"], "/download/checksums.txt") {
		t.Errorf("assets = %v", rel.Assets)
	}
}

func TestLatest_NoReleases(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "no releases") {
		t.Errorf("err = %v, want 'no releases'", err)
	}
}

func TestApply_ReplacesBinary(t *testing.T) {
	newBin := []byte("#!/bin/sh\necho new\n")
	archive := tarGz(t, map[string][]byte{"notch": newBin, "README.md": []byte("readme")})
	sums := []byte(sha(archive) + "  notch_linux_amd64.tar.gz\n" + "deadbeef  other.tar.gz\n")
	c := fakeRelease(t, "v0.9.0", map[string][]byte{"checksums.txt": sums, "notch_linux_amd64.tar.gz": archive})

	dir := t.TempDir()
	exe := filepath.Join(dir, "notch")
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(context.Background(), rel, "linux", "amd64", exe); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBin) {
		t.Errorf("binary = %q, want new binary", got)
	}
	info, _ := os.Stat(exe)
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary mode %v is not executable", info.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestApply_ZipForWindows(t *testing.T) {
	newBin := []byte("MZ new")
	archive := zipArchive(t, map[string][]byte{"notch.exe": newBin, "LICENSE": []byte("mit")})
	sums := []byte(sha(archive) + "  notch_windows_amd64.zip\n")
	c := fakeRelease(t, "v0.9.0", map[string][]byte{"checksums.txt": sums, "notch_windows_amd64.zip": archive})

	exe := filepath.Join(t.TempDir(), "notch.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	rel, _ := c.Latest(context.Background())
	if err := c.Apply(context.Background(), rel, "windows", "amd64", exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); !bytes.Equal(got, newBin) {
		t.Errorf("binary = %q, want new binary", got)
	}
}

func TestApply_ChecksumMismatchLeavesBinaryAlone(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"notch": []byte("evil")})
	sums := []byte(strings.Repeat("0", 64) + "  notch_linux_amd64.tar.gz\n")
	c := fakeRelease(t, "v0.9.0", map[string][]byte{"checksums.txt": sums, "notch_linux_amd64.tar.gz": archive})

	dir := t.TempDir()
	exe := filepath.Join(dir, "notch")
	os.WriteFile(exe, []byte("old"), 0o700)

	rel, _ := c.Latest(context.Background())
	err := c.Apply(context.Background(), rel, "linux", "amd64", exe)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("binary was modified: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestApply_MissingAsset(t *testing.T) {
	c := fakeRelease(t, "v0.9.0", map[string][]byte{"checksums.txt": nil})
	rel, _ := c.Latest(context.Background())
	err := c.Apply(context.Background(), rel, "linux", "amd64", filepath.Join(t.TempDir(), "notch"))
	if err == nil || !strings.Contains(err.Error(), "no asset for linux/amd64") {
		t.Errorf("err = %v", err)
	}
}

func TestApply_ArchiveWithoutBinary(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"README.md": []byte("just docs")})
	sums := []byte(sha(archive) + "  notch_linux_amd64.tar.gz\n")
	c := fakeRelease(t, "v0.9.0", map[string][]byte{"checksums.txt": sums, "notch_linux_amd64.tar.gz": archive})
	rel, _ := c.Latest(context.Background())
	err := c.Apply(context.Background(), rel, "linux", "amd64", filepath.Join(t.TempDir(), "notch"))
	if err == nil || !strings.Contains(err.Error(), "does not contain notch") {
		t.Errorf("err = %v", err)
	}
}
