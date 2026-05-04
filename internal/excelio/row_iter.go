package excelio

// row_iter.go：定义 excelio 对外的流式行迭代器统一接口。
//
// 原 RowIterator（excelize.Rows 的包装）是具体 struct；A 引入 xlsxreader 作为第二个
// 后端后需要多态能力：extractor 的扫描循环在"快引擎"失败时自动回退到 excelize。
//
// 设计原则：
//   - RowIter 是只读接口，只定义扫描阶段需要的最小方法集；
//   - 具体 struct 保留各自实现细节，但都满足 RowIter；
//   - 调用方（extractor.processFile）持有 RowIter 而不是具体类型，运行时决定用哪一种后端。

// RowIter 是行迭代器通用接口。符合流式读协议：
//
//	for it.Next() {
//	    cells, _ := it.Columns()
//	    // ...
//	}
//	if err := it.Err(); err != nil { ... }
//	_ = it.Close()
//
// Columns() 返回值必须是"从 A 列开始的密集切片"（空 cell 位置填 ""），
// 以兼容 excelize.Rows.Columns() 的既有语义。这样 extractor 的 MatchRow / Apply
// 不用关心底层读引擎差异。
type RowIter interface {
	Next() bool
	Columns() ([]string, error)
	RowNum() int
	Err() error
	Close() error
}

// 编译期断言：两个具体实现都满足 RowIter。
// 如果以后 RowIter 改签名，这里会立刻编译失败，防止其中一个实现漏同步。
var (
	_ RowIter = (*RowIterator)(nil)
	_ RowIter = (*FastRowIterator)(nil)
)
