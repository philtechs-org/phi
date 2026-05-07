package installer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Do executes a named script from package.json's "scripts" field.
// Pre- and post-hooks (pre<name>, post<name>) run in sequence around the
// main script. Extra args are appended to the main script command, so
// `phi do dev --port 3000` runs `<dev script> --port 3000`.
//
// node_modules/.bin is prepended to PATH so locally-installed CLI tools
// (vite, next, tsc, jest, …) work without a global install.
func Do(name string, extra []string) error {
	if name == "" {
		return errors.New("run: script name required")
	}
	body, err := os.ReadFile("package.json")
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("no package.json in current directory")
		}
		return err
	}
	body = bytes.TrimPrefix(body, utf8BOM)

	var data struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("parse package.json: %w", err)
	}
	if data.Scripts == nil || data.Scripts[name] == "" {
		known := availableScripts(data.Scripts)
		if len(known) == 0 {
			return fmt.Errorf("no %q script in package.json (no scripts defined)", name)
		}
		return fmt.Errorf("no %q script in package.json. available: %s",
			name, strings.Join(known, ", "))
	}

	for _, phase := range []string{"pre" + name, name, "post" + name} {
		script := data.Scripts[phase]
		if script == "" {
			continue
		}
		fullCmd := script
		if phase == name && len(extra) > 0 {
			fullCmd = script + " " + strings.Join(extra, " ")
		}
		fmt.Printf("\n> %s@%s %s\n> %s\n\n",
			gjsonString(body, "name"),
			gjsonString(body, "version"),
			phase, fullCmd)
		if err := runUserScript(".", fullCmd); err != nil {
			return fmt.Errorf("%s: %w", phase, err)
		}
	}
	return nil
}

// Exec runs a binary from node_modules/.bin with the given args.
// Like npm/pnpm exec but never auto-installs — the binary must already be
// on disk. Useful for one-shot tool invocations: `phi exec eslint .`,
// `phi exec tsc --watch`. The binary's package was scanned at install
// time, so it's already passed phi's analysis.
func Exec(binary string, args []string) error {
	if binary == "" {
		return errors.New("exec: binary name required")
	}
	binDir := filepath.Join("node_modules", ".bin")
	if !binaryExists(filepath.Join(binDir, binary)) {
		return fmt.Errorf("%q not found in %s — install the providing package first",
			binary, binDir)
	}
	full := binary
	if len(args) > 0 {
		full = binary + " " + strings.Join(args, " ")
	}
	return runUserScript(".", full)
}

func binaryExists(path string) bool {
	candidates := []string{path}
	if runtime.GOOS == "windows" {
		candidates = []string{path + ".cmd", path + ".exe", path + ".bat", path}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

// runUserScript runs script via the platform shell with PATH augmented to
// include node_modules/.bin. Stdio is wired to the parent so the user
// sees the dev server / build output live.
func runUserScript(dir, script string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", script)
	} else {
		cmd = exec.Command("sh", "-c", script)
	}
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	binDir, err := filepath.Abs(filepath.Join(dir, "node_modules", ".bin"))
	if err == nil {
		cmd.Env = augmentPath(os.Environ(), binDir)
	}
	return cmd.Run()
}

// augmentPath prepends prefix to whichever PATH-like env var is set
// (case-insensitive — Windows uses "Path", POSIX uses "PATH").
func augmentPath(env []string, prefix string) []string {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	for i, e := range env {
		eq := strings.Index(e, "=")
		if eq <= 0 {
			continue
		}
		if strings.EqualFold(e[:eq], "PATH") {
			env[i] = e[:eq+1] + prefix + sep + e[eq+1:]
			return env
		}
	}
	return append(env, "PATH="+prefix)
}

func availableScripts(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// gjsonString reads a top-level string field from raw JSON bytes without
// requiring a struct. Returns "" on miss; used for nice "> name@version"
// banners before each script run.
func gjsonString(body []byte, key string) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}
