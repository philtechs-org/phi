package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/philtechs-org/phi/internal/workspace"
)

// linkWorkspaces creates a node_modules/<workspace-name> entry for each
// workspace pointing to its source directory. On Windows we use a junction
// (works without admin); elsewhere a regular symlink. Existing entries (e.g.
// from a previous install) are removed first so the workspace version wins
// over any registry copy.
func linkWorkspaces(workspaces []*workspace.Workspace) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, ws := range workspaces {
		linkPath := filepath.Join("node_modules", filepath.FromSlash(ws.Name))
		if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", ws.Name, err)
		}
		if err := os.RemoveAll(linkPath); err != nil {
			return fmt.Errorf("clear %s: %w", linkPath, err)
		}
		target := ws.Dir
		if !filepath.IsAbs(target) {
			target = filepath.Join(cwd, target)
		}
		if err := createDirLink(target, linkPath); err != nil {
			return fmt.Errorf("link %s: %w", ws.Name, err)
		}
	}
	return nil
}

func createDirLink(target, link string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("mklink: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return os.Symlink(target, link)
}
