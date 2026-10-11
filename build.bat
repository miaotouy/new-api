@echo off
setlocal enabledelayedexpansion

pushd "%~dp0"

set "SKIP_WEB=0"
if /i "%~1"=="--skip-web" set "SKIP_WEB=1"

where git >nul 2>nul || (echo [ERROR] git not found in PATH & exit /b 1)
where go >nul 2>nul || (echo [ERROR] go not found in PATH & exit /b 1)

for /f "delims=" %%i in ('git describe --tags --match "v[0-9]*" 2^>nul') do set "VERSION=%%i"
if not defined VERSION (
	echo [ERROR] failed to resolve version via git describe
	exit /b 1
)

echo [INFO] version: %VERSION%

if "%SKIP_WEB%"=="1" (
	echo [INFO] skip web build ^(--skip-web^)
	goto :build_go
)

:build_web
where bun >nul 2>nul || (echo [ERROR] bun not found in PATH & exit /b 1)
echo [INFO] building web frontend...
pushd web
call bun install --frozen-lockfile || (popd & exit /b 1)
set "DISABLE_ESLINT_PLUGIN=true"
set "VITE_REACT_APP_VERSION=%VERSION%"
call bun run build || (popd & exit /b 1)
popd

:build_go
echo [INFO] building backend...
set "GOWORK=off"
set "CGO_ENABLED=0"
set "OUTPUT=new-api-%VERSION%.exe"
go build -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=%VERSION%" -o "%OUTPUT%" . || exit /b 1

echo [OK] output: %OUTPUT%
popd
endlocal
