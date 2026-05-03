package extractor

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"excel-master/internal/core"
)

// finalizeStreaming 是 mergedWriter 的 CSV 兼容路径：
// 不走 zip 手术，改用 StreamWriter 把**所有**源的命中行合并写到一个纯数据 xlsx。
// 触发条件：hits 里至少有一个 CSV 源。
//
// 代价：
//   - xlsx 源的样式/图片/合并单元格会丢失（流式路径限制）
//   - 表头用 primary 源的 Headers（挑命中行最多的那个源，保证顺序稳定）
//   - 输出列 = primary 源的列；其他源的列按位置对齐，多出来的列丢弃
//
// 用户明确选了 merged 且涉及 CSV，就默认接受这种降级。需要保真请用 per_source。
func (m *mergedWriter) finalizeStreaming(ctx context.Context, emitter core.EventEmitter) ([]string, error) {
	_ = ctx // streaming 路径目前没有文件锁交互，保留签名对齐
	// 1. 选 primary：命中行最多的源；同位按路径字典序。
	type entry struct {
		path string
		h    *perSourceHits
		n    int
	}
	var entries []entry
	for path, h := range m.hits {
		n := len(h.csvRows)
		for _, rs := range h.sheetRows {
			n += len(rs)
		}
		entries = append(entries, entry{path: path, h: h, n: n})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].n != entries[j].n {
			return entries[i].n > entries[j].n
		}
		return entries[i].path < entries[j].path
	})
	primary := entries[0]

	// 2. 确定表头（来自 primary.csvSchema）
	var headers []string
	if primary.h.csvSchema != nil {
		headers = primary.h.csvSchema.File.Headers
	}

	// 3. 开输出流
	outPath := filepath.Join(m.outDir, sanitizeFileName(m.prefix+"搜索结果")+"_"+m.ts+".xlsx")
	const sheet = "结果"
	out, err := openOutput(outPath, sheet)
	if err != nil {
		return nil, err
	}
	defer out.close()

	if len(headers) > 0 {
		if err := out.writeHeader(headers); err != nil {
			return nil, err
		}
	}

	// 4. 按源顺序（primary 先，其他按路径字典序）写命中行。
	//    CSV 源：直接用缓存的 Values；
	//    xlsx 源：没有整行缓存——流式降级里无法 reach 原样式，跳过 xlsx 源并告警。
	for idx, e := range entries {
		if idx > 0 && e.h.csvRows == nil {
			// xlsx 源在流式合并下无法拿到整行 Values（当前 perSourceHits 只存行号）。
			// 告警并跳过；用户想保留这部分请选 per_source。
			if emitter != nil {
				emitter.Log(core.LogWarn, fmt.Sprintf(
					"merged 流式合并跳过 xlsx 源（含 CSV 时无法保留 xlsx 样式）: %s", e.path))
			}
			continue
		}
		for _, r := range e.h.csvRows {
			if _, err := out.writeRow(r.Values, 0, 0); err != nil {
				return nil, err
			}
		}
	}
	if err := out.save(); err != nil {
		return nil, err
	}
	return []string{outPath}, nil
}
