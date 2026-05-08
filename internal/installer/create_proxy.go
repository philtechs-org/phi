package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// createProxy installs the scaffolder package into a temp directory using
// phi's normal install pipeline (scan + extract, no lifecycle scripts),
// then executes the scaffolder's binary in the user's current directory.
// The temp directory is removed on exit regardless of success.
func createProxy(spec FrameworkSpec, projectName string, extraArgs []string) error {
	originalCwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "phi-create-"+spec.Name+"-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	fmt.Printf("phi create: scaffolding %s -> %s\n", spec.Name, projectName)
	fmt.Printf("auditing scaffolder package: %s\n\n", spec.Package)

	if err := installScaffolder(tmpDir, spec.Package); err != nil {
		return fmt.Errorf("audit/install %s: %w", spec.Package, err)
	}

	binPath, err := resolveBinary(tmpDir, spec.Binary)
	if err != nil {
		return err
	}

	args := buildScaffolderArgs(spec, projectName, extraArgs)

	fmt.Printf("\nrunning: %s %s\n\n", spec.Binary, strings.Join(args, " "))
	if err := runScaffolderBinary(originalCwd, tmpDir, binPath, args); err != nil {
		return fmt.Errorf("scaffolder exited with error: %w", err)
	}

	fmt.Printf("\nok: created %s/ via %s\n", projectName, spec.Name)
	fmt.Println("  next steps:")
	fmt.Printf("    cd %s\n", projectName)
	fmt.Println("    phi install         # install deps with phi's safe pipeline")
	return nil
}

// installScaffolder runs phi's install pipeline inside tmpDir to fetch and
// extract pkg + its transitive deps. We change cwd briefly because the
// installer reads/writes files relative to cwd; cwd is restored by the
// caller via originalCwd.
func installScaffolder(tmpDir, pkg string) error {
	prev, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(tmpDir); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(prev) }()

	// Bootstrap a minimal package.json so resolveTargets has something to
	// merge our arg into. (Install would otherwise error out with
	// "no packages to install" if the user passed only a single target via
	// args; safer to make it explicit.)
	pj := []byte("{\n  \"name\": \"phi-scaffold\",\n  \"version\": \"0.0.0\",\n  \"private\": true\n}\n")
	if err := os.WriteFile("package.json", pj, 0o644); err != nil {
		return fmt.Errorf("seed package.json: %w", err)
	}

	return InstallWith([]string{pkg}, Options{
		// Scaffolder installs are ephemeral — the temp dir is wiped after
		// the scaffolder runs once. Skipping the REVIEW prompt avoids
		// quizzing the user about legitimate dynamic-code patterns in
		// scaffolder transitive deps (ajv, pino, etc). BLOCKED packages
		// still abort; the user's actual project deps will be reviewed
		// normally when they run `phi install` in the new project.
		AutoApproveReview: true,
		Quiet:             true,
	})
}

// buildScaffolderArgs assembles the final argv passed to the scaffolder
// binary. Argv layout:
//
//	<spec.Subcommand...> <project-name> <FlagDefaults minus user-overridden> <user-args>
//
// Subcommand tokens go BEFORE the project name to match scaffolders like
// `fastify generate <name>` and `nest new <name>`. FlagDefaults go AFTER
// because they're flag/value pairs (e.g. `--template react-ts`); a user
// flag with the same name suppresses our default.
func buildScaffolderArgs(spec FrameworkSpec, projectName string, extraArgs []string) []string {
	overridden := userOverriddenFlags(extraArgs)
	defaults := filterArgs(spec.FlagDefaults, overridden)

	out := make([]string, 0, len(spec.Subcommand)+1+len(defaults)+len(extraArgs))
	out = append(out, spec.Subcommand...)
	out = append(out, projectName)
	out = append(out, defaults...)
	out = append(out, extraArgs...)
	return out
}

// userOverriddenFlags returns the set of long-flag names the user has
// supplied. Used to suppress our DefaultArgs entries that would conflict.
func userOverriddenFlags(args []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			name := strings.SplitN(strings.TrimPrefix(a, "--"), "=", 2)[0]
			out[name] = true
		}
	}
	return out
}

// filterArgs removes `--flag value` pairs from defaults whose flag name is
// in the override set. Bare positional defaults pass through unchanged.
func filterArgs(defaults []string, override map[string]bool) []string {
	if len(override) == 0 {
		return defaults
	}
	out := make([]string, 0, len(defaults))
	for i := 0; i < len(defaults); i++ {
		a := defaults[i]
		if strings.HasPrefix(a, "--") {
			name := strings.SplitN(strings.TrimPrefix(a, "--"), "=", 2)[0]
			if override[name] {
				// Skip the flag and, if separated, its value.
				if !strings.Contains(a, "=") && i+1 < len(defaults) && !strings.HasPrefix(defaults[i+1], "-") {
					i++
				}
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// resolveBinary locates the scaffolder's executable inside the temp
// install's node_modules/.bin. On Windows the shim is .cmd; on POSIX it's
// a shell script with the bare name.
func resolveBinary(tmpDir, binary string) (string, error) {
	binDir := filepath.Join(tmpDir, "node_modules", ".bin")
	candidates := []string{filepath.Join(binDir, binary)}
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(binDir, binary+".cmd"),
			filepath.Join(binDir, binary+".exe"),
			filepath.Join(binDir, binary+".bat"),
			filepath.Join(binDir, binary),
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("binary %q not found in %s after install", binary, binDir)
}

// runScaffolderBinary executes the scaffolder with cwd set to the user's
// original directory so the new project lands beside their other work.
// PATH is augmented so any sub-binaries the scaffolder itself spawns
// resolve from the temp install's .bin first.
func runScaffolderBinary(cwd, tmpDir, binPath string, args []string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// On Windows .cmd shims must be invoked through cmd /c so the
		// shell processes the batch wrapper. Direct exec on a .cmd file
		// fails with "is not a valid Win32 application" via syscall.
		shellArgs := append([]string{"/c", binPath}, args...)
		cmd = exec.Command("cmd", shellArgs...)
	} else {
		cmd = exec.Command(binPath, args...)
	}
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	binDir, err := filepath.Abs(filepath.Join(tmpDir, "node_modules", ".bin"))
	if err == nil {
		cmd.Env = augmentPath(os.Environ(), binDir)
	} else {
		cmd.Env = os.Environ()
	}
	return cmd.Run()
}
