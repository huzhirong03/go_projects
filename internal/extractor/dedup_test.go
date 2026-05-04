package extractor

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

// mkDeduper 是测试辅助：按单列 + headers 构造 deduper 并 Bind。
// 等价于老 API `newDeduper(col)` + `Bind(headers)`，减少测试 boilerplate。
func mkDeduper(col string, headers []string) *deduper {
	d := newDeduper(dedupConfig{Columns: []string{col}})
	d.Bind(headers)
	return d
}

// mkDeduperMulti 多列版本；cols 为空时构造出的 deduper 是 no-op。
func mkDeduperMulti(cols []string, headers []string, ignoreSpace, ignoreCase bool) *deduper {
	d := newDeduper(dedupConfig{
		Columns:     cols,
		IgnoreSpace: ignoreSpace,
		IgnoreCase:  ignoreCase,
	})
	d.Bind(headers)
	return d
}

func TestDeduper_DisabledOnEmptyColumn(t *testing.T) {
	d := newDeduper(dedupConfig{Columns: nil})
	d.Bind([]string{"name", "age"})
	if d.Enabled() {
		t.Fatalf("空 Columns 的 deduper 应为 disabled")
	}
	if d.ShouldDrop("", []any{"anything"}) {
		t.Errorf("disabled deduper ShouldDrop 应恒返回 false")
	}
}

func TestDeduper_DisabledWhenColumnNotFound(t *testing.T) {
	d := newDeduper(dedupConfig{Columns: []string{"not_exist"}})
	indices := d.Bind([]string{"name", "age"})
	if len(indices) != 1 || indices[0] != -1 {
		t.Errorf("列不存在应在 indices 里留 -1，实际 %v", indices)
	}
	if d.Enabled() {
		t.Errorf("找不到列的 deduper 应为 disabled")
	}
	if d.ShouldDrop("", []any{"anything"}) {
		t.Errorf("列不存在时 ShouldDrop 应恒返回 false")
	}
}

func TestDeduper_BasicDedup(t *testing.T) {
	d := mkDeduper("product", []string{"id", "product", "price"})
	if !d.Enabled() {
		t.Fatalf("绑定成功应为 enabled")
	}

	if d.ShouldDrop("", []any{"1", "Toyota", 100}) {
		t.Errorf("首次出现不应被 drop")
	}
	if !d.ShouldDrop("", []any{"2", "Toyota", 200}) {
		t.Errorf("第二次 Toyota 应被 drop（同 bucket）")
	}
	if d.ShouldDrop("", []any{"3", "Honda", 300}) {
		t.Errorf("Honda 首次出现不应被 drop")
	}
	if !d.ShouldDrop("", []any{"4", "Honda", 400}) {
		t.Errorf("第二次 Honda 应被 drop")
	}
}

// 空值不参与去重：两行 product 都是 nil/空 -> 两行都保留
func TestDeduper_EmptyValueNotDeduped(t *testing.T) {
	d := mkDeduper("product", []string{"id", "product"})

	cases := []any{nil, "", "   ", "\t\n"}
	for i, v := range cases {
		if d.ShouldDrop("", []any{i, v}) {
			t.Errorf("case %d 空值 %v 不应被 drop", i, v)
		}
	}
}

// 桶隔离：per_keyword 场景下同一 product 在不同 kw 桶各自保留首次
func TestDeduper_BucketIsolation(t *testing.T) {
	d := mkDeduper("product", []string{"id", "product"})

	if d.ShouldDrop("丰田", []any{1, "卡罗拉"}) {
		t.Errorf("丰田桶首次卡罗拉不应 drop")
	}
	if d.ShouldDrop("本田", []any{2, "卡罗拉"}) {
		t.Errorf("本田桶里的卡罗拉不应被丰田桶的历史影响")
	}
	if !d.ShouldDrop("丰田", []any{3, "卡罗拉"}) {
		t.Errorf("丰田桶第二次卡罗拉应 drop")
	}
}

// excelize.Cell 包装的 Value 参与去重（公式 cell 应该按其值去重，不是按公式文本）
func TestDeduper_ExcelizeCellValue(t *testing.T) {
	d := mkDeduper("product", []string{"id", "product"})

	cell1 := excelize.Cell{Value: "Toyota", Formula: "=A1&B1"}
	cell2 := excelize.Cell{Value: "Toyota", Formula: "=X1"}
	if d.ShouldDrop("", []any{1, cell1}) {
		t.Errorf("首次 cell 不应 drop")
	}
	if !d.ShouldDrop("", []any{2, cell2}) {
		t.Errorf("不同公式但相同 Value 应去重")
	}
}

