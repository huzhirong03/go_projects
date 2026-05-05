Attribute VB_Name = "ExtractByKeywordFull"
Option Explicit

' --- Win32 API 声明（用于图片复制时的剪贴板管理） ---
' 大量连续 Shape.Copy 会让 Excel 剪贴板进入"上一个还没释放就来下一个"的竞态，
' 触发 -2147221040 (CLIPBRD_E_CANT_OPEN)。解决办法：Copy 前先 Sleep 一小会让
' 前一次剪贴板彻底释放，失败时再重试。
#If VBA7 Then
    Private Declare PtrSafe Sub Sleep Lib "kernel32" (ByVal dwMilliseconds As Long)
#Else
    Private Declare Sub Sleep Lib "kernel32" (ByVal dwMilliseconds As Long)
#End If

' ====================================================================
' Excel 拆合大师 · VBA 完整对比版（带格式 / 公式 / 行高列宽 / 图片）
'
' 用途：跟 Go 程序"单文件提取 + 保留图片"做**公平对比**。
'
' 行为对齐：
'   · 关键词包含匹配（InStr，跟 Go 程序"包含"模式一致）
'   · 处理 ActiveWorkbook 的所有 Sheet
'   · 跳过第 1 行（表头）
'   · 任一单元格包含关键词 → 整行命中
'   · 保留：列宽 / 行高 / 样式 / 合并单元格 / 公式 / 图片跟随行
'   · 新文件模式：输出每个源 Sheet 一个同名输出 Sheet（只含表头 + 命中行）
'
' 性能说明（跟现有 ExtractByKeyword.bas 的区别）：
'   ExtractByKeyword.bas       → 只复制值（理论最快，不公平对比）
'   ExtractByKeywordFull.bas   → 格式 + 公式 + 行高列宽 + 图片（公平对比）
'
' 用法：
'   1. 打开你要测试的源 xlsx（宏处理 ActiveWorkbook）
'   2. Alt+F11 → 文件 → 导入文件 → 选 ExtractByKeywordFull.bas
'   3. F5 跑 ExtractByKeywordFull 子过程
'   4. 弹窗 1：输入关键词（如"汉族"）
'   5. 弹窗 2：选输出方式（是=新文件 / 否=写回源文件新 Sheet）
'   6. 完成后弹窗显示总耗时 / 扫描行数 / 命中行数 / 复制图片数
'
' 输出：
'   · 新文件模式：固定输出到 F:\桌面\测试拆分图片\原文件名_VBA完整结果_时间戳.xlsx
'     （文件夹不存在时自动创建）
'   · 写回模式：在源文件末尾新增 "搜索_关键词" Sheet，请记得 Ctrl+S 保存
' ====================================================================

' 输出目录（写死在这里，跟 Go 程序对齐的测试路径）
Private Const OUTPUT_DIR As String = "F:\桌面\测试拆分图片"

