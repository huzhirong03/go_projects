# Excel 大文件工具 · 开发进度

> 截止 2026-05-03：第 1-3 周后端 + 前端骨架完成，等待 `wails dev` 真机验证后进入第 4 周打包。

---

## 一、完成度矩阵

| 模块 | 状态 | 单测 | 位置 |
|---|---|---|---|
| `internal/core` 领域模型 / 错误 / 事件常量 | ✅ | — | `@g:\dai_ma\go_projects\excel-master\internal\core` |
| `internal/excelio` 流式读写 + 图片迁移 | ✅ | 3 | `@g:\dai_ma\go_projects\excel-master\internal\excelio` |
| `internal/matcher` 4 种匹配模式 | ✅ | 13（含子测） | `@g:\dai_ma\go_projects\excel-master\internal\matcher` |
| `internal/extractor` 批量提取 + 3 策略 | ✅ | 3 端到端 | `@g:\dai_ma\go_projects\excel-master\internal\extractor` |
| `internal/splitter` 3 种单文件拆分 | ✅ | 5 | `@g:\dai_ma\go_projects\excel-master\internal\splitter` |
| `internal/pipeline` 事件 + 取消 | ✅ | — | `@g:\dai_ma\go_projects\excel-master\internal\pipeline` |
| `internal/service` 应用服务层 | ✅ | — | `@g:\dai_ma\go_projects\excel-master\internal\service` |
| `pkg/logger` 统一日志 | ✅ | — | `@g:\dai_ma\go_projects\excel-master\pkg\logger` |
| `app.go` Wails 桥接（7 个方法≤5行） | ✅ | — | `@g:\dai_ma\go_projects\excel-master\app.go` |
| `frontend/` Vue 3 骨架（2 页 + api + store + 组件） | ✅ 代码完成 | — | `@g:\dai_ma\go_projects\excel-master\frontend\src` |
| CLI：`extract-cli` | ✅ 已用真实样本验证 | — | `@g:\dai_ma\go_projects\excel-master\cmd\extract-cli` |
| CLI：`gen-fixture` 生成测试样本 | ✅ | — | `@g:\dai_ma\go_projects\excel-master\cmd\gen-fixture` |

**总单测数**：24+ 个，全绿，`go vet` 零警告。

---

## 二、V1.0 功能清单

### 已完成（后端）

- 文件夹批量关键词提取：✅
- 4 种匹配模式（精准 + 包含 + 拼音全拼 + 拼音首字母）：✅
- 3 种输出策略（按关键词分 / 合并 / 按源文件分）：✅
- 多文件表头不一致（顺序不同/缺列）自动对齐：✅
- 图片跟随行迁移（单元格多图也行）：✅
- 单文件 3 种拆分（按 Sheet / 行数 / 列值）：✅
- 流式读写，1GB 文件不 OOM：✅（架构保证，待压测）
- Ctrl+C 取消 / 任务 ID 注册表：✅
- Wails 事件推送（progress/log/done/error）：✅

### 已完成（前端）

- Vue 3 + Vite 骨架
- 顶部导航 + 两个主视图（批量提取 / 单文件拆分）
- 路径选择器（文件/文件夹，调用原生对话框）
- 关键词输入 + 匹配模式勾选
- 搜索范围选择（全列 / 指定列，指定列时读表头让用户勾）
- 进度面板（状态徽章 + 进度条 + 日志窗口 + 取消按钮 + 结果汇总 + 打开输出目录）
- 深色主题全局样式

### 待做（第 4 周计划）

- 预扫描提示（"共 N 行、M 张图、预计 X 秒"）
- `internal/config/detector.go` 智能降级（按内存/CPU 自动调并发）
- 大文件压测（真实 1GB/2GB 带图 Excel）
- `wails build` 生产打包 + UPX 压缩
- 用户手册 README
- **`wails dev` 真机验证前端**（最紧急）

---

## 三、如何运行和验证

### 准备：安装依赖（只做一次）

1. Go 1.23+（已装）
2. Node.js 16+（Wails 前端需要）
3. Wails CLI：
   ```powershell
   go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
   wails doctor    # 检查环境
   ```

### 路径：在 `excel-master\` 目录下操作

