# CSV 支持设计文档（V1.5 待开发）

> 目标：让 `excel-master` 支持把 `.csv` 作为**输入源**参与"批量关键词提取"和"按 Sheet/行/列拆分"工作流，输出仍统一为 `.xlsx`。
> 不在范围：CSV 作为输出格式（短期内不做）；`.xls` 老格式（建议直接砍掉，详见 §9）。
> 工时预估：1 ~ 1.5 人日（含中文乱码处理 + 单测 + 前端）。

---

## 1. 背景与目标

### 1.1 现状

- 项目当前只接收 `.xlsx`（`.xlsm` 实质走同样路径）。扫描白名单写在 `internal/extractor/scanner.go:58`：
  ```go
  if !strings.EqualFold(filepath.Ext(name), ".xlsx") { continue }
  ```
- 所有读取通过 `internal/excelio/reader.go` 的 `excelize.OpenFile`。
- 输出走两条路径：流式 StreamWriter（per_keyword/merged）和 zip 手术（per_source 原汁原味）。

### 1.2 目标

- 用户在文件夹里混放 `.xlsx` + `.csv` 时，CSV 也能被识别、读取、参与抽取/拆分。
- CSV 没有 Sheet/样式/图片/合并单元格，输出退化为"纯数据 xlsx"——这是 CSV 自身限制，可接受。
- **绝不引入 GBK 乱码**：自动嗅探编码 + 用户可手动覆盖。

### 1.3 非目标

- 不输出 CSV（V1.5 不做，等真有人提）。
- 不支持 `.xls`（建议砍掉，§9）。
- 不支持 TSV/管道符等其他分隔符的"自动猜"——改为让用户在 UI 显式选分隔符。

---

## 2. 中文乱码——核心问题与方案

### 2.1 中文 CSV 编码现状（业务侧）

| 来源 | 常见编码 | 备注 |
|---|---|---|
| ERP/用友/金蝶导出 | **GBK / GB18030** | 国内最常见 |
| 数据库工具导出（Navicat/DBeaver） | **UTF-8（带 BOM）** | 默认带 `EF BB BF` |
| Excel "另存为 CSV" | **GBK（中文 Windows 默认 ANSI）** | 大坑，看似 UTF-8 实际是 GBK |
| 跨境/英文系统 | UTF-8（无 BOM） | |
| 老系统 | GB2312 | GBK 严格超集，按 GBK 解码可兼容 |

**结论：必须同时支持 UTF-8（带/不带 BOM）+ GBK/GB18030，而且默认会遇到 GBK。**

### 2.2 编码识别策略（已调研，方案确定）

调研结果：

- **`encoding/csv` 不识别编码**：标准库假设输入是 UTF-8，遇到 GBK 直接乱码。
- **GBK/GB18030 解码库**：用 `golang.org/x/text/encoding/simplifiedchinese`（官方维护，靠谱），不要用 `axgle/mahonia`、`djimenez/iconv-go`（第三方、不维护）。
- **自动嗅探库**：用 `github.com/saintfish/chardet`（Mozilla chardet 的 Go port），支持 utf-8 / utf-16le/be / gbk / gb18030 / big5。轻量、纯 Go，无 cgo。

**最终策略（三级识别）**：

1. **BOM 优先**（最快、最准）：
   - `EF BB BF` → UTF-8
   - `FF FE` → UTF-16LE
   - `FE FF` → UTF-16BE
   - `00 00 FE FF` / `FF FE 00 00` → UTF-32（罕见，可不处理）
2. **chardet 嗅探**：读文件**头部 8 KB** 喂给 `chardet.NewTextDetector().DetectBest()`，置信度 ≥ 50 时采用结果。
3. **兜底**：默认 GBK（中文 Windows 最常见，比假设 UTF-8 更不容易翻车）。

**用户覆盖**：UI 提供"编码"下拉框，选项 `自动 / UTF-8 / UTF-8 BOM / GBK / GB18030 / Big5`，默认"自动"。检测失败或乱码时用户可手动指定。

### 2.3 BOM 处理

- 读取时如果识别到 UTF-8 BOM，**必须跳过头 3 字节**，否则第一个字段会带个不可见的 `\uFEFF`，关键词匹配会全部失效（这是历史 #1 大坑）。

