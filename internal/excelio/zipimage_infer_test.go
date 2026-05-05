package excelio

import "testing"

// TestInferTwoCellAnchorCxCy_SameColSameRow 同列同行（图片完全在一格内部）：
// cx = to.colOff - from.colOff, cy = to.rowOff - from.rowOff
func TestInferTwoCellAnchorCxCy_SameColSameRow(t *testing.T) {
	a := parsedAnchor{
		row: 2, col: 9, toRow: 2, toCol: 9,
		fromColOff: 19050, fromRowOff: 19050,
		toColOff: 409575, toRowOff: 742950,
	}
	cx, cy := inferTwoCellAnchorCxCy(a, nil)
	if cx != 390525 {
		t.Errorf("cx = %d, want 390525", cx)
	}
	if cy != 723900 {
		t.Errorf("cy = %d, want 723900", cy)
	}
}

// TestInferTwoCellAnchorCxCy_SameColAcrossOneRow 同列跨 1 行（用户实际数据）：
// 源 anchor from.col=to.col=9, from.row=2 to.row=3, from.rowOff=19050, to.rowOff=0
// 假设 row 2 的行高是 60 pt
// cy = 60pt * 12700 EMU - 19050 + 0 = 762000 - 19050 = 742950 EMU
func TestInferTwoCellAnchorCxCy_SameColAcrossOneRow(t *testing.T) {
	a := parsedAnchor{
		row: 2, col: 9, toRow: 3, toCol: 9,
		fromColOff: 19050, fromRowOff: 19050,
		toColOff: 409575, toRowOff: 0,
	}
	rowHeights := map[int]float64{2: 60.0}
	cx, cy := inferTwoCellAnchorCxCy(a, rowHeights)
	wantCx := int64(409575 - 19050) // 390525
	wantCy := int64(60*12700) - 19050
	if cx != wantCx {
		t.Errorf("cx = %d, want %d", cx, wantCx)
	}
	if cy != wantCy {
		t.Errorf("cy = %d, want %d", cy, wantCy)
	}
}

// TestInferTwoCellAnchorCxCy_AcrossMultipleRows 跨 3 行（row 2~4）：
// cy = (row2.ht + row3.ht + row4.ht) * 12700 - from.rowOff + to.rowOff
func TestInferTwoCellAnchorCxCy_AcrossMultipleRows(t *testing.T) {
	a := parsedAnchor{
		row: 2, col: 1, toRow: 5, toCol: 1,
		fromColOff: 0, fromRowOff: 10000,
		toColOff: 500000, toRowOff: 5000,
	}
	rowHeights := map[int]float64{2: 30, 3: 40, 4: 50}
	_, cy := inferTwoCellAnchorCxCy(a, rowHeights)
	wantCy := int64((30+40+50)*12700) - 10000 + 5000
	if cy != wantCy {
		t.Errorf("cy = %d, want %d", cy, wantCy)
	}
}

// TestInferTwoCellAnchorCxCy_NoRowHeights 未提供 rowHeights → 用默认 15pt
func TestInferTwoCellAnchorCxCy_NoRowHeights(t *testing.T) {
	a := parsedAnchor{
		row: 2, col: 1, toRow: 3, toCol: 1,
		fromRowOff: 0, toRowOff: 0,
	}
	_, cy := inferTwoCellAnchorCxCy(a, nil)
	wantCy := int64(15 * 12700)
	if cy != wantCy {
		t.Errorf("cy = %d, want %d", cy, wantCy)
	}
}

// TestInferTwoCellAnchorCxCy_AcrossCols 跨列 → cx = 0（不精确计算，避免列宽 EMU 换算）
func TestInferTwoCellAnchorCxCy_AcrossCols(t *testing.T) {
	a := parsedAnchor{
		row: 2, col: 1, toRow: 2, toCol: 3,
		fromColOff: 0, toColOff: 10000,
	}
	cx, _ := inferTwoCellAnchorCxCy(a, nil)
	if cx != 0 {
		t.Errorf("跨列 cx 应为 0（不精确计算），实际 %d", cx)
	}
}

// TestInferTwoCellAnchorCxCy_NotTwoCell toRow = 0 表示不是 twoCell → 返回 0,0
func TestInferTwoCellAnchorCxCy_NotTwoCell(t *testing.T) {
	a := parsedAnchor{row: 2, col: 1, toRow: 0}
	cx, cy := inferTwoCellAnchorCxCy(a, nil)
	if cx != 0 || cy != 0 {
		t.Errorf("非 twoCell 应返回 0,0，实际 cx=%d cy=%d", cx, cy)
	}
}
