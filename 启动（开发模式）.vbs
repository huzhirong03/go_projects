' Excel 大文件工具 - 开发模式启动器
' 双击运行即可，无需任何命令行知识。
'
' 做什么：
'   1. 自动定位本文件所在目录（= 项目根目录）
'   2. 打开一个 PowerShell 窗口显示后端/前端日志（方便排错）
'   3. 启动 wails dev，几十秒后会自动弹出"Excel 大文件工具"窗口
'
' 想停止：关掉弹出的 GUI 窗口，或在 PowerShell 窗口里按 Ctrl+C。
' 移动项目：整个 excel-master 文件夹可以随便搬到别处，这个脚本仍然能用。

Option Explicit

Dim fso, shell, projDir, cmd, psScript, tempDir, tempFile, stream

Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")

' Locate project root = this script's own directory
projDir = fso.GetParentFolderName(WScript.ScriptFullName)

' Sanity check: wails.json must exist
If Not fso.FileExists(projDir & "\wails.json") Then
    MsgBox "wails.json not found." & vbCrLf & _
        "Put this launcher in the excel-master project root folder." & vbCrLf & vbCrLf & _
        "Current dir: " & projDir, vbCritical, "Launcher"
    WScript.Quit 1
End If

' Write a PowerShell script file in %TEMP%, then invoke it.
' This avoids any encoding issue with inline Chinese in the command line.
tempDir = shell.ExpandEnvironmentStrings("%TEMP%")
tempFile = tempDir & "\excel-master-launcher.ps1"

psScript = "$OutputEncoding = [System.Text.Encoding]::UTF8" & vbCrLf & _
    "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8" & vbCrLf & _
    "Set-Location -LiteralPath '" & projDir & "'" & vbCrLf & _
    "Write-Host '=== Excel 大文件工具 · 开发模式 ===' -ForegroundColor Cyan" & vbCrLf & _
    "Write-Host ('项目路径: ' + (Get-Location).Path) -ForegroundColor Gray" & vbCrLf & _
    "Write-Host '首次启动需 10-30 秒编译前端，GUI 窗口会自动弹出...' -ForegroundColor Yellow" & vbCrLf & _
    "Write-Host ''" & vbCrLf & _
    "wails dev"

' Write the script as UTF-8 with BOM so PowerShell keeps Chinese intact
Const adTypeBinary = 1
Const adTypeText = 2
Const adSaveCreateOverWrite = 2

Set stream = CreateObject("ADODB.Stream")
stream.Type = adTypeText
stream.Charset = "UTF-8"
stream.Open
stream.WriteText psScript
stream.SaveToFile tempFile, adSaveCreateOverWrite
stream.Close

' Launch PowerShell with the generated script. -NoExit keeps window open.
cmd = "powershell.exe -NoExit -ExecutionPolicy Bypass -File """ & tempFile & """"
shell.Run cmd, 1, False
