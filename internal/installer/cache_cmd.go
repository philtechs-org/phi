package installer

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/philtechs-org/phi/internal/cache"
)

// CacheStat prints the size and entry count of the on-disk tarball cache.
func CacheStat() error {
	s, err := cache.Stat()
	if err != nil {
		return err
	}
	fmt.Printf("Cache: %s\n", s.Path)
	fmt.Printf("  tarballs: %d\n", s.Count)
	fmt.Printf("  size:     %s\n", humanBytes(s.Bytes))
	return nil
}

// CacheClean removes tarballs older than the given age. age=0 wipes everything.
func CacheClean(age time.Duration) error {
	n, b, err := cache.Clean(age)
	if err != nil {
		return err
	}
	if age == 0 {
		fmt.Printf("Removed %d tarball(s), %s\n", n, humanBytes(b))
	} else {
		fmt.Printf("Removed %d tarball(s) older than %s (%s)\n", n, age, humanBytes(b))
	}
	return nil
}

// ParseAge accepts Go's time.Duration format plus a "d" suffix for days
// (`30d`, `2w` is not supported — use `14d`).
func ParseAge(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid days: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
