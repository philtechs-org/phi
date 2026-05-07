package analyzer

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/dop251/goja/ast"
)

type detectionContext struct {
	packageName string
	file        string
}

type detector struct {
	name        string
	description string
	severity    Severity
	patterns    []*regexp.Regexp
	// matcher, if non-nil, replaces patterns. Used for detectors that need
	// awareness of the package's own identity (e.g. credential theft).
	matcher func(content string, ctx detectionContext) (evidence string, ok bool)
}

func mustCompile(pats ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

var detectors = []detector{
	{
		name:        "Network Exfiltration",
		description: "Outbound calls to known exfiltration services or hidden domains",
		severity:    SeverityHigh,
		patterns: mustCompile(
			`https?://[^\s'"<>]*\.onion\b`,
			`https?://(?:[^\s'"<>/]+\.)?(?:pastebin\.com|hastebin\.com|requestbin\.io|webhook\.site|ngrok\.io|transfer\.sh|anonfiles\.com|gofile\.io|paste\.ee|controlc\.com)\b`,
		),
	},
	{
		name:        "File System Access",
		description: "Reads of OS-level sensitive paths",
		severity:    SeverityHigh,
		patterns: mustCompile(
			`/etc/passwd\b`,
			`/etc/shadow\b`,
			`\.aws/credentials\b`,
			`\.kube/config\b`,
			`\.docker/config\.json\b`,
		),
	},
	{
		// AST-validated: regex pre-filter matches any "eval" or "child_process"
		// substring; the smart matcher then parses the file with goja and only
		// fires when the AST has a real CallExpression to eval(...) or
		// child_process.<exec|spawn|...>(...). String/comment occurrences are
		// suppressed. On parse failure (TS, ES2022+ syntax goja can't handle)
		// we fall back to the regex match to preserve coverage.
		name:        "Arbitrary Code Execution",
		description: "Direct execution of arbitrary code",
		severity:    SeverityCritical,
		matcher:     detectArbitraryCodeExecution,
	},
	{
		// AST-validated for the same reason: new Function() in source comments
		// or string literals (eslint, parsers, lint-rule libraries) shouldn't
		// fire. Severity stays HIGH because dynamic compilation is legitimately
		// used by validator generators, JSON serializers, and route compilers.
		name:        "Dynamic Code Compilation",
		description: "String-to-function compilation (legitimately used by code-generation libraries)",
		severity:    SeverityHigh,
		matcher:     detectDynamicCodeCompilation,
	},
	{
		name:        "Code Obfuscation",
		description: "Techniques used to hide malicious payloads",
		severity:    SeverityCritical,
		matcher:     detectCodeObfuscation,
	},
	{
		name:        "Credential Theft",
		description: "Access to API keys, tokens, or credential files",
		severity:    SeverityCritical,
		matcher:     detectCredentialTheft,
	},
	{
		name:        "Install Script Abuse",
		description: "Lifecycle scripts (preinstall/install/postinstall) that pipe remote code into a shell or run inline JS",
		severity:    SeverityCritical,
		matcher:     detectInstallScriptAbuse,
	},
	{
		name:        "Crypto Mining",
		description: "Cryptocurrency mining APIs and pool connections",
		severity:    SeverityCritical,
		patterns: mustCompile(
			`(?i)coinhive`,
			`(?i)\bmonero\b|\bxmr\b`,
			`stratum\+tcp://`,
			`(?i)cryptonight`,
		),
	},
	{
		name:        "Wallet Drain",
		description: "Cryptocurrency wallet access or transfer patterns",
		severity:    SeverityCritical,
		patterns: mustCompile(
			`web3\.eth\.sendTransaction`,
			`ethers\.Wallet\b`,
			`(?i)\bdrainTokens?\b`,
			`(?i)\bdrainWallet\b`,
		),
	},
	{
		name:        "Reverse Shell",
		description: "Patterns used to open a remote shell back to an attacker",
		severity:    SeverityCritical,
		patterns: mustCompile(
			`/bin/(?:bash|sh)\s+-i`,
			`/dev/tcp/`,
			`\bmkfifo\b`,
			`nc\s+-e\s+/bin/`,
		),
	},
}

// AST-validated detectors for code-execution patterns.
//
// The regex pre-filter eliminates files that don't contain any candidate
// substring (cheap fast-path). For files that do match, we parse with goja
// and walk the AST: a detection fires only when the matching token is a
// real call/new expression, not a string literal, comment, or identifier
// reference. Files goja can't parse (TypeScript, ES2022+) fall back to the
// regex match for safety — better to flag than miss.
var (
	arbCodeExecRe   = regexp.MustCompile(`\beval\s*\(|child_process\.(?:exec|spawn|execSync|spawnSync)\s*\(`)
	dynCodeCompRe   = regexp.MustCompile(`new\s+Function\s*\(`)
	parsableJSExts  = []string{".js", ".cjs", ".mjs"}
)

func isParsableJS(file string) bool {
	lower := strings.ToLower(file)
	for _, ext := range parsableJSExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func detectArbitraryCodeExecution(content string, ctx detectionContext) (string, bool) {
	loc := arbCodeExecRe.FindStringIndex(content)
	if loc == nil {
		return "", false
	}
	if !isParsableJS(ctx.file) {
		return content[loc[0]:loc[1]], true
	}
	prog, err := parseJS(ctx.file, content)
	if err != nil {
		return content[loc[0]:loc[1]], true
	}
	var found string
	walkAST(prog, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		c, ok := n.(*ast.CallExpression)
		if !ok {
			return true
		}
		if isCallTo(c, "eval") {
			found = "eval(...)"
			return false
		}
		if isMemberCall(c, "child_process", "exec", "spawn", "execSync", "spawnSync") {
			_, prop, _ := memberAccess(c.Callee)
			found = "child_process." + prop + "(...)"
			return false
		}
		return true
	})
	return found, found != ""
}

func detectDynamicCodeCompilation(content string, ctx detectionContext) (string, bool) {
	loc := dynCodeCompRe.FindStringIndex(content)
	if loc == nil {
		return "", false
	}
	if !isParsableJS(ctx.file) {
		return content[loc[0]:loc[1]], true
	}
	prog, err := parseJS(ctx.file, content)
	if err != nil {
		return content[loc[0]:loc[1]], true
	}
	var found string
	walkAST(prog, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		ne, ok := n.(*ast.NewExpression)
		if !ok {
			return true
		}
		if isNewOf(ne, "Function") {
			found = "new Function(...)"
			return false
		}
		return true
	})
	return found, found != ""
}

