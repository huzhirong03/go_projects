// Command split-cli 是 internal/splitter 的命令行薄包装，用于原汁原味路径的集成验证。
//
// 示例：
//
//	split-cli -mode by_sheet -src testdata_rich/x.xlsx -out test_out_bysheet
//	split-cli -mode by_rows -src testdata_rich/x.xlsx -out test_out_byrows -rows 5
//	split-cli -mode by_column -src testdata_rich/x.xlsx -out test_out_bycol -col 类别
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/pipeline"
	"excel-master/internal/splitter"
)

func main() {
	var (
		mode      = flag.String("mode", "", "拆分模式: by_sheet | by_rows | by_column")
		src       = flag.String("src", "", "源文件 xlsx 路径（必填）")
		outDir    = flag.String("out", "", "输出目录，默认在源文件同目录新建 split_<时间戳>")
		rows      = flag.Int("rows", 5, "by_rows 模式的每片行数")
		col       = flag.String("col", "", "by_column 模式的列名")
		headerRow = flag.Int("header", 1, "表头行号 (1-based, 0=无表头)")
		noImage   = flag.Bool("no-image", false, "不保留图片（仅影响统计，原汁原味仍会带图）")
	)
	flag.Parse()

	if *src == "" || *mode == "" {
		flag.Usage()
		os.Exit(2)
	}

	out := *outDir
	if out == "" {
		out = filepath.Join(filepath.Dir(*src), "split_"+time.Now().Format("20060102_150405"))
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		exitWith("创建输出目录失败: " + err.Error())
	}

	task := core.SplitTask{
		SourcePath:     *src,
		OutputDir:      out,
		HeaderRow:      *headerRow,
		RowsPerFile:    *rows,
		SplitColumn:    *col,
		PreserveImages: !*noImage,
	}

	ctx := context.Background()
	start := time.Now()

	var (
		result *splitter.Result
		err    error
	)
	switch *mode {
	case "by_sheet":
		task.Mode = core.SplitBySheet
		result, err = splitter.SplitBySheet(ctx, task, pipeline.LogEmitter{})
	case "by_rows":
		task.Mode = core.SplitByRows
		result, err = splitter.SplitByRows(ctx, task, pipeline.LogEmitter{})
	case "by_column":
		task.Mode = core.SplitByColumn
		result, err = splitter.SplitByColumn(ctx, task, pipeline.LogEmitter{})
	default:
		exitWith("未知 mode: " + *mode)
	}
	if err != nil {
		exitWith("拆分失败: " + err.Error())
	}

	fmt.Printf("[完成] 耗时 %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("  扫描行 : %d\n", result.RowsScanned)
	fmt.Printf("  分片数 : %d\n", result.PartsCreated)
	fmt.Printf("  图片数 : %d\n", result.ImagesMigrated)
	fmt.Printf("  输出文件:\n")
	for _, p := range result.OutputFiles {
		fmt.Printf("    - %s\n", p)
	}
}

func exitWith(msg string) {
	fmt.Fprintln(os.Stderr, "[错误] "+msg)
	os.Exit(1)
}
