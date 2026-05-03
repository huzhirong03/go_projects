# Excel 拆合大师 (Excel Master Suite)

> 一键拆分、提取合并 Excel 工作簿，**100% 保留样式 / 图片 / 公式 / 合并单元格**。

![version](https://img.shields.io/badge/version-v1.0.0-blue) ![platform](https://img.shields.io/badge/platform-Windows%2010%2B-success) ![license](https://img.shields.io/badge/license-Authorized%20Use%20Only-orange)

---

## 这是什么

为日常办公场景设计的 Excel 批量处理工具，专注两件事：

1. **拆分** — 把一个 xlsx 按多种规则拆成多个文件
2. **提取合并** — 从多个 xlsx 中按关键词或条件筛选数据，汇总到新文件

跟市面同类工具相比，最大特点是 **100% 保真**：拆完 / 提完的文件里图片、合并单元格、公式、样式全部跟原文件一模一样。

## 功能一览

### 批量提取

- 支持源：`.xlsx` / `.xlsm` / `.csv`（CSV 自动识别 GBK / UTF-8 编码）
- 关键词匹配：精确 / 包含 / 拼音首字母 / 全拼
- 高级筛选：数值范围、日期、文本、空值，多列 AND / OR 组合
- 输出策略：合并到一个文件 / 每个源文件一个 / 写回源文件新增 Sheet

### 单文件拆分

- 按 Sheet 拆 — 每个 Sheet 一个独立 xlsx
- 按行数拆 — 每 N 行一个分片
- 按列值拆 — 相同列值的行汇总到一个文件
- 按关键词拆 — 每个关键词的命中行一个文件

### 全程保真

通过底层 zip 手术绕开 excelize Save 的样式丢失问题，保留：

- 单元格样式、字体、颜色、边框
- 合并单元格、条件格式、数据验证
- 公式（行号自动偏移）、冻结窗格、列宽行高、筛选器
- 单元格内嵌图片（drawing 锚点重映射）

## 系统要求

- **Windows 10 1809（2018 年 10 月）或更高版本**
- **不需要预装 Excel**
- **不需要联网** — 内置 WebView2 Fixed Version Runtime

## 快速开始（学员）

1. 解压 `excel-master-vX.Y.Z.zip` 到任意位置（U 盘 / D 盘 / 桌面都行）
2. 双击 `excel-master.exe`
3. 详细使用方法请参考视频教程

> 💡 配置和日志都会在 exe 同目录生成。整个文件夹可任意拷贝、移动，**不会写注册表、不污染系统盘**。

## 反馈 bug

软件菜单 → "打开日志文件夹" → 把最新的 `task-*.log` + 截图发邮箱：**379705723@qq.com**

---

## 项目结构（开发者）

```
excel-master/
├── main.go               启动入口（log tee + Wails Run）
├── app.go                Wails 桥接层（每个方法 ≤ 5 行）
├── internal/             所有业务代码
│   ├── core/             领域模型 + 接口 + 错误类型 + 版本常量
│   ├── splitter/         拆分模块（按行 / Sheet / 列值 / 关键词）
│   ├── extractor/        提取模块（关键词匹配 + 高级筛选）
│   ├── excelio/          excelize 流式封装 + zip 手术
│   ├── matcher/          关键词匹配引擎（含拼音）
│   ├── filter/           高级筛选条件引擎
│   ├── pipeline/         任务调度 / 进度 / 取消
│   ├── service/          Wails 入口编排层（含 panic recover）
│   └── config/           配置 & 智能降级
├── frontend/             Vue 3 + Vite 前端
│   └── src/
│       ├── views/        SplitView / ExtractView 等页面
│       ├── components/   通用组件
│       ├── api/          唯一调用 Go 的地方（封装 wailsjs RPC）
│       └── stores/       Pinia 状态
├── cmd/                  命令行工具集
│   ├── extract-cli/      批量提取 CLI（无 GUI 跑）
│   ├── split-cli/        单文件拆分 CLI
│   ├── gen-fixture/      生成测试样本
│   ├── diag-*/           开发期诊断工具（不打包进主应用）
│   └── verify-*/         结果校验工具
└── pkg/logger/           可复用日志包
```

### 开发命令

```powershell
# 启动 dev 模式（前后端热重载）
wails dev

# 跑全部 Go 测试
go test ./... -count=1

# 前端构建
cd frontend; npm run build

# 打包 release exe（带 WebView2 Fixed Version）
wails build -webview2 browser
```

### 架构原则

- **业务代码必须在 `internal/`**，不放根目录
- **`app.go` 每方法 ≤ 5 行**，全部转发到 `internal/service/`
- **前端调 Go 只走 `frontend/src/api/`**，组件里禁止 `window.go.xxx`
- **跨模块依赖走 `internal/core/` 的接口**，不直接 import 实现
- **大文件流式 API**，禁止 `GetRows` 一次加载

详见 `internal/core/types.go` 的接口定义。

## 版本与变更

- 当前版本：**v1.0.0**
- 完整变更历史：[`CHANGELOG.md`](./CHANGELOG.md)

## 授权

仅授权学员学习与日常办公使用，详见 [`LICENSE`](./LICENSE)。

商业授权请邮件咨询：379705723@qq.com
