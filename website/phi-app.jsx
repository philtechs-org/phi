/* phi homepage — interactive components */

const { useState, useEffect, useRef, useMemo } = React;

/* ============================================================
 * LIVE SCANNER PANEL  (hero right side)
 * ========================================================== */

// Sample feed for the hero scanner. Real packages with real verdicts:
//   - lodash@4.17.20 has 5 OSV advisories (sum to ~70).
//   - ua-parser-js@0.7.29 was a confirmed malicious release in Oct 2021
//     (crypto miner + Windows password stealer).
//   - node-ipc@10.1.1 was the protestware release in Mar 2022.
//   - The REVIEW-flagged ones (zod, pino, esbuild) match phi's actual
//     corpus output for those versions on v0.1.0.
const PKG_FEED = [
  { name: "react",        ver: "18.3.1",  files: 102,  score: 0,   verdict: "SAFE",    sev: "v-safe"   },
  { name: "next",         ver: "15.0.3",  files: 1843, score: 5,   verdict: "SAFE",    sev: "v-safe"   },
  { name: "lodash",       ver: "4.17.20", files: 1047, score: 70,  verdict: "BLOCKED", sev: "v-block"  },
  { name: "express",      ver: "4.21.0",  files: 67,   score: 0,   verdict: "SAFE",    sev: "v-safe"   },
  { name: "ua-parser-js", ver: "0.7.29",  files: 14,   score: 95,  verdict: "BLOCKED", sev: "v-block"  },
  { name: "zod",          ver: "3.23.8",  files: 38,   score: 20,  verdict: "REVIEW",  sev: "v-review" },
  { name: "axios",        ver: "1.7.7",   files: 156,  score: 0,   verdict: "SAFE",    sev: "v-safe"   },
  { name: "fastify",      ver: "5.0.0",   files: 234,  score: 10,  verdict: "SAFE",    sev: "v-safe"   },
  { name: "ws",           ver: "8.18.0",  files: 41,   score: 0,   verdict: "SAFE",    sev: "v-safe"   },
  { name: "vite",         ver: "6.0.1",   files: 412,  score: 5,   verdict: "SAFE",    sev: "v-safe"   },
  { name: "esbuild",      ver: "0.24.0",  files: 8,    score: 25,  verdict: "REVIEW",  sev: "v-review" },
  { name: "pino",         ver: "9.5.0",   files: 79,   score: 55,  verdict: "REVIEW",  sev: "v-review" },
  { name: "node-ipc",     ver: "10.1.1",  files: 96,   score: 70,  verdict: "BLOCKED", sev: "v-block"  },
];

function ScoreBar({ score, sev }) {
  return (
    <span className={`score-bar ${sev}`}>
      <span className="meter"><i style={{ width: `${score}%` }} /></span>
      <span className="num" style={{ minWidth: 22 }}>{score}</span>
    </span>
  );
}

