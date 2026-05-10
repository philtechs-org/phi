package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/philtechs-org/phi/internal/advisory"
	"github.com/philtechs-org/phi/internal/analyzer"
)

func PrintBanner() {
	bold := color.New(color.Bold, color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	fmt.Println(bold("Phi") + dim("  · install-time interception · supply chain firewall"))
	fmt.Println()
}

// Spinner is phi's indeterminate-progress indicator — used during
// phases where we don't know the total ahead of time (most importantly
// the resolver's BFS over npm packuments). Same cross-shell-uniform
// approach as Progress: ASCII frames + carriage return + fixed-width
// space-pad so it renders identically on cmd.exe, PowerShell, Windows
// Terminal, git-bash, macOS Terminal, iTerm, and Linux ttys. Writes to
// stderr so a piped stdout (e.g. JSON mode) stays clean.
type Spinner struct {
	msg     string
	out     io.Writer
	stop    chan struct{}
	done    chan struct{}
	width   int
	started bool
}

const spinnerWidth = 64

func NewSpinner(msg string) *Spinner {
	return &Spinner{
		msg:   msg,
		out:   os.Stderr,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		width: spinnerWidth,
	}
}

// Start begins the animation in a background goroutine. Safe to call
// once; subsequent calls are no-ops.
func (s *Spinner) Start() {
	if s == nil || s.started {
		return
	}
	s.started = true
	go func() {
		defer close(s.done)
		// ASCII-only frames so the spinner renders the same on every
		// terminal we support (Windows console fonts that lack braille
		// glyphs included).
		frames := []string{"|", "/", "-", "\\"}
		i := 0
		draw := func() {
			line := fmt.Sprintf("  %s %s", frames[i], s.msg)
			if len(line) >= s.width {
				line = line[:s.width-1]
			}
			fmt.Fprintf(s.out, "\r%-*s", s.width, line)
			i = (i + 1) % len(frames)
		}
		// Draw the first frame immediately so the user sees the message
		// without waiting a full tick — otherwise there's a perceptible
		// 120ms gap between the banner and any spinner output.
		draw()
		t := time.NewTicker(120 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				// Clear the line so the caller's follow-up print lands
				// at column 0 with no spinner residue.
				fmt.Fprintf(s.out, "\r%s\r", strings.Repeat(" ", s.width))
				return
			case <-t.C:
				draw()
			}
		}
	}()
}

// Stop ends the animation and clears the line. Caller is responsible
// for printing whatever comes next.
func (s *Spinner) Stop() {
	if s == nil || !s.started {
		return
	}
	close(s.stop)
	<-s.done
	s.started = false
}

// Done replaces the spinner with a single final line — useful for
// "resolving... 53 packages" style completions where the dynamic state
// resolves to a static result.
func (s *Spinner) Done(finalLine string) {
	s.Stop()
	if s == nil {
		return
	}
	fmt.Fprintln(s.out, finalLine)
}

// Progress is phi's in-place scan indicator. Replaces schollz/progressbar
// because that library wasn't emitting ANSI clear-line codes on every
// platform we ship to, leaving residual characters when the bar's
// rendered width changed between updates ("loader duplicates everywhere
// and not uniformly"). The simpler approach below uses a fixed-width
// formatted line + carriage return + space-pad to a known width, which
// renders uniformly on cmd.exe, PowerShell, Windows Terminal, git-bash,
// macOS Terminal, iTerm, and Linux ttys without depending on ANSI
// support.
type Progress struct {
	total    int
	count    int
	width    int
	throttle time.Duration
	lastDraw time.Time
	mu       sync.Mutex
	out      io.Writer
}

const progressBarWidth = 24 // visual bar segment

func NewProgress(total int) *Progress {
	return &Progress{
		total:    total,
		width:    72,
		throttle: 80 * time.Millisecond,
		out:      os.Stderr,
	}
}

// Tick increments the counter and redraws — but only if enough time has
// passed since the last draw, or if this is the final tick. Throttling
// keeps the output from flickering when many goroutines complete in
// quick succession.
func (p *Progress) Tick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p == nil || p.total <= 0 {
		return
	}
	p.count++
	now := time.Now()
	final := p.count >= p.total
	if !final && now.Sub(p.lastDraw) < p.throttle {
		return
	}
	p.lastDraw = now
	p.draw()
}

// draw renders the current progress to the output stream. Caller must
// hold p.mu.
func (p *Progress) draw() {
	pct := p.count * 100 / p.total
	if pct > 100 {
		pct = 100
	}
	filled := pct * progressBarWidth / 100
	bar := strings.Repeat("█", filled) + strings.Repeat(" ", progressBarWidth-filled)
	line := fmt.Sprintf("  scanning [%s] %3d%%  (%d/%d)", bar, pct, p.count, p.total)
	// Pad to the fixed width so any prior longer text is fully overwritten,
	// then carriage return so the next draw rewrites this one.
	if len(line) < p.width {
		line += strings.Repeat(" ", p.width-len(line))
	}
	fmt.Fprintf(p.out, "\r%s", line)
}

// Done finalizes the indicator: draws the 100% state and then clears the
// line entirely so subsequent prints land at column 0 with no leftover
// characters from the bar.
func (p *Progress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p == nil || p.total <= 0 {
		return
	}
	if p.count < p.total {
		p.count = p.total
	}
	p.draw()
	// Wipe the line: \r + spaces + \r. The trailing \r leaves the cursor
	// at column 0 with the line cleared.
	fmt.Fprintf(p.out, "\r%s\r", strings.Repeat(" ", p.width))
}

