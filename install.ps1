# phi installer for Windows.
#
# Usage:
#   iwr -useb https://phi.philtechs.org/install.ps1 | iex
#   # or pin a version:
#   $args = @('-Version','v0.1.0'); iwr -useb .../install.ps1 | iex

param(
    [string]$Version = '',
    [string]$InstallDir = "$env:LOCALAPPDATA\phi"
)

$ErrorActionPreference = 'Stop'
$repo = 'philtechs-org/phi'

# Show-DefenderBlockHelp prints a clear remediation message when Windows
# Defender's heuristic flags phi.exe — a common false positive on every
# unsigned Go binary (gh, cosign, goreleaser, etc.) because Defender's
# behavioral heuristics match patterns that real Go-based malware also
# uses. The install script verifies sha256 against checksums.txt before
# this point, so if execution reaches here, the bytes ARE the published
# release.
function Show-DefenderBlockHelp {
    param(
        [string]$InstallDir,
        [string]$Stage = 'execute',  # 'copy' (mid-install) or 'execute' (post-install verify)
        [string]$Target
    )
    Write-Host ''
    Write-Host '----------------------------------------------------------------'
    Write-Host 'WINDOWS DEFENDER FALSE POSITIVE'
    Write-Host '----------------------------------------------------------------'
    if ($Stage -eq 'copy') {
        Write-Host "Defender quarantined phi.exe before it could be installed."
    } else {
        Write-Host "Defender allowed install but blocks execution of phi.exe."
        Write-Host "The binary is on disk at: $Target"
    }
    Write-Host ''
    Write-Host 'This is a known false positive on unsigned Go binaries (same'
    Write-Host 'issue affects gh, cosign, and most Go-built CLIs). Phi is open'
    Write-Host 'source and the install script verified the binary sha256 matches'
    Write-Host 'checksums.txt — the bytes ARE the published release.'
    Write-Host ''
    Write-Host 'Unblock with one of these (run PowerShell as Administrator):'
    Write-Host ''
    Write-Host "  # Recommended: exempt the install dir"
    Write-Host "  Add-MpPreference -ExclusionPath `"$InstallDir`""
    if ($Stage -eq 'execute') {
        Write-Host ''
        Write-Host "  # OR one-shot unblock the binary"
        Write-Host "  Unblock-File `"$Target`""
    } else {
        Write-Host ''
        Write-Host "  # Then re-run the installer:"
        Write-Host "  iwr -useb https://phi.philtechs.org/install.ps1 | iex"
    }
    Write-Host ''
    Write-Host 'More info + how phi is reporting this to Microsoft:'
    Write-Host '  https://phi.philtechs.org/faq.html#windows-defender'
    Write-Host '----------------------------------------------------------------'
}

# Detect arch
switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $arch = 'x86_64' }
    default { Write-Error "phi-install: unsupported arch $($env:PROCESSOR_ARCHITECTURE)"; exit 1 }
}

# Resolve version
if (-not $Version) {
    Write-Host 'phi-install: querying latest release...'
    $latest = Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$repo/releases/latest"
    $Version = $latest.tag_name
    if (-not $Version) {
        Write-Error 'phi-install: could not determine latest release'
        exit 1
    }
}
$verNoV = $Version.TrimStart('v')

$archive = "phi_${verNoV}_Windows_${arch}.zip"
$url = "https://github.com/$repo/releases/download/$Version/$archive"
$sumsUrl = "https://github.com/$repo/releases/download/$Version/checksums.txt"

Write-Host "phi-install: downloading $archive"

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "phi-install-$([System.Guid]::NewGuid().Guid)")
try {
    Invoke-WebRequest -UseBasicParsing -Uri $url     -OutFile (Join-Path $tmp $archive)
    Invoke-WebRequest -UseBasicParsing -Uri $sumsUrl -OutFile (Join-Path $tmp 'checksums.txt')

    # Verify checksum
    $expected = (Get-Content (Join-Path $tmp 'checksums.txt') |
                 Where-Object { $_ -match "  $archive$" } |
                 ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    if (-not $expected) {
        Write-Error "phi-install: $archive not in checksums.txt"
        exit 1
    }
    $actual = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) {
        Write-Error "phi-install: checksum mismatch`n  expected: $expected`n  actual:   $actual"
        exit 1
    }
    Write-Host 'phi-install: checksum OK'

    # Extract + install
    Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir | Out-Null
    }

    # Copy can fail if Defender real-time scanning quarantines the binary
    # mid-write. Detect that explicitly so the user gets a useful message
    # instead of a generic "Access denied".
    try {
        Copy-Item -Path (Join-Path $tmp 'phi.exe') -Destination (Join-Path $InstallDir 'phi.exe') -Force
    } catch {
        if ($_.Exception.Message -match 'virus|potentially unwanted') {
            Show-DefenderBlockHelp -InstallDir $InstallDir -Stage 'copy'
            exit 1
        }
        throw
    }

    $target = Join-Path $InstallDir 'phi.exe'
    Write-Host "phi-install: installed phi $Version at $target"

    # PATH check
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$InstallDir*") {
        Write-Host ''
        Write-Host "Add $InstallDir to your PATH so 'phi' works in any shell. Run this once:"
        Write-Host ''
        Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$InstallDir`", 'User')"
        Write-Host ''
        Write-Host '(or open Settings -> System -> About -> Advanced system settings -> Environment Variables)'
    }

    # Final sanity check — invokes phi to confirm the binary works. This
    # is also the most common point at which Windows Defender's heuristic
    # blocks unsigned Go binaries (common across `gh`, `cosign`, etc., not
    # phi-specific). The binary's sha256 was already verified above; it
    # IS the real release. Detect, explain, exit clean.
    try {
        & $target version
    } catch {
        if ($_.Exception.Message -match 'virus|potentially unwanted') {
            Show-DefenderBlockHelp -InstallDir $InstallDir -Stage 'execute' -Target $target
            # Don't fail the install — the file is on disk, just blocked
            # from running. The user can fix this with one Defender command.
            exit 0
        }
        throw
    }
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
