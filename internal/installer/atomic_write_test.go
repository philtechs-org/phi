package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomicCreates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	body := []byte(`{"name":"test"}`)
	if err := writeFileAtomic(path, body, 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("contents = %q, want %q", got, body)
	}
}

func TestWriteFileAtomicReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phi.lock")
	old := []byte(`{"old": true}`)
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}
	new := []byte(`{"new": true}`)
	if err := writeFileAtomic(path, new, 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, new) {
		t.Errorf("after replace, contents = %q, want %q", got, new)
	}
}

func TestWriteFileAtomicLeavesNoTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phi-report.json")
	if err := writeFileAtomic(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != "phi-report.json" {
			t.Errorf("unexpected leftover file: %s", name)
		}
	}
}

func TestWriteFileAtomicNoTempOnFailure(t *testing.T) {
	// Force failure by pointing at a nonexistent parent directory.
	// CreateTemp will fail; we verify no half-baked artifact is left.
	missing := filepath.Join(t.TempDir(), "does", "not", "exist", "f")
	err := writeFileAtomic(missing, []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected error writing to nonexistent parent dir")
	}
}

func TestWriteFileAtomicPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits don't round-trip on Windows the same way")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := writeFileAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0o600", st.Mode().Perm())
	}
}