### 2.4 解码管线（Go 代码草图）

```go
// internal/excelio/csv_reader.go
import (
    "bufio"
    "encoding/csv"
    "io"
    "os"
    "unicode/utf8"

    "github.com/saintfish/chardet"
    "golang.org/x/text/encoding"
    "golang.org/x/text/encoding/simplifiedchinese"
    "golang.org/x/text/encoding/unicode"
    "golang.org/x/text/transform"
)

// DetectEncoding 读文件头嗅探编码。返回 encoding.Encoding 和是否需要跳 BOM。
func DetectEncoding(path string, override string) (encoding.Encoding, bool, error) {
    f, err := os.Open(path)
    if err != nil { return nil, false, err }
    defer f.Close()

    head := make([]byte, 8192)
    n, _ := io.ReadFull(f, head)
    head = head[:n]

    // 1. BOM
    switch {
    case n >= 3 && head[0]==0xEF && head[1]==0xBB && head[2]==0xBF:
        return unicode.UTF8, true, nil
    case n >= 2 && head[0]==0xFF && head[1]==0xFE:
        return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), true, nil
    case n >= 2 && head[0]==0xFE && head[1]==0xFF:
        return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), true, nil
    }

    // 2. 用户显式指定
    if e := encodingByName(override); e != nil {
        return e, false, nil
    }

    // 3. chardet
    d := chardet.NewTextDetector()
    if r, err := d.DetectBest(head); err == nil && r.Confidence >= 50 {
        if e := encodingByName(r.Charset); e != nil {
            return e, false, nil
        }
    }

    // 4. 兜底：UTF-8 验证通过就 UTF-8，否则 GBK
    if utf8.Valid(head) {
        return unicode.UTF8, false, nil
    }
    return simplifiedchinese.GBK, false, nil
}

// OpenCSVReader 返回一个解码后的 *csv.Reader，行为对齐 xlsx 的 Rows() 流式迭代。
func OpenCSVReader(path, encOverride, delim string) (*csv.Reader, io.Closer, error) {
    enc, skipBOM, err := DetectEncoding(path, encOverride)
    if err != nil { return nil, nil, err }

    f, err := os.Open(path)
    if err != nil { return nil, nil, err }

    var r io.Reader = f
    if enc != nil && enc != unicode.UTF8 {
        r = transform.NewReader(f, enc.NewDecoder())
    }
    br := bufio.NewReaderSize(r, 64*1024)
    if skipBOM {
        // transform 后 BOM 可能已被剥（UTF-8）或没了（UTF-16），保险起见 peek
        if b, _ := br.Peek(3); len(b) == 3 && b[0]==0xEF && b[1]==0xBB && b[2]==0xBF {
            br.Discard(3)
        }
    }

    cr := csv.NewReader(br)
    cr.LazyQuotes = true        // 容忍非法引号（业务 CSV 经常有）
    cr.FieldsPerRecord = -1     // 允许每行字段数不同（不要 Go 默认的"全文件第一行决定列数"）
    if delim != "" && len([]rune(delim)) == 1 {
        cr.Comma = []rune(delim)[0]
    }
    cr.ReuseRecord = true       // 性能：复用底层 slice
    return cr, f, nil
}
```

依赖：

```bash
go get golang.org/x/text/encoding
go get github.com/saintfish/chardet
```

### 2.5 写出回 xlsx 时的注意

- `excelize.SetCellValue` 接受 Go 原生 `string`（UTF-8）。只要前面解码到位，写出就是干净的 UTF-8，Excel 打开零乱码。
- **不要**给输出 xlsx 加 BOM/编码声明，xlsx 内部 XML 自带声明。

---

## 3. 架构落地

### 3.1 抽象：源文件类型

新增一个枚举区分 `xlsx` / `csv`，扫描器和处理器按类型分发：

```go
// internal/core/source_type.go
type SourceKind int
const (
    SourceXLSX SourceKind = iota
    SourceCSV
)

func DetectSourceKind(path string) SourceKind {
    ext := strings.ToLower(filepath.Ext(path))
    switch ext {
    case ".csv": return SourceCSV
    default:     return SourceXLSX
    }
}
```