function LiveScanner() {
  const [head, setHead] = useState(0);  // index of next package to scan
  const VISIBLE = 8;

  // tick to advance scanned cursor
  useEffect(() => {
    const t = setInterval(() => {
      setHead(h => (h + 1) % (PKG_FEED.length + 6));  // pause at end before looping
    }, 900);
    return () => clearInterval(t);
  }, []);

  const rows = useMemo(() => {
    const start = Math.max(0, Math.min(head - VISIBLE + 1, PKG_FEED.length - VISIBLE));
    return PKG_FEED.slice(start, start + VISIBLE).map((p, i) => ({
      ...p,
      globalIdx: start + i,
      isScanning: start + i === head && head < PKG_FEED.length,
      isPending:  start + i > head,
    }));
  }, [head]);

  const scanned = Math.min(head + 1, PKG_FEED.length);
  const blocked = PKG_FEED.slice(0, scanned).filter(p => p.verdict === "BLOCKED").length;
  const review  = PKG_FEED.slice(0, scanned).filter(p => p.verdict === "REVIEW").length;
  const safe    = scanned - blocked - review;

  return (
    <div className="scanner">
      <div className="scanner-head">
        <div className="scanner-title">
          <span className="live-dot"></span>
          <span>scanning · my-app/package-lock.json</span>
        </div>
        <div className="scanner-stat">
          <b>{scanned}</b><span style={{ color: "var(--ink-mute)" }}>/{PKG_FEED.length}</span>
        </div>
        <div className="scanner-stat" style={{ color: "var(--ink-mute)" }}>
          0.{(scanned * 73).toString().padStart(3, "0")}s
        </div>
      </div>

      <div className="scanner-body">
        {rows.map((p) => (
          <div
            key={p.globalIdx}
            className={`pkg-row ${p.isScanning ? "scanning" : ""}`}
            style={{ opacity: p.isPending ? 0.35 : 1 }}
          >
            <span className="idx">{String(p.globalIdx + 1).padStart(2, "0")}</span>
            <span className="name">
              {p.name}<span className="ver">@{p.ver}</span>
            </span>
            <span className="files">{p.files}f</span>
            <span className="score">
              {p.isPending ? (
                <span style={{ color: "var(--ink-mute)" }}>—</span>
              ) : p.isScanning ? (
                <span className="mono" style={{ color: "var(--signal)" }}>···</span>
              ) : (
                <ScoreBar score={p.score} sev={p.sev} />
              )}
            </span>
            <span className="verdict">
              {p.isPending || p.isScanning ? (
                <span style={{ color: "var(--ink-mute)" }}>—</span>
              ) : (
                <span className={`verdict-pill ${p.sev}`}>{p.verdict}</span>
              )}
            </span>
          </div>
        ))}
      </div>

      <div className="scanner-foot">
        <div className="cell">
          <div className="lbl">SCANNED</div>
          <div className="val">{scanned}</div>
        </div>
        <div className="cell">
          <div className="lbl">SAFE</div>
          <div className="val" style={{ color: "var(--safe)" }}>{safe}</div>
        </div>
        <div className="cell">
          <div className="lbl">REVIEW</div>
          <div className="val" style={{ color: "var(--mod)" }}>{review}</div>
        </div>
        <div className="cell">
          <div className="lbl">BLOCKED</div>
          <div className="val crit">{blocked}</div>
        </div>
      </div>
    </div>
  );
}

/* ============================================================
 * DETECTOR GRID
 * ========================================================== */

