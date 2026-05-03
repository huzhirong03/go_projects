# 一键打包 Excel 拆合大师为发布产物。
#
# 用法：
#   pwsh ./scripts/build-release.ps1                 # embed 模式（默认）：单 exe + 文档，zip 打包
#   pwsh ./scripts/build-release.ps1 -Mode embed     # 同上：WebView2 installer 内嵌进 exe，首次启动静默装
#   pwsh ./scripts/build-release.ps1 -Mode fixed     # Fixed Version 模式：exe + webview2_runtime/ 文件夹（真绿色）
#
# 两种模式对比：
#   embed（默认）：
#     - 学员体验：单 exe 双击 → 首次静默装 WebView2 到系统 → 后续直接跑
#     - exe 大约 140 MB（含 installer），zip 压缩后约 50 MB
#     - 缺点：会写 %LOCALAPPDATA%\Microsoft\EdgeWebView，不是真绿色
#
#   fixed：
#     - 学员体验：解压 zip → 双击 exe，零写系统
#     - 体积：exe ~20 MB + webview2_runtime/ ~150 MB，zip 压缩后 ~70 MB
#     - 需要先跑 ./scripts/install-webview2-runtime.ps1 准备 runtime
#
# 产物：build/release/excel-master-vX.Y.Z-{mode}.zip

[CmdletBinding()]
param(
    [ValidateSet('embed', 'fixed')]
    [string]$Mode = 'embed'
)

$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$buildBin    = Join-Path $projectRoot 'build\bin'
$wv2Dir      = Join-Path $projectRoot 'webview2_runtime'
$releaseDir  = Join-Path $projectRoot 'build\release'

Write-Host "==== 打包 Excel 拆合大师（mode = $Mode）====" -ForegroundColor Cyan
Set-Location $projectRoot

# 1. 校验 wails
$null = & wails version 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Error "wails 命令未找到。请先 'go install github.com/wailsapp/wails/v2/cmd/wails@latest'"
    exit 1
}

# 2. fixed 模式额外校验：webview2_runtime 必须已就位
if ($Mode -eq 'fixed') {
    if (-not (Test-Path (Join-Path $wv2Dir 'msedgewebview2.exe'))) {
        Write-Host "找不到 webview2_runtime/msedgewebview2.exe" -ForegroundColor Yellow
        Write-Host "请先跑：./scripts/install-webview2-runtime.ps1 -CabPath <下载的 cab 文件>"
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

# 5. wails build：根据 mode 选择 -webview2 参数
#    - embed：installer 内嵌进 exe
#    - fixed：浏览器路径模式，exe 在运行时通过 main.go 的 webview2FixedRuntimePath 找到 runtime
$wv2Param = @{
    embed = 'embed'
    fixed = 'browser'
}[$Mode]

Write-Host ""
Write-Host "==== wails build -webview2 $wv2Param ====" -ForegroundColor Cyan
& wails build -clean -platform windows/amd64 -webview2 $wv2Param -ldflags "-s -w"
if ($LASTEXITCODE -ne 0) {
    Write-Error "wails build 失败 (exit=$LASTEXITCODE)"
    exit 1
}

# 6. fixed 模式：拷 webview2_runtime 到 bin 旁
if ($Mode -eq 'fixed') {
    $binWv2 = Join-Path $buildBin 'webview2_runtime'
    Write-Host ""
    Write-Host "==== 拷贝 webview2_runtime → build/bin/ ====" -ForegroundColor Cyan
    Copy-Item -Path $wv2Dir -Destination $binWv2 -Recurse -Force
    $wv2Mb = [Math]::Round(((Get-ChildItem $binWv2 -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
    Write-Host "WebView2 runtime: $wv2Mb MB"
}

# 7. 拷文档到 bin 旁，跟 exe 一起进 zip
Write-Host ""
Write-Host "==== 拷贝 LICENSE / README / CHANGELOG ====" -ForegroundColor Cyan
Copy-Item (Join-Path $projectRoot 'LICENSE')      $buildBin -Force
Copy-Item (Join-Path $projectRoot 'README.md')    $buildBin -Force
Copy-Item (Join-Path $projectRoot 'CHANGELOG.md') $buildBin -Force

# 8. 报告 exe 体积
$exePath = Join-Path $buildBin 'excel-master.exe'
if (Test-Path $exePath) {
    $exeMb = [Math]::Round(((Get-Item $exePath).Length / 1MB), 1)
    Write-Host "excel-master.exe: $exeMb MB"
}

# 9. 打 zip
$zipName = "excel-master-v$version-$Mode.zip"
$zipPath = Join-Path $releaseDir $zipName
Write-Host ""
Write-Host "==== 压缩为 $zipName ====" -ForegroundColor Cyan

# Compress-Archive 在大目录上慢，用 .NET ZipFile 更快
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
if ($Mode -eq 'embed') {
    Write-Host "学员使用：解压 zip → 双击 excel-master.exe（首次启动会装 WebView2，几秒后自动好）"
} else {
    Write-Host "学员使用：解压 zip → 双击 excel-master.exe（零写系统，整个文件夹可移动）"
}
