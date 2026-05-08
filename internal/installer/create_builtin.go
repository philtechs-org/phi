package installer

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// templatesFS embeds the built-in scaffold templates. Each top-level
// directory under templates/ corresponds to a framework name with
// Mode == ModeBuiltin in the registry.
//
//go:embed all:templates
var templatesFS embed.FS

// createBuiltin copies the embedded template tree for spec into
// projectName/, performing ${NAME} substitution on files whose path ends
// in ".tmpl" (the .tmpl suffix is stripped on write). Existing
// projectName/ is refused by the caller (Create checks first).
func createBuiltin(spec FrameworkSpec, projectName string, _ []string) error {
	root := filepath.ToSlash(filepath.Join("templates", spec.Name))

	if _, err := fs.Stat(templatesFS, root); err != nil {
		return fmt.Errorf("template %q not embedded in phi binary", spec.Name)
	}

	fmt.Printf("phi create: scaffolding %s -> %s\n", spec.Name, projectName)
	fmt.Printf("using built-in template (no network fetch)\n\n")

	if err := os.MkdirAll(projectName, 0o755); err != nil {
		return fmt.Errorf("create %s/: %w", projectName, err)
	}

	written := 0
	walkErr := fs.WalkDir(templatesFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := relTemplatePath(root, path)
		if err != nil {
			return err
		}
		if rel == "" {
			return nil // root itself
		}

		dest := filepath.Join(projectName, rel)
		// Strip .tmpl suffix on write so users get clean filenames.
		stripped := strings.TrimSuffix(dest, ".tmpl")
		isTemplate := stripped != dest
		dest = stripped

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		body, err := templatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if isTemplate {
			body = []byte(strings.ReplaceAll(string(body), "${NAME}", projectName))
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		written++
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	fmt.Printf("ok: created %s/ from built-in %s template (%d files)\n", projectName, spec.Name, written)
	fmt.Println("  next steps:")
	fmt.Printf("    cd %s\n", projectName)
	fmt.Println("    phi install         # install deps with phi's safe pipeline")
	fmt.Println("    phi do dev          # start the dev server")
	return nil
}

// relTemplatePath returns the path relative to the template root, using
// forward slashes (embed.FS uses slash paths) but converted to OS
// separators in the output. Returns "" when path == root.
func relTemplatePath(root, path string) (string, error) {
	if path == root {
		return "", nil
	}
	prefix := root + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("path %q outside template root %q", path, root)
	}
	return filepath.FromSlash(strings.TrimPrefix(path, prefix)), nil
}
