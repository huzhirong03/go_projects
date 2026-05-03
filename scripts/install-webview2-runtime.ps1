# 把微软下载的 WebView2 Fixed Version cab 包解压到项目的 webview2_runtime/ 目录。
#
# 用法：
#   pwsh ./scripts/install-webview2-runtime.ps1 -CabPath "C:\Downloads\Microsoft.WebView2.FixedVersionRuntime.131.0.2903.99.x64.cab"
#
# 微软官方下载入口：
#   https://developer.microsoft.com/en-us/microsoft-edge/webview2/
#   下拉到 "Get the Fixed Version" → 选 x64 → 下载到本地
#
# 脚本流程：
#   1. 校验输入 cab 文件存在
#   2. 用 Windows 自带的 expand.exe 解压 cab 到临时目录
#   3. 把内层 Microsoft.WebView2.FixedVersionRuntime.X.Y.Z.x64 文件夹改名为 webview2_runtime/
#   4. 验证 msedgewebview2.exe 在解压后的根目录里
#
# 解压成功后，项目根目录会出现 webview2_runtime/ 文件夹（已加入 .gitignore）。
# 后续 wails build 自动用它作为 Fixed Version Runtime。

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, HelpMessage = "WebView2 Fixed Version cab 文件路径")]
    [string]$CabPath
)

$ErrorActionPreference = 'Stop'

# 项目根目录 = 脚本上一级
$projectRoot = Split-Path -Parent $PSScriptRoot
$targetDir   = Join-Path $projectRoot 'webview2_runtime'
$tmpDir      = Join-Path $projectRoot ('.wv2-extract-' + [Guid]::NewGuid().ToString('N'))

Write-Host "==== 安装 WebView2 Fixed Version Runtime ====" -ForegroundColor Cyan

# 1. 校验输入
if (-not (Test-Path -LiteralPath $CabPath)) {
    Write-Error "cab 文件不存在：$CabPath"
    exit 1
}
$cabSizeMB = [Math]::Round((Get-Item -LiteralPath $CabPath).Length / 1MB, 1)
Write-Host "源 cab：$CabPath ($cabSizeMB MB)"

# 2. 已存在的 runtime 处理：让用户确认覆盖
if (Test-Path -LiteralPath $targetDir) {
    Write-Host "已存在目标目录：$targetDir" -ForegroundColor Yellow
    $resp = Read-Host "覆盖? [y/N]"
    if ($resp -ne 'y' -and $resp -ne 'Y') {
        Write-Host "已取消" -ForegroundColor Yellow
        exit 0
    }
    Remove-Item -LiteralPath $targetDir -Recurse -Force
}

# 3. 解压 cab
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
Write-Host "解压到临时目录：$tmpDir"
$expandResult = & expand.exe $CabPath -F:* $tmpDir 2>&1
if ($LASTEXITCODE -ne 0) {
    Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host $expandResult
    Write-Error "expand.exe 解压失败 (exit=$LASTEXITCODE)"
    exit 1
}

# 4. cab 内层结构：Microsoft.WebView2.FixedVersionRuntime.X.Y.Z.x64\msedgewebview2.exe
#    用 Copy + Remove 比 Move-Item 更稳：
#    - Windows Defender 实时扫描新文件时会短暂锁定，Move-Item 会报 "访问被拒绝"
#    - Copy 不依赖源文件独占锁，成功后再清临时目录
Write-Host "移动解压内容到 webview2_runtime/（可能需要十几秒）..."
$inner = Get-ChildItem -LiteralPath $tmpDir -Directory `
    | Where-Object { $_.Name -like 'Microsoft.WebView2.FixedVersionRuntime.*' } `
    | Select-Object -First 1

function CopyWithRetry($src, $dst) {
    # robocopy: /E 递归含空目录, /NFL/NDL/NJH/NJS 静音, /R:3 /W:1 自动重试 3 次间隔 1 秒
    # robocopy 的 exit code 0-7 都算成功，>=8 才是真失败
    $null = & robocopy $src $dst /E /NFL /NDL /NJH /NJS /R:3 /W:1
    if ($LASTEXITCODE -ge 8) {
        throw "robocopy failed with exit code $LASTEXITCODE"
    }
    # PowerShell 的 $LASTEXITCODE 残留会污染后续检查，清零
    $global:LASTEXITCODE = 0
}

if (-not $inner) {
    # cab 解压后可能直接就是文件而非子文件夹，检查 msedgewebview2.exe 是否在 tmpDir 根下
    if (Test-Path (Join-Path $tmpDir 'msedgewebview2.exe')) {
        CopyWithRetry $tmpDir $targetDir
    } else {
        Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
        Write-Error "cab 解压后没找到 Microsoft.WebView2.FixedVersionRuntime.*.x64 文件夹，也没找到 msedgewebview2.exe，包格式异常"
        exit 1
    }
} else {
    CopyWithRetry $inner.FullName $targetDir
}
# 清理临时目录（如果某些文件仍被 Defender 锁，允许失败——后续跑一次就清干净）
Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue

# 5. 验证
$wv2Exe = Join-Path $targetDir 'msedgewebview2.exe'
if (-not (Test-Path -LiteralPath $wv2Exe)) {
    Write-Error "解压后未找到 msedgewebview2.exe，路径：$wv2Exe"
    exit 1
}
$totalSize = [Math]::Round(((Get-ChildItem -LiteralPath $targetDir -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Write-Host ""
Write-Host "==== 安装完成 ====" -ForegroundColor Green
Write-Host "目标：$targetDir"
Write-Host "大小：$totalSize MB"
Write-Host "msedgewebview2.exe 已就位"
Write-Host ""
Write-Host "下一步：跑 ./scripts/build-release.ps1 一键打包"
