package extractor

// scan_iterator.go：扫描迭代器工厂 —— 统一调度 xlsxreader 快路径 / excelize 慢路径。
//
// 为什么需要：A 引入 xlsxreader 作为扫描后端后，extractor.processFile 不应直接耦合
// 任何具体后端。这里定义统一入口 openScanIterator，按"快优先 + 失败回退"策略选引擎，
// 业务代码只用 excelio.RowIter 接口，不感知底层差异。
//
// 选择策略：
//   1. 先 xlsxreader.OpenFile：纯 Go SAX 解析，PoC 实测比 excelize 快 1.51×；
//   2. 任何步骤失败（不标准 xlsx / 加密 / 稀有元素）→ 回退 excelize.Reader.Iterate；
//   3. 回退路径完全等价于以前的代码，零业务回归。
//
// 注意：Fast 引擎只用于"扫描阶段"。命中行的公式查询、行高、样式、图片仍走 excelize
// （由调用方持有的 *excelio.Reader 提供）；fast 引擎拿到 cells 后即与 RowIterator 等效，
// 不接管下游写路径。

import (
	"excel-master/internal/core"
	"excel-master/internal/excelio"
)

// openScanIterator 返回扫描用的 RowIter + cleanup 闭包。
//
// excelizeReader 必须已由调用方打开（因为命中行后处理仍要用），失败回退路径会复用它。
//
// 失败处理：
//   - fast 路径打开失败 → 记 warn，回退 excelize（不报错给调用方）
//   - excelize 也失败 → 真错误向上传
//
// 调用方使用模板：
//
//	it, cleanup, err := openScanIterator(r, path, sheet, emitter)
//	if err != nil { return ... }
//	defer cleanup()
//	for it.Next() { ... }
//	if err := it.Err(); err != nil { ... }
func openScanIterator(
	excelizeReader *excelio.Reader, path, sheet string, emitter core.EventEmitter,
) (excelio.RowIter, func(), error) {
	// 第一选择：xlsxreader 快路径
	fr, err := excelio.OpenFast(path)
	if err == nil {
		it, err2 := fr.Iterate(sheet)
		if err2 == nil {
			cleanup := func() {
				_ = it.Close()
				_ = fr.Close()
			}
			return it, cleanup, nil
		}
		_ = fr.Close()
		emitter.Log(core.LogWarn, "xlsxreader Iterate 失败，回退 excelize: "+err2.Error())
	} else {
		emitter.Log(core.LogWarn, "xlsxreader 打开失败，回退 excelize: "+err.Error())
	}

	// 回退：excelize 慢路径
	it, err3 := excelizeReader.Iterate(sheet)
	if err3 != nil {
		return nil, nil, err3
	}
	cleanup := func() { _ = it.Close() }
	return it, cleanup, nil
}
