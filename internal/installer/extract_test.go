package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func makeRawTarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typeflag
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o644,
			Size:     int64(len(e.body)),
			Typeflag: typ,
			Linkname: e.linkname,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg && e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
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

func TestExtract_HappyPath(t *testing.T) {
	dest := t.TempDir()
	data := makeRawTarball(t, []tarEntry{
		{name: "package/index.js", body: "module.exports = 1;"},
		{name: "package/lib/util.js", body: "exports.x = 2;"},
		{name: "package/package.json", body: `{"name":"x"}`},
	})
	if err := Extract(data, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	mustRead(t, filepath.Join(dest, "index.js"), "module.exports = 1;")
	mustRead(t, filepath.Join(dest, "lib", "util.js"), "exports.x = 2;")
	mustRead(t, filepath.Join(dest, "package.json"), `{"name":"x"}`)
}

func TestExtract_StripsFirstSegment(t *testing.T) {
	dest := t.TempDir()
	// Some tarballs use a different top-level dir name; first segment is always stripped.
	data := makeRawTarball(t, []tarEntry{
		{name: "weird-prefix/index.js", body: "ok"},
	})
	if err := Extract(data, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	mustRead(t, filepath.Join(dest, "index.js"), "ok")
}

func TestExtract_RejectsParentEscape(t *testing.T) {
	dest := t.TempDir()
	data := makeRawTarball(t, []tarEntry{
		{name: "package/../../evil.js", body: "owned"},
	})
	if err := Extract(data, dest); err == nil {
		t.Errorf("expected error for parent-escape path")
	}
}

func TestExtract_RejectsAbsolutePath(t *testing.T) {
	dest := t.TempDir()
	data := makeRawTarball(t, []tarEntry{
		{name: "package//etc/passwd", body: "owned"},
	})
	if err := Extract(data, dest); err == nil {
		t.Errorf("expected error for absolute path after strip")
	}
}

func TestExtract_RejectsBackslash(t *testing.T) {
	dest := t.TempDir()
	data := makeRawTarball(t, []tarEntry{
		{name: "package/..\\..\\evil", body: "owned"},
	})
	if err := Extract(data, dest); err == nil {
		t.Errorf("expected error for backslash path")
	}
}

func TestExtract_SkipsSymlinks(t *testing.T) {
	dest := t.TempDir()
	data := makeRawTarball(t, []tarEntry{
		{name: "package/index.js", body: "real"},
		{name: "package/link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	})
	if err := Extract(data, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
		t.Errorf("symlink should not have been created, got err=%v", err)
	}
}

func mustRead(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s: got %q, want %q", path, string(got), want)
	}
}
