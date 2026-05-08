package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// writeFileAtomic writes data to path through a sibling temp file and
// then renames it onto the target. Either the new content fully
// replaces the old or the old is preserved — never a half-written
// intermediate.
//
// Why this matters: the alternative (os.WriteFile) opens the destination
// for write, which truncates the existing file before writing the new
// content. A process crash or power loss between truncate and write
// leaves the user with an empty / partial package.json or phi.lock —
// far worse than the original state. We use this for every file that
// represents non-recreatable user state (package.json) or expensive
// derived state (phi.lock, phi-report.json) where corruption would
// either lose data or cause confusing follow-up failures.
//
// Atomicity comes from os.Rename, which is a single inode swap on
// POSIX (rename(2)) and a MoveFileExW with MOVEFILE_REPLACE_EXISTING on
// Windows (Go wraps both transparently). Both are atomic at the
// filesystem level. Same-directory temp file is required — cross-device
// rename is NOT atomic and falls back to copy + delete, which we'd
// rather not do silently.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// Hidden, prefix-marked temp file so a crashed write leaves a
	// recognizably-phi artifact rather than something that looks like a
	// stray dotfile from the user's own editor.
	tmp, err := os.CreateTemp(dir, "."+base+".phi-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	// Sync before close — without this, the rename can race the write
	// on systems that buffer aggressively (some Linux fs configs).
	// Best-effort: ignore Sync errors on platforms (e.g. Plan9) where
	// the syscall is a no-op or restricted.
	_ = tmp.Sync()
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Match permissions. CreateTemp uses 0o600 by default; we need 0o644
	// for files that other tools (npm, editors) read.
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		// Windows occasionally fails the rename if the destination is
		// open in another process (an editor previewing the file). Give
		// one targeted hint without retrying — repeated retries can mask
		// real permission problems.
		if runtime.GOOS == "windows" {
			return fmt.Errorf("rename %s onto %s: %w (close any editor / file watcher holding it open)", tmpPath, path, err)
		}
		return fmt.Errorf("rename %s onto %s: %w", tmpPath, path, err)
	}
	committed = true
	return nil
}