// Code-obfuscation detector internals.
//
// The hex-escape pattern matches \xNN sequences of 6+, which is a classic
// obfuscator output shape — but a long run of a single byte (e.g. \x07
// repeated as a delimiter in prettier's flow plugin) also matches the regex
// while being clearly benign. To distinguish, the matched substring must
// contain at least 4 distinct hex pairs. Real obfuscators emit varied bytes
// because they encode actual code; uniform-byte fixtures never do.
var (
	obfHexEscapeRe  = regexp.MustCompile(`(?:\\x[0-9a-fA-F]{2}){6,}`)
	obfHexPairRe    = regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)
	obfBase64Re     = regexp.MustCompile(`Buffer\.from\(\s*['"][A-Za-z0-9+/=]{40,}['"]\s*,\s*['"]base64['"]\s*\)`)
	obfFromCharCode = regexp.MustCompile(`String\.fromCharCode\s*\(\s*(?:0x[0-9a-fA-F]+|\d+)(?:\s*,\s*(?:0x[0-9a-fA-F]+|\d+)){3,}`)
	obfAtobLong     = regexp.MustCompile(`atob\s*\(\s*['"][A-Za-z0-9+/=]{40,}['"]\s*\)`)
)

func detectCodeObfuscation(content string, ctx detectionContext) (string, bool) {
	if loc := obfHexEscapeRe.FindStringIndex(content); loc != nil {
		evidence := content[loc[0]:loc[1]]
		if hasDiverseHexEscapes(evidence, 4) {
			return evidence, true
		}
	}
	for _, re := range []*regexp.Regexp{obfBase64Re, obfFromCharCode, obfAtobLong} {
		if loc := re.FindStringIndex(content); loc != nil {
			return content[loc[0]:loc[1]], true
		}
	}
	return "", false
}

func hasDiverseHexEscapes(s string, minDistinct int) bool {
	seen := map[string]struct{}{}
	for _, m := range obfHexPairRe.FindAllStringSubmatch(s, -1) {
		seen[strings.ToLower(m[1])] = struct{}{}
		if len(seen) >= minDistinct {
			return true
		}
	}
	return false
}

// Install-script abuse detector internals.
//
// Generic patterns like `node -e ` or `curl | bash` appear all over real
// repos — in test scripts, prepublish hooks, build commands. The actual
// attack vector is the npm install lifecycle (preinstall/install/postinstall),
// because those run automatically when a user `npm install`s the package.
// We parse package.json and only inspect those three fields.
var scriptAbusePatterns = []*regexp.Regexp{
	regexp.MustCompile(`curl\s+[^|]*\|\s*(?:bash|sh|zsh)`),
	regexp.MustCompile(`wget\s+[^|]*\|\s*(?:bash|sh|zsh)`),
	regexp.MustCompile(`node\s+-e\s+`),
}

