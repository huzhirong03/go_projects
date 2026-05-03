# 打包发布指南

> 把 Excel 拆合大师打包成绿色 zip，发给学员双击即用，不依赖系统装 WebView2。

## 前置准备（一次性）

### 1. 装 Wails CLI

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 2. 下载 WebView2 Fixed Version Runtime

**官方下载入口**：

> https://developer.microsoft.com/en-us/microsoft-edge/webview2/

页面下拉到 **"Get the Fixed Version"**，选 **x64** 平台，下载最新 stable 版本（约 130MB cab 文件）。

### 3. 解压到项目

```powershell
pwsh ./scripts/install-webview2-runtime.ps1 -CabPath "C:\Downloads\Microsoft.WebView2.FixedVersionRuntime.131.0.2903.99.x64.cab"
```

成功后项目根目录出现 `webview2_runtime/`（已 gitignore，不入库）。

---

## 一键打包

每次发版本就跑这一条命令：

```powershell
pwsh ./scripts/build-release.ps1
```

**产物**：`build/release/excel-master-v1.0.0.zip`（约 60-80MB，zip 压缩）

**zip 内部结构**：

```
excel-master-v1.0.0.zip
└── (解压后)
    ├── excel-master.exe          (~15-20 MB)
    ├── webview2_runtime/         (~150 MB 解压后)
    │   ├── msedgewebview2.exe
    │   ├── *.dll, *.pak, ...
    │   └── locales/, etc.
    ├── LICENSE
    ├── README.md
    └── CHANGELOG.md
```

学员**双击 exe** 即可运行，不需要装 WebView2、不需要联网、不写注册表。

---

## 升级版本号流程

发新版本前，**3 处必须同步改**（脚本会校验一致性）：

1. `internal/core/version.go` 里的 `Version` 常量
2. `wails.json` 里的 `info.productVersion`
3. `CHANGELOG.md` 顶部加一节新版本

跑 `./scripts/build-release.ps1` 自动用 `version.go` 里的版本号给 zip 命名。

---

## 不打包 WebView2（小体积发布）

如果学员电脑都已装 WebView2（Win11 / Win10 21H2+ 默认装），可以省 130MB：

```powershell
pwsh ./scripts/build-release.ps1 -SkipWebView2
```

**风险**：老 Win10（< 1809） / Win10 LTSC 没装 WebView2 的电脑会闪退。

---

## 验证清单

打包完成后建议人工 sanity check：

| 步 | 验证项 | 预期 |
|---|---|---|
| 1 | 解压 zip 到一个空文件夹 | 出现 exe + webview2_runtime/ + 文档 |
| 2 | 把整个文件夹拷到 U 盘 / D 盘 / 网盘 | 都能跑 |
| 3 | 双击 exe | 1-3 秒打开窗口，标题显示 "Excel 拆合大师 v1.0.0" |
| 4 | 试一次提取 + 一次拆分 | 功能正常 |
| 5 | 关软件 | exe 同目录出现 `config.json` + `logs/` 子目录 |
| 6 | 删整个文件夹 | 系统 C 盘没有任何残留（除非用户跑过 fallback 模式） |

---

## 故障排查

### exe 双击没反应 / 闪退

**可能 1**：缺 WebView2 但没带 runtime → 加 `-SkipWebView2` 时确认学员装了 WebView2

**可能 2**：WebView2 Fixed Version 版本与系统不兼容 → 下载更老版本（v118 兼容性更好）

**可能 3**：杀毒软件拦截 → 学员加白名单或换免杀打包

### "找不到 msedgewebview2.exe"

打包脚本会自检。如果手动验证：`build/bin/webview2_runtime/msedgewebview2.exe` 必须存在。

### exe 大但 webview2_runtime 没拷过来

检查打包脚本是否报"找不到 webview2_runtime"错。或者跑 `pwsh ./scripts/install-webview2-runtime.ps1` 重新解压。

---

## 后续优化方向（v1.x 之后再考虑）

- **代码签名**：买 EV 代码签名证书，避免 SmartScreen "未知发布者" 警告
- **自动更新**：集成 go-selfupdate 检查 GitHub Release 新版本
- **更小的 WebView2**：去掉 locales 里学员用不到的非中文/英文语言包，可省 30MB