### 3.2 数据流

```
┌─────────────┐    ┌──────────────────┐    ┌──────────────────┐
│  *.xlsx     │───▶│ excelio.Reader   │───▶│ Rows() 迭代      │──┐
└─────────────┘    └──────────────────┘    └──────────────────┘  │
                                                                  ├─▶ 统一行流（[]string）
┌─────────────┐    ┌──────────────────┐    ┌──────────────────┐  │
│  *.csv      │───▶│ csv_reader       │───▶│ csv.Reader.Read()│──┘
└─────────────┘    └──────────────────┘    └──────────────────┘
                                                                       │
                                                                       ▼
                                                          ┌──────────────────────┐
                                                          │ extractor / splitter │
                                                          │ 行级关键词匹配/拆分   │
                                                          └──────────────────────┘
                                                                       │
                                                                       ▼
                                                          ┌──────────────────────┐
                                                          │ xlsx StreamWriter    │
                                                          └──────────────────────┘
```

**关键点**：CSV 永远不走 zip 手术（`zipsurgery.go`），因为 CSV 没有 OOXML 结构。所以 CSV 输入时：

- per_source 模式：等价于"读 CSV → 全部命中行原样写一个 xlsx"。
- per_keyword/merged 模式：跟现有路径一致，只是源换成 csv 行。

### 3.3 模块改动清单

| 模块 | 文件 | 改动 |
|---|---|---|
| 核心 | `internal/core/source_type.go`（新建） | 定义 `SourceKind` + 检测函数 |
| IO | `internal/excelio/csv_reader.go`（新建） | `DetectEncoding` / `OpenCSVReader` |
| IO | `internal/excelio/csv_reader_test.go`（新建） | UTF-8 / UTF-8 BOM / GBK / GB18030 / 各种分隔符样本 |
| 扫描 | `internal/extractor/scanner.go` | 白名单加 `.csv`；CSV 单元 `SheetName` 固定为 `"CSV"`，`Headers` 通过读首行获得 |
| 抽取 | `internal/extractor/extractor.go` | `processFile` 按 `SourceKind` 分发；CSV 路径走 csv reader 流式行循环 |
| 抽取写出 | `internal/extractor/writer_per_source.go` | CSV 源的 per_source：跳过 zip 手术，直接 StreamWriter 输出"过滤后的纯数据 xlsx" |
| 拆分 | `internal/splitter/by_rows.go` / `by_keyword.go` / `by_column.go` | 同样按 `SourceKind` 分发；`by_sheet.go` 对 CSV 提示"不适用"或退化为单输出 |
| 任务参数 | `internal/core/types.go` | 任务 `Options` 加 `CSVEncoding string`、`CSVDelimiter string` |
| Wails 桥 | `app.go` | 透传新参数（≤ 5 行规则保持） |
| 前端 | `frontend/src/types/*` | 镜像新参数 |
| 前端 | `frontend/src/views/ExtractView.vue` `SplitView.vue` | 任务表单加"CSV 编码"和"分隔符"两个下拉（仅当源含 .csv 时显示） |
| 前端 | `frontend/src/components/PathPicker.vue` | `mode="file"` 时文件过滤加 `*.csv` |

---

## 4. 边界细节（容易翻车的点）

### 4.1 表头识别

- xlsx 路径里 `headerRow` 是 1-based 物理行号。
- CSV 同样按 1-based 行号（CSV 无 Sheet 概念，文件首行=1）。
- 注意 `csv.Reader` 的 `Read()` 一次返回一条记录，逻辑行号即可。

### 4.2 CSV 字段内换行

- CSV 规范允许字段内带 `\n`（用 `"..."` 包起来）。**不能用按 `\n` split 的偷懒做法**。`encoding/csv` 已经处理对，不要绕过它。

### 4.3 字段数不一致

- 真实 CSV 经常有"前几行少几列"的脏数据。设 `cr.FieldsPerRecord = -1` 容忍；写 xlsx 时按表头长度补齐空字符串。

### 4.4 大 CSV 的内存