// strict 比较（默认两个开关都 false 时）：带尾空格不等价；大小写不等价
func TestDeduper_StrictComparison(t *testing.T) {
	d := mkDeduper("x", []string{"x"})

	inputs := []any{"丰田", "丰田 ", "Toyota", "TOYOTA"}
	for i, v := range inputs {
		if d.ShouldDrop("", []any{v}) {
			t.Errorf("strict 模式下 %v (%d) 不应被视为重复", v, i)
		}
	}
	// 真重复
	if !d.ShouldDrop("", []any{"丰田"}) {
		t.Errorf("真正相同的字符串 '丰田' 应被 drop")
	}
}

// 跨类型等价：int(1) / float(1.0) / string("1") 在 fmt.Sprint 下输出都是 "1"，
// 会被视为重复。这是有意的 normalize 行为——Excel 解析同一列混合类型很常见。
func TestDeduper_CrossTypeNumericEquivalence(t *testing.T) {
	d := mkDeduper("id", []string{"id"})

	if d.ShouldDrop("", []any{int(1)}) {
		t.Errorf("int(1) 首次不应 drop")
	}
	if !d.ShouldDrop("", []any{float64(1.0)}) {
		t.Errorf("float64(1.0) 应被视为与 int(1) 等价")
	}
	if !d.ShouldDrop("", []any{"1"}) {
		t.Errorf("string \"1\" 应被视为与 int(1) 等价")
	}
	// 但 "1" 和 "1.0" 字符串是不同的
	if d.ShouldDrop("", []any{"1.0"}) {
		t.Errorf("string \"1.0\" 与 int(1) 不等价（其 fmt.Sprint 输出为 \"1.0\"）")
	}
}

// values 长度小于索引时不 drop（跨文件 schema 不一致的防御）
func TestDeduper_ShortRow(t *testing.T) {
	d := mkDeduper("product", []string{"id", "product", "price"}) // keyIdx=1
	if d.ShouldDrop("", []any{"only_id"}) {
		t.Errorf("values 长度不够时不应 drop")
	}
}

