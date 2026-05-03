package extractor

import (
	"context"
	"fmt"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/excelio"
	"excel-master/internal/matcher"
	"excel-master/internal/pipeline"
)

// processCSVFile 处理一个 CSV 源单元。CSV 没 Sheet/样式/图片/合并/公式，
// 退化为"按行流式 → 关键词匹配 → EmitRow（仅文本）"。
//
// 与 xlsx 的 processFile 保持相同签名，主循环按 SourceKind 分发。
func processCSVFile(
	ctx context.Context,
	fs *FileSchema,
	schema *UnifiedSchema,
	eng *matcher.Engine,
	unifiedSearchCols []int,
	task core.ExtractTask,
	ow OutputWriter,
	emitter core.EventEmitter,
) (int, bool, error) {
	// 1) 打开（含文件占用 retry/skip/cancel）
	tOpen := time.Now()
	r, skipped, err := openCSVWithPrompt(ctx, emitter, fs.File.Path, csvOptionsFromTask(task))
	if err != nil {
		return 0, false, err
	}
	if skipped {
		return 0, true, nil
	}
	defer r.Close()
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 打开 CSV %v (%s): %s",
		time.Since(tOpen).Round(time.Millisecond), r.Encoding(), fs.File.Path))

	// 2) 文件级搜索列
	fileSearchCols := fs.FileSearchColumns(unifiedSearchCols)

	// 3) 流式行迭代
	tScan := time.Now()
	matched := 0
	for r.Next() {
		row := r.Row()
		if row <= task.HeaderRow {
			continue
		}
		if row%progressEvery == 0 {
			if err := pipeline.CheckCancel(ctx); err != nil {
				return matched, false, err
			}
			emitter.Progress(core.Progress{
				Stage:   "scanning",
				Message: fmt.Sprintf("扫描 CSV 行 %d (%s)", row, fs.File.Path),
			})
		}

		cells := r.Record()
		kw := eng.MatchRow(cells, fileSearchCols)
		if kw == "" {
			continue
		}
		// CSV 无公式，传 nil；按 schema 列对齐
		values := fs.AlignRowWithFormulas(cells, nil, len(schema.Columns))
		if err := ow.EmitRow(MatchedRow{
			SourceFile: fs.File.Path,
			SourceRow:  row,
			MatchedKW:  kw,
			Values:     values,
			// CSV 没图、没行高
		}, fs); err != nil {
			return matched, false, err
		}
		matched++
	}
	if err := r.Err(); err != nil {
		return matched, false, err
	}
	emitter.Log(core.LogInfo, fmt.Sprintf("[TIMING] 扫描匹配 CSV %v: 命中 %d 行",
		time.Since(tScan).Round(time.Millisecond), matched))
	return matched, false, nil
}

// openCSVWithPrompt 打开 CSV，遇到文件被占用时复用现有 askFileOpenDecision 流程。
func openCSVWithPrompt(
	ctx context.Context, emitter core.EventEmitter, path string, opts excelio.CSVOptions,
) (*excelio.CSVReader, bool, error) {
	for {
		switch askOfficeLockDecision(ctx, emitter, path) {
		case fileOpenRetry:
			continue
		case fileOpenSkip:
			emitter.Log(core.LogWarn, "已跳过正在打开的 CSV: "+path)
			return nil, true, nil
		case fileOpenCancel:
			return nil, false, core.ErrCanceled
		}
		r, err := excelio.OpenCSV(path, opts)
		if err == nil {
			return r, false, nil
		}
		switch askFileOpenDecision(ctx, emitter, path, err) {
		case fileOpenRetry:
			continue
		case fileOpenSkip:
			emitter.Log(core.LogWarn, "已跳过无法读取的 CSV: "+path)
			return nil, true, nil
		case fileOpenAbort:
			return nil, false, err
		default:
			return nil, false, core.ErrCanceled
		}
	}
}

// csvOptionsFromTask 从 ExtractTask 抽出 CSV 相关参数。
// V1.5 commit 3 阶段 task 里还没 CSVEncoding / CSVDelimiter 字段，先返回空值（自动嗅探）。
// commit 6 把这两个字段补到 core.ExtractTask 后，本函数会读取它们。
func csvOptionsFromTask(_ core.ExtractTask) excelio.CSVOptions {
	return excelio.CSVOptions{}
}
