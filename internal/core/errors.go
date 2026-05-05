package core

import (
	"errors"
	"fmt"
)

// 领域错误前哨。所有模块的错误都必须是 AppError 或 wrap 到 AppError。
var (
	ErrInvalidTask    = errors.New("invalid task")
	ErrFileNotFound   = errors.New("file not found")
	ErrUnsupportedFmt = errors.New("unsupported file format")
	ErrHeaderNotFound = errors.New("header row not found")
	ErrColumnNotFound = errors.New("column not found in header")
	ErrCanceled       = errors.New("task canceled")
	ErrImageMigrate   = errors.New("image migration failed")
	ErrOutputConflict = errors.New("output path already exists")
	ErrCSVDecode      = errors.New("csv decode failed")
)

// AppError.Code 常量：跨模块统一标识，前端按此分类提示。
const (
	CodeSourceFormatUnsupported = "SOURCE_FORMAT_UNSUPPORTED"
	CodeCSVOpenFailed           = "CSV_OPEN_FAILED"
	CodeCSVDecodeFailed         = "CSV_DECODE_FAILED"
	CodeCSVEncodingUnknown      = "CSV_ENCODING_UNKNOWN"

	// CodeEmptySheet Sheet 完全空（0 行）。语义跟 "HEADER_ROW_MISSING" 区分：
	// 后者是"sheet 有数据但行数 < headerRow"（用户填错 headerRow），仍属配置错误；
	// EMPTY_SHEET 是"sheet 一行都没有"（典型场景：Excel 转 CSV 时留下的空 Sheet1），
	// 调用方应该跳过该 sheet + 记 warning，不要 fatal。
	CodeEmptySheet = "EMPTY_SHEET"
)

// AppError 统一应用错误类型，便于前端按 Code 展示。
type AppError struct {
	Code    string // 机读码，如 "EXCEL_READ_FAILED"
	Message string // 面向用户的可读信息
	Cause   error  // 底层原因
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Cause }

// Wrap 把底层错误包成 AppError。
func Wrap(code, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Cause: cause}
}

// New 生成一个不带底层原因的 AppError。
func New(code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// IsEmptySheet 判断 err 是否为 EMPTY_SHEET 错误（经过 errors.As 解包）。
// 调用方遇到这种错误应"跳过该 Sheet + 继续处理其他 Sheet"，而不是 fatal。
// 典型场景：Excel 把 CSV 另存为 xlsx 时残留的空 Sheet1、用户模板里预留的空白 Sheet 等。
func IsEmptySheet(err error) bool {
	var ae *AppError
	return errors.As(err, &ae) && ae.Code == CodeEmptySheet
}
