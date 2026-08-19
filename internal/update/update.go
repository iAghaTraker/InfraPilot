// Package update implements signed-by-checksum release discovery and installation.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const Repository = "iAghaTraker/InfraPilot"

const MetadataPath = "/etc/infrapilot/install.json"

type Metadata struct {
	Method    string   `json:"method"`
	Prefix    string   `json:"prefix"`
	BinaryDir string   `json:"binary_dir"`
	Services  []string `json:"services"`
}

type Runner func(context.Context, string, ...string) error

func commandRunner(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func LoadMetadata(path string) (Metadata, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var m Metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return Metadata{}, err
	}
	if m.Method != "binary" || !filepath.IsAbs(m.BinaryDir) {
		return Metadata{}, fmt.Errorf("invalid installation metadata")
	}
	return m, nil
}

func Detect(metadataPath, executable string) (Metadata, error) {
	if m, err := LoadMetadata(metadataPath); err == nil {
		return m, nil
	}
	p, _ := filepath.EvalSymlinks(executable)
	if p == "/usr/local/bin/infrapilot" || executable == "/usr/local/bin/infrapilot" {
		return Metadata{Method: "binary", Prefix: "/usr/local", BinaryDir: "/usr/local/bin", Services: []string{"infrapilot-agent.service", "infrapilot-web.service"}}, nil
	}
	return Metadata{}, fmt.Errorf("cannot determine installation method; reinstall InfraPilot to record installation metadata")
}

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
	// Release artifacts are tarballs; extract the selected CLI binary.
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		gz, e := gzip.NewReader(strings.NewReader(string(data)))
		if e != nil {
			return e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if filepath.Base(h.Name) == "infrapilot" {
				data, e = io.ReadAll(io.LimitReader(tr, 128<<20))
				if e != nil {
					return e
				}
				break
			}
		}
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

// InstallAll transactionally replaces all release binaries, restarts services,
// verifies their health, and restores the previous binaries on any failure.
func (c Client) InstallAll(ctx context.Context, release Release, metadata Metadata, run Runner) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("binary update must run as root")
	}
	if run == nil {
		run = commandRunner
	}
	var artifact, checksums Asset
	for _, a := range release.Assets {
		if a.Name == AssetName() {
			artifact = a
		}
		if a.Name == "checksums.txt" {
			checksums = a
		}
	}
	if artifact.URL == "" || checksums.URL == "" {
		return fmt.Errorf("release is missing %s or checksums.txt", AssetName())
	}
	download := func(url string, limit int64) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("download returned HTTP %s", resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, limit))
	}
	archive, err := download(artifact.URL, 256<<20)
	if err != nil {
		return err
	}
	sums, err := download(checksums.URL, 2<<20)
	if err != nil {
		return err
	}
	expected := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.TrimPrefix(f[1], "*") == artifact.Name {
			expected = f[0]
		}
	}
	if expected == "" || !VerifySHA256(archive, expected) {
		return fmt.Errorf("download checksum verification failed")
	}
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	names := []string{"infrapilot", "infrapilot-agent", "infrapilot-web"}
	files := map[string][]byte{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		for _, name := range names {
			if filepath.Base(h.Name) == name {
				b, e := io.ReadAll(io.LimitReader(tr, 128<<20))
				if e != nil {
					return e
				}
				files[name] = b
			}
		}
	}
	for _, name := range names {
		if len(files[name]) == 0 {
			return fmt.Errorf("release archive is missing %s", name)
		}
	}
	backups := map[string]string{}
	installed := []string{}
	rollback := func(cause error) error {
		for _, name := range installed {
			target := filepath.Join(metadata.BinaryDir, name)
			_ = os.Remove(target)
			_ = os.Rename(backups[name], target)
		}
		for _, svc := range metadata.Services {
			_ = run(ctx, "systemctl", "restart", svc)
		}
		return fmt.Errorf("update failed and was rolled back: %w", cause)
	}
	for _, name := range names {
		target := filepath.Join(metadata.BinaryDir, name)
		backup := target + ".update-backup"
		_ = os.Remove(backup)
		if err := os.Rename(target, backup); err != nil {
			return rollback(err)
		}
		backups[name] = backup
		tmp, err := os.CreateTemp(metadata.BinaryDir, ".infrapilot-update-")
		if err != nil {
			return rollback(err)
		}
		tmpName := tmp.Name()
		if _, err = tmp.Write(files[name]); err == nil {
			err = tmp.Sync()
		}
		if err == nil {
			err = tmp.Chmod(0755)
		}
		if e := tmp.Close(); err == nil {
			err = e
		}
		if err == nil {
			err = os.Rename(tmpName, target)
		}
		_ = os.Remove(tmpName)
		if err != nil {
			return rollback(err)
		}
		installed = append(installed, name)
	}
	for _, svc := range metadata.Services {
		if err := run(ctx, "systemctl", "restart", svc); err != nil {
			return rollback(err)
		}
	}
	for _, svc := range metadata.Services {
		if err := run(ctx, "systemctl", "is-active", "--quiet", svc); err != nil {
			return rollback(fmt.Errorf("health check failed for %s: %w", svc, err))
		}
	}
	if err := run(ctx, filepath.Join(metadata.BinaryDir, "infrapilot"), "doctor"); err != nil {
		return rollback(fmt.Errorf("doctor health check failed: %w", err))
	}
	for _, backup := range backups {
		_ = os.Remove(backup)
	}
	return nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