- 流式读，单行处理，绝不 `cr.ReadAll()`。
- `bufio.NewReaderSize(r, 64*1024)` 64KB 缓冲足够。
- 进度反馈：CSV 没"总行数"元数据，按已读字节 / 文件总字节计算百分比（`f.Stat().Size()` + 包一层 `*countingReader`）。

### 4.5 取消支持

- 行循环里每 N 行（建议 1000）`select` 一次 `ctx.Done()`，对齐现有 xlsx 路径风格。

### 4.6 进度回调字段对齐

- `core.EventProgress` 现有字段 `CurrentFile` `Processed` `Total` 直接复用；CSV 没 Sheet 不用动 `CurrentSheet`，传 `"CSV"`。

### 4.7 文件锁/占用

- CSV 也要走现有的 `askFileOpenDecision` retry/skip/cancel 流程，因为 Excel 打开 CSV 时同样会锁文件。判断逻辑：`os.Open` 报 `sharing violation` 或 Office `~$xxx` 锁文件存在。

### 4.8 BOM 二次写入

- 输出永远是 xlsx，**绝不**给输出加 UTF-8 BOM。

### 4.9 chardet 误判

- chardet 对小样本（< 1KB）容易把 GBK 误判为 ISO-8859-x。所以读头 8KB；样本不足时直接走兜底分支。

---

## 5. 测试矩阵

| 用例 | 输入 | 期望 |
|---|---|---|
| T1 | UTF-8 无 BOM，纯英文 | 正确读取 |
| T2 | UTF-8 BOM，含中文 | 跳 BOM，中文正确 |
| T3 | GBK，含中文 | chardet 识别为 gb*，解码后正确 |
| T4 | GB18030，含罕见字（如𠮷） | 走 GB18030 解码 |
| T5 | UTF-16 LE BOM | 通过 BOM 路径正确解码 |
| T6 | 字段内带 `\n` | csv.Reader 正确解析 |
| T7 | 字段内带逗号、双引号 | LazyQuotes 容忍 |
| T8 | 每行字段数不一致 | 不报错，写 xlsx 时按表头补齐 |
| T9 | 用户手动选 GBK 但文件实为 UTF-8 | 按用户选择走 GBK，可能乱码（用户责任） |
| T10 | 1GB 大 CSV | 流式不爆内存，进度按字节推进 |
| T11 | 文件被 Excel 占用 | 触发 file-blocked retry/skip/cancel |
| T12 | per_source 模式 + CSV | 正确退化为"过滤行 → 纯数据 xlsx" |
| T13 | by_sheet 拆分 + CSV | 友好提示"CSV 无 Sheet 概念，不适用此模式" |
| T14 | 混合文件夹（xlsx + csv） | 两类都被扫描、各自处理、结果合并 |

测试样本放 `testdata/csv/`，按编码命名：`utf8.csv` `utf8_bom.csv` `gbk.csv` `gb18030.csv` `utf16le.csv` `dirty.csv` `huge.csv`（huge 可在测试里临时生成，避免污染仓库）。

---

## 6. UI 改动细节

### 6.1 表单新增字段（仅 ExtractView/SplitView 检测到源含 .csv 时显示）

```
┌─ CSV 选项 ──────────────────────────┐
│ 编码： [自动 ▾]                      │
│        UTF-8 / UTF-8 BOM /          │
│        GBK / GB18030 / Big5         │
│                                      │
│ 分隔符： [逗号 ,▾]                   │
│         逗号 , / 分号 ; / Tab        │
└──────────────────────────────────────┘
```

- 默认折叠在"高级选项"里。
- 仅当扫描结果中存在 `.csv` 时面板才展开（避免干扰纯 xlsx 用户）。

### 6.2 PathPicker 文件过滤

- `chooseFile` 当前只接受单个 dialog title，需扩展支持 filter 列表。
- 后端 Wails `runtime.OpenFileDialog` 加 `Filters: []runtime.FileFilter{ {DisplayName: "Excel/CSV", Pattern: "*.xlsx;*.xlsm;*.csv"} }`。

---

## 7. 性能与精准度评估

