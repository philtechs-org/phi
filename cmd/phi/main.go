package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/philtechs-org/phi/internal/installer"
	"github.com/philtechs-org/phi/internal/ui"
)

// Build metadata. Defaults are used by `go build`; goreleaser overrides
// these via -ldflags -X main.<name>=<value> for tagged releases.
var (
	version   = "0.1.0-dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
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
		opts, _ := parseFlags(args)
		exitOnErr(installer.AuditWith(opts))
	case "init":
		exitOnErr(installer.Init(parseInitFlags(args)))
	case "do", "d":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "phi: do requires a script name")
			os.Exit(2)
		}
		exitOnErr(installer.Do(args[0], args[1:]))
	case "exec", "x":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "phi: exec requires a binary name")
			os.Exit(2)
		}
		exitOnErr(installer.Exec(args[0], args[1:]))
	case "dev", "build", "start", "test", "lint", "preview", "prod":
		exitOnErr(installer.Do(cmd, args))
	case "why":
		_, rest := parseFlags(args)
		exitOnErr(installer.Why(rest))
	case "outdated":
		exitOnErr(installer.Outdated())
	case "cache":
		exitOnErr(handleCache(args))
	case "version", "-v", "--version":
		fmt.Printf("phi %s (commit %s, built %s)\n", version, commit, buildDate)
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
