# One-click Windows exe build script.
# Usage:
#   powershell -ExecutionPolicy Bypass -File bin\build-exe.ps1
# Options:
#   -VersionTag <tag> : git tag pattern to describe (default: v[0-9]*)
#   -NoFrontend       : skip frontend build (reuse existing web/dist)
#   -OutDir <dir>     : output directory (default: repo root)

param(
    [string]$VersionTag = "v[0-9]*",
    [switch]$NoFrontend,
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not $OutDir) {
    $OutDir = $root
}
if (Test-Path $OutDir) {
    $OutDir = (Resolve-Path $OutDir).Path
} else {
    New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
    $OutDir = (Resolve-Path $OutDir).Path
}

# 1. Resolve version from the same upstream-style tags used by release.yml.
Write-Host "==> Resolving version (git describe --tags --match '$VersionTag') ..." -ForegroundColor Cyan
$exactVersion = git tag --points-at HEAD --list $VersionTag | Select-Object -First 1
if ($LASTEXITCODE -eq 0 -and $exactVersion) {
    $version = $exactVersion
} else {
    $version = git describe --tags --always --long --match $VersionTag
}
if ($LASTEXITCODE -ne 0 -or -not $version) {
    Write-Error "Failed to resolve version. Check that the git tag exists."
}
Write-Host "    Version: $version" -ForegroundColor Green

# VERSION is intentionally not modified. Release/local builds derive the version
# from Git, while CI workflows provide VERSION explicitly where Docker needs it.

# 2. Build frontend (web/dist is embedded into the exe)
if (-not $NoFrontend) {
    Write-Host "==> Building frontend (bun run build) ..." -ForegroundColor Cyan
    Push-Location (Join-Path $root "web")
    try {
        bun install --frozen-lockfile
        $env:DISABLE_ESLINT_PLUGIN = "true"
        $env:VITE_REACT_APP_VERSION = $version
        bun run build
        if ($LASTEXITCODE -ne 0) { Write-Error "Frontend build failed." }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "==> Skipping frontend build (-NoFrontend)" -ForegroundColor Yellow
}

# 3. Compile Go backend into exe. Keep the name consistent with release.yml.
$exeName = "new-api-$version.exe"
$exePath = Join-Path $OutDir $exeName
Write-Host "==> Building exe: $exePath ..." -ForegroundColor Cyan
go mod download
go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$version'" -o $exePath
if ($LASTEXITCODE -ne 0) { Write-Error "Go build failed." }

# 4. Verify artifact
$file = Get-Item $exePath
Write-Host ""
Write-Host "Build done:" -ForegroundColor Green
Write-Host "  $($file.FullName)" -ForegroundColor Green
Write-Host "  Size: $([math]::Round($file.Length / 1MB, 2)) MB"
Write-Host "  Version: $version"