const DETECTORS = [
  {
    sym: "Ace",
    name: "Arbitrary Code Execution",
    sev: "crit", pts: 35,
    layer: "AST validated",
    examples: "eval(...) · child_process.exec / spawn",
    body: [
      <>Direct execution of arbitrary code. Phi parses every <code>.js / .cjs / .mjs</code> file with goja and only fires on real <code>CallExpression</code> nodes.</>,
      <>References inside string literals, comments, or identifier names are suppressed. eslint's source contains <code>eval</code> in regex patterns &mdash; AST validation correctly skips those.</>,
    ],
  },
  {
    sym: "Dyn",
    name: "Dynamic Code Compilation",
    sev: "high", pts: 20,
    layer: "AST validated",
    examples: "new Function(...)",
    body: [
      <>String-to-function compilation. Legitimately used by validator generators (zod, ajv), JSON serializers (<code>fast-json-stringify</code>), route compilers (<code>find-my-way</code>), and prettier.</>,
      <>Demoted to HIGH so a single hit lands in REVIEW &mdash; combined with a CRITICAL or another HIGH it can still escalate.</>,
    ],
  },
  {
    sym: "Obf",
    name: "Code Obfuscation",
    sev: "crit", pts: 35,
    layer: "Pattern · diversity-checked",
    examples: "\\xNN runs · Buffer.from(b64) · String.fromCharCode",
    body: [
      <>Techniques used to hide malicious payloads: hex-escape sequences of 6+ pairs, base64-decoded buffers, charcode arrays of 4+, <code>atob</code>.</>,
      <>The hex matcher applies a <em>diversity check</em> &mdash; uniform-byte runs (e.g. <code>\x07\x07\x07…</code>) are skipped, because real obfuscators emit varied bytes.</>,
    ],
  },
  {
    sym: "Cred",
    name: "Credential Theft",
    sev: "crit", pts: 35,
    layer: "Smart matcher",
    examples: ".npmrc · .netrc · id_rsa · AWS_SECRET_*",
    body: [
      <>Access to API keys, tokens, or credential files. Knows the package's normalized name and silently skips reads of its own env vars &mdash; <code>resend</code> reading <code>RESEND_API_KEY</code> doesn't fire.</>,
      <>Unrelated package reading <code>AWS_SECRET_ACCESS_KEY</code> does. File-based references &mdash; <code>.npmrc, .netrc, id_rsa, id_ed25519</code> &mdash; fire unconditionally.</>,
    ],
  },
  {
    sym: "Inst",
    name: "Install Script Abuse",
    sev: "crit", pts: 35,
    layer: "Smart matcher",
    examples: "preinstall · install · postinstall",
    body: [
      <>Lifecycle scripts that pipe remote code into a shell. Phi only inspects the three install hooks of <code>package.json</code>.</>,
      <>Test scripts, prepublish hooks, and build scripts that happen to use <code>node -e</code> or <code>curl | sh</code> don't fire. ljharb-style utility false positives eliminated.</>,
    ],
  },
  {
    sym: "Min",
    name: "Crypto Mining",
    sev: "crit", pts: 35,
    layer: "Pattern",
    examples: "stratum+tcp:// · CoinHive · CryptoNight",
    body: [
      <>Cryptocurrency mining APIs and pool connections: CoinHive, Monero/XMR references, <code>stratum+tcp://</code> URLs, CryptoNight algorithm references.</>,
    ],
  },
  {
    sym: "Wlt",
    name: "Wallet Drain",
    sev: "crit", pts: 35,
    layer: "Pattern",
    examples: "web3.eth.sendTransaction · ethers.Wallet · drainTokens",
    body: [
      <>Cryptocurrency wallet access or transfer patterns. Picks up <code>web3.eth.sendTransaction</code>, <code>ethers.Wallet</code> instantiation, and obvious function names (<code>drainTokens</code>, <code>drainWallet</code>).</>,
    ],
  },
  {
    sym: "Sh",
    name: "Reverse Shell",
    sev: "crit", pts: 35,
    layer: "Pattern",
    examples: "/bin/bash -i · /dev/tcp/ · mkfifo · nc -e",
    body: [
      <>Patterns used to open a remote shell back to an attacker: <code>/bin/bash -i</code>, redirections through <code>/dev/tcp/</code>, <code>mkfifo</code> pipelines, <code>nc -e /bin/</code>.</>,
    ],
  },
  {
    sym: "Net",
    name: "Network Exfiltration",
    sev: "high", pts: 20,
    layer: "Narrow allowlist",
    examples: ".onion · pastebin · webhook.site · ngrok · transfer.sh",
    body: [
      <>Outbound calls to known exfiltration services or hidden domains: <code>.onion</code>, pastebin, hastebin, requestbin, webhook.site, ngrok.io, transfer.sh, anonfiles, gofile, paste.ee, controlc.</>,
      <>Generic <code>fetch</code>, <code>axios</code>, and <code>require('http')</code> patterns produce too many false positives on legitimate API clients &mdash; not flagged here.</>,
    ],
  },
  {
    sym: "Tsq",
    name: "Typosquatting",
    sev: "high", pts: 20,
    layer: "Levenshtein · distance 1",
    examples: "lodahs · expres · axiios · reactt · vuee",
    body: [
      <>Package name within Levenshtein distance 1 of a popular package (lodash, express, axios, react, vue, …).</>,
      <>Distance-2 was tried originally but produced false positives on legitimate short names like <code>fecha</code> matching <code>mocha</code>.</>,
    ],
  },
  {
    sym: "Fs",
    name: "File System Access",
    sev: "high", pts: 20,
    layer: "Pattern",
    examples: "/etc/passwd · .aws/credentials · .kube/config",
    body: [
      <>Reads of OS-level sensitive paths: <code>/etc/passwd</code>, <code>/etc/shadow</code>, <code>.aws/credentials</code>, <code>.kube/config</code>, <code>.docker/config.json</code>.</>,
      <>Generic <code>../../</code> path traversal patterns are deliberately not flagged — they fire on every legit relative path in tests and fixtures.</>,
    ],
  },
];

