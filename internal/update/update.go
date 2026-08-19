// Package update implements signed-by-checksum release discovery and installation.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Repository = "iAghaTraker/InfraPilot"

type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Body    string  `json:"body"`
	Assets  []Asset `json:"assets"`
}
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func Compare(a, b string) int {
	a, b = strings.TrimPrefix(strings.TrimSpace(a), "v"), strings.TrimPrefix(strings.TrimSpace(b), "v")
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		ai, bi := 0, 0
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &ai)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &bi)
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}

func VerifySHA256(data []byte, expected string) bool {
	sum := sha256.Sum256(data)
	return strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(expected))
}

type Client struct {
	HTTP    *http.Client
	BaseURL string
}

func (c Client) latest(ctx context.Context) (Release, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+Repository+"/releases/latest", nil)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Release{}, fmt.Errorf("release lookup returned HTTP %s", resp.Status)
	}
	var r Release
	return r, json.NewDecoder(resp.Body).Decode(&r)
}
func (c Client) Latest(ctx context.Context) (Release, error) { return c.latest(ctx) }

func AssetName() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	}
	return "infrapilot-linux-" + arch
}

func Install(ctx context.Context, release Release, binaryPath string, confirm bool) error {
	if !confirm {
		return fmt.Errorf("update requires explicit confirmation")
	}
	name := AssetName()
	var a *Asset
	for i := range release.Assets {
		if release.Assets[i].Name == name || strings.HasPrefix(release.Assets[i].Name, name+".") {
			a = &release.Assets[i]
			break
		}
	}
	if a == nil {
		return fmt.Errorf("release has no asset for %s", runtime.GOARCH)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return err
	}
	checksum := ""
	for _, x := range release.Assets {
		if strings.Contains(x.Name, "sha256") || strings.Contains(x.Name, "checksums") {
			cr, e := http.Get(x.URL)
			if e == nil {
				b, _ := io.ReadAll(cr.Body)
				cr.Body.Close()
				for _, line := range strings.Split(string(b), "\n") {
					fields := strings.Fields(line)
					if len(fields) >= 2 && strings.Contains(fields[1], a.Name) {
						checksum = fields[0]
					}
				}
			}
		}
	}
	if checksum == "" || !VerifySHA256(data, checksum) {
		return fmt.Errorf("download checksum verification failed")
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		return err
	}
	backup := binaryPath + ".backup"
	if err := os.Rename(binaryPath, backup); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(binaryPath), ".infrapilot-update-")
	if err != nil {
		_ = os.Rename(backup, binaryPath)
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0755)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Rename(backup, binaryPath)
		return err
	}
	if err = os.Rename(tmpName, binaryPath); err != nil {
		_ = os.Rename(backup, binaryPath)
		return err
	}
	return nil
}
