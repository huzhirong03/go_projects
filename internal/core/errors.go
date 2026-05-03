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