func TestDedupKeyForCell(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"empty-str", "", ""},
		{"whitespace", "   ", ""},
		{"tab-newline", "\t\n", ""},
		{"str", "Toyota", "Toyota"},
		{"str-trailing-space", "丰田 ", "丰田 "}, // strict 保留原样
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool", true, "true"},
		{"cell", excelize.Cell{Value: "ABC", Formula: "=X"}, "ABC"},
		{"cell-empty-value", excelize.Cell{Value: "", Formula: "=Y"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dedupKeyForCell(tt.in); got != tt.want {
				t.Errorf("dedupKeyForCell(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFindColumnIndex(t *testing.T) {
	cols := []string{"id", "product", "price"}
	if got := findColumnIndex(cols, "product"); got != 1 {
		t.Errorf("product idx = %d, want 1", got)
	}
	if got := findColumnIndex(cols, "not_exist"); got != -1 {
		t.Errorf("not_exist idx = %d, want -1", got)
	}
	if got := findColumnIndex(cols, ""); got != -1 {
		t.Errorf("空串 idx = %d, want -1", got)
	}
	// strict: Product 和 product 不等
	if got := findColumnIndex(cols, "Product"); got != -1 {
		t.Errorf("大小写应区分, Product idx = %d, want -1", got)
	}
}

// ======================================================================
// V1.2 新增测试：多列组合 + 归一化开关
// ======================================================================

// TestDeduper_MultiCol_Basic：2 列组合必须全相同才算重复
func TestDeduper_MultiCol_Basic(t *testing.T) {
	d := mkDeduperMulti([]string{"brand", "model"},
		[]string{"brand", "model", "price"}, false, false)
	if !d.Enabled() {
		t.Fatalf("双列绑定成功应为 enabled")
	}

	// (Toyota, Corolla) 首次
	if d.ShouldDrop("", []any{"Toyota", "Corolla", 100}) {
		t.Errorf("首次 (Toyota,Corolla) 不应 drop")
	}
	// 同品牌不同型号：不重复
	if d.ShouldDrop("", []any{"Toyota", "Camry", 200}) {
		t.Errorf("同品牌不同型号不应视作重复")
	}
	// 不同品牌同型号：不重复
	if d.ShouldDrop("", []any{"Honda", "Corolla", 300}) {
		t.Errorf("不同品牌同型号不应视作重复")
	}
	// 两列都相同：重复
	if !d.ShouldDrop("", []any{"Toyota", "Corolla", 999}) {
		t.Errorf("(Toyota,Corolla) 第二次应 drop")
	}
}

// TestDeduper_MultiCol_ThreeColumns：3 列组合边界
func TestDeduper_MultiCol_ThreeColumns(t *testing.T) {
	d := mkDeduperMulti([]string{"a", "b", "c"},
		[]string{"a", "b", "c"}, false, false)

	if d.ShouldDrop("", []any{"1", "2", "3"}) {
		t.Errorf("首次 (1,2,3) 不应 drop")
	}
	if !d.ShouldDrop("", []any{"1", "2", "3"}) {
		t.Errorf("同三列应 drop")
	}
	// 只要有一列不同就不是重复
	if d.ShouldDrop("", []any{"1", "2", "X"}) {
		t.Errorf("第三列不同不应 drop")
	}
}

// TestDeduper_MultiCol_AnyEmptyValueSkips：任一列空值则整行不去重
func TestDeduper_MultiCol_AnyEmptyValueSkips(t *testing.T) {
	d := mkDeduperMulti([]string{"a", "b"},
		[]string{"a", "b"}, false, false)

	// b 列为空：两行都保留
	if d.ShouldDrop("", []any{"X", ""}) {
		t.Errorf("b 为空第一行不应 drop")
	}
	if d.ShouldDrop("", []any{"X", ""}) {
		t.Errorf("b 为空第二行也不应 drop（空值不参与去重）")
	}
	// 再来一行 a 为空：同样保留
	if d.ShouldDrop("", []any{"", "Y"}) {
		t.Errorf("a 为空不应 drop")
	}
	// 两列都非空并已见过：drop
	if d.ShouldDrop("", []any{"X", "Y"}) {
		t.Errorf("(X,Y) 首次不应 drop")
	}
	if !d.ShouldDrop("", []any{"X", "Y"}) {
		t.Errorf("(X,Y) 第二次应 drop")
	}
}

// TestDeduper_MultiCol_PartialNotFound：任一列 Bind 失败 → 整体 no-op
func TestDeduper_MultiCol_PartialNotFound(t *testing.T) {
	d := newDeduper(dedupConfig{Columns: []string{"a", "not_exist"}})
	indices := d.Bind([]string{"a", "b"})
	if len(indices) != 2 || indices[0] != 0 || indices[1] != -1 {
		t.Errorf("indices 应为 [0,-1]，实际 %v", indices)
	}
	if d.Enabled() {
		t.Errorf("任一列 Bind 失败应 disabled")
	}
	// 所有行保留
	if d.ShouldDrop("", []any{"X", "Y"}) {
		t.Errorf("disabled 应恒不 drop")
	}
	if d.ShouldDrop("", []any{"X", "Y"}) {
		t.Errorf("disabled 应恒不 drop")
	}
}

// TestDeduper_IgnoreSpace：首尾空白归一化
func TestDeduper_IgnoreSpace(t *testing.T) {
	d := mkDeduperMulti([]string{"x"}, []string{"x"}, true, false)

	if d.ShouldDrop("", []any{"Apple"}) {
		t.Errorf("首次 Apple 不应 drop")
	}
	// 前后带空白的 "Apple" 应视作同一个
	if !d.ShouldDrop("", []any{"  Apple  "}) {
		t.Errorf("'  Apple  ' 应 drop（忽略前后空白）")
	}
	if !d.ShouldDrop("", []any{"\tApple\n"}) {
		t.Errorf("'\\tApple\\n' 应 drop（忽略前后空白）")
	}
	// 中间空白不被忽略
	if d.ShouldDrop("", []any{"App le"}) {
		t.Errorf("中间有空白的 'App le' 不应 drop")
	}
}

// TestDeduper_IgnoreCase：大小写归一化
func TestDeduper_IgnoreCase(t *testing.T) {
	d := mkDeduperMulti([]string{"x"}, []string{"x"}, false, true)

	if d.ShouldDrop("", []any{"Apple"}) {
		t.Errorf("首次 Apple 不应 drop")
	}
	if !d.ShouldDrop("", []any{"APPLE"}) {
		t.Errorf("'APPLE' 应 drop（忽略大小写）")
	}
	if !d.ShouldDrop("", []any{"apple"}) {
		t.Errorf("'apple' 应 drop")
	}
	// 中文 ignoreCase 不应影响（ToLower 对 CJK unicode 无变化）
	if d.ShouldDrop("", []any{"卡罗拉"}) {
		t.Errorf("首次中文不应 drop")
	}
	if !d.ShouldDrop("", []any{"卡罗拉"}) {
		t.Errorf("完全相同的中文应 drop")
	}
}

// TestDeduper_IgnoreSpaceAndCase：两个开关同时启用
func TestDeduper_IgnoreSpaceAndCase(t *testing.T) {
	d := mkDeduperMulti([]string{"x"}, []string{"x"}, true, true)

	if d.ShouldDrop("", []any{"Apple"}) {
		t.Errorf("首次 Apple 不应 drop")
	}
	if !d.ShouldDrop("", []any{"  APPLE  "}) {
		t.Errorf("'  APPLE  ' 应 drop（两个都忽略）")
	}
	if !d.ShouldDrop("", []any{"\tapple\n"}) {
		t.Errorf("'\\tapple\\n' 应 drop")
	}
}

// TestDeduper_MultiCol_NormalizePerColumn：归一化对每列独立生效
func TestDeduper_MultiCol_NormalizePerColumn(t *testing.T) {
	d := mkDeduperMulti([]string{"brand", "model"},
		[]string{"brand", "model"}, true, true)

	if d.ShouldDrop("", []any{"Toyota", "Corolla"}) {
		t.Errorf("首次不应 drop")
	}
	// 两列都带空白 + 大小写
	if !d.ShouldDrop("", []any{"  TOYOTA  ", "\tcorolla"}) {
		t.Errorf("两列都做归一化后应 drop")
	}
	// 只归一化后 brand 相同但 model 不同：不 drop
	if d.ShouldDrop("", []any{"toyota", "Camry"}) {
		t.Errorf("model 不同不应 drop")
	}
}

// ======================================================================
// V1.2 buildDedupConfig helper 测试
// ======================================================================

func TestBuildDedupConfig_EmptyBoth(t *testing.T) {
	cfg := buildDedupConfig("", nil, false, false)
	if len(cfg.Columns) != 0 {
		t.Errorf("两者都空应 Columns 空，实际 %v", cfg.Columns)
	}
}

func TestBuildDedupConfig_SingleOnly(t *testing.T) {
	cfg := buildDedupConfig("product", nil, false, false)
	if len(cfg.Columns) != 1 || cfg.Columns[0] != "product" {
		t.Errorf("单列应正常输出，实际 %v", cfg.Columns)
	}
}

func TestBuildDedupConfig_MultiOnly(t *testing.T) {
	cfg := buildDedupConfig("", []string{"brand", "model"}, false, false)
	if len(cfg.Columns) != 2 || cfg.Columns[0] != "brand" || cfg.Columns[1] != "model" {
		t.Errorf("多列应保持顺序，实际 %v", cfg.Columns)
	}
}

func TestBuildDedupConfig_Both_SingleFirst(t *testing.T) {
	cfg := buildDedupConfig("a", []string{"b", "c"}, false, false)
	if len(cfg.Columns) != 3 || cfg.Columns[0] != "a" || cfg.Columns[1] != "b" || cfg.Columns[2] != "c" {
		t.Errorf("single 应排首位，实际 %v", cfg.Columns)
	}
}

func TestBuildDedupConfig_DeduplicatesAndTrims(t *testing.T) {
	cfg := buildDedupConfig("a", []string{"a", "", "  ", "b", "b"}, false, false)
	if len(cfg.Columns) != 2 || cfg.Columns[0] != "a" || cfg.Columns[1] != "b" {
		t.Errorf("应去重去空白，实际 %v", cfg.Columns)
	}
}

func TestBuildDedupConfig_CapsAt3(t *testing.T) {
	cfg := buildDedupConfig("", []string{"a", "b", "c", "d", "e"}, false, false)
	if len(cfg.Columns) != 3 {
		t.Errorf("应截断到 3 列，实际 %d 列", len(cfg.Columns))
	}
	if cfg.Columns[0] != "a" || cfg.Columns[2] != "c" {
		t.Errorf("截断应保留前 3 个，实际 %v", cfg.Columns)
	}
}

func TestBuildDedupConfig_PreservesFlags(t *testing.T) {
	cfg := buildDedupConfig("a", nil, true, true)
	if !cfg.IgnoreSpace || !cfg.IgnoreCase {
		t.Errorf("flags 应保留，实际 ignoreSpace=%v ignoreCase=%v", cfg.IgnoreSpace, cfg.IgnoreCase)
	}
}
