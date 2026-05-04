' Excel Master - Dev Mode Launcher
' Double-click to start. No command line knowledge required.
'
' What it does:
'   1. Auto-locates this script's directory (= project root)
'   2. Runs "wails dev" silently in the background (no PowerShell window pops up)
'   3. After ~30 seconds, the GUI window opens automatically
'   4. All wails dev output is teed to dev.log next to wails.json
'
' To stop: close the GUI window, or kill wails processes via Task Manager.
'
' NOTE: comments are kept ASCII-only on purpose. Chinese in comments confuses
'       cscript/wscript on systems whose ANSI code page mangles the file.
Option Explicit

Dim fso, shell, projDir, cmd, psScript, tempDir, tempFile, stream, devLog

Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")

projDir = fso.GetParentFolderName(WScript.ScriptFullName)

If Not fso.FileExists(projDir & "\wails.json") Then
    MsgBox "wails.json not found in: " & projDir & vbCrLf & _
        "Place this launcher in the project root.", vbCritical, "Excel Master Launcher"
    WScript.Quit 1
End If

devLog = projDir & "\dev.log"
tempDir = shell.ExpandEnvironmentStrings("%TEMP%")
tempFile = tempDir & "\excel-master-launcher.ps1"

' Build the PowerShell script that wraps wails dev.
psScript = "$OutputEncoding = [System.Text.Encoding]::UTF8" & vbCrLf & _
    "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8" & vbCrLf & _
    "Set-Location -LiteralPath '" & projDir & "'" & vbCrLf & _
    "'=== Excel Master dev mode @ ' + (Get-Date) | Out-File '" & devLog & "' -Encoding utf8" & vbCrLf & _
    "wails dev *>> '" & devLog & "'"

' Save the script as UTF-8 (with BOM) so PowerShell keeps any Chinese intact.
Const adTypeText = 2
Const adSaveCreateOverWrite = 2

Set stream = CreateObject("ADODB.Stream")
stream.Type = adTypeText
stream.Charset = "UTF-8"
stream.Open
stream.WriteText psScript
stream.SaveToFile tempFile, adSaveCreateOverWrite
stream.Close

' Run PowerShell hidden (Run second arg = 0).
cmd = "powershell.exe -ExecutionPolicy Bypass -File """ & tempFile & """"
shell.Run cmd, 0, False
