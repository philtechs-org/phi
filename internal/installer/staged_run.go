package installer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/philtechs-org/phi/internal/cache"
	"github.com/philtechs-org/phi/internal/registry"
	"github.com/philtechs-org/phi/internal/resolver"
	"github.com/philtechs-org/phi/internal/scorer"
	"github.com/philtechs-org/phi/internal/ui"
)

// ExecOptions controls phi x's behavior — how it picks the package, whether
// it's allowed to fetch, and how it handles scan verdicts. Set by the CLI
// flag parser; the installer doesn't construct these directly.
type ExecOptions struct {
	// Package overrides the package name when it differs from the bin name.
	// Mirrors `npx -p typescript tsc`. May include @version.
	Package string
	// NoInstall: fail if the bin isn't already in node_modules/.bin. Strict
	// local mode for callers (CI, scripts) that don't want a silent network
	// fetch.
	NoInstall bool
	// Yes auto-approves any review-verdict packages encountered during the
	// scan of a freshly-fetched stage. Blocked verdicts still abort unless
	// Force is also set. Mirrors `npx -y`.
	Yes bool
	// Rescan invalidates the cached scan marker for the resolved version,
	// forcing a fresh fetch + scan even if a prior stage exists.
	Rescan bool
	// Force overrides blocked verdicts and proceeds anyway. Implies Yes.
	// Loud warning when used.
	Force bool
}

// Exec runs a binary, fetching and scanning the providing package if it
// isn't already in node_modules/.bin (npx parity). The dispatch logic:
//
//  1. Resolve (pkgName, versionSpec, binName) from spec + opts.Package.
//  2. If versionSpec is "latest" (unpinned) and the bin is in local
//     node_modules/.bin, run it from there — fastest path, no network.
//  3. Otherwise stage the package under the user's cache dir, scan it, and
//     run the bin from the staged install.
//
// If --no-install is set we never fall through to step 3 — strict-local.
func Exec(spec string, args []string, opts ExecOptions) error {
	if spec == "" {
		return errors.New("exec: package or binary name required")
	}
	pkgName, versionSpec, binName := parseRunSpec(spec, opts.Package)

	versionPinned := versionSpec != "" && versionSpec != "latest"
	if !versionPinned && binaryExists(filepath.Join("node_modules", ".bin", binName)) {
		return runLocalBin(binName, args)
	}
	if opts.NoInstall {
		return fmt.Errorf("%q not found in node_modules/.bin and --no-install was set", binName)
	}
	return runStaged(pkgName, versionSpec, binName, args, opts)
}

// parseRunSpec turns the first positional + the -p override into the three
// values the staged-run flow needs.
//
//	"cowsay"           → ("cowsay",     "latest", "cowsay")
//	"cowsay@1.5.0"     → ("cowsay",     "1.5.0",  "cowsay")
//	"@scope/pkg"       → ("@scope/pkg", "latest", "pkg")
//	"@scope/pkg@2.0.0" → ("@scope/pkg", "2.0.0",  "pkg")
//
// When pkgOverride is non-empty (from -p / --package), the spec is treated
// as the bin name and the package is taken from the override (which may
// itself include @version):
//
//	spec="tsc", override="typescript"        → ("typescript", "latest", "tsc")
//	spec="tsc", override="typescript@5.0.0"  → ("typescript", "5.0.0",  "tsc")
func parseRunSpec(spec, pkgOverride string) (pkgName, version, binName string) {
	if pkgOverride != "" {
		pkgName, version = splitNameVersion(pkgOverride)
		binName = spec
		return
	}
	pkgName, version = splitNameVersion(spec)
	binName = pkgName
	if i := strings.LastIndex(binName, "/"); i >= 0 {
		binName = binName[i+1:]
	}
	return
}

func runLocalBin(binName string, args []string) error {
	full := binName
	if len(args) > 0 {
		full = binName + " " + strings.Join(args, " ")
	}
	return runUserScript(".", full)
}

