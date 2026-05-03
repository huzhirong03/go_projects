package filter

import (
	"testing"
)

// 公共 headers：0=姓名 1=班级 2=总分 3=平均分 4=备注 5=下单日期 6=手机号 7=邮箱
var testHeaders = []string{"姓名", "班级", "总分", "平均分", "备注", "下单日期", "手机号", "邮箱"}

func mkrow(vals ...string) []string { return vals }

// 公共组装：Compile + 预期无错
func mustCompile(t *testing.T, mode Mode, conds ...Condition) *Filter {
	t.Helper()
	f, err := Compile(Spec{Mode: mode, Conditions: conds}, testHeaders)
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	return f
}

// --- 基本谓词 ---

func TestEqualAndNotEqual(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{Column: "班级", Op: OpEqual, Value: "六年级1班"})
	if !f.Apply(mkrow("张三", "六年级1班", "", "", "", "", "", "")) {
		t.Fatal("eq 应命中")
	}
	if f.Apply(mkrow("李四", "六年级2班", "", "", "", "", "", "")) {
		t.Fatal("eq 不该命中")
	}

	fn := mustCompile(t, ModeAll, Condition{Column: "班级", Op: OpNotEqual, Value: "六年级1班"})
	if fn.Apply(mkrow("张三", "六年级1班", "", "", "", "", "", "")) {
		t.Fatal("ne 不该命中")
	}
	if !fn.Apply(mkrow("李四", "六年级2班", "", "", "", "", "", "")) {
		t.Fatal("ne 应命中")
	}
}

func TestNumericCompare(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{Column: "总分", Op: OpGreaterOrEqual, Value: "400"})
	if !f.Apply(mkrow("", "", "441", "", "", "", "", "")) {
		t.Fatal("ge 441 >= 400 应命中")
	}
	if f.Apply(mkrow("", "", "399", "", "", "", "", "")) {
		t.Fatal("ge 399 不该命中")
	}
	if f.Apply(mkrow("", "", "abc", "", "", "", "", "")) {
		t.Fatal("非数值文本在数值比较下不该命中")
	}
	// 空 cell 不命中
	if f.Apply(mkrow("", "", "", "", "", "", "", "")) {
		t.Fatal("空 cell 不该命中数值比较")
	}
}

func TestBetween(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{Column: "总分", Op: OpBetween, Value: "400", Value2: "500"})
	if !f.Apply(mkrow("", "", "450", "", "", "", "", "")) {
		t.Fatal("450 ∈ [400,500] 应命中")
	}
	if !f.Apply(mkrow("", "", "400", "", "", "", "", "")) {
		t.Fatal("400 闭区间端点应命中")
	}
	if f.Apply(mkrow("", "", "501", "", "", "", "", "")) {
		t.Fatal("501 不该命中")
	}

	// min > max 自动归一化
	f2 := mustCompile(t, ModeAll, Condition{Column: "总分", Op: OpBetween, Value: "500", Value2: "400"})
	if !f2.Apply(mkrow("", "", "450", "", "", "", "", "")) {
		t.Fatal("min/max 反了后自动归一化，450 应命中")
	}

	// not_between
	fn := mustCompile(t, ModeAll, Condition{Column: "总分", Op: OpNotBetween, Value: "400", Value2: "500"})
	if !fn.Apply(mkrow("", "", "399", "", "", "", "", "")) {
		t.Fatal("not_between: 399 应命中")
	}
	if fn.Apply(mkrow("", "", "450", "", "", "", "", "")) {
		t.Fatal("not_between: 450 不该命中")
	}
}

func TestTextOps(t *testing.T) {
	// 包含（大小写不敏感）
	f := mustCompile(t, ModeAll, Condition{Column: "备注", Op: OpContains, Value: "退款"})
	if !f.Apply(mkrow("", "", "", "", "请尽快退款处理", "", "", "")) {
		t.Fatal("contains 应命中")
	}

	// starts_with
	fs := mustCompile(t, ModeAll, Condition{Column: "邮箱", Op: OpStartsWith, Value: "admin"})
	if !fs.Apply(mkrow("", "", "", "", "", "", "", "admin@qq.com")) {
		t.Fatal("starts_with 应命中")
	}
	if fs.Apply(mkrow("", "", "", "", "", "", "", "user@admin.com")) {
		t.Fatal("starts_with 不该命中（关键词在中间）")
	}

	// ends_with
	fe := mustCompile(t, ModeAll, Condition{Column: "邮箱", Op: OpEndsWith, Value: "@qq.com"})
	if !fe.Apply(mkrow("", "", "", "", "", "", "", "zhang@qq.com")) {
		t.Fatal("ends_with 应命中")
	}
	if fe.Apply(mkrow("", "", "", "", "", "", "", "zhang@163.com")) {
		t.Fatal("ends_with 不该命中")
	}
}

