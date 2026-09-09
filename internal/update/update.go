// Package update implements "notch update": it looks up the latest GitHub
// release, compares it to the running version, and swaps the current
// executable for the release binary. It talks to the same release assets
// install.sh does (goreleaser's notch_<os>_<arch>.tar.gz / .zip plus
// checksums.txt), so anything installable by the script is installable
// here too, and verifies the archive against checksums.txt before touching
// the binary.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are fetched from.
const Repo = "aliasproject/notch"

// maxArchiveBytes caps how much of a release archive is read into memory.
// Release archives are a few MB; this just stops a bad URL from streaming
// forever.
const maxArchiveBytes = 256 << 20

// Release is one GitHub release: its tag plus a name → download URL map of
// its assets.
type Release struct {
	Tag    string
	Assets map[string]string
}

// Version returns the release's version without the leading "v".
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Client fetches releases and applies them. The zero value is usable and
// talks to api.github.com; tests point APIBase at an httptest server.
type Client struct {
	// APIBase is the GitHub API root, without a trailing slash. Empty means
	// https://api.github.com.
	APIBase string
	// HTTP is the client used for every request. Nil means a client with a
	// generous timeout suitable for downloading a release archive.
	HTTP *http.Client
}

func (c *Client) apiBase() string {
	if c.APIBase != "" {
		return c.APIBase
	}
	return "https://api.github.com"
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Latest returns the newest non-draft, non-prerelease GitHub release.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	url := c.apiBase() + "/repos/" + Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "notch-update")

	resp, err := c.http().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, errors.New("no releases found")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch latest release: GitHub returned %s", resp.Status)
	}

	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	if body.TagName == "" {
		return Release{}, errors.New("release has no tag")
	}
	rel := Release{Tag: body.TagName, Assets: make(map[string]string, len(body.Assets))}
	for _, a := range body.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// AssetName is the archive filename goreleaser produces for an OS/arch
// pair — see .goreleaser.yaml's archives.name_template.
func AssetName(goos, goarch string) string {
	if goos == "windows" {
		return "notch_" + goos + "_" + goarch + ".zip"
	}
	return "notch_" + goos + "_" + goarch + ".tar.gz"
}

// Apply downloads the release archive for goos/goarch, verifies it against
// the release's checksums.txt, extracts the notch binary, and replaces the
// executable at exePath with it. On failure exePath is left untouched.
func (c *Client) Apply(ctx context.Context, rel Release, goos, goarch, exePath string) error {
	name := AssetName(goos, goarch)
	archiveURL, ok := rel.Assets[name]
	if !ok {
		return fmt.Errorf("release %s has no asset for %s/%s (%s)", rel.Tag, goos, goarch, name)
	}
	sumsURL, ok := rel.Assets["checksums.txt"]
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt", rel.Tag)
	}

	sums, err := c.get(ctx, sumsURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	want, err := lookupChecksum(sums, name)
	if err != nil {
		return err
	}

	archive, err := c.get(ctx, archiveURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s: refusing to install", name)
	}

	binName := "notch"
	if goos == "windows" {
		binName += ".exe"
	}
	bin, err := extractBinary(archive, name, binName)
	if err != nil {
		return err
	}

	return replaceExecutable(exePath, bin)
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "notch-update")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes))
}

// lookupChecksum finds the sha256 for name in a goreleaser checksums.txt
// ("<hex>  <filename>" per line).
func lookupChecksum(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// extractBinary pulls the file named binName out of a .tar.gz or .zip
// archive.
func extractBinary(archive []byte, archiveName, binName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open zip: %w", err)
		}
		for _, f := range zr.File {
			if path.Base(f.Name) != binName || f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, maxArchiveBytes))
		}
		return nil, fmt.Errorf("%s does not contain %s", archiveName, binName)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open tar.gz: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != binName {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, maxArchiveBytes))
	}
	return nil, fmt.Errorf("%s does not contain %s", archiveName, binName)
}

// replaceExecutable writes bin next to exePath and renames it into place,
// so the swap is atomic and a failure part-way leaves the old binary
// runnable. Windows won't let a running .exe be overwritten, so there the
// old binary is moved aside to <exe>.old first.
func replaceExecutable(exePath string, bin []byte) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".notch-update-*")
	if err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { os.Remove(tmpPath) }

	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("write new binary: %w", err)
	}

	mode := os.FileMode(0o755)
	if info, err := os.Stat(exePath); err == nil {
		mode = info.Mode().Perm() | 0o111
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod new binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		old := exePath + ".old"
		os.Remove(old)
		if err := os.Rename(exePath, old); err != nil {
			cleanup()
			return fmt.Errorf("move old binary aside: %w", err)
		}
		if err := os.Rename(tmpPath, exePath); err != nil {
			os.Rename(old, exePath)
			cleanup()
			return fmt.Errorf("install new binary: %w", err)
		}
		os.Remove(old)
		return nil
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		cleanup()
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

// ExecutablePath returns the real path of the running binary, following
// symlinks so a symlinked install is updated in place rather than having
// the link replaced by a file.
func ExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// IsNewer reports whether latest is a strictly newer version than current.
// Both accept an optional leading "v". Versions are compared numerically
// per dot-separated component; a pre-release suffix ("-rc1") sorts before
// the plain release with the same numbers. An unparseable current version
// (e.g. "dev") is never considered up to date, so callers should treat
// that case separately — see Comparable.
func IsNewer(current, latest string) bool {
	return compare(current, latest) < 0
}

// Comparable reports whether v looks like a release version IsNewer can
// reason about, as opposed to a local build's "dev" or "(devel)".
func Comparable(v string) bool {
	nums, _ := parse(v)
	return len(nums) > 0
}

func compare(a, b string) int {
	an, apre := parse(a)
	bn, bpre := parse(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var x, y int
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case apre == "" && bpre != "":
		return 1
	case apre != "" && bpre == "":
		return -1
	}
	return strings.Compare(apre, bpre)
}

// parse splits "v1.2.3-rc1" into [1 2 3] and "rc1". It returns no numbers
// at all if any component isn't an integer.
func parse(v string) ([]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	if v == "" {
		return nil, ""
	}
	parts := strings.Split(v, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, ""
		}
		nums = append(nums, n)
	}
	return nums, pre
}
