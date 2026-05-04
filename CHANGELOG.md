# 变更日志 (Changelog)

> Excel 拆合大师 · 大荣老师出品 — 每个版本的变更记录。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，版本号遵循 [SemVer](https://semver.org/lang/zh-CN/)。

---

## [Unreleased]

待发布的改动会先记录在这里，发版本时整理到下面带版本号的小节。

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