func TestInList(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{Column: "班级", Op: OpIn, Value: "六年级1班, 六年级2班,六年级3班"})
	if !f.Apply(mkrow("", "六年级1班", "", "", "", "", "", "")) {
		t.Fatal("in 应命中")
	}
	if f.Apply(mkrow("", "五年级1班", "", "", "", "", "", "")) {
		t.Fatal("in 不该命中")
	}

	fn := mustCompile(t, ModeAll, Condition{Column: "班级", Op: OpNotIn, Value: "六年级1班, 六年级2班"})
	if !fn.Apply(mkrow("", "五年级1班", "", "", "", "", "", "")) {
		t.Fatal("not_in 应命中")
	}
	if fn.Apply(mkrow("", "六年级1班", "", "", "", "", "", "")) {
		t.Fatal("not_in 不该命中")
	}
}

func TestEmpty(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{Column: "备注", Op: OpEmpty})
	if !f.Apply(mkrow("", "", "", "", "", "", "", "")) {
		t.Fatal("empty: 空 cell 应命中")
	}
	if !f.Apply(mkrow("", "", "", "", "   ", "", "", "")) {
		t.Fatal("empty: 全空白应命中")
	}
	if f.Apply(mkrow("", "", "", "", "x", "", "", "")) {
		t.Fatal("empty: 有值不该命中")
	}

	fn := mustCompile(t, ModeAll, Condition{Column: "备注", Op: OpNotEmpty})
	if fn.Apply(mkrow("", "", "", "", "", "", "", "")) {
		t.Fatal("not_empty: 空不该命中")
	}
	if !fn.Apply(mkrow("", "", "", "", "重要", "", "", "")) {
		t.Fatal("not_empty: 有值应命中")
	}
}

func TestDateBetween(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{
		Column: "下单日期", Op: OpDateBetween,
		Value: "2026-01-01", Value2: "2026-03-31",
	})
	if !f.Apply(mkrow("", "", "", "", "", "2026-02-15", "", "")) {
		t.Fatal("date_between: 2 月份应命中")
	}
	// 闭区间端点
	if !f.Apply(mkrow("", "", "", "", "", "2026-01-01", "", "")) {
		t.Fatal("date_between: 起点应命中")
	}
	if !f.Apply(mkrow("", "", "", "", "", "2026-03-31", "", "")) {
		t.Fatal("date_between: 终点应命中")
	}
	if f.Apply(mkrow("", "", "", "", "", "2026-04-01", "", "")) {
		t.Fatal("date_between: 区间外不该命中")
	}
	// 非日期文本
	if f.Apply(mkrow("", "", "", "", "", "not_date", "", "")) {
		t.Fatal("date_between: 非日期文本不该命中")
	}
}

func TestMatchFormat(t *testing.T) {
	f := mustCompile(t, ModeAll, Condition{Column: "手机号", Op: OpMatchFormat, Format: FormatPhoneCN})
	if !f.Apply(mkrow("", "", "", "", "", "", "13812345678", "")) {
		t.Fatal("phone_cn: 正常手机号应命中")
	}
	if f.Apply(mkrow("", "", "", "", "", "", "1234567", "")) {
		t.Fatal("phone_cn: 非手机号不该命中")
	}

	// not_match_format
	fn := mustCompile(t, ModeAll, Condition{Column: "手机号", Op: OpNotMatchFormat, Format: FormatPhoneCN})
	if fn.Apply(mkrow("", "", "", "", "", "", "13812345678", "")) {
		t.Fatal("not phone_cn: 正常手机号不该命中")
	}
	if !fn.Apply(mkrow("", "", "", "", "", "", "xyz", "")) {
		t.Fatal("not phone_cn: 非手机号应命中")
	}

	// email
	fe := mustCompile(t, ModeAll, Condition{Column: "邮箱", Op: OpMatchFormat, Format: FormatEmail})
	if !fe.Apply(mkrow("", "", "", "", "", "", "", "user@domain.com")) {
		t.Fatal("email 正常应命中")
	}
	if fe.Apply(mkrow("", "", "", "", "", "", "", "not-an-email")) {
		t.Fatal("email 非法不该命中")
	}
}

// --- 组合模式：AND / OR ---

func TestModeAll(t *testing.T) {
	f := mustCompile(t, ModeAll,
		Condition{Column: "总分", Op: OpGreaterOrEqual, Value: "400"},
		Condition{Column: "备注", Op: OpNotEmpty},
	)
	// 两个都满足
	if !f.Apply(mkrow("张三", "", "441", "", "重要", "", "", "")) {
		t.Fatal("AND 两条都满足应命中")
	}
	// 总分不满足
	if f.Apply(mkrow("张三", "", "300", "", "重要", "", "", "")) {
		t.Fatal("AND 一条不满足不该命中")
	}
	// 备注不满足
	if f.Apply(mkrow("张三", "", "441", "", "", "", "", "")) {
		t.Fatal("AND 一条不满足不该命中")
	}
}

