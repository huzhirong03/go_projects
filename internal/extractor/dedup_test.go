package extractor

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestDeduper_DisabledOnEmptyColumn(t *testing.T) {
	d := newDeduper("")
	d.Bind([]string{"name", "age"})
	if d.Enabled() {
		t.Fatalf("空列名 deduper 应为 disabled")
	}
	if d.ShouldDrop("", []any{"anything"}) {
		t.Errorf("disabled deduper ShouldDrop 应恒返回 false")
	}
}

func TestDeduper_DisabledWhenColumnNotFound(t *testing.T) {
	d := newDeduper("not_exist")
	keyIdx := d.Bind([]string{"name", "age"})
	if keyIdx != -1 {
		t.Errorf("列不存在应返回 -1 got %d", keyIdx)
	}
	if d.Enabled() {
		t.Errorf("找不到列的 deduper 应为 disabled")
	}
	if d.ShouldDrop("", []any{"anything"}) {
		t.Errorf("列不存在时 ShouldDrop 应恒返回 false")
	}
}

func TestDeduper_BasicDedup(t *testing.T) {
	d := newDeduper("product")
	d.Bind([]string{"id", "product", "price"})
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
	d := newDeduper("product")
	d.Bind([]string{"id", "product"})

	cases := []any{nil, "", "   ", "\t\n"}
	for i, v := range cases {
		if d.ShouldDrop("", []any{i, v}) {
			t.Errorf("case %d 空值 %v 不应被 drop", i, v)
		}
	}
}

// 桶隔离：per_keyword 场景下同一 product 在不同 kw 桶各自保留首次
func TestDeduper_BucketIsolation(t *testing.T) {
	d := newDeduper("product")
	d.Bind([]string{"id", "product"})

	// 桶 "丰田"
	if d.ShouldDrop("丰田", []any{1, "卡罗拉"}) {
		t.Errorf("丰田桶首次卡罗拉不应 drop")
	}
	// 桶 "本田" — 同样是卡罗拉，但不同桶，应保留
	if d.ShouldDrop("本田", []any{2, "卡罗拉"}) {
		t.Errorf("本田桶里的卡罗拉不应被丰田桶的历史影响")
	}
	// 桶 "丰田" 再来一次卡罗拉 -> drop
	if !d.ShouldDrop("丰田", []any{3, "卡罗拉"}) {
		t.Errorf("丰田桶第二次卡罗拉应 drop")
	}
}

// excelize.Cell 包装的 Value 参与去重（公式 cell 应该按其值去重，不是按公式文本）
func TestDeduper_ExcelizeCellValue(t *testing.T) {
	d := newDeduper("product")
	d.Bind([]string{"id", "product"})

	cell1 := excelize.Cell{Value: "Toyota", Formula: "=A1&B1"}
	cell2 := excelize.Cell{Value: "Toyota", Formula: "=X1"}
	if d.ShouldDrop("", []any{1, cell1}) {
		t.Errorf("首次 cell 不应 drop")
	}
	if !d.ShouldDrop("", []any{2, cell2}) {
		t.Errorf("不同公式但相同 Value 应去重")
	}
}

// strict 比较：带尾空格不等价；大小写不等价（字符串层面严格）
func TestDeduper_StrictComparison(t *testing.T) {
	d := newDeduper("x")
	d.Bind([]string{"x"})

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
// 会被视为重复。这是有意的 normalize 行为——Excel 解析同一列混合类型很常见
// （比如 "产品编号" 列有时读成数字有时读成字符串），用户预期这些是同一个 ID。
// 如果未来用户反馈需要类型敏感（比如某种"编号是字符串、数值是订单量"的场景），
// 再加开关；目前默认等价。
func TestDeduper_CrossTypeNumericEquivalence(t *testing.T) {
	d := newDeduper("id")
	d.Bind([]string{"id"})

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

// values 长度小于 keyIdx 时不 drop（跨文件 schema 不一致的防御）
func TestDeduper_ShortRow(t *testing.T) {
	d := newDeduper("product")
	d.Bind([]string{"id", "product", "price"}) // keyIdx=1

	// 一行只有 1 个值，keyIdx=1 越界
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
