' Excel 拆合大师 - 开发模式启动器
' 双击运行即可，无需任何命令行知识。
'
' 做什么：
'   1. 自动定位本文件所在目录（= 项目根目录）
'   2. 在后台静默跑 wails dev（不弹黑色 PowerShell 窗口）
'   3. 几十秒后自动弹出 "Excel 拆合大师" 窗口
'   4. 所有 wails dev 输出落到项目根的 dev.log（出错时可查）
'
' 想停止：关掉弹出的 GUI 窗口；或者用任务管理器结束 wails 进程
' 移动项目：整个 excel-master 文件夹可以随便搬到别处，这个脚本仍然能用。

Option Explicit

Dim fso, shell, projDir, cmd, psScript, tempDir, tempFile, stream, devLog

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

' dev.log lands next to wails.json so it gets shipped/cleaned with the project
devLog = projDir & "\dev.log"

' Write a PowerShell script file in %TEMP%, then invoke it.
' This avoids any encoding issue with inline Chinese in the command line.
tempDir = shell.ExpandEnvironmentStrings("%TEMP%")
tempFile = tempDir & "\excel-master-launcher.ps1"

' wails dev 输出 tee 到 dev.log；窗口本身被 .vbs 隐藏（shell.Run 第 2 参 = 0）
psScript = "$OutputEncoding = [System.Text.Encoding]::UTF8" & vbCrLf & _
    "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8" & vbCrLf & _
    "Set-Location -LiteralPath '" & projDir & "'" & vbCrLf & _
    "'=== Excel 拆合大师 · dev mode  ===' | Out-File '" & devLog & "' -Encoding utf8" & vbCrLf & _
    "wails dev *>> '" & devLog & "'"

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

' Launch PowerShell hidden (second arg = 0). 想看实时输出: tail -f dev.log 或 用 wails dev 自己跑
cmd = "powershell.exe -ExecutionPolicy Bypass -File """ & tempFile & """"
shell.Run cmd, 0, False