function DetectorGrid() {
  const [active, setActive] = useState(0);
  const det = DETECTORS[active];

  return (
    <>
      <div className="detectors">
        {DETECTORS.map((d, i) => (
          <button
            key={i}
            className={`det ${i === active ? "is-active" : ""}`}
            onClick={() => setActive(i)}
            style={{ all: "unset", display: "flex" }}
          >
            <div className="det" style={{
              all: "unset",
              display: "flex", flexDirection: "column",
              padding: "22px 20px 18px",
              minHeight: 200,
              width: "100%", height: "100%",
              background: i === active ? "var(--bg-3)" : "var(--bg-2)",
              cursor: "pointer",
              boxShadow: i === active ? "inset 2px 0 0 0 var(--signal)" : "none",
            }}>
              <div className="top">
                <span className="sym">{String(i + 1).padStart(2, "0")} · {d.sym}</span>
                <span className={`sev ${d.sev}`}>{d.sev === "crit" ? "CRITICAL" : "HIGH"}</span>
              </div>
              <div className="name">{d.name}</div>
              <div className="lede">{d.examples}</div>
              <div className="pts">
                <span>+{d.pts} pts</span>
                <span>{d.layer}</span>
              </div>
            </div>
          </button>
        ))}
      </div>

      <div className="det-detail">
        <div className="l">
          <div className="lbl">DETECTOR</div>
          <div className="v">{String(active + 1).padStart(2, "0")} · {det.sym}</div>
          <div className="lbl">SEVERITY</div>
          <div className="v" style={{ color: det.sev === "crit" ? "var(--crit)" : "var(--high)" }}>
            {det.sev === "crit" ? "CRITICAL · +35" : "HIGH · +20"}
          </div>
          <div className="lbl">METHOD</div>
          <div className="v">{det.layer}</div>
          <div className="lbl">SIGNATURES</div>
          <div className="v" style={{ color: "var(--ink-dim)", fontSize: 11, lineHeight: 1.5 }}>
            {det.examples}
          </div>
        </div>
        <div className="r">
          <div style={{ fontFamily: "var(--sans)", fontSize: 24, fontWeight: 500, letterSpacing: "-.02em", marginBottom: 14 }}>
            {det.name}
          </div>
          {det.body.map((p, i) => <p key={i}>{p}</p>)}
        </div>
      </div>
    </>
  );
}

/* ============================================================
 * VERDICT GAUGE
 * ========================================================== */

const SAMPLE_PKGS = [
  {
    id: "react", name: "react@18.3.1", desc: "the obvious one",
    score: 0, verdict: "SAFE", sev: "v-safe",
    hits: [],
  },
  {
    id: "pino", name: "pino@9.5.0", desc: "logger w/ fast-json",
    score: 30, verdict: "REVIEW", sev: "v-review",
    hits: [
      { sev: "high", what: "Dynamic Code Compilation", layer: "AST · new Function", pts: 20 },
      { sev: "mod",  what: "Network Exfiltration",    layer: "Pattern · transfer.sh ref",     pts: 10 },
    ],
  },
  {
    id: "lodash", name: "lodash@4.17.20", desc: "5 known CVEs",
    score: 70, verdict: "BLOCKED", sev: "v-block",
    hits: [
      { sev: "high", what: "GHSA-35jh-r3h4-6jhm", layer: "OSV · Command Injection", pts: 20 },
      { sev: "high", what: "GHSA-r5fr-rjxr-66jc", layer: "OSV · Code Injection (_.template)", pts: 20 },
      { sev: "mod",  what: "GHSA-29mw-wpgm-hmr9", layer: "OSV · ReDoS",             pts: 10 },
      { sev: "mod",  what: "GHSA-f23m-r3pf-42rh", layer: "OSV · Prototype Pollution", pts: 10 },
      { sev: "mod",  what: "GHSA-xxjr-mmjv-4gpg", layer: "OSV · Prototype Pollution (_.unset)", pts: 10 },
    ],
  },
  {
    id: "uaparser", name: "ua-parser-js@0.7.29", desc: "confirmed malware (Oct 2021)",
    score: 95, verdict: "BLOCKED", sev: "v-block",
    hits: [
      { sev: "crit", what: "Install Script Abuse",   layer: "postinstall · jsextension exec",  pts: 35 },
      { sev: "crit", what: "Crypto Mining",          layer: "Pattern · stratum+tcp pool",       pts: 35 },
      { sev: "high", what: "Network Exfiltration",   layer: "Pattern · attacker C2 host",       pts: 20 },
      { sev: "low",  what: "Code Obfuscation",       layer: "Pattern · base64 payload",         pts: 5  },
    ],
  },
];

