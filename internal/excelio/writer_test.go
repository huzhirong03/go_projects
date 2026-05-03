package excelio

import (
	"path/filepath"
	"testing"
)

func TestWriterStreamSaveRoundTrip(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.xlsx")

	w := NewWriter()
	sw, err := w.StreamFor("结果")
	if err != nil {
		t.Fatalf("StreamFor: %v", err)
	}
	if err := sw.WriteRow(1, []any{"名称", "数量"}); err != nil {
		t.Fatalf("WriteRow header: %v", err)
	}
	if err := sw.WriteRow(2, []any{"口红 A", 10}); err != nil {
		t.Fatalf("WriteRow data: %v", err)
	}
	if err := sw.WriteRow(3, []any{"眼影 B", 20}); err != nil {
		t.Fatalf("WriteRow data: %v", err)
	}
	if err := w.RemoveDefaultSheet(); err != nil {
		t.Fatalf("RemoveDefaultSheet: %v", err)
	}
	if err := w.Save(out); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 回读校验
	r, err := Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r.Close()

	sheets := r.SheetNames()
	if len(sheets) != 1 || sheets[0] != "结果" {
		t.Fatalf("sheets = %v, want [结果]", sheets)
	}

	it, _ := r.Iterate("结果")
	defer it.Close()

	var all [][]string
	for it.Next() {
		cols, _ := it.Columns()
		all = append(all, cols)
	}
	if len(all) != 3 {
		t.Fatalf("rows = %d, want 3", len(all))
	}
	if all[0][0] != "名称" || all[1][0] != "口红 A" || all[2][0] != "眼影 B" {
		t.Errorf("内容不匹配: %v", all)
	}
}