Public Sub ExtractByKeywordFull()
    Dim t0 As Double: t0 = Timer

    ' --- 1. 输入关键词 ---
    Dim kw As String
    kw = InputBox( _
        "请输入要搜索的关键词（包含匹配）：" & vbCrLf & vbCrLf & _
        "  · 扫描所有 Sheet 的所有列" & vbCrLf & _
        "  · 跳过第 1 行（表头）" & vbCrLf & _
        "  · 任一单元格包含此关键词的整行都会被提取" & vbCrLf & _
        "  · 保留：格式 / 公式 / 行高列宽 / 图片跟随行", _
        "VBA 完整版 · 性能对比")
    If Trim(kw) = "" Then
        MsgBox "未输入关键词，已取消。", vbInformation
        Exit Sub
    End If

    ' --- 2. 选输出方式 ---
    Dim choice As VbMsgBoxResult
    choice = MsgBox( _
        "结果输出到哪里？" & vbCrLf & vbCrLf & _
        "  【是】 = 创建新 xlsx 文件" & vbCrLf & _
        "           输出目录：" & OUTPUT_DIR & vbCrLf & vbCrLf & _
        "  【否】 = 写回源文件，新建一个 Sheet" & vbCrLf & _
        "           完成后请手动 Ctrl+S 保存" & vbCrLf & vbCrLf & _
        "  【取消】 = 不跑了", _
        vbYesNoCancel + vbQuestion, _
        "选择输出方式")
    If choice = vbCancel Then
        MsgBox "已取消。", vbInformation
        Exit Sub
    End If
    Dim writeBack As Boolean: writeBack = (choice = vbNo)

    ' --- 3. 源工作簿检查 ---
    Dim srcWb As Workbook
    Set srcWb = ActiveWorkbook
    If srcWb Is Nothing Then
        MsgBox "找不到当前工作簿（ActiveWorkbook）。请先打开源 xlsx 再运行。", vbExclamation
        Exit Sub
    End If

    ' --- 4. 性能开关 ---
    Dim oldScreen As Boolean: oldScreen = Application.ScreenUpdating
    Dim oldCalc As XlCalculation: oldCalc = Application.Calculation
    Dim oldEvents As Boolean: oldEvents = Application.EnableEvents
    Dim oldAlerts As Boolean: oldAlerts = Application.DisplayAlerts
    Application.ScreenUpdating = False
    Application.Calculation = xlCalculationManual
    Application.EnableEvents = False
    Application.DisplayAlerts = False

    On Error GoTo Cleanup

    ' --- 5. 准备输出工作簿 / Sheet ---
    Dim outWb As Workbook
    Dim sheetName As String
    sheetName = SanitizeSheetName("搜索_" & kw)

    ' 新文件模式下，记录 Workbooks.Add 自带的默认空 Sheet 引用（按引用删，不按名删，避免误伤同名源 Sheet）
    Dim defaultBlankSheets As Collection
    Set defaultBlankSheets = New Collection

    If writeBack Then
        Set outWb = srcWb
        ' 写回模式：只创建 1 个汇总 Sheet，所有源 Sheet 的命中行合并进来
        DeleteSheetIfExists outWb, sheetName
    Else
        ' 新文件模式：先确保输出目录存在
        EnsureDir OUTPUT_DIR
        Set outWb = Application.Workbooks.Add
        ' 缓存所有自带的默认 Sheet（通常 Sheet1，有的环境会 Sheet1/2/3）
        Dim dws As Worksheet
        For Each dws In outWb.Worksheets
            defaultBlankSheets.Add dws
        Next dws
    End If

    ' --- 6. 逐 Sheet 处理 ---
    Dim totalScanned As Long, totalHits As Long, totalImages As Long, totalImagesSkipped As Long
    Dim sheetCount As Long

    ' writeBack 模式共用一个输出 Sheet；new file 模式每个源 Sheet 一个输出 Sheet
    Dim aggOutSheet As Worksheet   ' writeBack 时的汇总 Sheet
    Dim aggOutRow As Long: aggOutRow = 1
    Dim aggHeaderWritten As Boolean: aggHeaderWritten = False

    If writeBack Then
        Set aggOutSheet = outWb.Sheets.Add(After:=outWb.Sheets(outWb.Sheets.Count))
        aggOutSheet.Name = sheetName
    End If

    Dim ws As Worksheet
    For Each ws In srcWb.Worksheets
        ' 跳过自己刚加的汇总 Sheet
        If writeBack And ws.Name = sheetName Then GoTo NextSheet

        Dim used As Range
        Set used = ws.UsedRange
        If used Is Nothing Then GoTo NextSheet
        If used.Rows.Count < 2 Or used.Columns.Count < 1 Then GoTo NextSheet

        sheetCount = sheetCount + 1

        ' 读 UsedRange 值（只用于命中判断，不用于写出）
        Dim data As Variant
        data = used.Value
        If Not IsArray(data) Then GoTo NextSheet

        Dim nRows As Long: nRows = UBound(data, 1)
        Dim nCols As Long: nCols = UBound(data, 2)
        Dim firstCol As Long: firstCol = used.Column
        Dim firstRow As Long: firstRow = used.Row

        ' 收集命中行号（源 sheet 的实际行号）
        Dim hitRows() As Long
        ReDim hitRows(1 To nRows)
        Dim hitN As Long: hitN = 0

        Dim r As Long, c As Long
        For r = 2 To nRows
            totalScanned = totalScanned + 1
            Dim isHit As Boolean: isHit = False
            For c = 1 To nCols
                If Not IsEmpty(data(r, c)) Then
                    If InStr(1, CStr(data(r, c)), kw, vbBinaryCompare) > 0 Then
                        isHit = True
                        Exit For
                    End If
                End If
            Next c
            If isHit Then
                hitN = hitN + 1
                hitRows(hitN) = firstRow + r - 1
            End If
        Next r

        If hitN = 0 Then GoTo NextSheet
        totalHits = totalHits + hitN

        ' 确定目标 Sheet 和起始输出行
        Dim outSheet As Worksheet
        Dim outStartRow As Long

        If writeBack Then
            Set outSheet = aggOutSheet
            ' 汇总 Sheet：首次遇到命中时写表头；之后 append
            If Not aggHeaderWritten Then
                CopyColumnWidths ws, outSheet, firstCol, nCols
                CopyOneRow ws, firstRow, outSheet, 1, firstCol, nCols
                aggOutRow = 2
                aggHeaderWritten = True
            End If
            outStartRow = aggOutRow
            aggOutRow = aggOutRow + hitN
        Else
            ' 新文件模式：为当前源 Sheet 单独建个同名输出 Sheet
            Set outSheet = outWb.Sheets.Add(After:=outWb.Sheets(outWb.Sheets.Count))
            On Error Resume Next
            outSheet.Name = ws.Name
            On Error GoTo Cleanup
            ' 表头（源第 firstRow 行）
            CopyColumnWidths ws, outSheet, firstCol, nCols
            CopyOneRow ws, firstRow, outSheet, 1, firstCol, nCols
            outStartRow = 2
        End If

        ' 批量复制命中行（Range.Union → Copy → PasteSpecial xlPasteAll）
        ' 这会带上：公式 / 样式 / 合并单元格 / 数据有效性 / 条件格式
        Dim srcUnion As Range
        Dim i As Long
        For i = 1 To hitN
            Dim rng As Range
            Set rng = ws.Range(ws.Cells(hitRows(i), firstCol), ws.Cells(hitRows(i), firstCol + nCols - 1))
            If srcUnion Is Nothing Then
                Set srcUnion = rng
            Else
                Set srcUnion = Union(srcUnion, rng)
            End If
        Next i

        If Not srcUnion Is Nothing Then
            srcUnion.Copy
            outSheet.Cells(outStartRow, 1).PasteSpecial Paste:=xlPasteAll
            Application.CutCopyMode = False
        End If

        ' 复制命中行的行高
        For i = 1 To hitN
            outSheet.Rows(outStartRow + i - 1).RowHeight = ws.Rows(hitRows(i)).RowHeight
        Next i

        ' 复制图片（跟随行）
        Dim imgCount As Long: imgCount = 0
        Dim imgSkipped As Long: imgSkipped = 0
        imgCount = CopyShapesForHitRows(ws, outSheet, hitRows, hitN, outStartRow, firstCol, imgSkipped)
        totalImages = totalImages + imgCount
        totalImagesSkipped = totalImagesSkipped + imgSkipped

