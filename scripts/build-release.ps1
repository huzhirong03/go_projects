# 一键打包 Excel 拆合大师为可分发的绿色 zip。
#
# 用法：
#   pwsh ./scripts/build-release.ps1
#   pwsh ./scripts/build-release.ps1 -SkipWebView2  # 不打包 WebView2 runtime（让用户依赖系统）
#
# 流程：
#   1. 校验前置条件（wails 命令、webview2_runtime 目录可选）
#   2. wails build 编译 exe
#   3. 把 webview2_runtime/ 整个拷贝到 build/bin/ 旁边
#   4. 拷 LICENSE / README / CHANGELOG 到 build/bin/
#   5. 打成 excel-master-vX.Y.Z.zip
#
# 产物：build/release/excel-master-vX.Y.Z.zip
#
# 学员只需：解压 zip → 双击 excel-master.exe（无需安装 WebView2、无需联网）

[CmdletBinding()]
param(
    [switch]$SkipWebView2 = $false
)

$ErrorActionPreference = 'Stop'

$projectRoot   = Split-Path -Parent $PSScriptRoot
$buildBin      = Join-Path $projectRoot 'build\bin'
$wv2Dir        = Join-Path $projectRoot 'webview2_runtime'
$releaseDir    = Join-Path $projectRoot 'build\release'

Write-Host "==== 打包 Excel 拆合大师 ====" -ForegroundColor Cyan
Set-Location $projectRoot

# 1. 校验 wails
$null = & wails version 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Error "wails 命令未找到。请先 'go install github.com/wailsapp/wails/v2/cmd/wails@latest'"
    exit 1
}

# 2. 校验 WebView2 runtime
if (-not $SkipWebView2) {
    if (-not (Test-Path (Join-Path $wv2Dir 'msedgewebview2.exe'))) {
        Write-Host "找不到 webview2_runtime/msedgewebview2.exe" -ForegroundColor Yellow
        Write-Host "请先跑：./scripts/install-webview2-runtime.ps1 -CabPath <下载的 cab 文件>"
        Write-Host "或加 -SkipWebView2 参数跳过（学员需自行预装 WebView2）"
        exit 1
    }
}

# 3. 从 internal/core/version.go 提取版本号
$verLine = Select-String -Path (Join-Path $projectRoot 'internal\core\version.go') -Pattern 'Version\s*=\s*"([^"]+)"'
if (-not $verLine) {
    Write-Error "无法从 internal/core/version.go 解析 Version 常量"
    exit 1
}
$version = ($verLine.Matches[0].Groups[1].Value).TrimStart('v')
Write-Host "版本号：v$version"

# 4. 清理旧产物
if (Test-Path $buildBin)   { Remove-Item $buildBin   -Recurse -Force }
if (Test-Path $releaseDir) { Remove-Item $releaseDir -Recurse -Force }
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null

# 5. wails build
Write-Host ""
Write-Host "==== wails build ====" -ForegroundColor Cyan
& wails build -clean -platform windows/amd64 -ldflags "-s -w"
if ($LASTEXITCODE -ne 0) {
    Write-Error "wails build 失败 (exit=$LASTEXITCODE)"
    exit 1
}

# 6. 拷 WebView2 runtime 到 bin 旁
if (-not $SkipWebView2) {
    $binWv2 = Join-Path $buildBin 'webview2_runtime'
    Write-Host ""
    Write-Host "==== 拷贝 webview2_runtime → build/bin/ ====" -ForegroundColor Cyan
    Copy-Item -Path $wv2Dir -Destination $binWv2 -Recurse -Force
    $wv2Mb = [Math]::Round(((Get-ChildItem $binWv2 -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
    Write-Host "WebView2 runtime: $wv2Mb MB"
}

# 7. 拷文档
Write-Host ""
Write-Host "==== 拷贝 LICENSE / README / CHANGELOG ====" -ForegroundColor Cyan
Copy-Item (Join-Path $projectRoot 'LICENSE')      $buildBin -Force
Copy-Item (Join-Path $projectRoot 'README.md')    $buildBin -Force
Copy-Item (Join-Path $projectRoot 'CHANGELOG.md') $buildBin -Force

# 8. 打 zip
$zipName = "excel-master-v$version.zip"
$zipPath = Join-Path $releaseDir $zipName
Write-Host ""
Write-Host "==== 压缩为 $zipName ====" -ForegroundColor Cyan

# Compress-Archive 在大文件夹上慢，用 .NET ZipFile 更快
Add-Type -AssemblyName System.IO.Compression.FileSystem
[IO.Compression.ZipFile]::CreateFromDirectory(
    $buildBin,
    $zipPath,
    [IO.Compression.CompressionLevel]::Optimal,
    $false  # 不在 zip 里多套一层 bin/ 目录
)

$zipMb = [Math]::Round(((Get-Item $zipPath).Length / 1MB), 1)
Write-Host ""
Write-Host "==== 打包完成 ====" -ForegroundColor Green
Write-Host "产物：$zipPath"
Write-Host "大小：$zipMb MB"
Write-Host ""
Write-Host "学员使用方式：解压 zip → 双击 excel-master.exe"
