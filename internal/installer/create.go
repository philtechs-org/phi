package installer

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// CreateMode selects how a framework's project skeleton is produced.
type CreateMode int

const (
	// ModeProxy installs a canonical scaffolder package (e.g. create-vite,
	// create-next-app) into a temp directory through phi's safe install
	// pipeline, then executes its binary with the user's args. The
	// scaffolder writes the actual project files.
	ModeProxy CreateMode = iota
	// ModeBuiltin copies a template baked into the phi binary via embed.FS.
	// Used for frameworks without a canonical scaffolder (Express).
	ModeBuiltin
)

// FrameworkSpec describes how `phi create <name>` produces a project.
type FrameworkSpec struct {
	Name        string
	Mode        CreateMode
	Package     string   // proxy mode: npm package providing the binary
	Binary      string   // proxy mode: binary name in node_modules/.bin (often == Package)
	Subcommand  []string // proxy mode: tokens placed BEFORE the project name (e.g. "generate", "new")
	FlagDefaults []string // proxy mode: flag/value pairs placed AFTER the project name; user-supplied flags with the same name override these
	Description string
}

// frameworks is the registry. Adding an entry here is all that's needed to
// expose a new `phi create <foo>` target.
var frameworks = map[string]FrameworkSpec{
	"react": {
		Name:         "react",
		Mode:         ModeProxy,
		Package:      "create-vite",
		Binary:       "create-vite",
		FlagDefaults: []string{"--template", "react-ts"},
		Description:  "React app via Vite (TypeScript)",
	},
	"next": {
		Name:    "next",
		Mode:    ModeProxy,
		Package: "create-next-app",
		Binary:  "create-next-app",
		// --skip-install: stop create-next-app from auto-running `npm install`
		// inside the new project — that would bypass phi's safe pipeline for
		// the project's actual deps. The user runs `phi install` afterwards.
		FlagDefaults: []string{"--skip-install"},
		Description:  "Next.js app (App Router)",
	},
	"fastify": {
		Name:        "fastify",
		Mode:        ModeProxy,
		Package:     "fastify-cli",
		Binary:      "fastify",
		Subcommand:  []string{"generate"},
		Description: "Fastify HTTP server",
	},
	"nest": {
		Name:       "nest",
		Mode:       ModeProxy,
		Package:    "@nestjs/cli",
		Binary:     "nest",
		Subcommand: []string{"new"},
		// --skip-install for the same reason as next: let phi install the
		// real deps so they're scanned, instead of letting nest run npm.
		FlagDefaults: []string{"--skip-install"},
		Description:  "NestJS application",
	},
	"express": {
		Name:        "express",
		Mode:        ModeBuiltin,
		Description: "Minimal Express server (built-in template)",
	},
}

// Create scaffolds a new project for the named framework. If framework is
// empty or unknown, returns an error and lists available frameworks.
//
// For proxy-mode frameworks, the scaffolder package is installed into a
// temp directory through phi's normal install pipeline (scan + extract,
// no lifecycle scripts). The scaffolder's binary is then executed with
// the project name and pass-through args. The temp directory is cleaned
// up on exit regardless of outcome.
//
// For builtin-mode frameworks (express), files are copied from an
// embedded template tree into a new directory with simple ${NAME}
// substitution in package.json.
func Create(framework, projectName string, extraArgs []string) error {
	if framework == "" {
		return errors.New(listFrameworksMsg("phi create requires a framework"))
	}
	spec, ok := frameworks[framework]
	if !ok {
		return errors.New(listFrameworksMsg(fmt.Sprintf("unknown framework %q", framework)))
	}
	if projectName == "" {
		return fmt.Errorf("phi create %s: project name required (e.g. `phi create %s my-app`)",
			framework, framework)
	}
	if err := validateProjectName(projectName); err != nil {
		return err
	}
	if _, err := os.Stat(projectName); err == nil {
		return fmt.Errorf("%q already exists in current directory", projectName)
	}

	switch spec.Mode {
	case ModeProxy:
		return createProxy(spec, projectName, extraArgs)
	case ModeBuiltin:
		return createBuiltin(spec, projectName, extraArgs)
	default:
		return fmt.Errorf("internal: unhandled mode for %q", framework)
	}
}

// ListFrameworks returns the registry sorted by name. Used by the CLI
// to render the no-arg help.
func ListFrameworks() []FrameworkSpec {
	out := make([]FrameworkSpec, 0, len(frameworks))
	for _, f := range frameworks {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func listFrameworksMsg(prefix string) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("\n\navailable frameworks:\n")
	for _, f := range ListFrameworks() {
		fmt.Fprintf(&b, "  %-8s  %s\n", f.Name, f.Description)
	}
	b.WriteString("\nusage: phi create <framework> <project-name> [-- pass-through-args...]")
	return b.String()
}

// validateProjectName rejects names that would cause problems on disk or
// when written into package.json. Mirrors the conservative rules in
// sanitizePkgName but errors instead of silently rewriting — the user
// asked for this name explicitly.
func validateProjectName(name string) error {
	if name == "" {
		return errors.New("project name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("project name %q is not allowed", name)
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return fmt.Errorf("project name %q contains an illegal character", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("project name %q starts with '.'", name)
	}
	return nil
}
