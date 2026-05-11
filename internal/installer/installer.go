package installer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/philtechs-org/phi/internal/cache"
	"github.com/philtechs-org/phi/internal/registry"
	"github.com/philtechs-org/phi/internal/resolver"
	"github.com/philtechs-org/phi/internal/scorer"
	"github.com/philtechs-org/phi/internal/ui"
	"github.com/philtechs-org/phi/internal/workspace"
	"github.com/tidwall/gjson"
)

const (
	phiVersion  = "0.1.0"
	lockPath    = "phi.lock"
	reportPath  = "phi-report.json"
	scanWorkers = 8
)

// Mode controls how install resolves the dependency tree.
type Mode int

const (
	// ModeAuto (default): use phi.lock if it covers package.json, else
	// resolve fresh and overwrite the lockfile.
	ModeAuto Mode = iota
	// ModeFrozen: phi.lock must exist and cover package.json exactly. Any
	// drift fails the install. CI mode.
	ModeFrozen
	// ModeNoLock: ignore phi.lock entirely and always resolve fresh. Used
	// internally by `phi update` and selectable via --no-lockfile.
	ModeNoLock
)

// Options controls install-time behavior.
type Options struct {
	// AllowScripts is the list of package names whose lifecycle scripts
	// (preinstall/install/postinstall) may be executed after extraction.
	AllowScripts []string
	// Mode selects the lockfile policy.
	Mode Mode
	// JSON suppresses human-readable output and emits the scan report to
	// stdout at the end. Review-flagged packages cause an error in JSON
	// mode (no interactive prompt is possible).
	JSON bool
	// SaveDev / SavePeer override the package.json target field for the
	// args passed to install. SaveDev moves to devDependencies, SavePeer
	// to peerDependencies. Default (both false) keeps the package in
	// whichever field it already lives, falling back to dependencies.
	SaveDev  bool
	SavePeer bool
	// SaveExact pins the resolved version without the caret prefix —
	// useful for tools where exact reproduction matters.
	SaveExact bool
	// NoAdvisories skips the OSV advisory query entirely. Useful for
	// offline installs and CI environments without internet egress.
	NoAdvisories bool
	// AutoApproveReview skips the interactive REVIEW prompt and proceeds
	// as if the user had answered yes. BLOCKED packages still abort the
	// install. Used by `phi create` for ephemeral scaffolder installs
	// where the project's actual deps will be reviewed by a later
	// `phi install` in the user's new project directory. Not exposed as
	// a CLI flag for the install command — bypassing review by default
	// would defeat phi's purpose.
	AutoApproveReview bool
	// Force overrides the BLOCKED verdict and proceeds with installation
	// anyway. Implies AutoApproveReview. The scan still runs and the
	// report is still written — phi's audit trail is preserved — but the
	// user has chosen to install regardless. Loud warning printed when
	// any blocked package is force-installed. Exposed as --force for the
	// install/update commands; meant for cases where a user trusts a
	// package that phi has flagged (false positive in a detector, or
	// known-but-acceptable risk).
	Force bool
	// Quiet suppresses the banner, progress bar, and per-package report
	// cards. Errors and warnings still print. Used internally by phi
	// create so the scaffolder's own UI dominates the user's terminal.
	Quiet bool
	// OmitDev skips devDependencies during resolution. Matches npm's
	// --omit=dev semantics. Useful for production installs where test
	// runners, linters, type defs, and build tooling aren't needed in
	// the deployed bundle. Only affects the root package.json today —
	// workspace-level filtering is a planned follow-up.
	OmitDev bool
}

func Install(args []string) error {
	return InstallWith(args, Options{})
}

func InstallWith(args []string, opts Options) error {
	return install(registry.New(), args, opts)
}

func Audit() error {
	return AuditWith(Options{})
}

func AuditWith(opts Options) error {
	return audit(registry.New(), opts)
}

func Update(args []string) error {
	return UpdateWith(args, Options{})
}

