# 精简 WebView2 Fixed Version Runtime，去掉办公场景用不到的内容。
#
# 跑一次就行。跑完的 webview2_runtime/ 直接用于打包。
# 重跑 install-webview2-runtime.ps1 会把完整版盖回来，如果需要还可以再瘦身。
#
# 精简项：
#   1. Locales/ 里 168 个语言包 → 只保留 zh-CN / en-US / en-GB（fallback）= 省约 115 MB
#   2. WidevineCdm/（视频 DRM，Excel 工具用不上）= 省约 19 MB
#   3. PdfPreview/（PDF 预览，用不上）= 省约 0.2 MB
#
# 总共省约 130+ MB，runtime 从 624 MB 降到约 490 MB，zip 从 280 MB 降到约 145 MB。

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$wv2Dir      = Join-Path $projectRoot 'webview2_runtime'

if (-not (Test-Path (Join-Path $wv2Dir 'msedgewebview2.exe'))) {
    Write-Error "找不到 webview2_runtime/msedgewebview2.exe，请先跑 install-webview2-runtime.ps1"
    exit 1
}

Write-Host "==== 精简 WebView2 Fixed Version Runtime ====" -ForegroundColor Cyan

function DirSizeMB($path) {
    if (-not (Test-Path $path)) { return 0 }
    $bytes = (Get-ChildItem $path -Recurse -File -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum
    return [Math]::Round($bytes / 1MB, 1)
}

$beforeMB = DirSizeMB $wv2Dir
Write-Host "精简前：$beforeMB MB"

# 1. 精简 Locales
$localesDir = Join-Path $wv2Dir 'Locales'
if (Test-Path $localesDir) {
    $keepLocales = @(
        'zh-CN.pak',      'copilot_overlay_strings_zh-CN.pak',
        'en-US.pak',      'copilot_overlay_strings_en-US.pak',
        'en-GB.pak',      'copilot_overlay_strings_en-GB.pak'
    )
    $deleted = 0; $freedBytes = 0
    Get-ChildItem $localesDir -File | ForEach-Object {
        if ($keepLocales -notcontains $_.Name) {
            $freedBytes += $_.Length
            Remove-Item $_.FullName -Force -ErrorAction SilentlyContinue
            $deleted++
        }
    }
    $freedMB = [Math]::Round($freedBytes / 1MB, 1)
    Write-Host "Locales 清理: 删除 $deleted 个文件, 省 $freedMB MB（保留 zh-CN / en-US / en-GB）"
}

# 2. 删 WidevineCdm（视频 DRM，Excel 应用用不上）
$widevineDir = Join-Path $wv2Dir 'WidevineCdm'
if (Test-Path $widevineDir) {
    $mb = DirSizeMB $widevineDir
    Remove-Item $widevineDir -Recurse -Force
    Write-Host "WidevineCdm/ 已删除（省 $mb MB）"
}

# 3. 删 PdfPreview（PDF 预览扩展，用不上）
$pdfDir = Join-Path $wv2Dir 'PdfPreview'
if (Test-Path $pdfDir) {
    $mb = DirSizeMB $pdfDir
    Remove-Item $pdfDir -Recurse -Force
    Write-Host "PdfPreview/ 已删除（省 $mb MB）"
}

$afterMB = DirSizeMB $wv2Dir
$savedMB = $beforeMB - $afterMB
Write-Host ""
Write-Host "==== 精简完成 ====" -ForegroundColor Green
Write-Host "精简前：$beforeMB MB"
Write-Host "精简后：$afterMB MB"
Write-Host "共省： $savedMB MB"
Write-Host ""
Write-Host "下一步：跑 ./scripts/build-release.ps1 -Mode fixed 重新打包"
