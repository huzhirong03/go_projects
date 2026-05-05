Attribute VB_Name = "ExtractByKeyword"
Option Explicit

' ====================================================================
' Excel 拆合大师 · VBA 性能对比脚本
'
' 用途：跟 Go 程序的"批量提取-单文件"模式做性能对比。
' 行为对齐：
'   · 关键词包含匹配（等价于 Go 程序勾选"精准+包含"两个匹配模式）
'   · 处理 ActiveWorkbook 的所有 Sheet
'   · 跳过第 1 行（表头）
'   · 任一单元格包含关键词 → 整行命中
'   · 输出表头 + 所有命中行
'
' 性能优化：
'   · 关 ScreenUpdating / Calculation / EnableEvents
'   · 一次 .Value 读整个 UsedRange 进二维数组（不逐 cell 读）
'   · 命中行批量数组写（不逐 cell 写）
'
' 用法：
'   1. 打开你要测试的源 xlsx（必须先打开它，宏处理的是 ActiveWorkbook）
'   2. Alt+F11 打开 VBA 编辑器
'   3. 文件 → 导入文件 → 选这个 .bas
'      或：直接复制全文粘贴到一个新模块里
'   4. F5 跑 ExtractByKeyword 子过程
'   5. 弹窗 1：输入关键词（如"汉族"）
'   6. 弹窗 2：选输出方式（是=新文件 / 否=写回源文件新 Sheet）
'   7. 完成后弹窗显示总耗时、扫描行数、命中行数
' ====================================================================

Public Sub ExtractByKeyword()
    Dim t0 As Double
    t0 = Timer

    ' --- 1. 输入关键词 ---
    Dim kw As String
    kw = InputBox( _
        "请输入要搜索的关键词（包含匹配）：" & vbCrLf & vbCrLf & _
        "  · 会扫描所有 Sheet 的所有列" & vbCrLf & _
        "  · 跳过第 1 行（表头）" & vbCrLf & _
        "  · 任一单元格包含此关键词的整行都会被提取", _
        "Excel 关键词提取 · VBA 性能测试")
    If Trim(kw) = "" Then
        MsgBox "未输入关键词，已取消。", vbInformation
        Exit Sub
    End If

    ' --- 2. 选输出方式 ---
    Dim choice As VbMsgBoxResult
    choice = MsgBox( _
        "结果要输出到哪里？" & vbCrLf & vbCrLf & _
        "  【是】 = 创建新 xlsx 文件（不动原文件）" & vbCrLf & _
        "  【否】 = 写回源文件，新建一个 Sheet" & vbCrLf & _
        "  【取消】 = 不跑了", _
        vbYesNoCancel + vbQuestion, _
        "选择输出方式")
    If choice = vbCancel Then
        MsgBox "已取消。", vbInformation
        Exit Sub
    End If
    Dim writeBack As Boolean
    writeBack = (choice = vbNo)

    ' --- 3. 性能开关 ---
    Dim oldScreen As Boolean: oldScreen = Application.ScreenUpdating
    Dim oldCalc As XlCalculation: oldCalc = Application.Calculation
    Dim oldEvents As Boolean: oldEvents = Application.EnableEvents
    Application.ScreenUpdating = False
    Application.Calculation = xlCalculationManual
    Application.EnableEvents = False

    On Error GoTo Cleanup

    Dim srcWb As Workbook
    Set srcWb = ActiveWorkbook
    If srcWb Is Nothing Then
        MsgBox "找不到当前工作簿（ActiveWorkbook）。请先打开源 xlsx 再运行。", vbExclamation
        GoTo Cleanup
    End If

    Dim sheetName As String
    sheetName = SanitizeSheetName("搜索_" & kw)

    Dim outWb As Workbook
    Dim outSheet As Worksheet
    If writeBack Then
        Set outWb = srcWb
        DeleteSheetIfExists outWb, sheetName
        Set outSheet = outWb.Sheets.Add(After:=outWb.Sheets(outWb.Sheets.Count))
        outSheet.Name = sheetName
    Else
        Set outWb = Application.Workbooks.Add
        Set outSheet = outWb.Sheets(1)
        outSheet.Name = sheetName
    End If

    ' --- 4. 扫描所有 Sheet 收集命中行 ---
    Dim totalScanned As Long, totalHits As Long, sheetCount As Long
    Dim headerWritten As Boolean: headerWritten = False
    Dim outRow As Long: outRow = 1

    Dim ws As Worksheet
    For Each ws In srcWb.Worksheets
        ' 跳过我们刚加的输出 Sheet 自己（writeBack 时）
        If writeBack And ws.Name = sheetName Then GoTo NextSheet

        Dim used As Range
        Set used = ws.UsedRange
        If used Is Nothing Then GoTo NextSheet
        If used.Rows.Count < 2 Or used.Columns.Count < 1 Then GoTo NextSheet

        sheetCount = sheetCount + 1

        ' 一次性读 .Value 到二维数组（性能关键）
        Dim data As Variant
        data = used.Value
        ' 单 cell 时 used.Value 不是数组，跳过
        If Not IsArray(data) Then GoTo NextSheet

        Dim nRows As Long, nCols As Long
        nRows = UBound(data, 1)
        nCols = UBound(data, 2)

        ' 写表头（仅首次；以 used 的第 1 行作为表头）
        If Not headerWritten Then
            Dim hdr As Variant
            ReDim hdr(1 To 1, 1 To nCols)
            Dim c As Long
            For c = 1 To nCols
                hdr(1, c) = data(1, c)
            Next c
            outSheet.Range(outSheet.Cells(outRow, 1), outSheet.Cells(outRow, nCols)).Value = hdr
            outRow = outRow + 1
            headerWritten = True
        End If

        ' 扫描第 2 行起，命中行先存到 hitBuf
        Dim hitBuf() As Variant
        ReDim hitBuf(1 To nRows, 1 To nCols)
        Dim hitCount As Long: hitCount = 0
        Dim r As Long
        For r = 2 To nRows
            totalScanned = totalScanned + 1
            Dim hit As Boolean: hit = False
            For c = 1 To nCols
                If Not IsEmpty(data(r, c)) Then
                    If InStr(1, CStr(data(r, c)), kw, vbBinaryCompare) > 0 Then
                        hit = True
                        Exit For
                    End If
                End If
            Next c
            If hit Then
                hitCount = hitCount + 1
                For c = 1 To nCols
                    hitBuf(hitCount, c) = data(r, c)
                Next c
            End If
        Next r

        ' 命中行整块批量写（性能关键）
        If hitCount > 0 Then
            Dim wbuf As Variant
            ReDim wbuf(1 To hitCount, 1 To nCols)
            Dim rr As Long
            For rr = 1 To hitCount
                For c = 1 To nCols
                    wbuf(rr, c) = hitBuf(rr, c)
                Next c
            Next rr
            outSheet.Range(outSheet.Cells(outRow, 1), outSheet.Cells(outRow + hitCount - 1, nCols)).Value = wbuf
            outRow = outRow + hitCount
            totalHits = totalHits + hitCount
        End If