// UpdateWith forces a fresh resolve regardless of phi.lock and overwrites the
// lockfile with the result. The args are merged into package.json's deps as
// extra targets.
func UpdateWith(args []string, opts Options) error {
	opts.Mode = ModeNoLock
	return install(registry.New(), args, opts)
}

func install(client *registry.Client, args []string, opts Options) error {
	targets, err := resolveTargets(args, opts)
	if err != nil {
		return err
	}
	direct := targetsToMap(targets)

	workspaces, _ := workspace.Discover(".")
	if len(workspaces) > 0 {
		direct, _ = workspace.Aggregate(direct, workspaces)
	}

	if len(direct) == 0 {
		return errors.New("no packages to install (and no package.json found)")
	}

	if !opts.JSON && !opts.Quiet {
		ui.PrintBanner()
		if len(workspaces) > 0 {
			fmt.Printf("monorepo: %d workspace(s) detected\n", len(workspaces))
		}
	}

	// Animated spinner during the resolver's BFS over npm packuments.
	// Without this the user sees the banner then silence until the
	// resolve either completes or surfaces a fetch timeout — and on a
	// flaky network the silence can run tens of seconds.
	var spinner *ui.Spinner
	if !opts.JSON && !opts.Quiet {
		spinner = ui.NewSpinner("resolving dependency tree...")
		spinner.Start()
	}

	tree, fromLock, err := loadTree(client, direct, opts)
	if err != nil {
		spinner.Stop()
		return err
	}
	if !opts.JSON && !opts.Quiet {
		if fromLock {
			spinner.Done(fmt.Sprintf("resolved (used phi.lock — %d packages)", len(tree.All)))
		} else {
			spinner.Done(fmt.Sprintf("resolved %d packages", len(tree.All)))
		}
	}
	if !opts.JSON && !opts.Quiet {
		for _, w := range tree.Warnings {
			ui.PrintWarning(w)
		}
	}
	if len(tree.All) == 0 {
		return errors.New("nothing to install (empty resolution tree)")
	}
	tree.Roots = rootsFromDirect(tree, direct)

	if !opts.JSON && !opts.Quiet {
		fmt.Printf("scanning %d packages...\n", len(tree.All))
	}
	scans, bufs, err := scanWithProgress(client, tree, opts.JSON || opts.Quiet)
	if err != nil {
		return err
	}

	advs := queryAdvisories(tree, opts)
	mergeAdvisories(scans, advs)

	if !opts.JSON && !opts.Quiet {
		for path, r := range scans {
			if r.Verdict != scorer.VerdictSafe || len(advs[path]) > 0 || len(r.Notices) > 0 {
				ui.PrintReportCard(r, advs[path])
			}
		}
	}

	blocked, review := splitVerdicts(scans)

	if len(blocked) > 0 {
		if !opts.Force {
			_ = WriteReport(reportPath, scans, advs)
			if opts.JSON {
				emitJSONReport()
			}
			return fmt.Errorf("install aborted: %d package(s) blocked; report written to %s (pass --force to override)",
				len(blocked), reportPath)
		}
		if !opts.Quiet {
			ui.PrintWarning(fmt.Sprintf(
				"--force: proceeding with %d BLOCKED package(s) — report still written to %s",
				len(blocked), reportPath))
			for _, r := range blocked {
				fmt.Printf("  forcing: %s@%s  score=%d  detections=%d\n",
					r.PackageName, r.PackageVersion, r.RiskScore, len(r.Detections))
			}
		}
	}
	if len(review) > 0 {
		switch {
		case opts.AutoApproveReview, opts.Force:
			if !opts.Quiet {
				if opts.Force {
					fmt.Printf("--force: auto-approving %d review-flagged package(s)\n", len(review))
				} else {
					fmt.Printf("auto-approving %d review-flagged package(s)\n", len(review))
				}
			}
		case opts.JSON:
			_ = WriteReport(reportPath, scans, advs)
			emitJSONReport()
			return fmt.Errorf("install aborted: %d package(s) flagged for review (non-interactive mode)", len(review))
		default:
			if !ui.PromptApproveTree(review) {
				_ = WriteReport(reportPath, scans, advs)
				return fmt.Errorf("install aborted by user; report written to %s", reportPath)
			}
		}
	}

	if !opts.JSON && !opts.Quiet {
		fmt.Println("\nextracting approved packages...")
	}
	// extracted: package name → install dir, used by bin shims and lifecycle
	// scripts. Only the hoisted (root-level) installs get bin shims;
	// nested duplicates would create conflicting shims at node_modules/.bin/.
	extracted := make(map[string]string, len(bufs))
	for installPath, data := range bufs {
		dest := filepath.FromSlash(installPath)
		if err := Extract(data, dest); err != nil {
			return fmt.Errorf("extract %s: %w", installPath, err)
		}
		// Only hoisted installs (no embedded /node_modules/ in the path)
		// participate in bin shims and lifecycle scripts.
		if !strings.Contains(installPath[len("node_modules/"):], "/node_modules/") {
			pkg := tree.All[installPath]
			extracted[pkg.Name] = dest
		}
	}

	if err := CreateBinShims("node_modules", extracted); err != nil && !opts.JSON && !opts.Quiet {
		ui.PrintWarning(fmt.Sprintf("bin shim creation failed: %v", err))
	}

	if len(workspaces) > 0 {
		if err := linkWorkspaces(workspaces); err != nil && !opts.JSON && !opts.Quiet {
			ui.PrintWarning(fmt.Sprintf("workspace links: %v", err))
		}
	}

	if len(opts.AllowScripts) > 0 {
		if err := RunLifecycleScripts(extracted, opts.AllowScripts); err != nil {
			return fmt.Errorf("lifecycle scripts: %w", err)
		}
	}

	if err := WriteLockfile(lockPath, tree, scans, "phi "+phiVersion); err != nil {
		return fmt.Errorf("write %s: %w", lockPath, err)
	}
	if err := WriteReport(reportPath, scans, advs); err != nil {
		return fmt.Errorf("write %s: %w", reportPath, err)
	}

	if !opts.Quiet {
		if err := persistArgsToPackageJSON(args, tree, opts); err != nil && !opts.JSON {
			ui.PrintWarning(fmt.Sprintf("update package.json: %v", err))
		}
	}

	switch {
	case opts.JSON:
		emitJSONReport()
	case opts.Quiet:
		// Caller (phi create) handles its own summary.
	default:
		ui.PrintInstallSummary(scans, lockPath, reportPath)
	}
	return nil
}

