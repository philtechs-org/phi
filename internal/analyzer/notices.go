package analyzer

import "strings"

// deprecatedPackages maps a package name to a one-line replacement note.
// These are informational only — they don't change the risk score or
// verdict, just surface a "you probably shouldn't be using this" message
// in the report card. Curated, not exhaustive — focus on packages that
// are still in heavy use despite documented deprecation, security history,
// or a clear stdlib/successor replacement.
var deprecatedPackages = map[string]string{
	// Sandbox library with 12 critical sandbox-escape CVEs through 2026
	// (CVE-2026-43997 / 44005 / 44006 / etc.). Architecturally cannot be
	// fully secured — every fix is followed by another bypass.
	"vm2": "use isolated-vm (V8-isolate-based, not a JS proxy sandbox)",

	// Discontinued by maintainer in 2020. Any project still using it has
	// rotted along with the rest of the request ecosystem.
	"request":          "discontinued 2020; use undici, axios, or got",
	"request-promise":  "discontinued 2020; use undici, axios, or got",
	"request-promise-native": "discontinued 2020; use undici, axios, or got",
	"har-validator":    "project archived (was a request dep)",

	// Built-in Node features now cover these; the packages are harmless
	// but pointless extra dependencies.
	"node-uuid": "renamed to uuid (the same package)",
	"left-pad":  "use String.prototype.padStart (built into Node since 8.x)",
	"is-array":  "use Array.isArray (stdlib)",

	// Lint/parse layer churn: the npm ecosystem moved on.
	"tslint":       "migrate to eslint with @typescript-eslint",
	"babel-eslint": "renamed to @babel/eslint-parser",

	// Predates npm's full reach but kept around in old projects. Not
	// actively maintained; npm/pnpm/yarn cover everything it does.
	"bower": "use npm, yarn, or pnpm",

	// Old crypto/hash packages with current safer replacements.
	"node-sass": "discontinued 2020; use sass (Dart Sass)",
}

// noticesFor returns informational notes that apply to the named package.
// Today this is just the deprecation list; future kinds (advisory history,
// maintainer-flag, etc.) plug in here without changing call sites.
func noticesFor(packageName string) []Notice {
	var out []Notice
	if msg, ok := deprecatedPackages[strings.ToLower(packageName)]; ok {
		out = append(out, Notice{Kind: "deprecated", Message: msg})
	}
	return out
}