NextSheet:
    Next ws

    ' --- 7. 新文件模式下删除 Workbooks.Add 自带的默认 Sheet（按引用，不按名）/ 保存 ---
    Dim savedPath As String: savedPath = ""
    If Not writeBack Then
        ' 删除自带空 Sheet（仅在我们已经加了其他 Sheet 时才删，避免工作簿没 Sheet）
        Dim itm As Variant
        For Each itm In defaultBlankSheets
            If outWb.Worksheets.Count > 1 Then
                itm.Delete
            End If
        Next itm

        savedPath = OUTPUT_DIR & "\" & TrimExt(srcWb.Name) & "_VBA完整结果_" & Format(Now, "yyyymmdd_hhmmss") & ".xlsx"
        outWb.SaveAs Filename:=savedPath, FileFormat:=xlOpenXMLWorkbook
    End If

Cleanup:
    Application.CutCopyMode = False
    Application.ScreenUpdating = oldScreen
    Application.Calculation = oldCalc
    Application.EnableEvents = oldEvents
    Application.DisplayAlerts = oldAlerts

    Dim elapsed As Double: elapsed = Timer - t0

    Dim mode As String
    If writeBack Then
        mode = "源文件新 Sheet（请 Ctrl+S 保存）"
    Else
        mode = "新文件"
    End If

    Dim msg As String
    msg = "========== VBA 完整版性能报告 ==========" & vbCrLf & vbCrLf & _
          "关键词：      " & kw & vbCrLf & _
          "处理 Sheet：  " & sheetCount & " 个" & vbCrLf & _
          "扫描行数：    " & Format(totalScanned, "#,##0") & vbCrLf & _
          "命中行数：    " & Format(totalHits, "#,##0") & vbCrLf & _
          "复制图片数：  " & Format(totalImages, "#,##0") & vbCrLf
    If totalImagesSkipped > 0 Then
        msg = msg & "跳过图片数：  " & Format(totalImagesSkipped, "#,##0") & "（剪贴板重试 3 次仍失败）" & vbCrLf
    End If
    msg = msg & _
          "总耗时：      " & Format(elapsed, "0.00") & " 秒" & vbCrLf & vbCrLf & _
          "输出方式：    " & mode
    If savedPath <> "" Then
        msg = msg & vbCrLf & "保存到：      " & savedPath
    End If

    If Err.Number <> 0 Then
        msg = msg & vbCrLf & vbCrLf & "⚠ 异常：" & Err.Description & "（错误号 " & Err.Number & "）"
    End If

    MsgBox msg, vbInformation, "完成"