// persistArgsToPackageJSON writes each install-arg name into package.json's
// dependencies, devDependencies (--save-dev), or peerDependencies
// (--save-peer). Args without a corresponding tree entry (e.g. resolution
// failed) are skipped. Errors on individual entries don't abort the install.
func persistArgsToPackageJSON(args []string, tree *resolver.Tree, opts Options) error {
	target := ""
	switch {
	case opts.SaveDev:
		target = "devDependencies"
	case opts.SavePeer:
		target = "peerDependencies"
	}
	for _, a := range args {
		name, userSpec := splitNameVersion(a)
		pkg := tree.Hoisted(name)
		if pkg == nil {
			continue
		}
		spec := packageJSONSpec(userSpec, pkg.Version, opts.SaveExact)
		if err := upsertDependency(name, spec, target); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func audit(client *registry.Client, opts Options) error {
	targets, err := resolveTargets(nil, opts)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("no package.json found in current directory")
	}
	direct := targetsToMap(targets)

	if !opts.JSON {
		ui.PrintBanner()
	}

	var spinner *ui.Spinner
	if !opts.JSON {
		spinner = ui.NewSpinner("resolving dependency tree...")
		spinner.Start()
	}

	tree, _, err := loadTree(client, direct, Options{Mode: ModeAuto})
	if err != nil {
		spinner.Stop()
		return err
	}
	if !opts.JSON {
		spinner.Done(fmt.Sprintf("resolved %d packages", len(tree.All)))
		for _, w := range tree.Warnings {
			ui.PrintWarning(w)
		}
	}
	if len(tree.All) == 0 {
		return errors.New("empty resolution tree")
	}

	if !opts.JSON {
		fmt.Printf("scanning %d packages...\n", len(tree.All))
	}
	scans, _, scanErr := scanWithProgress(client, tree, opts.JSON)

	advs := queryAdvisories(tree, opts)
	mergeAdvisories(scans, advs)

	if !opts.JSON {
		for path, r := range scans {
			if r.Verdict != scorer.VerdictSafe || len(advs[path]) > 0 || len(r.Notices) > 0 {
				ui.PrintReportCard(r, advs[path])
			}
		}
	}
	if err := WriteReport(reportPath, scans, advs); err != nil {
		return fmt.Errorf("write %s: %w", reportPath, err)
	}
	if opts.JSON {
		emitJSONReport()
	} else {
		ui.PrintAuditSummary(scans, reportPath)
	}
	return scanErr
}

// loadTree returns either the cached lockfile tree (if it covers direct and
// the mode allows) or a fresh resolution. fromLock is true in the first case.
func loadTree(client *registry.Client, direct map[string]string, opts Options) (*resolver.Tree, bool, error) {
	if opts.Mode != ModeNoLock {
		locked, err := ReadLockfile(lockPath)
		switch {
		case err == nil:
			if LockfileCovers(locked, direct) {
				return locked, true, nil
			}
			if opts.Mode == ModeFrozen {
				return nil, false, errors.New("--frozen-lockfile: phi.lock is out of sync with package.json")
			}
		case errors.Is(err, ErrNoLockfile):
			if opts.Mode == ModeFrozen {
				return nil, false, errors.New("--frozen-lockfile: phi.lock not found")
			}
		default:
			return nil, false, err
		}
	}
	tree, err := resolver.Resolve(client, direct)
	if err != nil {
		return nil, false, err
	}
	return tree, false, nil
}

func rootsFromDirect(tree *resolver.Tree, direct map[string]string) []*resolver.Pkg {
	var roots []*resolver.Pkg
	for name := range direct {
		if pkg := tree.Hoisted(name); pkg != nil {
			roots = append(roots, pkg)
		}
	}
	return roots
}

func emitJSONReport() {
	body, err := os.ReadFile(reportPath)
	if err != nil {
		return
	}
	os.Stdout.Write(body)
}

func scanWithProgress(client *registry.Client, tree *resolver.Tree, jsonMode bool) (map[string]*analyzer.AnalysisReport, map[string][]byte, error) {
	tick := func() {}
	if !jsonMode {
		p := ui.NewProgress(len(tree.All))
		tick = p.Tick
		defer p.Done()
	}
	return scanTree(client, tree, tick)
}

type target struct {
	name        string
	versionSpec string
}

// resolveTargets returns the union of package.json's deps and any explicit
// args. Args take precedence on name conflicts. If no package.json exists,
// args alone are used.
func resolveTargets(args []string, opts Options) ([]target, error) {
	body, err := os.ReadFile("package.json")
	pjMissing := os.IsNotExist(err)
	var pjTargets []target
	if err == nil {
		body = bytes.TrimPrefix(body, utf8BOM)
		keys := []string{"dependencies", "devDependencies"}
		if opts.OmitDev {
			keys = []string{"dependencies"}
		}
		for _, key := range keys {
			gjson.GetBytes(body, key).ForEach(func(k, v gjson.Result) bool {
				pjTargets = append(pjTargets, target{name: k.String(), versionSpec: v.String()})
				return true
			})
		}
	} else if !pjMissing {
		return nil, fmt.Errorf("read package.json: %w", err)
	}

	if len(args) == 0 {
		return pjTargets, nil
	}
	byName := make(map[string]target, len(pjTargets)+len(args))
	for _, t := range pjTargets {
		byName[t.name] = t
	}
	for _, a := range args {
		name, ver := splitNameVersion(a)
		byName[name] = target{name: name, versionSpec: ver}
	}
	out := make([]target, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	return out, nil
}

// splitNameVersion handles "name@spec" and "@scope/name@spec".
func splitNameVersion(s string) (string, string) {
	for i := 1; i < len(s); i++ {
		if s[i] == '@' {
			return s[:i], s[i+1:]
		}
	}
	return s, "latest"
}

func targetsToMap(ts []target) map[string]string {
	m := make(map[string]string, len(ts))
	for _, t := range ts {
		m[t.name] = t.versionSpec
	}
	return m
}

func splitVerdicts(scans map[string]*analyzer.AnalysisReport) (blocked, review []*analyzer.AnalysisReport) {
	for _, r := range scans {
		switch r.Verdict {
		case scorer.VerdictBlocked:
			blocked = append(blocked, r)
		case scorer.VerdictReview:
			review = append(review, r)
		}
	}
	return
}

// scanTree scans every install in the tree (each unique InstallPath), even
// when the same name is installed at multiple paths (hoisted vs nested).
// Returned maps are keyed by InstallPath so extraction can use the path
// directly without an extra lookup.
func scanTree(
	client *registry.Client,
	tree *resolver.Tree,
	onTick func(),
) (map[string]*analyzer.AnalysisReport, map[string][]byte, error) {
	type item struct {
		path string
		pkg  *resolver.Pkg
	}
	items := make([]item, 0, len(tree.All))
	for installPath, p := range tree.All {
		items = append(items, item{installPath, p})
	}

	sem := make(chan struct{}, scanWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex

	scans := make(map[string]*analyzer.AnalysisReport, len(items))
	bufs := make(map[string][]byte, len(items))
	var firstErr error
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	for _, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(it item) {
			defer wg.Done()
			defer func() { <-sem }()
			defer onTick()
			// Convert any panic into a clean error. Without this, a
			// panic deep in a goroutine (corrupt tarball blowing up
			// gzip/tar, a goja crash on edge-case JS, an OOB in any
			// detector) tears down the whole phi process — taking the
			// other 80 packages currently being scanned with it. Now
			// the panic is local to that one package: the user gets a
			// readable error and the rest of the tree finishes.
			defer func() {
				if r := recover(); r != nil {
					setErr(fmt.Errorf("panic scanning %s@%s: %v", it.pkg.Name, it.pkg.Version, r))
				}
			}()

			data, hit, _ := cache.Load(it.pkg.Integrity)
			if !hit {
				fetched, err := client.FetchTarball(it.pkg.Resolved)
				if err != nil {
					setErr(fmt.Errorf("fetch %s@%s: %w", it.pkg.Name, it.pkg.Version, err))
					return
				}
				data = fetched
			}
			if err := registry.VerifyIntegrity(data, it.pkg.Integrity); err != nil {
				setErr(fmt.Errorf("integrity %s@%s: %w", it.pkg.Name, it.pkg.Version, err))
				return
			}
			if !hit {
				_ = cache.Store(it.pkg.Integrity, data)
			}
			report, err := analyzer.Analyze(it.pkg.Name, it.pkg.Version, bytes.NewReader(data))
			if err != nil {
				setErr(fmt.Errorf("analyze %s@%s: %w", it.pkg.Name, it.pkg.Version, err))
				return
			}
			report.RiskScore = scorer.Score(report.Detections)
			report.Verdict = scorer.Verdict(report.RiskScore)

			mu.Lock()
			scans[it.path] = report
			bufs[it.path] = data
			mu.Unlock()
		}(it)
	}
	wg.Wait()
	return scans, bufs, firstErr
}