NextSheet:
    Next ws

    ' --- 5. 保存输出 ---
    Dim savedPath As String: savedPath = ""
    If writeBack Then
        ' 写回模式：让用户决定要不要保存（不强制 Save，避免锁文件）
        ' 提示用户手动 Ctrl+S 保存
    Else
        Dim defaultName As String
        defaultName = TrimExt(srcWb.Name) & "_VBA结果_" & Format(Now, "yyyymmdd_hhmmss") & ".xlsx"
        Dim chosen As Variant
        chosen = Application.GetSaveAsFilename( _
            InitialFileName:=defaultName, _
            FileFilter:="Excel 文件 (*.xlsx), *.xlsx", _
            Title:="保存提取结果")
        If TypeName(chosen) = "String" And chosen <> "False" Then
            outWb.SaveAs CStr(chosen), FileFormat:=xlOpenXMLWorkbook
            savedPath = CStr(chosen)
        End If
    End If

Cleanup:
    Application.ScreenUpdating = oldScreen
    Application.Calculation = oldCalc
    Application.EnableEvents = oldEvents

    Dim elapsed As Double
    elapsed = Timer - t0

    Dim mode As String
    If writeBack Then mode = "源文件新 Sheet（请记得 Ctrl+S 保存）" Else mode = "新文件"

    Dim msg As String
    msg = "========== VBA 性能测试报告 ==========" & vbCrLf & vbCrLf & _
          "关键词：    " & kw & vbCrLf & _
          "处理 Sheet：" & sheetCount & " 个" & vbCrLf & _
          "扫描行数：  " & Format(totalScanned, "#,##0") & vbCrLf & _
          "命中行数：  " & Format(totalHits, "#,##0") & vbCrLf & _
          "总耗时：    " & Format(elapsed, "0.00") & " 秒" & vbCrLf & vbCrLf & _
          "输出方式：  " & mode & vbCrLf & _
          "Sheet 名：  " & sheetName
    If savedPath <> "" Then
        msg = msg & vbCrLf & "保存到：    " & savedPath
    End If

    If Err.Number <> 0 Then
        msg = msg & vbCrLf & vbCrLf & "⚠ 异常：" & Err.Description & "（错误号 " & Err.Number & "）"
    End If

    MsgBox msg, vbInformation, "完成"
End Sub

' --- 工具函数 ---

' 清理 Sheet 名里 Excel 不允许的字符 + 截到 31 字
Private Function SanitizeSheetName(s As String) As String
    Dim r As String: r = s
    Dim bad As String: bad = "\/?*[]:"
    Dim i As Long
    For i = 1 To Len(bad)
        r = Replace(r, Mid(bad, i, 1), "_")
    Next i
    If Len(r) > 31 Then r = Left(r, 31)
    SanitizeSheetName = r
End Function

' 删除已存在的同名 Sheet
Private Sub DeleteSheetIfExists(wb As Workbook, name As String)
    Dim ws As Worksheet
    For Each ws In wb.Worksheets
        If ws.name = name Then
            Application.DisplayAlerts = False
            ws.Delete
            Application.DisplayAlerts = True
            Exit Sub
        End If
    Next ws
End Sub

' 去掉文件名末尾的 .xlsx / .xlsm / .xlsb / .csv
Private Function TrimExt(s As String) As String
    Dim lower As String: lower = LCase(s)
    Dim exts As Variant: exts = Array(".xlsx", ".xlsm", ".xlsb", ".csv")
    Dim i As Long
    For i = LBound(exts) To UBound(exts)
        If Right(lower, Len(exts(i))) = exts(i) Then
            TrimExt = Left(s, Len(s) - Len(exts(i)))
            Exit Function
        End If
    Next i
    TrimExt = s
End Function