End Sub

' --- 工具函数 ---

' 复制列宽（从源 Sheet 的 firstCol..firstCol+nCols-1 复制到目标 Sheet 的 1..nCols）
Private Sub CopyColumnWidths(srcWs As Worksheet, dstWs As Worksheet, firstCol As Long, nCols As Long)
    Dim c As Long
    For c = 1 To nCols
        dstWs.Columns(c).ColumnWidth = srcWs.Columns(firstCol + c - 1).ColumnWidth
    Next c
End Sub

' 把源 Sheet 的某一行（从 firstCol 起 nCols 列）整段复制到目标 Sheet 某行
' 带公式 / 格式 / 合并；用 PasteSpecial xlPasteAll
Private Sub CopyOneRow(srcWs As Worksheet, srcRow As Long, dstWs As Worksheet, dstRow As Long, firstCol As Long, nCols As Long)
    Dim srcRng As Range
    Set srcRng = srcWs.Range(srcWs.Cells(srcRow, firstCol), srcWs.Cells(srcRow, firstCol + nCols - 1))
    srcRng.Copy
    dstWs.Cells(dstRow, 1).PasteSpecial Paste:=xlPasteAll
    Application.CutCopyMode = False
    ' 同步行高
    dstWs.Rows(dstRow).RowHeight = srcWs.Rows(srcRow).RowHeight
End Sub

