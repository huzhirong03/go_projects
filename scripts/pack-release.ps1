# pack-release.ps1
# One-click packaging for Excel Master release zip.
#
# Modes:
#   -Mode Full (default): base on existing fixed zip, replace exe and docs, keep
#                         webview2_runtime offline runtime. ~247MB, good for
#                         users without WebView2 installed.
#   -Mode Slim:           pack only exe + docs, no webview2. ~18MB, good for
#                         modern Win10/Win11 users (WebView2 preinstalled) or
#                         tech users.
#
# Usage:
#   powershell.exe -ExecutionPolicy Bypass -File scripts\pack-release.ps1
#   powershell.exe -ExecutionPolicy Bypass -File scripts\pack-release.ps1 -Version v1.0.1 -Mode Slim
#
# Prereqs:
#   1. build\bin\excel-master.exe must be the latest wails build output.
#   2. Full mode needs build\release\excel-master-*-fixed.zip as webview2 source.

param(
    [string]$Version = "v1.0.0",
    [ValidateSet("Full", "Slim")]
    [string]$Mode = "Full"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $root

$exePath = Join-Path $root "build\bin\excel-master.exe"
if (-not (Test-Path $exePath)) {
    Write-Host "[FAIL] exe not found: $exePath" -ForegroundColor Red
    Write-Host "       please run: wails build -clean -ldflags=`"-s -w`"" -ForegroundColor Yellow
    exit 1
}
$exeInfo = Get-Item $exePath
Write-Host "[OK] exe: $exePath" -ForegroundColor Green
$exeMB = [math]::Round($exeInfo.Length / 1MB, 2)
Write-Host "     size=$exeMB MB  built=$($exeInfo.LastWriteTime)" -ForegroundColor Gray

$docs = @("README.md", "CHANGELOG.md", "LICENSE") | Where-Object { Test-Path (Join-Path $root $_) }

$outDir = Join-Path $root "build\release"
if (-not (Test-Path $outDir)) {
    New-Item -ItemType Directory -Path $outDir | Out-Null
}
$suffix = if ($Mode -eq "Slim") { "-slim" } else { "" }
$outZip = Join-Path $outDir "excel-master-$Version$suffix.zip"

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

# Full mode prerequisite check - do this BEFORE cleaning target,
# otherwise we'd leave the user with no zip if the source is missing.
$sourceZip = $null
$wvRuntimeDir = $null
if ($Mode -eq "Full") {
    $sourceZip = Get-ChildItem -Path $outDir -Filter "excel-master-*-fixed.zip" |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (-not $sourceZip) {
        # Fallback: pack directly from webview2_runtime/ at repo root
        $wvRuntimeDir = Join-Path $root "webview2_runtime"
        if (-not (Test-Path $wvRuntimeDir)) {
            Write-Host "[FAIL] Full mode needs one of:" -ForegroundColor Red
            Write-Host "       - build\release\excel-master-*-fixed.zip (preferred), or" -ForegroundColor Yellow
            Write-Host "       - webview2_runtime\ directory at repo root (fallback)" -ForegroundColor Yellow
            exit 1
        }
        Write-Host "[INFO] no source zip found; will pack webview2_runtime\ directly" -ForegroundColor Cyan
    }
}

if (Test-Path $outZip) {
    Write-Host "[CLEAN] removing existing $outZip" -ForegroundColor Yellow
    Remove-Item $outZip -Force
}

if ($Mode -eq "Full") {
    if ($sourceZip) {
        Write-Host "[MODE] Full: cloning webview2_runtime from $($sourceZip.Name)" -ForegroundColor Cyan
        Copy-Item $sourceZip.FullName $outZip -Force

        $zip = [System.IO.Compression.ZipFile]::Open($outZip, [System.IO.Compression.ZipArchiveMode]::Update)
        try {
            $targets = @("excel-master.exe") + $docs
            $toDelete = @($zip.Entries | Where-Object { $targets -contains $_.FullName })
            foreach ($e in $toDelete) {
                Write-Host "       - replacing $($e.FullName)" -ForegroundColor Gray
                $e.Delete()
            }
            [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $exePath, "excel-master.exe") | Out-Null
            foreach ($doc in $docs) {
                [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, (Join-Path $root $doc), $doc) | Out-Null
            }
        } finally {
            $zip.Dispose()
        }
    } else {
        Write-Host "[MODE] Full (fallback): packing exe + docs + webview2_runtime\" -ForegroundColor Cyan
        $zip = [System.IO.Compression.ZipFile]::Open($outZip, [System.IO.Compression.ZipArchiveMode]::Create)
        try {
            [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $exePath, "excel-master.exe") | Out-Null
            foreach ($doc in $docs) {
                [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, (Join-Path $root $doc), $doc) | Out-Null
            }
            $rootLen = $root.Length + 1
            $files = Get-ChildItem -Path $wvRuntimeDir -Recurse -File
            $count = 0
            foreach ($f in $files) {
                $rel = $f.FullName.Substring($rootLen) -replace '\\', '/'
                [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $f.FullName, $rel) | Out-Null
                $count++
                if ($count % 20 -eq 0) { Write-Host "       packed $count / $($files.Count) runtime files" -ForegroundColor Gray }
            }
            Write-Host "       packed $count runtime files" -ForegroundColor Gray
        } finally {
            $zip.Dispose()
        }
    }
} else {
    Write-Host "[MODE] Slim: exe + docs only (no webview2_runtime)" -ForegroundColor Cyan
    $zip = [System.IO.Compression.ZipFile]::Open($outZip, [System.IO.Compression.ZipArchiveMode]::Create)
    try {
        [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, $exePath, "excel-master.exe") | Out-Null
        foreach ($doc in $docs) {
            [System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, (Join-Path $root $doc), $doc) | Out-Null
        }
    } finally {
        $zip.Dispose()
    }
}

$outInfo = Get-Item $outZip
$outMB = [math]::Round($outInfo.Length / 1MB, 2)
Write-Host ""
Write-Host "[DONE] $outZip" -ForegroundColor Green
Write-Host "       size=$outMB MB" -ForegroundColor Gray
Write-Host ""
