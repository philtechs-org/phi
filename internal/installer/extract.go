package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract unpacks an npm package tarball (gzip+tar) into dest. The leading
// path component (typically "package/") is stripped, matching npm's own
// behavior. Symlinks and hardlinks are skipped in Tier 1. Path traversal
// attempts cause the extraction to abort.
func Extract(tarball []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		name := stripFirstSegment(hdr.Name)
		if name == "" {
			continue
		}
		if !safeRelPath(name) {
			return fmt.Errorf("unsafe path in tarball: %s", hdr.Name)
		}

		target := filepath.Join(dest, filepath.FromSlash(name))
		rel, err := filepath.Rel(dest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe path in tarball: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeFile(target, tr, hdr.Mode); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Tier 1: skip; symlink policy lives in Tier 2.
			continue
		}
	}
}

func writeFile(path string, src io.Reader, mode int64) error {
	perm := os.FileMode(mode) & 0o777
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

func stripFirstSegment(name string) string {
	i := strings.IndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

func safeRelPath(name string) bool {
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return false
	}
	if strings.Contains(name, "\\") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