' 复制图片（跟随行）：遍历源 Sheet 所有 Shape，TopLeftCell.Row 在命中行集合里就复制
' 返回复制数量
'
' VBA 坑点（踩过才知道）：
'   1. ScreenUpdating = False 时 Shape.Copy/Paste 会粘出"空白框"。必须临时开启。
'   2. dstWs.Paste 必须在 dstWs 被 Activate 之后调用，否则图片要么粘不上、要么粘到错 sheet。
'   3. Shape.Copy 后要给 Excel 一点时间让剪贴板就绪（DoEvents），不然偶发拿到空图。
Private Function CopyShapesForHitRows(srcWs As Worksheet, dstWs As Worksheet, hitRows() As Long, hitN As Long, dstStartRow As Long, firstCol As Long, ByRef skippedOut As Long) As Long
    Dim n As Long: n = 0
    Dim skipped As Long: skipped = 0
    If srcWs.Shapes.Count = 0 Then
        CopyShapesForHitRows = 0
        skippedOut = 0
        Exit Function
    End If

    ' 构建 源行号 → 新行号 的映射字典
    Dim dict As Object
    Set dict = CreateObject("Scripting.Dictionary")
    Dim i As Long
    For i = 1 To hitN
        dict.Add hitRows(i), dstStartRow + i - 1
    Next i

    ' --- 关键修复：图片复制期间临时开 ScreenUpdating + 保存原激活 Sheet ---
    Dim prevScreen As Boolean: prevScreen = Application.ScreenUpdating
    Dim prevActive As Object: Set prevActive = ActiveSheet
    Application.ScreenUpdating = True

    Dim shp As Shape
    For Each shp In srcWs.Shapes
        ' TopLeftCell 可能不存在（极少数类型的 Shape），用 On Error 兜
        On Error Resume Next
        Dim srcRow As Long, srcCol As Long
        srcRow = shp.TopLeftCell.Row
        srcCol = shp.TopLeftCell.Column
        If Err.Number <> 0 Then
            Err.Clear
            On Error GoTo 0
            GoTo NextShape
        End If
        On Error GoTo 0

        If dict.Exists(srcRow) Then
            Dim dstRow As Long: dstRow = CLng(dict(srcRow))
            ' 记录 shape 在源 cell 里的相对偏移
            Dim xOff As Double, yOff As Double
            xOff = shp.Left - srcWs.Cells(srcRow, srcCol).Left
            yOff = shp.Top - srcWs.Cells(srcRow, srcCol).Top

            ' --- 带重试的复制粘贴 ---
            ' 大量连续 Shape.Copy 会间歇触发 CLIPBRD_E_CANT_OPEN；重试 3 次，每次递增 sleep
            Dim copyOK As Boolean: copyOK = False
            Dim attempt As Long
            Dim shapeBefore As Long
            For attempt = 1 To 3
                ' 清干净剪贴板状态再 Copy
                Application.CutCopyMode = False
                DoEvents
                Sleep 20 * attempt          ' 20ms / 40ms / 60ms 渐进等待

                On Error Resume Next
                shp.Copy
                If Err.Number <> 0 Then
                    Err.Clear
                    On Error GoTo 0
                    GoTo RetryCopy
                End If
                On Error GoTo 0

                DoEvents
                dstWs.Activate              ' Paste 目标必须是激活的 Sheet
                shapeBefore = dstWs.Shapes.Count

                On Error Resume Next
                dstWs.Paste
                If Err.Number <> 0 Then
                    Err.Clear
                    On Error GoTo 0
                    GoTo RetryCopy
                End If
                On Error GoTo 0
                DoEvents

                ' Paste 成功的标志：新增了一个 shape
                If dstWs.Shapes.Count > shapeBefore Then
                    copyOK = True
                    Exit For
                End If
RetryCopy:
            Next attempt

            If Not copyOK Then
                skipped = skipped + 1
                GoTo NextShape
            End If

            Dim newShp As Shape
            Set newShp = dstWs.Shapes(dstWs.Shapes.Count)

            ' 定位到目标位置；目标列 = srcCol - firstCol + 1
            Dim dstCol As Long: dstCol = srcCol - firstCol + 1
            If dstCol < 1 Then dstCol = 1
            newShp.Left = dstWs.Cells(dstRow, dstCol).Left + xOff
            newShp.Top = dstWs.Cells(dstRow, dstCol).Top + yOff
            newShp.Width = shp.Width
            newShp.Height = shp.Height
            ' 保持"大小位置随单元格变化"
            newShp.Placement = xlMoveAndSize

            n = n + 1
        End If
NextShape:
    Next shp

    ' 恢复原状态
    Application.CutCopyMode = False
    On Error Resume Next
    prevActive.Activate
    On Error GoTo 0
    Application.ScreenUpdating = prevScreen

    ' 彻底清 Err 残留，避免主流程误把"已处理过的图片 Copy 重试错误"当成未处理异常上报
    Err.Clear

    skippedOut = skipped
    CopyShapesForHitRows = n
End Function

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
            ws.Delete
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

' 确保输出目录存在（不存在就递归创建）
Private Sub EnsureDir(path As String)
    If Len(Dir(path, vbDirectory)) = 0 Then
        ' 简单递归：逐级创建父目录
        Dim parent As String
        parent = Left(path, InStrRev(path, "\") - 1)
        If Len(parent) > 3 Then EnsureDir parent
        MkDir path
    End If
End Sub

