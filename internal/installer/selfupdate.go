package installer

import (
	"archive/tar"
	"archive/zip"
	"bufio"
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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/philtechs-org/phi/internal/ui"
)

// Self-update endpoints + asset naming. The archive name format is set by
// goreleaser config (phi_<ver>_<OS>_<arch>.<ext>) and matches what the
// install scripts use, so any release that the install scripts can fetch
// will also work for self-update.
const (
	selfUpdateAPIBase   = "https://api.github.com/repos/philtechs-org/phi/releases"
	selfUpdateUserAgent = "phi-self-update"
)

// SelfUpdateOptions controls phi self-update behavior.
type SelfUpdateOptions struct {
	// CheckOnly reports whether a newer version is available without
	// actually replacing the binary. Exit code is still 0 on success.
	CheckOnly bool
	// Version pins the install to a specific tag (e.g. "v0.1.2"). When
	// empty, the latest release is used.
	Version string
	// Yes skips the interactive confirmation prompt. Required for
	// non-interactive use (CI, scripts).
	Yes bool
}

type ghAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Assets  []ghAsset `json:"assets"`
}

// SelfUpdate replaces the running phi binary with the latest (or pinned)
// release from GitHub. The new binary is verified against checksums.txt
// before installation. On Windows, where a running .exe can't be
// overwritten in place, the current binary is renamed to .old first;
// CleanupSelfUpdateLeftovers removes that .old file on next startup.
func SelfUpdate(currentVersion string, opts SelfUpdateOptions) error {
	rel, err := fetchRelease(opts.Version)
	if err != nil {
		return fmt.Errorf("fetch release info: %w", err)
	}

	// Don't strip "-dev" here. A local "0.2.0-dev" build is pre-release of
	// 0.2.0, and self-update from a dev build should fetch the published
	// 0.2.0 binary, not claim up-to-date.
	curVer := strings.TrimPrefix(currentVersion, "v")
	tgtVer := strings.TrimPrefix(rel.TagName, "v")

	if opts.CheckOnly {
		if curVer == tgtVer {
			fmt.Printf("phi %s is up to date\n", currentVersion)
		} else {
			fmt.Printf("phi %s available (you have %s)\n", rel.TagName, currentVersion)
			fmt.Printf("  release notes: %s\n", rel.HTMLURL)
			fmt.Println("  run 'phi self-update' to install")
		}
		return nil
	}

	if curVer == tgtVer && opts.Version == "" {
		fmt.Printf("phi %s is already up to date\n", currentVersion)
		return nil
	}

	asset, checksums, err := pickReleaseAssets(rel)
	if err != nil {
		return err
	}

	// Preflight: confirm we can actually write to the install dir
	// BEFORE spending a network round-trip + multiple MB of bandwidth
	// on a download we can't deploy. Catches the classic
	// "phi installed in /usr/local/bin, user isn't sudo" case with a
	// clear error instead of a confusing post-download permission
	// failure.
	if err := preflightInstallDir(); err != nil {
		return err
	}

	if !opts.Yes {
		fmt.Printf("update phi from %s to %s? [y/N] ", currentVersion, rel.TagName)
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(sc.Text())), "y") {
			return errors.New("update cancelled")
		}
	}

	fmt.Printf("downloading %s (%.1f MB)...\n", asset.Name, float64(asset.Size)/(1024*1024))
	archiveBytes, err := downloadAsset(asset.DownloadURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}

	if checksums.DownloadURL == "" {
		ui.PrintWarning("checksums.txt not in release assets; skipping integrity verification")
	} else {
		fmt.Println("verifying checksum...")
		sumsBody, err := downloadAsset(checksums.DownloadURL)
		if err != nil {
			return fmt.Errorf("download checksums.txt: %w", err)
		}
		if err := verifyAssetChecksum(asset.Name, archiveBytes, sumsBody); err != nil {
			return err
		}
	}

	fmt.Println("extracting binary...")
	binaryBytes, err := extractPhiBinary(asset.Name, archiveBytes)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	fmt.Println("installing...")
	if err := replaceRunningBinary(binaryBytes); err != nil {
		return fmt.Errorf("install new binary: %w", err)
	}

	fmt.Printf("\nok: phi updated to %s\n", rel.TagName)
	fmt.Printf("  release notes: %s\n", rel.HTMLURL)
	return nil
}

// preflightInstallDir verifies that the directory containing the
// running phi binary is writable BEFORE we download anything. Catches
// the common "installed system-wide, user isn't root" case with a
// clear, actionable message instead of a generic post-download failure.
//
// We open a sibling temp file with the same prefix the real install
// uses; if create succeeds, we have enough permission for the eventual
// rename-over. If it fails, surface a platform-aware suggestion
// (sudo on Unix, "run from elevated PowerShell" on Windows).
func preflightInstallDir() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	dir := filepath.Dir(self)
	probe, err := os.CreateTemp(dir, ".phi-preflight-*")
	if err != nil {
		hint := "the directory may need elevated permissions"
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			hint = "try: sudo phi self-update --yes"
		} else if runtime.GOOS == "windows" {
			hint = "try running from an Administrator PowerShell, or reinstall to a user-writable location"
		}
		return fmt.Errorf("can't write to install directory %s: %w\n  %s", dir, err, hint)
	}
	probePath := probe.Name()
	probe.Close()
	_ = os.Remove(probePath)
	return nil
}