function VerdictLab() {
  const [pickIdx, setPickIdx] = useState(0);
  const pkg = SAMPLE_PKGS[pickIdx];
  const score = pkg.score;
  const pct = Math.min(100, score);
  const sevColor = pkg.sev === "v-safe" ? "var(--safe)"
                  : pkg.sev === "v-review" ? "var(--mod)"
                  : "var(--crit)";

  return (
    <div className="verdict-lab">
      {/* gauge side */}
      <div className="gauge-wrap">
        <div className="gauge-head">
          <span>RISK SCORE · 0–100</span>
          <span>verdict thresholds: 20 · 60</span>
        </div>

        <div className="gauge">
          <div className="zone zone-safe">SAFE</div>
          <div className="zone zone-review">REVIEW</div>
          <div className="zone zone-block">BLOCKED</div>
          <div className="ticks">
            {Array.from({ length: 10 }, (_, i) => <i key={i} />)}
          </div>
          <div className="needle" style={{ left: `calc(${pct}% - 1px)`, background: sevColor }}></div>
        </div>

        <div className="gauge-readout">
          <div className="score-big" style={{ color: sevColor }}>{score}</div>
          <div className="verdict-big" style={{ color: sevColor }}>{pkg.verdict}</div>
          <div className="pkg-info">
            <div><b>{pkg.name}</b></div>
            <div style={{ marginTop: 4 }}>{pkg.desc}</div>
          </div>
        </div>

        <div className="pkg-tabs">
          {SAMPLE_PKGS.map((p, i) => (
            <button
              key={p.id}
              className={`pkg-tab ${i === pickIdx ? "active" : ""}`}
              onClick={() => setPickIdx(i)}
            >
              <div className="nm">{p.name.split("@")[0]}</div>
              <div className="ds">SCORE {p.score}</div>
            </button>
          ))}
        </div>
      </div>

      {/* hits side */}
      <div className="hits">
        <div className="hits-head">
          <span>DETECTOR HITS · {pkg.hits.length}</span>
          <span>{pkg.name}</span>
        </div>
        <div className="hits-list">
          {pkg.hits.length === 0 ? (
            <div style={{ padding: "60px 24px", textAlign: "center" }}>
              <div className="mono" style={{ color: "var(--safe)", fontSize: 13, marginBottom: 8 }}>
                ✓ no detector hits
              </div>
              <div className="mono" style={{ color: "var(--ink-mute)", fontSize: 11 }}>
                clean across 11 detectors and the OSV feed
              </div>
            </div>
          ) : (
            pkg.hits.map((h, i) => (
              <div className="hit" key={i}>
                <span className={`sev ${h.sev}`}>
                  {h.sev === "crit" ? "CRITICAL" : h.sev === "high" ? "HIGH" : "MODERATE"}
                </span>
                <span className="what">
                  {h.what}
                  <span className="layer">· {h.layer}</span>
                </span>
                <span className="pts">+<b>{h.pts}</b></span>
              </div>
            ))
          )}
        </div>
        {pkg.hits.length > 0 && (
          <div className="hits-head" style={{ borderTop: "1px solid var(--rule)", borderBottom: "none" }}>
            <span>SUM</span>
            <span style={{ color: sevColor }}>+{score} → {pkg.verdict}</span>
          </div>
        )}
      </div>
    </div>
  );
}

/* ============================================================
 * CLI DEMO
 * ========================================================== */

