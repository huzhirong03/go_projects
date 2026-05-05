package extractor

import "testing"

func TestCoerceScalar(t *testing.T) {
	tests := []struct {
		in       string
		wantNum  bool
		wantF    float64
		wantStr  string
	}{
		// 应转为数字
		{"89", true, 89, ""},
		{"436", true, 436, ""},
		{"1.5", true, 1.5, ""},
		{"0", true, 0, ""},
		{"-42", true, -42, ""},
		{"-3.14", true, -3.14, ""},
		{"1234567890", true, 1234567890, ""}, // 10 位整数刚好通过

		// 应保留字符串
		{"", false, 0, ""},
		{"abc", false, 0, "abc"},
		{"0123", false, 0, "0123"},         // 前导 0（学号/邮编常见）
		{"00", false, 0, "00"},
		{"1.50", false, 0, "1.50"},         // 尾随 0
		{"+89", false, 0, "+89"},           // 带正号
		{"13812345678", false, 0, "13812345678"}, // 手机号 11 位
		{"310101199001011234", false, 0, "310101199001011234"}, // 身份证 18 位
		{"1e5", false, 0, "1e5"},           // 科学计数法
		{"1E3", false, 0, "1E3"},
		{" 89", false, 0, " 89"},           // 前导空格
		{"89 ", false, 0, "89 "},           // 尾随空格
		{"12,345", false, 0, "12,345"},     // 千分位
	}
	for _, tc := range tests {
		got := coerceScalar(tc.in)
		if tc.wantNum {
			f, ok := got.(float64)
			if !ok {
				t.Errorf("coerceScalar(%q) = %T(%v), want float64", tc.in, got, got)
				continue
			}
			if f != tc.wantF {
				t.Errorf("coerceScalar(%q) = %v, want %v", tc.in, f, tc.wantF)
			}
		} else {
			s, ok := got.(string)
			if !ok {
				t.Errorf("coerceScalar(%q) = %T(%v), want string", tc.in, got, got)
				continue
			}
			want := tc.wantStr
			if want == "" {
				want = tc.in
			}
			if s != want {
				t.Errorf("coerceScalar(%q) = %q, want %q", tc.in, s, want)
			}
		}
	}
}
