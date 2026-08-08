# One-click Windows exe build script.
# Usage:
#   powershell -ExecutionPolicy Bypass -File bin\build-exe.ps1
# Options:
#   -VersionTag <tag> : git tag prefix to describe (default: v0.1111*)
#   -NoFrontend       : skip frontend build (reuse existing web/dist)
#   -KeepVersion      : do not update VERSION file
#   -OutDir <dir>     : output directory (default: repo root)

param(
    [string]$VersionTag = "v0.1111*",
    [switch]$NoFrontend,
    [switch]$KeepVersion,
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

# 1. Resolve version from git describe (same as release.yml)
Write-Host "==> Resolving version (git describe --tags --match '$VersionTag') ..." -ForegroundColor Cyan
$version = git describe --tags --always --long --match $VersionTag
if ($LASTEXITCODE -ne 0 -or -not $version) {
    Write-Error "Failed to resolve version. Check that the git tag exists."
}
Write-Host "    Version: $version" -ForegroundColor Green

# 2. Update VERSION file
if (-not $KeepVersion) {
    Write-Host "==> Updating VERSION file ..." -ForegroundColor Cyan
    [System.IO.File]::WriteAllText((Join-Path $root "VERSION"), $version, (New-Object System.Text.UTF8Encoding $false))
}

# 3. Build frontend (web/dist is embedded into the exe)
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

# 4. Compile Go backend into exe
$exeName = "new-api_$version.exe"
$exePath = Join-Path $OutDir $exeName
Write-Host "==> Building exe: $exePath ..." -ForegroundColor Cyan
go mod download
go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$version'" -o $exePath
if ($LASTEXITCODE -ne 0) { Write-Error "Go build failed." }

# 5. Verify artifact
$file = Get-Item $exePath
Write-Host ""
Write-Host "Build done:" -ForegroundColor Green
Write-Host "  $($file.FullName)" -ForegroundColor Green
Write-Host "  Size: $([math]::Round($file.Length / 1MB, 2)) MB"
Write-Host "  Version: $version"