const CLI_TABS = [
  {
    label: "phi audit",
    content: (
      <>
        <div><span className="pr">$</span> phi audit</div>
        <div className="dim">Phi  · secure package manager</div>
        <div className="dim">resolving dependency tree...</div>
        <div className="dim">scanning 1 packages...</div>
        {"\n"}
        <div>lodash@4.17.20  files=1047  score=<span className="er">70</span>  verdict=<span className="er">BLOCKED</span></div>
        <div>  - <span className="er">[HIGH]</span>     advisory GHSA-35jh-r3h4-6jhm — Command Injection in lodash</div>
        <div>  - <span className="er">[HIGH]</span>     advisory GHSA-r5fr-rjxr-66jc — Code Injection via <span className="b">_.template</span></div>
        <div>  - <span className="wn">[MODERATE]</span> advisory GHSA-29mw-wpgm-hmr9 — Regular Expression DoS</div>
        <div>  - <span className="wn">[MODERATE]</span> advisory GHSA-f23m-r3pf-42rh — Prototype Pollution via array path</div>
        <div>  - <span className="wn">[MODERATE]</span> advisory GHSA-xxjr-mmjv-4gpg — Prototype Pollution in <span className="b">_.unset</span></div>
        {"\n"}
        <div>audit: 1 scanned (<span className="ok">safe=0</span> <span className="wn">review=0</span> <span className="er">blocked=1</span>)</div>
        <div>  report: <span className="b">phi-report.json</span></div>
      </>
    ),
  },
  {
    label: "phi install",
    content: (
      <>
        <div><span className="pr">$</span> phi install</div>
        <div className="dim">Phi  · secure package manager</div>
        <div className="dim">resolving dependency tree...</div>
        <div className="dim">scanning 12 packages...</div>
        {"\n"}
        <div>lodash@4.17.20  files=1047  score=<span className="er">70</span>  verdict=<span className="er">BLOCKED</span></div>
        <div>  - <span className="er">[HIGH]</span>     advisory GHSA-35jh-r3h4-6jhm — Command Injection in lodash</div>
        <div>  - <span className="wn">[MODERATE]</span> advisory GHSA-29mw-wpgm-hmr9 — Regular Expression DoS</div>
        <div className="dim">  ...</div>
        {"\n"}
        <div className="er">phi: install aborted: 1 package(s) blocked; report written to phi-report.json</div>
        <div className="dim">      pin a safe version (lodash@^4.17.21) and re-run.</div>
      </>
    ),
  },
  {
    label: "phi do",
    content: (
      <>
        <div><span className="pr">$</span> phi do dev</div>
        {"\n"}
        <div><span className="dim">&gt;</span> my-app@0.1.0 dev</div>
        <div><span className="dim">&gt;</span> next dev</div>
        {"\n"}
        <div className="dim">  ▲ Next.js 15.0.3</div>
        <div className="dim">  - Local:   http://localhost:3000</div>
        <div className="dim">  - Network: http://192.168.1.42:3000</div>
        {"\n"}
        <div className="ok"> ✓ Ready in 1.4s</div>
        {"\n"}
        <div className="dim mono">node_modules/.bin is on PATH — installed CLIs work without npx.</div>
      </>
    ),
  },
  {
    label: "phi why",
    content: (
      <>
        <div><span className="pr">$</span> phi why lodash</div>
        <div>lodash@4.17.20</div>
        <div>  <span className="b">my-app</span> &gt; lodash</div>
        <div>  <span className="b">my-app</span> &gt; express-validator &gt; lodash.merge &gt; lodash</div>
      </>
    ),
  },
];

function CliDemo() {
  const [tab, setTab] = useState(0);
  return (
    <div className="cli">
      <div className="cli-head">
        <div className="tabs">
          {CLI_TABS.map((t, i) => (
            <span key={i} className={`tab ${i === tab ? "active" : ""}`} onClick={() => setTab(i)}>
              {t.label}
            </span>
          ))}
        </div>
        <span className="dim mono" style={{ color: "var(--ink-mute)" }}>~/projects/my-app</span>
      </div>
      <div className="cli-body">{CLI_TABS[tab].content}</div>
    </div>
  );
}

/* ============================================================
 * MOUNT
 * ========================================================== */

ReactDOM.createRoot(document.getElementById("scanner-mount")).render(<LiveScanner />);
ReactDOM.createRoot(document.getElementById("detectors-mount")).render(<DetectorGrid />);
ReactDOM.createRoot(document.getElementById("verdict-mount")).render(<VerdictLab />);
ReactDOM.createRoot(document.getElementById("cli-mount")).render(<CliDemo />);