func TestModeAny(t *testing.T) {
	f := mustCompile(t, ModeAny,
		Condition{Column: "总分", Op: OpGreaterOrEqual, Value: "400"},
		Condition{Column: "备注", Op: OpContains, Value: "保送"},
	)
	// 总分满足即可
	if !f.Apply(mkrow("", "", "441", "", "", "", "", "")) {
		t.Fatal("OR 只要一条满足即命中")
	}
	// 仅备注
	if !f.Apply(mkrow("", "", "300", "", "保送生", "", "", "")) {
		t.Fatal("OR 备注满足即命中")
	}
	// 都不满足
	if f.Apply(mkrow("", "", "300", "", "普通", "", "", "")) {
		t.Fatal("OR 都不满足不该命中")
	}
}

// --- 边界 / 错误 ---

func TestEmptySpecIsZero(t *testing.T) {
	f, _ := Compile(Spec{}, testHeaders)
	if !f.IsZero() {
		t.Fatal("空 Spec 应 IsZero")
	}
	if !f.Apply(mkrow("", "", "", "", "", "", "", "")) {
		t.Fatal("IsZero 的 filter Apply 永远 true")
	}
}

func TestNilApplyAlwaysTrue(t *testing.T) {
	var f *Filter
	if !f.Apply(mkrow("a", "b")) {
		t.Fatal("nil Filter.Apply 应返回 true")
	}
}

func TestMissingColumnCollected(t *testing.T) {
	f, err := Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "不存在的列", Op: OpEqual, Value: "x"},
			{Column: "总分", Op: OpGreaterOrEqual, Value: "400"},
		},
	}, testHeaders)
	if err != nil {
		t.Fatalf("Compile 不该硬失败: %v", err)
	}
	if len(f.MissingColumns) != 1 || f.MissingColumns[0] != "不存在的列" {
		t.Fatalf("应记录 missing 列，实际 %v", f.MissingColumns)
	}
	// 其余条件仍生效
	if !f.Apply(mkrow("", "", "441", "", "", "", "", "")) {
		t.Fatal("剩下的条件应照常求值")
	}
}

func TestColumnCaseAndTrim(t *testing.T) {
	// headers 里叫 "总分"，筛选用 "  总分  "
	f, err := Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "  总分  ", Op: OpGreaterOrEqual, Value: "400"},
		},
	}, testHeaders)
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	if len(f.MissingColumns) != 0 {
		t.Fatalf("带空格的列名应通过 trim 匹配，而不是 missing: %v", f.MissingColumns)
	}
	if !f.Apply(mkrow("", "", "450", "", "", "", "", "")) {
		t.Fatal("应命中")
	}

	// 英文列名大小写不敏感
	eh := []string{"Name", "Amount"}
	f2, err := Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "AMOUNT", Op: OpGreaterThan, Value: "10"},
		},
	}, eh)
	if err != nil {
		t.Fatalf("Compile 失败: %v", err)
	}
	if len(f2.MissingColumns) != 0 {
		t.Fatalf("大小写不敏感应匹配，而不是 missing: %v", f2.MissingColumns)
	}
	if !f2.Apply(mkrow("bob", "25")) {
		t.Fatal("应命中")
	}
}

func TestCompileHardErrors(t *testing.T) {
	// between 值不是数
	_, err := Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "总分", Op: OpBetween, Value: "abc", Value2: "def"},
		},
	}, testHeaders)
	if err == nil {
		t.Fatal("between 非数值应 Compile 硬失败")
	}

	// 日期值非法
	_, err = Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "下单日期", Op: OpDateBetween, Value: "bad", Value2: "worse"},
		},
	}, testHeaders)
	if err == nil {
		t.Fatal("date_between 非法日期应 Compile 硬失败")
	}

	// 未知格式
	_, err = Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "手机号", Op: OpMatchFormat, Format: "unknown_format"},
		},
	}, testHeaders)
	if err == nil {
		t.Fatal("未知格式应 Compile 硬失败")
	}

	// 未知 op
	_, err = Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "手机号", Op: Op("xxx"), Value: "1"},
		},
	}, testHeaders)
	if err == nil {
		t.Fatal("未知 op 应 Compile 硬失败")
	}

	// in 列表空
	_, err = Compile(Spec{
		Mode: ModeAll,
		Conditions: []Condition{
			{Column: "班级", Op: OpIn, Value: "   "},
		},
	}, testHeaders)
	if err == nil {
		t.Fatal("in 空列表应 Compile 硬失败")
	}
}

// --- 性能相关：预设正则缓存只编译一次 ---

func TestPresetRegexCached(t *testing.T) {
	re1 := compilePreset(FormatPhoneCN)
	re2 := compilePreset(FormatPhoneCN)
	if re1 != re2 {
		t.Fatal("预设正则应缓存复用同一 *Regexp 实例")
	}
}
