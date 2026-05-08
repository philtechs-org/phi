package installer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestVerifyAssetChecksumOK(t *testing.T) {
	body := []byte("hello world")
	sum := sha256.Sum256(body)
	checksums := []byte(fmt.Sprintf("%s  phi_0.2.0_Linux_x86_64.tar.gz\n", hex.EncodeToString(sum[:])))
	if err := verifyAssetChecksum("phi_0.2.0_Linux_x86_64.tar.gz", body, checksums); err != nil {
		t.Errorf("expected match, got %v", err)
	}
}

func TestVerifyAssetChecksumMismatch(t *testing.T) {
	body := []byte("hello world")
	checksums := []byte("0000000000000000000000000000000000000000000000000000000000000000  phi_0.2.0_Linux_x86_64.tar.gz\n")
	err := verifyAssetChecksum("phi_0.2.0_Linux_x86_64.tar.gz", body, checksums)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected checksum mismatch error, got %v", err)
	}
}

func TestVerifyAssetChecksumMissingEntry(t *testing.T) {
	body := []byte("hello world")
	checksums := []byte("aaa  phi_0.2.0_Darwin_x86_64.tar.gz\n")
	err := verifyAssetChecksum("phi_0.2.0_Linux_x86_64.tar.gz", body, checksums)
	if err == nil || !strings.Contains(err.Error(), "no checksum entry") {
		t.Errorf("expected no-entry error, got %v", err)
	}
}

func TestVerifyAssetChecksumIgnoresBlanksAndComments(t *testing.T) {
	body := []byte("hello world")
	sum := sha256.Sum256(body)
	checksums := []byte(fmt.Sprintf("# header line\n\n%s  phi_0.2.0_Linux_x86_64.tar.gz\n", hex.EncodeToString(sum[:])))
	if err := verifyAssetChecksum("phi_0.2.0_Linux_x86_64.tar.gz", body, checksums); err != nil {
		t.Errorf("expected match (with header + blank lines), got %v", err)
	}
}

func TestPlatformAssetTokens(t *testing.T) {
	osTitle, archTitle, ext, err := platformAssetTokens()
	if err != nil {
		t.Skipf("unsupported platform for self-update: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	want := map[string]struct{ os, arch, ext string }{
		"linux/amd64":   {"Linux", "x86_64", ".tar.gz"},
		"linux/arm64":   {"Linux", "arm64", ".tar.gz"},
		"darwin/amd64":  {"Darwin", "x86_64", ".tar.gz"},
		"darwin/arm64":  {"Darwin", "arm64", ".tar.gz"},
		"windows/amd64": {"Windows", "x86_64", ".zip"},
	}
	key := runtime.GOOS + "/" + runtime.GOARCH
	if w, ok := want[key]; ok {
		if osTitle != w.os || archTitle != w.arch || ext != w.ext {
			t.Errorf("platformAssetTokens() = (%q, %q, %q), want (%q, %q, %q)",
				osTitle, archTitle, ext, w.os, w.arch, w.ext)
		}
	}
}

func TestPickReleaseAssetsMatchesPlatform(t *testing.T) {
	osTitle, archTitle, ext, err := platformAssetTokens()
	if err != nil {
		t.Skipf("unsupported platform for self-update: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	wantName := fmt.Sprintf("phi_0.2.0_%s_%s%s", osTitle, archTitle, ext)
	rel := &ghRelease{
		TagName: "v0.2.0",
		Assets: []ghAsset{
			{Name: "phi_0.2.0_Linux_x86_64.tar.gz", DownloadURL: "u1"},
			{Name: "phi_0.2.0_Linux_arm64.tar.gz", DownloadURL: "u2"},
			{Name: "phi_0.2.0_Darwin_x86_64.tar.gz", DownloadURL: "u3"},
			{Name: "phi_0.2.0_Darwin_arm64.tar.gz", DownloadURL: "u4"},
			{Name: "phi_0.2.0_Windows_x86_64.zip", DownloadURL: "u5"},
			{Name: "checksums.txt", DownloadURL: "uc"},
		},
	}
	binary, checksums, err := pickReleaseAssets(rel)
	if err != nil {
		t.Fatal(err)
	}
	if binary.Name != wantName {
		t.Errorf("binary asset = %q, want %q", binary.Name, wantName)
	}
	if checksums.Name != "checksums.txt" {
		t.Errorf("checksums asset = %q, want checksums.txt", checksums.Name)
	}
}

func TestPickReleaseAssetsErrorsOnMissing(t *testing.T) {
	rel := &ghRelease{
		TagName: "v0.99.0",
		Assets:  []ghAsset{{Name: "README.md"}},
	}
	if _, _, err := pickReleaseAssets(rel); err == nil {
		t.Error("expected error when no platform asset present")
	}
}

func TestExtractFromZip(t *testing.T) {
	want := []byte("\x7FELFFAKEBINARY")
	body := makeZip(t, "phi", want)
	got, err := extractFromZip(body, "phi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractFromZipMissing(t *testing.T) {
	body := makeZip(t, "other", []byte("x"))
	if _, err := extractFromZip(body, "phi"); err == nil {
		t.Error("expected error when binary not present")
	}
}

func TestExtractFromTarGz(t *testing.T) {
	want := []byte("\x7FELFFAKEBINARY")
	body := makeTarGz(t, "phi", want)
	got, err := extractFromTarGz(body, "phi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractPhiBinaryRoutes(t *testing.T) {
	want := []byte("BINARY")
	zipBytes := makeZip(t, expectedBinaryName(), want)
	tgzBytes := makeTarGz(t, expectedBinaryName(), want)

	if _, err := extractPhiBinary("phi_0.2.0_Windows_x86_64.zip", zipBytes); err != nil {
		t.Errorf("zip route failed: %v", err)
	}
	if _, err := extractPhiBinary("phi_0.2.0_Linux_x86_64.tar.gz", tgzBytes); err != nil {
		t.Errorf("tar.gz route failed: %v", err)
	}
	if _, err := extractPhiBinary("phi_0.2.0_unknown.7z", tgzBytes); err == nil {
		t.Error("expected error for unknown archive format")
	}
}

func expectedBinaryName() string {
	if runtime.GOOS == "windows" {
		return "phi.exe"
	}
	return "phi"
}

func makeZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    0o755,
		Size:    int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
