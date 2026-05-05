package extractor

import "excel-master/internal/excelio"

// coerceScalar 薄包装到 excelio.CoerceScalar，避免逻辑重复。
// 详细规则见 excelio/coerce.go 的 CoerceScalar 注释。
func coerceScalar(s string) any { return excelio.CoerceScalar(s) }