| 维度 | xlsx | csv | 备注 |
|---|---|---|---|
| 1GB 文件读取 | 几分钟（zip 解压 + XML 解析） | < 30s | CSV 快 5-10× |
| 内存占用 | excelize 全量读 800MB+ | < 50MB（流式） | CSV 优势明显 |
| 样式保真 | 100%（zip 手术） | N/A（CSV 无样式） | |
| 关键词命中精度 | 100% | 100%（编码正确时） | 编码错才会乱码 |
| 对现有 xlsx 路径影响 | — | **零影响** | CSV 走全新分支，主流程不动 |

---

## 8. 实施步骤（建议顺序，可拆 commit）

1. **基础 IO 层**：新增 `internal/core/source_type.go` 和 `internal/excelio/csv_reader.go` + 单元测试 T1-T7。
2. **扫描层**：扩展 `scanner.go` 白名单，CSV 单元的 SheetName 设 `"CSV"`，Headers 读首行。
3. **抽取主路径**：`extractor.go` 按 SourceKind 分发，per_keyword 和 merged 路径打通。
4. **per_source 退化路径**：CSV 源走 StreamWriter 输出，跳过 zip 手术。
5. **拆分模块**：by_rows / by_keyword / by_column 分发；by_sheet 友好降级。
6. **后端参数 + Wails 桥**：CSVEncoding/CSVDelimiter 透传。
7. **前端**：表单增量、PathPicker filter、文件占用流程对齐。
8. **集成测试 T10-T14**，跑一遍真实 ERP 导出的 GBK CSV。
9. **文档**：更新 `README.md` 的"支持格式"段落；本设计文档归档。

---

## 9. 顺手清理：移除 .xls 支持（可选但强烈建议）

### 9.1 现状的尴尬

- `scanner.go` 实际只识别 `.xlsx`，所以"是否支持 .xls"目前其实是**没有**的。但产品文档/用户预期可能误以为有。
- 强行加 `.xls` 写支持需要 Java POI 或 COM，跟项目架构正交，不值得。

### 9.2 行动

- 在 README 和 UI 提示明确"仅支持 `.xlsx` / `.xlsm` / `.csv`"。
- 用户传 `.xls` 时给清晰错误：`SOURCE_FORMAT_UNSUPPORTED`，提示"请用 Excel/WPS 另存为 .xlsx 后重试"。

---

## 10. 风险与回滚

| 风险 | 缓解 |
|---|---|
| chardet 在某些样本上判错 | 提供"手动选编码"出口；默认兜底 GBK 而非 UTF-8 |
| CSV 输入混入 xlsx 主流程出 bug | 全程按 `SourceKind` 分发，**不修改** xlsx 既有代码 |
| 大 CSV 进度卡顿 | 按字节比例 emit progress，每 1000 行 emit 一次 |
| 用户期待 CSV 输出 | 文档明确说明"V1.5 仅支持 CSV 输入"，下一版本再评估 |

回滚：CSV 是纯增量分支，砍掉 `csv_reader.go` 和 scanner 白名单条目即可恢复纯 xlsx 行为。

---

## 11. 参考资料

- Go 官方编码包：https://pkg.go.dev/golang.org/x/text/encoding/simplifiedchinese
- chardet（Mozilla port）：https://github.com/saintfish/chardet
- CSV 规范 RFC 4180：https://datatracker.ietf.org/doc/html/rfc4180
- Stack Overflow"Reading a non UTF-8 text file in Go"：https://stackoverflow.com/q/10277933
- 中文 CSV 乱码踩坑（参考非选用方案）：https://www.cnblogs.com/DillGao/p/8710558.html

---

## 12. 验收清单（Definition of Done）

- [ ] `go test ./internal/excelio/...` 通过，包含编码识别 7 个用例
- [ ] `go test ./internal/extractor/...` 通过（含 CSV 集成测试）
- [ ] `go test ./internal/splitter/...` 通过
- [ ] `npm run build` 通过
- [ ] 手测：真实 ERP 导出的 GBK CSV 抽取关键词，输出 xlsx 中文无乱码
- [ ] 手测：UTF-8 BOM CSV 首列关键词匹配命中（验证 BOM 已剥）
- [ ] 手测：1GB CSV 流式处理内存峰值 < 200MB
- [ ] 手测：CSV 文件被 Excel 占用时弹出 retry/skip/cancel 对话框
- [ ] README 更新支持格式段落
