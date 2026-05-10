package installer

import "testing"

func TestParseRunSpec(t *testing.T) {
	tests := []struct {
		name        string
		spec        string
		pkgOverride string
		wantPkg     string
		wantVersion string
		wantBin     string
	}{
		{
			name:        "bare name defaults version=latest, bin=name",
			spec:        "cowsay",
			wantPkg:     "cowsay",
			wantVersion: "latest",
			wantBin:     "cowsay",
		},
		{
			name:        "name@version pins the version",
			spec:        "cowsay@1.5.0",
			wantPkg:     "cowsay",
			wantVersion: "1.5.0",
			wantBin:     "cowsay",
		},
		{
			name:        "scoped package, bin name defaults to last segment",
			spec:        "@scope/pkg",
			wantPkg:     "@scope/pkg",
			wantVersion: "latest",
			wantBin:     "pkg",
		},
		{
			name:        "scoped package with version",
			spec:        "@scope/pkg@2.0.0",
			wantPkg:     "@scope/pkg",
			wantVersion: "2.0.0",
			wantBin:     "pkg",
		},
		{
			name:        "package override sets package, spec is the bin",
			spec:        "tsc",
			pkgOverride: "typescript",
			wantPkg:     "typescript",
			wantVersion: "latest",
			wantBin:     "tsc",
		},
		{
			name:        "package override carrying its own version",
			spec:        "tsc",
			pkgOverride: "typescript@5.0.0",
			wantPkg:     "typescript",
			wantVersion: "5.0.0",
			wantBin:     "tsc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, ver, bin := parseRunSpec(tt.spec, tt.pkgOverride)
			if pkg != tt.wantPkg {
				t.Errorf("pkg: got %q, want %q", pkg, tt.wantPkg)
			}
			if ver != tt.wantVersion {
				t.Errorf("version: got %q, want %q", ver, tt.wantVersion)
			}
			if bin != tt.wantBin {
				t.Errorf("bin: got %q, want %q", bin, tt.wantBin)
			}
		})
	}
}