### 方式 A：纯后端 CLI 验证（不用前端）

```powershell
# 1. 生成测试样本（一次即可）
go build -o gen-fixture.exe .\cmd\gen-fixture
.\gen-fixture.exe -out .\testdata_samples

# 2. 构建 CLI
go build -o extract-cli.exe .\cmd\extract-cli

# 3. 跑各种策略
.\extract-cli.exe -src .\testdata_samples -kw "口红, yy, fd" -out .\out_a -strategy per_keyword
.\extract-cli.exe -src .\testdata_samples -kw "口红" -out .\out_b -strategy merged
.\extract-cli.exe -src .\testdata_samples -kw "口红, 眼影" -out .\out_c -strategy per_source
```

**已验证结果**（2026-05-03 06:46 本机实测）：
- 6 行命中、6 张图迁移、3 个输出文件、56ms 完成
- 输出文件用 Excel 打开可见图片正确跟随行

### 方式 B：前端 GUI 开发模式（**推荐你下一步跑这个**）

```powershell
# 在 excel-master 目录下
wails dev
```

首次启动会：
1. 自动重新生成 `frontend/wailsjs/go/main/App.js`（把 app.go 的 7 个方法绑定给前端）
2. 启动前端 Vite dev server
3. 打开原生窗口加载 UI（可热更新）

**应该看到**：
- 顶部"Excel 大文件工具"品牌名 + 两个 tab
- 切换到"批量提取"页：
  - "源文件夹"字段 + "浏览文件夹"按钮（点击弹原生对话框）
  - 关键词输入框
  - 三个匹配模式复选框（默认全选）
  - 表头行号（默认 1）
  - 搜索范围（默认全列）
  - 输出策略三选一（默认每关键词一个）
  - 保留图片（默认开）
  - "开始提取"按钮
  - 底下的进度面板（初始空白）

### 方式 C：打包出 exe（第 4 周会做）

```powershell
wails build
# 输出：build/bin/excel-master.exe
```

---

## 四、测试样本

执行过 `gen-fixture.exe` 后，`testdata_samples/` 目录下有：

- `供应商A_美妆目录.xlsx`：5 款美妆，完整表头 `[产品名, 型号, 价格, 库存, 产品图]`，全部带图
- `供应商B_护肤目录.xlsx`：4 款护肤，表头**顺序不同** `[型号, 产品名, 价格, 产品图]`，部分带图
- `供应商C_杂货目录.xlsx`：3 款杂货，**缺"型号"列** `[产品名, 价格, 产品图]`，部分带图
- `~$临时锁.xlsx` / `说明.txt`：垃圾文件，用于验证扫描器过滤能力

**推荐测试脚本**（GUI 里照着填）：
- 源文件夹：`<项目>\testdata_samples`
- 输出目录：`<项目>\testdata_gui_out`
- 关键词：`口红, yy, fd`（覆盖中文 + 拼音首字母两种）
- 匹配模式：三个都勾
- 搜索范围：全列
- 输出策略：每关键词一个文件
- 保留图片：勾上

**预期结果**：命中 6 行、迁移 6 张图、生成 3 个输出文件（口红_xxx.xlsx / yy_xxx.xlsx / fd_xxx.xlsx）。

---

## 五、已知风险 / 待处理

1. **wails dev 从未运行过** —— 前端事件通路、bindings 生成、Vue 组件渲染全部是代码层面保证，真机可能有问题
2. **大文件未压测** —— 目前最大测过 15 行，1GB 带图场景没验证
3. **预扫描提示未实现** —— 用户点"开始"后要等一会儿才看到进度
4. **wailsjs/go/main/App.js 仍是旧的 Greet 绑定** —— 第一次跑 `wails dev` 时会自动重新生成

---

## 六、三周开发踩过的坑（完整教训在规则文件）

- ✅ `excelize.NewFile()` 默认的空 Sheet1 必须显式删
- ✅ `go mod tidy` 会删除未引用依赖
- ✅ `image: unknown format` 必须 blank import `_ "image/png"` 到 excelio 包

完整坑 + 解决方式见：`@g:\dai_ma\go_projects\.windsurf\rules\excel-tool-rules.md` 第 3.2 节。
