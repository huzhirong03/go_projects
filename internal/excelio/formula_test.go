package excelio

import "testing"

func TestRewriteFormulaSameRow(t *testing.T) {
	cases := []struct {
		name    string
		formula string
		srcRow  int
		dstRow  int
		want    string
		wantOk  bool
	}{
		// 同行简单乘法：偏移
		{"simple_mul", "=B5*C5", 5, 2, "=B2*C2", true},
		// 嵌入函数：偏移
		{"with_func", "=ROUND(B5*C5,2)", 5, 2, "=ROUND(B2*C2,2)", true},
		// 列绝对、行相对：偏移
		{"abs_col", "=$B5*C5", 5, 2, "=$B2*C2", true},
		// 行绝对：不安全
		{"abs_row", "=B$5*C5", 5, 2, "", false},
		// 跨行单 cell：不安全
		{"cross_row", "=B4*C5", 5, 2, "", false},
		// 跨行范围：不安全
		{"cross_row_range", "=SUM(B5:B10)", 5, 2, "", false},
		// 同行范围（少见但语义合法）：每端点都在 srcRow → 偏移
		{"same_row_range", "=SUM(B5:E5)", 5, 2, "=SUM(B2:E2)", true},
		// 跨 Sheet：不安全
		{"cross_sheet", "=Sheet2!B5", 5, 2, "", false},
		// 跨工作簿：不安全
		{"cross_workbook", "=[Book2.xlsx]Sheet1!B5", 5, 2, "", false},
		// 没有 cell 引用：安全
		{"no_refs", "=10+20", 5, 2, "=10+20", true},
		// 已经在目标行（无意义但应安全）
		{"noop", "=B2*C2", 2, 2, "=B2*C2", true},
		// 多列长引用
		{"long_col", "=AA5*AB5", 5, 2, "=AA2*AB2", true},
		// 空公式
		{"empty", "", 5, 2, "", false},
		// 没有 = 前缀（excelize 偶尔返回这种）
		{"no_eq", "B5*C5", 5, 2, "=B2*C2", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RewriteFormulaSameRow(tc.formula, tc.srcRow, tc.dstRow)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v (got=%q)", ok, tc.wantOk, got)
			}
			if ok && got != tc.want {
				t.Errorf("got = %q, want %q", got, tc.want)
			}
		})
	}
}
