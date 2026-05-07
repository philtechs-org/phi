package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/philtechs-org/phi/internal/ui"
)

var lifecyclePhases = []string{"preinstall", "install", "postinstall"}

// RunLifecycleScripts executes preinstall/install/postinstall scripts only
// for packages explicitly named in allowedPkgs. The Tier 1 default is to
// pass an empty allowlist — no scripts run unless the user opts in via
// --allow-scripts.
func RunLifecycleScripts(extracted map[string]string, allowedPkgs []string) error {
	if len(allowedPkgs) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(allowedPkgs))
	for _, p := range allowedPkgs {
		allow[p] = true
	}
	for pkgName, pkgDir := range extracted {
		if !allow[pkgName] {
			continue
		}
		scripts, err := readScripts(pkgDir)
		if err != nil {
			ui.PrintWarning(fmt.Sprintf("read scripts %s: %v", pkgName, err))
			continue
		}
		for _, phase := range lifecyclePhases {
			cmd := scripts[phase]
			if cmd == "" {
				continue
			}
			ui.PrintWarning(fmt.Sprintf("running %s for %s: %s", phase, pkgName, cmd))
			if err := runScript(pkgDir, cmd); err != nil {
				return fmt.Errorf("%s %s: %w", phase, pkgName, err)
			}
		}
	}
	return nil
}

func readScripts(pkgDir string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return nil, err
	}
	var data struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data.Scripts, nil
}

func runScript(pkgDir, script string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", script)
	} else {
		cmd = exec.Command("sh", "-c", script)
	}
	cmd.Dir = pkgDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
