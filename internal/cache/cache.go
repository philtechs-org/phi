// Package cache stores npm tarballs locally so repeat installs of the same
// resolved version don't refetch from the registry. Cache key is the npm
// integrity string (e.g. "sha512-…"); since the key is the content hash, any
// successful read is implicitly content-verified by the caller.
package cache

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Path returns the on-disk path that would be used to cache the given
// integrity string. Returns "" if the integrity is malformed.
func Path(integrity string) string {
	key := keyFor(integrity)
	if key == "" {
		return ""
	}
	dir, err := dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, key+".tgz")
}

// Load returns cached tarball bytes for the given integrity. (nil, false, nil)
// means "not cached"; (nil, false, err) is a real I/O error worth surfacing.
func Load(integrity string) ([]byte, bool, error) {
	p := Path(integrity)
	if p == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// Store writes data into the cache under the integrity key. A failure to
// write is non-fatal — callers can swallow the error and proceed; the next
// install will simply refetch.
func Store(integrity string, data []byte) error {
	p := Path(integrity)
	if p == "" {
		return fmt.Errorf("cache: malformed integrity %q", integrity)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "phi", "tarballs"), nil
}

// Dir returns the on-disk cache directory. Public so phi cache commands can
// report it to the user.
func Dir() (string, error) {
	return dir()
}

// RunDir returns the on-disk staging directory for a `phi x` (npx-style) run
// of <name>@<version>. The directory is NOT created — caller decides when.
// Scoped names ("@scope/pkg") have their slash replaced with a separator that
// is valid on every filesystem we ship to.
func RunDir(name, version string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	safeName := strings.ReplaceAll(name, "/", "__")
	return filepath.Join(base, "phi", "run", safeName+"@"+version), nil
}

// Stats summarizes the on-disk cache.
type Stats struct {
	Path  string
	Count int
	Bytes int64
}

// Stat walks the cache directory and reports the number and total size of
// cached tarballs. A missing cache directory is not an error — Stats.Count
// is just zero.
func Stat() (Stats, error) {
	p, err := dir()
	if err != nil {
		return Stats{}, err
	}
	s := Stats{Path: p}
	err = filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		s.Count++
		s.Bytes += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return s, nil
	}
	return s, err
}

// Clean removes cached tarballs older than the given age. Returns the count
// and total bytes removed. Pass time.Duration(0) to remove everything.
func Clean(olderThan time.Duration) (count int, bytes int64, err error) {
	p, err := dir()
	if err != nil {
		return 0, 0, err
	}
	cutoff := time.Now().Add(-olderThan)
	walkErr := filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if olderThan > 0 && info.ModTime().After(cutoff) {
			return nil
		}
		if e := os.Remove(path); e == nil {
			count++
			bytes += info.Size()
		}
		return nil
	})
	if os.IsNotExist(walkErr) {
		return count, bytes, nil
	}
	return count, bytes, walkErr
}

// keyFor converts "sha512-<base64>" into "sha512-<hex>" so the result is
// safe across all filesystems (no slashes, plus, equals).
func keyFor(integrity string) string {
	parts := strings.SplitN(integrity, "-", 2)
	if len(parts) != 2 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	return parts[0] + "-" + hex.EncodeToString(raw)
}
