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
    Copy-Item -Path (Join-Path $tmp 'phi.exe') -Destination (Join-Path $InstallDir 'phi.exe') -Force

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

    & $target version
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
