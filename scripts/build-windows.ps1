<#
.SYNOPSIS
    Build GoogleTakeoutFixer binaries on Windows.

.DESCRIPTION
    Locates GCC (via scoop mingw or PATH) and compiles both the GUI and CLI
    binaries using CGO. Requires Go and mingw to be installed.

    Install prerequisites:
        scoop install go mingw

.PARAMETER Target
    Which binary to build: "gui", "cli", or "all" (default).

.PARAMETER OutDir
    Directory to write the binaries to. Defaults to the project root.

.EXAMPLE
    .\scripts\build-windows.ps1

.EXAMPLE
    .\scripts\build-windows.ps1 -Target cli
#>
param(
    [ValidateSet("gui","cli","all")]
    [string]$Target = "all",

    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot

if ($OutDir -eq "") { $OutDir = $projectRoot }

# --- Locate GCC ---
$gcc = Get-Command gcc -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
if (-not $gcc) {
    # Try scoop mingw
    $scoopMingw = Join-Path $env:USERPROFILE "scoop\apps\mingw\current\bin\gcc.exe"
    if (Test-Path $scoopMingw) {
        $gcc = $scoopMingw
    }
}
if (-not $gcc) {
    Write-Host "[ERROR] GCC not found. Install via: scoop install mingw" -ForegroundColor Red
    exit 1
}
Write-Host "GCC : $gcc" -ForegroundColor Gray

# --- Locate Go ---
$go = Get-Command go -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source
if (-not $go) {
    Write-Host "[ERROR] Go not found. Install via: scoop install go" -ForegroundColor Red
    exit 1
}
$goVersion = & $go version
Write-Host "Go  : $goVersion" -ForegroundColor Gray
Write-Host "Out : $OutDir" -ForegroundColor Gray
Write-Host ""

$env:CGO_ENABLED = "1"
$env:CC = $gcc

function Invoke-Build {
    param([string]$name, [string]$outFile, [string]$ldflags)

    Write-Host "Building $name..." -ForegroundColor Cyan
    $start = Get-Date
    & $go build -ldflags $ldflags -o $outFile ./cmd/main.go
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[ERROR] Build failed for $name (exit $LASTEXITCODE)" -ForegroundColor Red
        exit $LASTEXITCODE
    }
    $elapsed = [math]::Round(((Get-Date) - $start).TotalSeconds, 1)
    $size = [math]::Round((Get-Item $outFile).Length / 1MB, 1)
    Write-Host "  -> $outFile ($size MB, ${elapsed}s)" -ForegroundColor Green
}

Set-Location $projectRoot

if ($Target -eq "gui" -or $Target -eq "all") {
    Invoke-Build -name "GoogleTakeoutFixer.exe (GUI)" `
                 -outFile (Join-Path $OutDir "GoogleTakeoutFixer.exe") `
                 -ldflags "-s -w -H=windowsgui"
}

if ($Target -eq "cli" -or $Target -eq "all") {
    Invoke-Build -name "GoogleTakeoutFixerCLI.exe (CLI)" `
                 -outFile (Join-Path $OutDir "GoogleTakeoutFixerCLI.exe") `
                 -ldflags "-s -w"
}

Write-Host ""
Write-Host "Done." -ForegroundColor Green
