# 变更日志 (Changelog)

> Excel 拆合大师 · 大荣老师出品 — 每个版本的变更记录。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，版本号遵循 [SemVer](https://semver.org/lang/zh-CN/)。

---

## [Unreleased]

待发布的改动会先记录在这里，发版本时整理到下面带版本号的小节。

---

## [v1.3.0] - 2026-05-05

### 新增 (Added)

- **单源 per_keyword 走 zip 手术**：批量提取"新文件 + 按关键词拆分"在源是单个
  xlsx 文件时，输出由 excelize 流式重建升级为字节级 zip 手术，达成**100% 样式 +
  100% 图片保真**，处理速度比原 excelize 路径快约 7 倍。
  - 新增 `internal/excelio/newfile_zip.go.ExtractToNewFileSurgery(src, dst, specs)`：
    源 xlsx + N 个 InplaceSheetSpec → 输出**只含筛选 sheet 的全新 xlsx**。
    实现复用 `planInplaceSpecs` / `writePlannedSheet` / `rewriteSheetXML` /
    `rewriteDrawingXML` / `CoerceStringCellsToNumbers` / `unshareFormulasInSheet`，
    新增 `rebuildWorkbookForNewFile` / `rebuildWorkbookRelsForNewFile` /
    `rebuildContentTypesForNewFile`（replace 语义而非 inplace 的 append 语义）。
  - 新增 `internal/extractor/writer_per_keyword_surgery.go.perKeywordSurgeryWriter`：
    EmitRow 仅累积命中行号（kw → sheet → []rowNum），Finalize 时每个关键词
    一次 `ExtractToNewFileSurgery` 调用产出 N 文件。
  - `extractor.newOutputWriter` 加 `singleXlsxSource bool` 参数；per_keyword 策略
    按此分流：单 xlsx 源走 surgery，多源 / CSV 走原 perKeywordWriter（excelize）。
  - 新增 `extractor.isSingleXlsxSource(files)` 工具函数。

### 修改 (Changed)

- **保真度全景**：
  | 输出策略 | 单源 xlsx | 多源 / 含 CSV |
  |---|---|---|
  | per_source | 100%（已是 surgery） | 100%（已是 surgery） |
  | merged | 100%（finalizeSingleSource 已是 surgery） | 98%（excelize 流式合并）|
  | per_keyword | **100% ← 本版本升级** | 98%（excelize 流式合并）|
  | inplace | 100%（已是 surgery） | n/a |

### 修复 (Fixed)

- **新文件模式图片变形**（接续 v1.2.2 修复，从 98% 提升到 100%）：
  - v1.2.2 通过传 OffsetX/Y + DefaultRowHeight + ScaleX/Y 让 excelize 渲染接近源
    （误差 ~5px，跨 1.1 行而非 1 行），但仍是 excelize 重建路径
  - v1.3.0 单源场景**绕开 excelize**，drawing.xml + media 字节级搬运 + 行号重写：
    输出 anchor 与源逐字节一致（含 colOff=19050 / rowOff=19050 / editAs="oneCell" /
    `<a:extLst>` / `<a16:creationId>` 全字段）

### 性能 (Performance)

- 用户 fixture `03_员工花名册_2万行带照片.xlsx`（命中 2500 行 + 2500 张图）：
  | 路径 | 时间 | 输出大小 |
  |---|---|---|
  | v1.2.2 excelize | ~30s | 168 KB（图片重新编码后变小）|
  | v1.3.0 surgery | **4.3s** | 315 KB（保留源 media 原字节）|

### 测试 (Tests)

- `excelio/newfile_zip_test.go`: 5 个用例
  - `TestExtractToNewFileSurgery_Basic`: 行过滤 + 图片迁移 + 跨行号重写
  - `TestExtractToNewFileSurgery_MultipleSheets`: 多 spec → 多 sheet
  - `TestExtractToNewFileSurgery_NoOldEntries`: 旧 sheet/calcChain 已删除、
    media 已保留、worksheets/ 下严格只有新 sheet
  - `TestExtractToNewFileSurgery_NilKeepRowsFull`: KeepRows=nil 全行保留
  - `TestExtractToNewFileSurgery_Errors`: empty specs / dst 已存在 / 源 sheet 不存在
- 既有测试无破坏（CSV per_keyword 仍走原 excelize 路径）

### 限制 (Limitations)

- **多源 per_keyword / merged 仍是 98%**：跨源样式索引（styles.xml / theme /
  sharedStrings）需要重映射器才能 100% 保真，工程量 1-2 周。
  目前丢失项：条件格式、数据验证、自定义数字格式中的高级特性（学员场景罕见）。
  保留项：列宽行高、字体、填充、边框、合并单元格、图片首位（98% 视觉效果）。
- **CSV 源 per_keyword 仍是 98%**：CSV 不是 zip，无法做手术；保留 excelize 流式。

---

## [v1.2.2] - 2026-05-05

### 修复 (Fixed)

- **数字列以文本形式存储（左上角绿三角）**：源文件里本应是数字的列（如分数、金额、总分），
  经过批量提取后在结果文件里变成"文本"状态，Excel 打开显示绿色三角警告，且无法做
  sum/avg 等数值运算。
  - 根因 1（新文件模式）：excelize 流式读 `Rows.Columns()` 返回 `[]string`，写入时用
    字符串 `SetRow` → Excel 识别为文本列
  - 根因 2（inplace 模式）：源 xlsx 里数字列若以 shared string / inline string 存储
    （从第三方系统导出 / VBA 写入的 xlsx 常见），zip 手术字节级复制后仍保留 `t="s"` /
    `t="inlineStr"` 属性 → Excel 识别为文本
  - 修复：新增 `excelio.CoerceScalar()` 智能识别字符串里的数字，并分两层应用：
    - writer 层：`extractor/writer_common.go` 的 `buildAdjustedRow` 对每个 string value
      尝试 coerce 为 float64（覆盖"新文件"输出路径）
    - xml 层：`excelio.CoerceStringCellsToNumbers()` 扫描 sheet.xml，把 t="s" /
      t="inlineStr" 且内容能被识别为数字的 cell 改写为纯数字 cell（覆盖 inplace
      zip 手术路径）
  - 保守规则（避免误转字符串语义的字段）：
    - 含 `e/E`（科学计数法）、整数位 > 10（手机号/身份证）、ParseFloat 失败、
      round-trip 严格不等（`0123` `1.50` `+89` 等格式化差异）→ 保留字符串
    - 含 `<f>` 公式的 cell / rich text（多段 `<r>`）的 shared string 不动

- **新文件模式图片变形（图片跨多行撑大）**：批量提取"新文件"输出目标下，源文件里的
  图片（typical twoCellAnchor editAs="oneCell" + spPr/xfrm/ext.cx/cy=0）被写到新
  xlsx 后占据 2~3 个单元格高度，Excel 打开显示图片"跨多行且被拉伸"。inplace 模式
  无此问题（走 zip 手术字节级复制）。
  - 根因 1：`zipimage_parse.go.decodeTwoCellAnchor` 只从 spPr/xfrm/ext 读 cx/cy，
    而许多 xlsx（WPS 导出 / 业务系统生成）把这两个字段写成 0（真实渲染尺寸由
    from-to 网格推导，不在 ext 里）。导致 `buildGraphicOptions` 的 ScaleX/ScaleY
    退化为 1.0，excelize 按图片原始像素（如 200×270px）插入，远超单元格。
  - 根因 2：excelize.AddPictureFromBytes 在 twoCellAnchor 模式下按目标 sheet 的
    `defaultRowHeight`（默认 15pt）反算 to.row。新文件未设 defaultRowHeight，
    源行高 36pt 的图片被按 15pt 切成 ~2.4 行。
  - 修复 1：`zipimage.go.inferTwoCellAnchorCxCy`——当 ext.cx/cy=0 时，从 from-to
    网格 + rowHeights map 反推真实渲染 EMU。cy 支持同行 / 跨任意行，cx 仅同列
    精确计算（跨列留给 excelize 默认兜底，罕见场景）。
  - 修复 2：`writer_common.go.ensureDefaultHeightForPics` + `writer.go.SetSheetDefaultRowHeight`
    ——首次带图命中行写入前，把源行 ht 复制到目标 sheet 的 defaultRowHeight
    （通过 excelize.SheetPropsOptions），让 excelize 用对的行高 base 反算 to.row。
  - 修复 3：`zipimage.go.buildGraphicOptions` 把源 anchor 的 from.colOff / rowOff
    转为像素填入 `GraphicOptions.OffsetX/OffsetY`（1 px = 9525 EMU），保留图片
    在 cell 内的顶部 / 左侧边距（典型值 rowOff=19050 EMU ≈ 2px）。否则 excelize
    默认把图片紧贴 cell 左上角，用户看到"图片布满单元格、没有空位"。
  - 效果：用户案例（源 36pt、照片 ≈34px、rowOff=2px）修复前图片跨 3 行渲染
    （971550 EMU，222% 源尺寸 + 无上边距），修复后跨 2 行（485775 EMU，
    111% 源尺寸 + 2px 上边距），视觉接近 98% 源文件效果。剩余 11% 尺寸误差
    是 excelize positionObjectPixels 内部算法固有偏差，彻底消除需要直接改
    drawing.xml（留给后续版本）。

### 测试 (Tests)

- `excelio/coerce_cells_test.go`: 覆盖 `CoerceScalar` / `CoerceStringToNumber` /
  `parseSharedStrings` / 4 种 `CoerceStringCellsToNumbers` 场景（shared/inline/
  公式跳过/已数字跳过）
- `excelio/zipimage_infer_test.go`: 覆盖 `inferTwoCellAnchorCxCy` 的 6 种场景
  （同列同行 / 同列跨 1 行 / 跨多行 / 无 rowHeights / 跨列 / 非 twoCell）
- `extractor/coerce_test.go`: 端到端 smoke 测覆盖 writer 层的 coerce 路径

---

## [v1.2.1] - 2026-05-05

### 修改 (Changed)

- **inplace 备份命名升级**：从 `源文件.xlsx.bak` 改成 `源文件名_备份_yyyyMMdd_HHmmss.xlsx`
  - 保留原扩展名（.xlsx → .xlsx），双击可直接用 Excel 打开（不再需要手动改后缀）
  - 多次 inplace 不互相覆盖，时间戳让每次备份成为独立的"版本快照"
  - 防同秒撞名：脚本连跑时尾部追加 `_2 _3 ...` 序号
  - UI 标签从 "写回前先备份源文件 (.bak)" 简化为 "写回前先备份源文件"，并加 hover 提示说明命名规则
- 单点修改 `internal/excelio/inplace.go` 的 `BackupCopy()` 函数，批量提取和单文件拆分两边自动同步生效

### 测试 (Tests)

- `TestBackupCopy`: 验证保留扩展名、命名含 `_备份_`、两次调用产生不同文件、两份备份都不被覆盖
- `TestExtractInplace_BackupSource` / `TestSplitByRowsInplaceBackup`: 用 `filepath.Glob` 适配新命名

---

## [v1.2.0] - 2026-05-05

### 新增 (Added)

- **多列组合去重**：去重列从 1 个扩展到最多 3 个组合键（品牌+型号+批次这样的多维度唯一性）
  - UI 提供 3 个列下拉（列 1 必填，列 2/3 可选）；动态提示会把组合列串成「A+B+C」列组合
  - 后端用 `\x01` 控制字符拼接列值做 key，Excel 单元格不可能含该字符，理论零冲突
  - 任一列为空 → 整行不参与去重（跟单列语义一致，避免把"空缺未填"误判为一组）
  - CLI `extract-cli` 新增 `-dedup-cols` 参数（逗号分隔列名）
- **归一化开关**：UI 两个 checkbox，去重时统一对所有选定列生效
  - ✅ **忽略前后空白**：`strings.TrimSpace`（只去首尾，不去中间）—— 解决手动录入带空格的常见问题
  - ✅ **忽略大小写**：`strings.ToLower`（英文字母生效，中文 CJK unicode 无变化）
  - CLI 新增 `-dedup-ignore-space` / `-dedup-ignore-case` flag

### 修改 (Changed)

- Deduper 内部重构：`newDeduper(col string)` → `newDeduper(cfg dedupConfig)`；所有 writer 构造函数同步升级。对外前后端 API 完全向后兼容，V1.1 的 `DedupColumn` 单列字段仍正常工作
- Service 层新增 `sanitizeDedupColumns` 辅助函数：前端列表先 Trim + 过滤空值，再下沉到 extractor 的 `buildDedupConfig` 做去重合并和 3 列截断

### 测试 (Tests)

- Deduper 单元测 + buildDedupConfig 单元测共 15 个 case（原 7 个迁移 + V1.2 新增 8 个）
- Extract 集成测新增 5 个 V1.2 场景（多列严格/忽略大小写/同时忽略空白大小写/V1.1 向后兼容/两代字段共用）
- 7 个内部 Go 包全绿

---

## [v1.1.0] - 2026-05-04

### 新增 (Added)

- **去重功能**：批量提取 / 单文件按关键词拆分时，可按指定列去重，保留首次出现的行
  - 严格相等比较（不忽略大小写/空白），整数 / 浮点数 / 字符串"1" 视为等价
  - 空值不参与去重（每个空值视作独立行保留）
  - 自动按输出策略推导去重范围：
    - 合并到一个文件 → 全局跨文件去重
    - 每个关键词一个文件 → 每个关键词文件内独立去重
    - 每个源文件一个 → 每个源文件内独立去重
    - 写回源文件新 Sheet → 新 Sheet 内独立去重
  - 图片自动跟随保留行，被丢弃的行连同图片一起丢弃，零额外配置
  - 跨文件 schema 不一致时（A 有该列、B 没有）：B 文件的行全保留 + warning 日志，避免误删数据
- **CLI** `extract-cli` 新增 `-dedup` 参数，方便自动化场景使用

### 修改 (Changed)

- **性能优化**：fixture 01（10 万行 × 14 列扫描）从 ~50 秒降到 ~17 秒
  - matcher.Engine 加 ASCII 快路径：纯中文/数字关键词跳过 ToLower 调用
  - 一次 zip 扫描合并 SheetHasFormulas + RowHeights 两次预探针
  - 命中行图片改用 archive/zip 直读，绕过 excelize 的 O(N²) anchor 扫描

### 文档 (Documentation)

- 修正 CHANGELOG 中"拼音匹配"的过时描述（功能已在 V1.0 性能优化中移除，commit 47787b7）

---

## [v1.0.0] - 2026 待定

首次正式发布。

### 核心功能

- **批量提取**：从多个 Excel/CSV 文件中按关键词或高级条件筛选行，汇总到新 xlsx
  - 支持精确匹配、包含匹配（大小写不敏感，中英文混合关键词友好）
  - 高级筛选：数值范围、日期、文本、空值、多列 AND/OR 组合
  - 支持 .xlsx / .xlsm / .csv 三种格式输入（CSV 自动识别 GBK/UTF-8 编码）
  - 三种输出策略：合并到一个文件 / 每个源文件一个 / 写回源文件新增 Sheet
- **单文件拆分**：把一个 xlsx 拆成多个文件
  - 按 Sheet 拆分：每个 Sheet 输出一个 xlsx
  - 按行数拆分：每 N 行一个分片
  - 按列值拆分：相同列值的行汇总到一个文件
  - 按关键词拆分：每个关键词命中的行输出一个文件
- **保真处理**：通过 zip 手术绕过 excelize Save 的样式丢失问题
  - 100% 保留：单元格样式、合并单元格、条件格式、数据验证、公式（行号自动偏移）
  - 100% 保留：单元格内嵌图片（drawing 锚点重映射）
  - 100% 保留：冻结窗格、列宽、行高、筛选器

### 工程基础

- **流式处理**：禁止 GetRows 全量加载，所有大文件用 excelize Rows() 迭代器
- **任务可取消**：每个长任务接收 context，前端可一键取消
- **进度实时反馈**：处理进度通过 EventEmit 推送，UI 不会"假死"
- **panic 恢复**：异步任务 panic 自动转成 UI 错误事件，进程不崩
- **绿色 exe**：配置文件 / 日志优先 exe 同目录，整个文件夹拷贝到任意位置都能跑

### 兼容性 / 系统要求

- Windows 10 1809（2018 年 10 月）或更高版本
- 内置 WebView2 Fixed Version Runtime，**无需用户预装、无需联网**
- 不依赖 Microsoft Excel 安装

---

<!--
后续每发版本时模板：

## [vX.Y.Z] - YYYY-MM-DD

### 新增 (Added)
- ...

### 修改 (Changed)
- ...

### 修复 (Fixed)
- ...

### 移除 (Removed)
- ...

### 安全 (Security)
- ...
-->
