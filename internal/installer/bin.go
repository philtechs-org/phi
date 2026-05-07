package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CreateBinShims walks each extracted package's bin field and writes shim
// scripts under <nodeModulesDir>/.bin so installed CLIs are runnable from
// the project root. Tier 1 supports the bin-as-string and bin-as-map shapes.
func CreateBinShims(nodeModulesDir string, extracted map[string]string) error {
	if len(extracted) == 0 {
		return nil
	}
	binDir := filepath.Join(nodeModulesDir, ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	for pkgName, pkgDir := range extracted {
		body, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
		if err != nil {
			continue
		}
		for shim, script := range parseBinField(body, pkgName) {
			if err := writeShim(binDir, shim, pkgName, script); err != nil {
				return fmt.Errorf("shim %s for %s: %w", shim, pkgName, err)
			}
		}
	}
	return nil
}

func parseBinField(packageJSON []byte, pkgName string) map[string]string {
	var data struct {
		Name string          `json:"name"`
		Bin  json.RawMessage `json:"bin"`
	}
	if err := json.Unmarshal(packageJSON, &data); err != nil {
		return nil
	}
	if data.Name == "" {
		data.Name = pkgName
	}
	out := map[string]string{}
	if len(data.Bin) == 0 {
		return out
	}
	var asString string
	if err := json.Unmarshal(data.Bin, &asString); err == nil {
		shim := data.Name
		if i := strings.LastIndex(shim, "/"); i >= 0 {
			shim = shim[i+1:]
		}
		out[shim] = asString
		return out
	}
	var asMap map[string]string
	if err := json.Unmarshal(data.Bin, &asMap); err == nil {
		for k, v := range asMap {
			out[k] = v
		}
	}
	return out
}

func writeShim(binDir, shimName, pkgName, scriptPath string) error {
	scriptPath = strings.TrimPrefix(scriptPath, "./")
	if runtime.GOOS == "windows" {
		return writeWindowsShim(binDir, shimName, pkgName, scriptPath)
	}
	return writeUnixShim(binDir, shimName, pkgName, scriptPath)
}

func writeWindowsShim(binDir, shimName, pkgName, scriptPath string) error {
	target := filepath.Join(binDir, shimName+".cmd")
	rel := filepath.ToSlash(filepath.Join("..", pkgName, scriptPath))
	rel = strings.ReplaceAll(rel, "/", "\\")
	body := "@ECHO off\r\nnode \"%~dp0\\" + rel + "\" %*\r\n"
	return os.WriteFile(target, []byte(body), 0o755)
}

func writeUnixShim(binDir, shimName, pkgName, scriptPath string) error {
	target := filepath.Join(binDir, shimName)
	rel := filepath.ToSlash(filepath.Join("..", pkgName, scriptPath))
	body := "#!/bin/sh\nexec node \"$(dirname \"$0\")/" + rel + "\" \"$@\"\n"
	return os.WriteFile(target, []byte(body), 0o755)
}