// runStaged is the npx-equivalent path: resolve, scan, extract into a cache
// dir, and run. A successful first run leaves a `.phi-scan-passed` marker so
// repeat invocations skip straight to step 4.
func runStaged(pkgName, versionSpec, binName string, args []string, opts ExecOptions) error {
	client := registry.New()
	direct := map[string]string{pkgName: versionSpec}

	ui.PrintBanner()

	spinner := ui.NewSpinner("resolving dependency tree...")
	spinner.Start()
	tree, _, err := loadTree(client, direct, Options{Mode: ModeNoLock})
	if err != nil {
		spinner.Stop()
		return fmt.Errorf("resolve %s@%s: %w", pkgName, versionSpec, err)
	}
	hoisted := tree.Hoisted(pkgName)
	if hoisted == nil {
		spinner.Stop()
		return fmt.Errorf("resolver returned no hoisted entry for %q", pkgName)
	}
	spinner.Done(fmt.Sprintf("resolved %s@%s (%d packages)", pkgName, hoisted.Version, len(tree.All)))

	stageDir, err := cache.RunDir(pkgName, hoisted.Version)
	if err != nil {
		return fmt.Errorf("cache dir: %w", err)
	}
	marker := filepath.Join(stageDir, ".phi-scan-passed")
	if !opts.Rescan {
		if _, err := os.Stat(marker); err == nil {
			return execStagedBin(stageDir, binName, args)
		}
	}

	fmt.Printf("scanning %d packages...\n", len(tree.All))
	scans, bufs, scanErr := scanWithProgress(client, tree, false)
	if scanErr != nil {
		return scanErr
	}

	advs := queryAdvisories(tree, Options{})
	mergeAdvisories(scans, advs)

	for path, r := range scans {
		if r.Verdict != scorer.VerdictSafe || len(advs[path]) > 0 || len(r.Notices) > 0 {
			ui.PrintReportCard(r, advs[path])
		}
	}

	blocked, review := splitVerdicts(scans)
	if len(blocked) > 0 {
		if !opts.Force {
			return fmt.Errorf("phi x aborted: %d package(s) blocked (pass --force to override)", len(blocked))
		}
		ui.PrintWarning(fmt.Sprintf("--force: proceeding with %d BLOCKED package(s)", len(blocked)))
	}
	if len(review) > 0 {
		switch {
		case opts.Yes, opts.Force:
			fmt.Printf("auto-approving %d review-flagged package(s)\n", len(review))
		default:
			if !ui.PromptApproveTree(review) {
				return errors.New("phi x aborted by user")
			}
		}
	}

	if err := stageInstall(stageDir, tree, bufs); err != nil {
		return err
	}
	return execStagedBin(stageDir, binName, args)
}

// stageInstall extracts the resolved tree into stageDir.tmp, writes the
// scan-passed marker, then atomically renames into place. On failure the tmp
// dir is cleaned up so the next run starts from scratch instead of resuming
// a half-broken stage.
func stageInstall(stageDir string, tree *resolver.Tree, bufs map[string][]byte) (err error) {
	if rmErr := os.RemoveAll(stageDir); rmErr != nil {
		return fmt.Errorf("clean stale stage: %w", rmErr)
	}
	tmp := stageDir + ".tmp"
	if rmErr := os.RemoveAll(tmp); rmErr != nil {
		return fmt.Errorf("clean stale tmp: %w", rmErr)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()

	extracted := make(map[string]string, len(bufs))
	for installPath, data := range bufs {
		dest := filepath.Join(tmp, filepath.FromSlash(installPath))
		if err := Extract(data, dest); err != nil {
			return fmt.Errorf("extract %s: %w", installPath, err)
		}
		// Only hoisted (root-level) installs get bin shims, matching the
		// install path's convention.
		if !strings.Contains(installPath[len("node_modules/"):], "/node_modules/") {
			pkg := tree.All[installPath]
			extracted[pkg.Name] = dest
		}
	}
	if err := CreateBinShims(filepath.Join(tmp, "node_modules"), extracted); err != nil {
		return fmt.Errorf("bin shims: %w", err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".phi-scan-passed"), nil, 0o644); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(stageDir), 0o755); err != nil {
		return fmt.Errorf("mkdir stage parent: %w", err)
	}
	if err := os.Rename(tmp, stageDir); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmp, stageDir, err)
	}
	return nil
}

// execStagedBin runs the named bin from a previously-staged install.
// PATH is augmented with the staged .bin dir so deps the bin invokes
// (typescript's tsc calling tsserver, prettier calling its plugins, …) are
// resolvable. Cwd stays as the user's pwd — this matches npx, where the bin
// sees the project it's being run against, not the cache.
func execStagedBin(stageDir, binName string, args []string) error {
	binDir := filepath.Join(stageDir, "node_modules", ".bin")
	if !binaryExists(filepath.Join(binDir, binName)) {
		return fmt.Errorf("bin %q not found in staged install at %s", binName, binDir)
	}
	full := binName
	if len(args) > 0 {
		full = binName + " " + strings.Join(args, " ")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", full)
	} else {
		cmd = exec.Command("sh", "-c", full)
	}
	cmd.Dir = "."
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if abs, err := filepath.Abs(binDir); err == nil {
		cmd.Env = augmentPath(os.Environ(), abs)
	}
	return cmd.Run()
}
