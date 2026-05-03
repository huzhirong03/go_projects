# 打包发布指南

> 把 Excel 拆合大师打包成可分发产物，发给学员使用。

## 推荐流程：embed 模式（默认）

**学员体验**：双击单个 exe → 第一次启动静默装 WebView2 到系统 → 之后秒开（像微信首次安装的体验）。

**前置准备**（一次性）：

```powershell
# 装 Wails CLI（已装跳过）
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

**每次发版本**：

```powershell
pwsh ./scripts/build-release.ps1
```

**产物**：`build/release/excel-master-v1.0.0-embed.zip`，约 50MB。

**zip 内部**：

```
excel-master-v1.0.0-embed.zip
└── (解压后)
    ├── excel-master.exe         （~140 MB，含 WebView2 installer）
    ├── LICENSE
    ├── README.md
    └── CHANGELOG.md
```

学员**双击 exe** 即可。首次启动会**短暂**联机装 WebView2 到 `%LOCALAPPDATA%\Microsoft\EdgeWebView`（约 5-10 秒），之后所有启动都是秒开。

---

## 备选流程：Fixed Version 模式（真绿色）

**学员体验**：解压 zip → 双击 exe，**零写系统**，整个文件夹可移动到 U 盘 / 网盘 / 任意盘。

适合：你需要"完全便携、不污染系统盘"的发布场景。

**前置**：

1. 装 Wails CLI（同上）
2. 下载 WebView2 Fixed Version Runtime（约 130MB cab 文件）
   - 入口：https://developer.microsoft.com/en-us/microsoft-edge/webview2/
   - 选 "Get the Fixed Version" → x64
3. 解压到项目：

```powershell
pwsh ./scripts/install-webview2-runtime.ps1 -CabPath "C:\Downloads\Microsoft.WebView2.FixedVersionRuntime.131.0.2903.99.x64.cab"
```

**打包**：

```powershell
pwsh ./scripts/build-release.ps1 -Mode fixed
```

**产物**：`build/release/excel-master-v1.0.0-fixed.zip`，约 70MB。

**zip 内部**：

```
excel-master-v1.0.0-fixed.zip
└── (解压后)
    ├── excel-master.exe         （~20 MB）
    ├── webview2_runtime/        （~150 MB 解压后）
    │   ├── msedgewebview2.exe
    │   └── *.dll, *.pak, ...
    ├── LICENSE
    ├── README.md
    └── CHANGELOG.md
```

---

## 升级版本号

发新版本前 **3 处必须同步改**（脚本会校验一致性）：

1. `internal/core/version.go` 里的 `Version` 常量
2. `wails.json` 里的 `info.productVersion`
3. `CHANGELOG.md` 顶部加一节新版本

跑 `./scripts/build-release.ps1` 自动用 `version.go` 里的版本号给 zip 命名。

---

## 验证清单

打包完成后建议人工 sanity check：

| 步 | 验证项 | 预期 |
|---|---|---|
| 1 | 把 zip 拷到一个**没装过 WebView2 的电脑**上解压 | 出现 exe + 文档 |
| 2 | 双击 exe | embed 模式：首次 5-10 秒后窗口出现；fixed 模式：1-3 秒立刻出现 |
| 3 | 窗口标题显示 "Excel 拆合大师 v1.0.0" | ✅ |
| 4 | topbar 显示 "Excel 拆合大师 v1.0.0 · 大荣老师出品" | ✅ |
| 5 | 试一次提取 + 一次拆分 | 功能正常 |
| 6 | 关软件 | exe 同目录出现 `config.json` + `logs/` 子目录 |
| 7 | 点击右上角 "📂 日志" 按钮 | 自动打开 logs 文件夹 |

---

## 故障排查

### exe 双击没反应 / 闪退

- **embed 模式**：可能 WebView2 安装失败 → 让学员手动装 [Microsoft 官方 WebView2 Runtime](https://go.microsoft.com/fwlink/p/?LinkId=2124703)
- **fixed 模式**：检查 `webview2_runtime/msedgewebview2.exe` 是否在解压后的位置；版本与系统不兼容时换更老版本（v118 兼容性最好）

### 杀毒软件拦截

加白名单。这是 Wails / Go 编译产物的常见误报，无解（除非买代码签名证书）。

### "找不到 msedgewebview2.exe"（fixed 模式）

打包脚本会自检。如果手动验证：`build/bin/webview2_runtime/msedgewebview2.exe` 必须存在。

---

## 后续优化方向（v1.x 之后再考虑）

- **代码签名**：买 EV 代码签名证书，避免 SmartScreen "未知发布者" 警告
- **自动更新**：集成 go-selfupdate 检查 GitHub Release 新版本
- **更小的 Fixed Version**：去掉 locales 里学员用不到的非中文/英文语言包，可省 30MB
