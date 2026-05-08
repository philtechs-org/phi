package installer

import (
	"reflect"
	"strings"
	"testing"
)

func TestListFrameworksContainsAll(t *testing.T) {
	want := []string{"react", "next", "express", "fastify", "nest"}
	got := ListFrameworks()
	if len(got) != len(want) {
		t.Fatalf("got %d frameworks, want %d", len(got), len(want))
	}
	have := map[string]bool{}
	for _, f := range got {
		have[f.Name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("missing framework %q in registry", name)
		}
	}
}

func TestValidateProjectName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"my-app", false},
		{"app_v2", false},
		{"App", false},
		{"", true},
		{".", true},
		{"..", true},
		{".hidden", true},
		{"foo/bar", true},
		{"foo\\bar", true},
		{"foo:bar", true},
		{"foo*", true},
		{"foo?", true},
	}
	for _, c := range cases {
		err := validateProjectName(c.name)
		if (err != nil) != c.wantErr {
			t.Errorf("validateProjectName(%q) err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestBuildScaffolderArgsDefaultsApplied(t *testing.T) {
	spec := FrameworkSpec{
		FlagDefaults: []string{"--template", "react-ts"},
	}
	got := buildScaffolderArgs(spec, "demo", nil)
	want := []string{"demo", "--template", "react-ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildScaffolderArgsUserOverridesDefault(t *testing.T) {
	spec := FrameworkSpec{
		FlagDefaults: []string{"--template", "react-ts"},
	}
	got := buildScaffolderArgs(spec, "demo", []string{"--template", "vanilla"})
	want := []string{"demo", "--template", "vanilla"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildScaffolderArgsUserOverridesEqualsForm(t *testing.T) {
	spec := FrameworkSpec{
		FlagDefaults: []string{"--template", "react-ts"},
	}
	got := buildScaffolderArgs(spec, "demo", []string{"--template=vanilla"})
	want := []string{"demo", "--template=vanilla"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildScaffolderArgsExtraAppended(t *testing.T) {
	spec := FrameworkSpec{
		FlagDefaults: []string{"--lang", "ts"},
	}
	got := buildScaffolderArgs(spec, "demo", []string{"--strict"})
	want := []string{"demo", "--lang", "ts", "--strict"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildScaffolderArgsSubcommandBeforeName(t *testing.T) {
	spec := FrameworkSpec{
		Subcommand: []string{"generate"},
	}
	got := buildScaffolderArgs(spec, "demo", nil)
	want := []string{"generate", "demo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildScaffolderArgsSubcommandPlusFlagDefaults(t *testing.T) {
	spec := FrameworkSpec{
		Subcommand:   []string{"new"},
		FlagDefaults: []string{"--package-manager", "phi"},
	}
	got := buildScaffolderArgs(spec, "demo", []string{"--strict"})
	want := []string{"new", "demo", "--package-manager", "phi", "--strict"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRelTemplatePath(t *testing.T) {
	cases := []struct {
		root, path string
		want       string
		wantErr    bool
	}{
		{"templates/express", "templates/express", "", false},
		{"templates/express", "templates/express/package.json.tmpl", "package.json.tmpl", false},
		{"templates/express", "templates/express/src/index.js", strings.ReplaceAll("src/index.js", "/", string([]byte{47}[0])), false},
		{"templates/express", "templates/other/foo", "", true},
	}
	for _, c := range cases {
		got, err := relTemplatePath(c.root, c.path)
		if (err != nil) != c.wantErr {
			t.Errorf("relTemplatePath(%q, %q) err=%v wantErr=%v", c.root, c.path, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want && got != strings.ReplaceAll(c.want, "/", `\`) {
			t.Errorf("relTemplatePath(%q, %q) = %q, want %q", c.root, c.path, got, c.want)
		}
	}
}

func TestListFrameworksMsgIncludesAll(t *testing.T) {
	msg := listFrameworksMsg("test")
	for _, f := range []string{"react", "next", "express", "fastify", "nest"} {
		if !strings.Contains(msg, f) {
			t.Errorf("listFrameworksMsg missing %q", f)
		}
	}
	if !strings.Contains(msg, "usage:") {
		t.Error("listFrameworksMsg missing usage line")
	}
}
