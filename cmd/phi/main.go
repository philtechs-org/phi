package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/philtechs-org/phi/internal/installer"
	"github.com/philtechs-org/phi/internal/ui"
)

// version is set by `go build`'s default ("0.1.0-dev") or overridden by
// goreleaser via -ldflags -X main.version=<tag> on tagged releases.
var version = "0.3.0"

func main() {
	// Sweep up the .old binary left by a previous Windows self-update.
	// No-op on Unix and on first runs.
	installer.CleanupSelfUpdateLeftovers()

	if len(os.Args) < 2 {
		ui.PrintHelp()
		return
	}
	cmd, args := os.Args[1], os.Args[2:]

	switch cmd {
	case "install", "add", "i", "a":
		opts, rest := parseFlags(args)
		exitOnErr(installer.InstallWith(rest, opts))
	case "update", "u":
		opts, rest := parseFlags(args)
		exitOnErr(installer.UpdateWith(rest, opts))
	case "remove", "rm", "uninstall":
		_, rest := parseFlags(args)
		exitOnErr(installer.Remove(rest))
	case "audit":
		if len(args) > 0 && args[0] == "fix" {
			exitOnErr(installer.AuditFix(parseAuditFixFlags(args[1:])))
			return
		}
		opts, _ := parseFlags(args)
		exitOnErr(installer.AuditWith(opts))
	case "init":
		exitOnErr(installer.Init(parseInitFlags(args)))
	case "create":
		framework, name, extra := parseCreateArgs(args)
		exitOnErr(installer.Create(framework, name, extra))
	case "do", "d":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "phi: do requires a script name")
			os.Exit(2)
		}
		exitOnErr(installer.Do(args[0], args[1:]))
	case "exec", "x":
		execOpts, rest := parseExecFlags(args)
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "phi: exec requires a package or binary name")
			os.Exit(2)
		}
		exitOnErr(installer.Exec(rest[0], rest[1:], execOpts))
	case "dev", "build", "start", "test", "lint", "preview", "prod":
		exitOnErr(installer.Do(cmd, args))
	case "why":
		_, rest := parseFlags(args)
		exitOnErr(installer.Why(rest))
	case "outdated":
		exitOnErr(installer.Outdated())
	case "cache":
		exitOnErr(handleCache(args))
	case "self-update", "selfupdate":
		exitOnErr(installer.SelfUpdate(version, parseSelfUpdateFlags(args)))
	case "version", "-v", "--version":
		fmt.Println("phi", version)
	case "help", "-h", "--help":
		ui.PrintHelp()
	default:
		fmt.Fprintf(os.Stderr, "phi: unknown command %q\n", cmd)
		ui.PrintHelp()
		os.Exit(2)
	}
}

// parseFlags pulls all phi options out of args, returning the rest as
// positional package targets. Unrecognized flags pass through as targets
// (which will fail in the resolver if they aren't valid package names).
func parseFlags(args []string) (installer.Options, []string) {
	var opts installer.Options
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--allow-scripts":
			if i+1 < len(args) {
				opts.AllowScripts = append(opts.AllowScripts, splitCSV(args[i+1])...)
				i++
			}
		case strings.HasPrefix(a, "--allow-scripts="):
			opts.AllowScripts = append(opts.AllowScripts,
				splitCSV(strings.TrimPrefix(a, "--allow-scripts="))...)
		case a == "--json":
			opts.JSON = true
		case a == "--frozen-lockfile":
			opts.Mode = installer.ModeFrozen
		case a == "--no-lockfile":
			opts.Mode = installer.ModeNoLock
		case a == "--save-dev" || a == "-D":
			opts.SaveDev = true
		case a == "--save-peer":
			opts.SavePeer = true
		case a == "--save-exact" || a == "-E":
			opts.SaveExact = true
		case a == "--no-advisories":
			opts.NoAdvisories = true
		case a == "--force" || a == "-f":
			opts.Force = true
		default:
			rest = append(rest, a)
		}
	}
	return opts, rest
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseCreateArgs splits `phi create <framework> <name> [-- pass-through...]`
// into its three pieces. Anything after a literal `--` is forwarded to the
// scaffolder verbatim. If `--` is omitted, all trailing args are forwarded.
func parseCreateArgs(args []string) (framework, name string, extra []string) {
	if len(args) >= 1 {
		framework = args[0]
	}
	if len(args) >= 2 {
		name = args[1]
	}
	if len(args) > 2 {
		rest := args[2:]
		for i, a := range rest {
			if a == "--" {
				extra = append(extra, rest[i+1:]...)
				return
			}
		}
		extra = append(extra, rest...)
	}
	return
}

