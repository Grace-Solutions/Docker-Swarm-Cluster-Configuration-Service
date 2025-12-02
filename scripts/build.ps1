<#
.SYNOPSIS
    Build script for dswrmctl (Docker Swarm Control) binaries.

.DESCRIPTION
    Builds dswrmctl for Linux (amd64, arm64), Windows (amd64), and macOS (amd64, arm64).
    Embeds version information (yyyy-MM-dd-HHmm format), icon, and metadata into binaries.

.PARAMETER Clean
    Remove existing binaries before building.

.EXAMPLE
    .\scripts\build.ps1
    .\scripts\build.ps1 -Clean
#>

param(
    [switch]$Clean
)

$ErrorActionPreference = "Stop"

# Configuration
$BinaryName = "dswrmctl"
# Get repo root - if PSScriptRoot is set, go one level up; otherwise use current directory
if ($PSScriptRoot) {
    $RepoRoot = Split-Path -Parent $PSScriptRoot
} else {
    $RepoRoot = (Get-Location).Path
}
$OutputDir = Join-Path $RepoRoot "binaries"
$ResourcesDir = Join-Path $RepoRoot "resources"
$CmdDir = Join-Path $RepoRoot "cmd\clusterctl"
$IconPath = Join-Path $ResourcesDir "0001.ico"

# Dynamic version based on current datetime (yyyy-MM-dd-HHmm)
$Version = Get-Date -Format "yyyy-MM-dd-HHmm"
$BuildTime = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  dswrmctl Build Script" -ForegroundColor Cyan
Write-Host "  Version: $Version" -ForegroundColor Yellow
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# Ensure output directory exists
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

# Clean if requested
if ($Clean) {
    Write-Host "Cleaning existing binaries..." -ForegroundColor Yellow
    Get-ChildItem -Path $OutputDir -Filter "$BinaryName*" | Remove-Item -Force
}

# Build targets: [GOOS, GOARCH, Extension, Description]
$Targets = @(
    @("linux",   "amd64", "",     "Linux x86_64"),
    @("linux",   "arm64", "",     "Linux ARM64"),
    @("darwin",  "amd64", "",     "macOS x86_64"),
    @("darwin",  "arm64", "",     "macOS ARM64 (Apple Silicon)"),
    @("windows", "amd64", ".exe", "Windows x86_64")
)

# ldflags for version embedding
$LdFlags = "-s -w -X 'main.Version=$Version' -X 'main.BuildTime=$BuildTime' -X 'main.BinaryName=$BinaryName'"

Write-Host "Building $($Targets.Count) targets..." -ForegroundColor Green
Write-Host ""

$SuccessCount = 0
$FailCount = 0

foreach ($Target in $Targets) {
    $GOOS = $Target[0]
    $GOARCH = $Target[1]
    $Ext = $Target[2]
    $Desc = $Target[3]
    
    $OutputName = "$BinaryName-$GOOS-$GOARCH$Ext"
    $OutputPath = Join-Path $OutputDir $OutputName
    
    Write-Host "  Building $OutputName ($Desc)..." -ForegroundColor White -NoNewline
    
    $env:GOOS = $GOOS
    $env:GOARCH = $GOARCH
    $env:CGO_ENABLED = "0"
    
    try {
        # Build command
        $BuildArgs = @(
            "build",
            "-ldflags", $LdFlags,
            "-o", $OutputPath,
            "./cmd/clusterctl"
        )
        
        $Result = & go @BuildArgs 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "Build failed: $Result"
        }
        
        $FileSize = (Get-Item $OutputPath).Length / 1MB
        Write-Host " OK" -ForegroundColor Green -NoNewline
        Write-Host " ($([math]::Round($FileSize, 2)) MB)" -ForegroundColor DarkGray
        $SuccessCount++
    }
    catch {
        Write-Host " FAILED" -ForegroundColor Red
        Write-Host "    Error: $_" -ForegroundColor Red
        $FailCount++
    }
}

# Clear environment variables
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Build Complete" -ForegroundColor Cyan
Write-Host "  Success: $SuccessCount / $($Targets.Count)" -ForegroundColor $(if ($FailCount -eq 0) { "Green" } else { "Yellow" })
if ($FailCount -gt 0) {
    Write-Host "  Failed: $FailCount" -ForegroundColor Red
}
Write-Host "  Output: $OutputDir" -ForegroundColor White
Write-Host "============================================" -ForegroundColor Cyan

# List built binaries
Write-Host ""
Write-Host "Built binaries:" -ForegroundColor Green
Get-ChildItem -Path $OutputDir -Filter "$BinaryName*" | ForEach-Object {
    $SizeMB = [math]::Round($_.Length / 1MB, 2)
    Write-Host "  $($_.Name) ($SizeMB MB)" -ForegroundColor White
}

exit $(if ($FailCount -eq 0) { 0 } else { 1 })