func PrintReportCard(r *analyzer.AnalysisReport, advisories []*advisory.Advisory) {
	verdictColor := color.New(color.Bold).SprintFunc()
	switch r.Verdict {
	case "safe":
		verdictColor = color.New(color.FgGreen, color.Bold).SprintFunc()
	case "review":
		verdictColor = color.New(color.FgYellow, color.Bold).SprintFunc()
	case "blocked":
		verdictColor = color.New(color.FgRed, color.Bold).SprintFunc()
	}
	fmt.Printf("\n%s@%s  files=%d  score=%d  verdict=%s\n",
		r.PackageName, r.PackageVersion, r.FilesScanned, r.RiskScore,
		verdictColor(strings.ToUpper(r.Verdict)))
	for _, d := range r.Detections {
		fmt.Printf("  - [%s] %s — %s\n",
			strings.ToUpper(string(d.Severity)), d.Detector, truncate(d.Evidence, 60))
	}
	for _, a := range advisories {
		sev := string(a.Severity)
		if sev == "" {
			sev = "UNKNOWN"
		}
		fmt.Printf("  - [%s] advisory %s — %s\n",
			sev, a.ID, truncate(a.Summary, 70))
	}
	cyan := color.New(color.FgCyan).SprintFunc()
	for _, n := range r.Notices {
		fmt.Printf("  %s %s — %s\n", cyan("note"), n.Kind, truncate(n.Message, 80))
	}
}

func PrintError(pkg string, err error) {
	red := color.New(color.FgRed).SprintFunc()
	fmt.Fprintf(os.Stderr, "%s %s: %v\n", red("error"), pkg, err)
}

func PrintWarning(msg string) {
	yellow := color.New(color.FgYellow).SprintFunc()
	fmt.Fprintf(os.Stderr, "%s %s\n", yellow("warning"), msg)
}

func PromptApproveTree(review []*analyzer.AnalysisReport) bool {
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	fmt.Printf("\n%s %d package(s) flagged for review:\n", yellow("REVIEW"), len(review))
	for _, r := range review {
		fmt.Printf("  - %s@%s  score=%d  detections=%d\n",
			r.PackageName, r.PackageVersion, r.RiskScore, len(r.Detections))
	}
	fmt.Print("Continue with install? [y/N] ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}

func PrintInstallSummary(scans map[string]*analyzer.AnalysisReport, lockPath, reportPath string) {
	c := countByVerdict(scans)
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Println()
	fmt.Printf("%s installed %d package(s) (safe=%d review=%d)\n",
		green("✔"), c.installed, c.safe, c.review)
	fmt.Printf("  lockfile: %s\n", lockPath)
	fmt.Printf("  report:   %s\n", reportPath)
}

func PrintAuditSummary(scans map[string]*analyzer.AnalysisReport, reportPath string) {
	c := countByVerdict(scans)
	fmt.Println()
	fmt.Printf("audit: %d scanned (safe=%d review=%d blocked=%d)\n",
		c.total, c.safe, c.review, c.blocked)
	fmt.Printf("  report: %s\n", reportPath)
}

func PrintHelp() {
	fmt.Println(`phi — secure package manager for Node.js

Commands:
  phi init [--yes]                Create package.json + .gitignore + README.md
  phi create <framework> <name>   Scaffold a new project (react, next, express, fastify, nest)
  phi install (i, a) [pkg...]     Scan and install packages (union of args and package.json)
  phi update (u) [pkg...]         Re-resolve and install fresh, ignoring phi.lock
  phi remove (rm) <pkg...>        Drop packages from package.json, phi.lock, and node_modules
  phi audit                       Scan all dependencies without installing
  phi audit fix [--apply|--force] Propose fixes for fixable issues; --apply rewrites package.json
  phi do (d) <script> [args...]   Run a script from package.json (with node_modules/.bin on PATH)
  phi exec (x) <pkg>[@<ver>] [args...]
                                  Run a binary; auto-fetch+scan like npx if not in node_modules
                                  Flags: -p <pkg>  --no-install  -y  --rescan  -f
  phi dev | build | start | ...   Direct shortcuts: same as "phi do <name>"
  phi outdated                    Show direct deps with newer versions available
  phi why <pkg>                   Show why a package is in the dependency tree
  phi cache stat                  Show on-disk tarball cache size
  phi cache clean [--older-than]  Prune cache entries (default: older than 30d)
  phi self-update [--check]       Update phi itself to the latest GitHub release
  phi version                     Show version
  phi help                        Show this help

Flags (install/update/audit):
  --allow-scripts a,b       Run lifecycle scripts only for the named packages
  --frozen-lockfile         Require phi.lock to exactly cover package.json (CI mode)
  --no-lockfile             Force fresh resolve, ignore phi.lock
  --json                    Suppress UI; emit phi-report.json to stdout
  --save-dev / -D           Write to devDependencies (move from dependencies if needed)
  --save-peer               Write to peerDependencies
  --save-exact / -E         Pin without caret prefix
  --no-advisories           Skip OSV vulnerability database query (offline mode)
  --force / -f              Override BLOCKED verdicts and install anyway (report still written)`)
}

type counts struct {
	total, safe, review, blocked, installed int
}

func countByVerdict(scans map[string]*analyzer.AnalysisReport) counts {
	var c counts
	for _, r := range scans {
		c.total++
		switch r.Verdict {
		case "safe":
			c.safe++
			c.installed++
		case "review":
			c.review++
			c.installed++
		case "blocked":
			c.blocked++
		}
	}
	return c
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