// CleanupSelfUpdateLeftovers removes the .old file left behind by a
// previous Windows self-update. Best-effort; failures are silent because
// the file doesn't shadow anything and will eventually be removed by
// disk cleanup or another self-update cycle.
func CleanupSelfUpdateLeftovers() {
	if runtime.GOOS != "windows" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	_ = os.Remove(self + ".old")
}

func fetchRelease(version string) (*ghRelease, error) {
	url := selfUpdateAPIBase + "/latest"
	if version != "" {
		v := "v" + strings.TrimPrefix(version, "v")
		url = selfUpdateAPIBase + "/tags/" + v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", selfUpdateUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("release %q not found on github.com/philtechs-org/phi", version)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github API: %s", resp.Status)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, errors.New("github API returned a release with no tag_name")
	}
	return &rel, nil
}

func pickReleaseAssets(rel *ghRelease) (binary, checksums ghAsset, err error) {
	osTitle, archTitle, ext, err := platformAssetTokens()
	if err != nil {
		return ghAsset{}, ghAsset{}, err
	}

	// Goreleaser produces:
	//   phi_<ver>_Linux_x86_64.tar.gz
	//   phi_<ver>_Darwin_arm64.tar.gz
	//   phi_<ver>_Windows_x86_64.zip
	//   checksums.txt
	suffix := fmt.Sprintf("_%s_%s%s", osTitle, archTitle, ext)
	for _, a := range rel.Assets {
		switch {
		case strings.HasSuffix(a.Name, suffix):
			binary = a
		case a.Name == "checksums.txt":
			checksums = a
		}
	}
	if binary.Name == "" {
		return ghAsset{}, ghAsset{},
			fmt.Errorf("no asset for %s/%s in release %s", osTitle, archTitle, rel.TagName)
	}
	return binary, checksums, nil
}

func platformAssetTokens() (osTitle, archTitle, ext string, err error) {
	switch runtime.GOOS {
	case "linux":
		osTitle, ext = "Linux", ".tar.gz"
	case "darwin":
		osTitle, ext = "Darwin", ".tar.gz"
	case "windows":
		osTitle, ext = "Windows", ".zip"
	default:
		return "", "", "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		archTitle = "x86_64"
	case "arm64":
		archTitle = "arm64"
	default:
		return "", "", "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}
	return osTitle, archTitle, ext, nil
}

func downloadAsset(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", selfUpdateUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// verifyAssetChecksum hashes the downloaded archive and compares against
// the matching line in checksums.txt. Returns a clear error on mismatch
// or missing entry — the entire point of self-update is supply-chain
// integrity, so a missing checksum is a hard fail (caller skips the call
// only if the release predates checksums.txt entirely).
func verifyAssetChecksum(assetName string, body, checksums []byte) error {
	got := sha256.Sum256(body)
	gotHex := hex.EncodeToString(got[:])

	for _, raw := range strings.Split(string(checksums), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Goreleaser format: "<sha256>  <filename>"
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == assetName {
			if parts[0] == gotHex {
				return nil
			}
			return fmt.Errorf("checksum mismatch for %s: archive sha256 %s, expected %s",
				assetName, gotHex, parts[0])
		}
	}
	return fmt.Errorf("no checksum entry for %s in checksums.txt", assetName)
}

func extractPhiBinary(archiveName string, body []byte) ([]byte, error) {
	binaryName := "phi"
	if runtime.GOOS == "windows" {
		binaryName = "phi.exe"
	}
	switch {
	case strings.HasSuffix(archiveName, ".zip"):
		return extractFromZip(body, binaryName)
	case strings.HasSuffix(archiveName, ".tar.gz"), strings.HasSuffix(archiveName, ".tgz"):
		return extractFromTarGz(body, binaryName)
	}
	return nil, fmt.Errorf("unsupported archive format: %s", archiveName)
}

func extractFromZip(body []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in zip archive", name)
}

func extractFromTarGz(body []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in tar.gz archive", name)
}

// replaceRunningBinary writes binaryBytes over the current executable's
// path. On Unix this is a single os.Rename — the kernel keeps the
// running file alive via its open inode while the path is replaced. On
// Windows the running .exe can't be overwritten, but it CAN be renamed,
// so we move it to .old before installing the new one. The .old sibling
// is harmless (different name) and gets removed on next startup by
// CleanupSelfUpdateLeftovers.
func replaceRunningBinary(binaryBytes []byte) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".phi-update-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w (insufficient permission? try running with sudo / from an elevated shell)", dir, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(binaryBytes); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		oldPath := self + ".old"
		_ = os.Remove(oldPath) // any leftover from a previous update
		if err := os.Rename(self, oldPath); err != nil {
			return fmt.Errorf("rename current binary: %w", err)
		}
		if err := os.Rename(tmpPath, self); err != nil {
			// Best-effort restore of the original.
			_ = os.Rename(oldPath, self)
			return fmt.Errorf("install new binary: %w", err)
		}
	} else {
		if err := os.Rename(tmpPath, self); err != nil {
			return fmt.Errorf("install new binary: %w", err)
		}
	}
	committed = true
	return nil
}
