package installer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitOptions controls phi init behavior.
type InitOptions struct {
	Name        string
	Version     string
	Description string
	Author      string
	License     string
	Yes         bool // non-interactive, accept defaults
	Force       bool // overwrite existing package.json
}

// Init creates package.json (and a starter .gitignore + README if absent).
// Refuses to clobber an existing package.json unless Force is set.
func Init(opts InitOptions) error {
	if _, err := os.Stat("package.json"); err == nil && !opts.Force {
		return errors.New("package.json already exists (pass --force to overwrite)")
	}

	cwd, _ := os.Getwd()
	if opts.Name == "" {
		opts.Name = sanitizePkgName(filepath.Base(cwd))
	}
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.License == "" {
		opts.License = "MIT"
	}

	if !opts.Yes {
		if err := promptInit(&opts); err != nil {
			return err
		}
	}

	pkg := map[string]any{
		"name":    opts.Name,
		"version": opts.Version,
	}
	if opts.Description != "" {
		pkg["description"] = opts.Description
	}
	if opts.Author != "" {
		pkg["author"] = opts.Author
	}
	pkg["license"] = opts.License
	pkg["scripts"] = map[string]string{
		"start": "node index.js",
	}
	pkg["dependencies"] = map[string]string{}

	body, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile("package.json", append(body, '\n'), 0o644); err != nil {
		return err
	}
	created := []string{"package.json"}

	if _, err := os.Stat(".gitignore"); os.IsNotExist(err) {
		gi := "node_modules/\nphi.lock\nphi-report.json\n.env\n.env.local\n.DS_Store\n"
		if err := os.WriteFile(".gitignore", []byte(gi), 0o644); err == nil {
			created = append(created, ".gitignore")
		}
	}

	if _, err := os.Stat("README.md"); os.IsNotExist(err) {
		desc := opts.Description
		if desc == "" {
			desc = "Project description here."
		}
		readme := fmt.Sprintf("# %s\n\n%s\n\n## Install\n\n```sh\nphi install\n```\n", opts.Name, desc)
		if err := os.WriteFile("README.md", []byte(readme), 0o644); err == nil {
			created = append(created, "README.md")
		}
	}

	fmt.Printf("created %s\n", strings.Join(created, ", "))
	fmt.Println()
	fmt.Println("next steps:")
	fmt.Println("  phi install <pkg>     add a dependency (scanned before install)")
	fmt.Println("  phi audit             scan all dependencies without installing")
	return nil
}

func promptInit(opts *InitOptions) error {
	sc := bufio.NewScanner(os.Stdin)
	fmt.Println("phi init — press enter to accept the default in (parens)")
	fmt.Println()

	fields := []struct {
		label  string
		target *string
	}{
		{"name", &opts.Name},
		{"version", &opts.Version},
		{"description", &opts.Description},
		{"author", &opts.Author},
		{"license", &opts.License},
	}
	for _, f := range fields {
		prompt := fmt.Sprintf("  %s", f.label)
		if *f.target != "" {
			prompt += fmt.Sprintf(" (%s)", *f.target)
		}
		prompt += ": "
		fmt.Print(prompt)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return err
			}
			return nil
		}
		if v := strings.TrimSpace(sc.Text()); v != "" {
			*f.target = v
		}
	}
	fmt.Println()
	return nil
}

// sanitizePkgName lowercases and strips characters npm doesn't allow in
// package names — keeping it conservative for new init'd projects.
func sanitizePkgName(s string) string {
	s = strings.ToLower(s)
	var out strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			out.WriteRune(r)
		case r == ' ':
			out.WriteRune('-')
		}
	}
	name := out.String()
	if name == "" {
		name = "my-app"
	}
	return name
}