// parseExecFlags peels phi's exec flags off the front of args, then stops as
// soon as it sees the first positional (the package/bin name) or an explicit
// `--` terminator. Everything from that point on is forwarded to the bin
// verbatim — without this, `phi x prettier --write src/` would have its
// `--write` swallowed by phi's flag parser.
func parseExecFlags(args []string) (installer.ExecOptions, []string) {
	var opts installer.ExecOptions
	rest := make([]string, 0, len(args))
	seenPositional := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if seenPositional {
			rest = append(rest, a)
			continue
		}
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		switch {
		case a == "--package" || a == "-p":
			if i+1 < len(args) {
				opts.Package = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--package="):
			opts.Package = strings.TrimPrefix(a, "--package=")
		case a == "--no-install":
			opts.NoInstall = true
		case a == "--yes" || a == "-y":
			opts.Yes = true
		case a == "--rescan":
			opts.Rescan = true
		case a == "--force" || a == "-f":
			opts.Force = true
		default:
			rest = append(rest, a)
			seenPositional = true
		}
	}
	return opts, rest
}

func parseAuditFixFlags(args []string) installer.FixOptions {
	var opts installer.FixOptions
	for _, a := range args {
		switch a {
		case "--apply":
			opts.Apply = true
		case "--force", "-f":
			opts.Force = true
			opts.Apply = true
		}
	}
	return opts
}

func parseSelfUpdateFlags(args []string) installer.SelfUpdateOptions {
	var opts installer.SelfUpdateOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--check":
			opts.CheckOnly = true
		case a == "--yes" || a == "-y":
			opts.Yes = true
		case a == "--version" && i+1 < len(args):
			opts.Version = args[i+1]
			i++
		case strings.HasPrefix(a, "--version="):
			opts.Version = strings.TrimPrefix(a, "--version=")
		}
	}
	return opts
}

func parseInitFlags(args []string) installer.InitOptions {
	var opts installer.InitOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--yes" || a == "-y":
			opts.Yes = true
		case a == "--force" || a == "-f":
			opts.Force = true
		case a == "--name" && i+1 < len(args):
			opts.Name = args[i+1]
			i++
		case a == "--version" && i+1 < len(args):
			opts.Version = args[i+1]
			i++
		case a == "--description" && i+1 < len(args):
			opts.Description = args[i+1]
			i++
		case a == "--author" && i+1 < len(args):
			opts.Author = args[i+1]
			i++
		case a == "--license" && i+1 < len(args):
			opts.License = args[i+1]
			i++
		}
	}
	return opts
}

func handleCache(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("cache: subcommand required (stat | clean)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "stat":
		return installer.CacheStat()
	case "clean":
		age := 30 * 24 * time.Hour
		for i := 0; i < len(rest); i++ {
			a := rest[i]
			switch {
			case a == "--all":
				age = 0
			case a == "--older-than" && i+1 < len(rest):
				d, err := installer.ParseAge(rest[i+1])
				if err != nil {
					return err
				}
				age = d
				i++
			case strings.HasPrefix(a, "--older-than="):
				d, err := installer.ParseAge(strings.TrimPrefix(a, "--older-than="))
				if err != nil {
					return err
				}
				age = d
			}
		}
		return installer.CacheClean(age)
	default:
		return fmt.Errorf("cache: unknown subcommand %q (stat | clean)", sub)
	}
}

func exitOnErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "phi:", err)
	os.Exit(1)
}