func detectInstallScriptAbuse(content string, ctx detectionContext) (string, bool) {
	if !strings.HasSuffix(strings.ToLower(ctx.file), "package.json") {
		return "", false
	}
	var data struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return "", false
	}
	for _, phase := range []string{"preinstall", "install", "postinstall"} {
		script := data.Scripts[phase]
		if script == "" {
			continue
		}
		for _, pat := range scriptAbusePatterns {
			if loc := pat.FindStringIndex(script); loc != nil {
				return phase + ": " + script[loc[0]:loc[1]], true
			}
		}
	}
	return "", false
}

// Credential-theft detector internals.
//
// Generic process.env.<*KEY|*TOKEN|*SECRET|*PASSWORD> patterns produce too many
// false positives because every API client legitimately reads its own API key
// from the environment. The smart matcher only fires when:
//   - the env var name appears in a well-known third-party credential list
//     (AWS, GitHub, npm, etc.), AND
//   - the env var name does not overlap with the package's own normalized name
//     (so resend reading RESEND_API_KEY is silent, while a malicious package
//     reading AWS_SECRET_ACCESS_KEY is not).
// File-based credential references (.npmrc, .netrc, id_rsa) are unambiguous
// signals and fire regardless of context.
var (
	credFileRe = regexp.MustCompile(`\.npmrc\b|\.netrc\b|\bid_rsa\b|\bid_ed25519\b`)
	envVarRe   = regexp.MustCompile(`process\.env(?:\.([A-Z_][A-Z0-9_]*)|\[\s*['"]([A-Z_][A-Z0-9_]*)['"]\s*\])`)
)

var thirdPartyCreds = map[string]bool{
	"AWS_ACCESS_KEY_ID":              true,
	"AWS_SECRET_ACCESS_KEY":          true,
	"AWS_SECRET_KEY":                 true,
	"AWS_SESSION_TOKEN":              true,
	"AZURE_TOKEN":                    true,
	"AZURE_CLIENT_SECRET":            true,
	"GCP_KEY":                        true,
	"GOOGLE_APPLICATION_CREDENTIALS": true,
	"GITHUB_TOKEN":                   true,
	"GH_TOKEN":                       true,
	"GITHUB_PAT":                     true,
	"GITLAB_TOKEN":                   true,
	"NPM_TOKEN":                      true,
	"NPM_AUTH_TOKEN":                 true,
	"HEROKU_API_KEY":                 true,
	"DOCKER_PASSWORD":                true,
	"CIRCLE_TOKEN":                   true,
	"DISCORD_TOKEN":                  true,
	"DISCORD_BOT_TOKEN":              true,
	"SLACK_TOKEN":                    true,
	"SLACK_BOT_TOKEN":                true,
	"PYPI_TOKEN":                     true,
	"TWILIO_AUTH_TOKEN":              true,
	"STRIPE_SECRET_KEY":              true,
	"MAILGUN_API_KEY":                true,
	"SENDGRID_API_KEY":               true,
}

func detectCredentialTheft(content string, ctx detectionContext) (string, bool) {
	if loc := credFileRe.FindStringIndex(content); loc != nil {
		return content[loc[0]:loc[1]], true
	}
	pkgUpper := normalizePkgName(ctx.packageName)
	for _, m := range envVarRe.FindAllStringSubmatch(content, -1) {
		envVar := m[1]
		if envVar == "" {
			envVar = m[2]
		}
		if isOwnConfig(envVar, pkgUpper) {
			continue
		}
		if thirdPartyCreds[envVar] {
			return "process.env." + envVar, true
		}
	}
	return "", false
}

func normalizePkgName(s string) string {
	s = strings.TrimPrefix(s, "@")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return strings.ToUpper(s)
}

// isOwnConfig matches when an env var name and a package name share enough of
// a recognizable prefix that the env var is plausibly the package's own
// config. Whole-string overlap matches first; otherwise we look for any
// underscore-token of length >= 4 from the package name inside the env var.
func isOwnConfig(envVar, pkgUpper string) bool {
	if pkgUpper == "" {
		return false
	}
	if strings.Contains(envVar, pkgUpper) || strings.Contains(pkgUpper, envVar) {
		return true
	}
	for _, tok := range strings.Split(pkgUpper, "_") {
		if len(tok) >= 4 && strings.Contains(envVar, tok) {
			return true
		}
	}
	return false
}
