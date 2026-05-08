package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/philtechs-org/phi/internal/advisory"
	"github.com/philtechs-org/phi/internal/analyzer"
	"github.com/schollz/progressbar/v3"
)

func PrintBanner() {
	bold := color.New(color.Bold, color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	fmt.Println(bold("Phi") + dim("  · secure package manager"))
	fmt.Println()
}

func NewProgressBar(n int) *progressbar.ProgressBar {
	return progressbar.NewOptions(n,
		progressbar.OptionSetDescription("scanning"),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
	)
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
  phi do (d) <script> [args...]   Run a script from package.json (with node_modules/.bin on PATH)
  phi exec (x) <bin> [args...]    Run a binary from node_modules/.bin
  phi dev | build | start | ...   Direct shortcuts: same as "phi do <name>"
  phi outdated                    Show direct deps with newer versions available
  phi why <pkg>                   Show why a package is in the dependency tree
  phi cache stat                  Show on-disk tarball cache size
  phi cache clean [--older-than]  Prune cache entries (default: older than 30d)
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
