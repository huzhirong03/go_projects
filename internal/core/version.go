package core

// 应用元信息常量。
// 改版本号时必须同步：
//   - CHANGELOG.md 加一节
//   - wails.json 的 info.productVersion
//
// 这三处不一致会让用户看到的版本号自相矛盾，发版本前必须自查。

const (
	// AppName 中文应用名，前端 About 弹窗 / Toast 友好提示用。
	AppName = "Excel 拆合大师"

	// WindowTitle 主窗口标题栏显示的文本（任务栏/Alt+Tab 也用这个）。
	// 独立于 AppName：品牌宣传文案可换，但 AppName/AppNameEn 保持稳定供
	// exe 属性页 / wails.json productName 用，避免破坏注册表/快捷方式等。
	WindowTitle = "学AI 提效率，办公不费力"

	// AppNameEn 英文名，wails.json info.productName / 系统 exe 属性页用。
	AppNameEn = "Excel Master Suite"

	// Version 当前应用版本号。语义化版本：vMAJOR.MINOR.PATCH
	//   - MAJOR：破坏性改动（用户配置/输出格式不兼容）
	//   - MINOR：新增功能 / 兼容改动
	//   - PATCH：修 bug，无新功能
	Version = "v1.4.1"

	// BrandTagline 品牌副标，跟在版本号后面用 · 分隔显示。
	// 学员一眼能看到出处，做教程视频时也是天然的口播识别。
	BrandTagline = "大荣老师出品"

	// Author 作者署名，About 弹窗 / 系统属性页用。
	Author = "huzhirong03"

	// AuthorEmail 反馈邮箱。
	AuthorEmail = "379705723@qq.com"

	// Copyright 版权声明字符串。
	Copyright = "Copyright © 2026 huzhirong03"
)
