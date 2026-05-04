// Command extract-cli 是 internal/extractor 的命令行薄包装，
// 用于第 2 周后端验收：让用户用真实文件夹 + 真实带图 Excel 跑一趟完整流程。
//
// 用法示例：
//
//	extract-cli.exe -src "D:\产品目录" -kw "口红,fd" -out "D:\提取结果"
//	extract-cli.exe -src "D:\产品目录" -kw "口红" -strategy merged -no-image
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"excel-master/internal/core"
	"excel-master/internal/extractor"
	"excel-master/internal/matcher"
	"excel-master/internal/pipeline"
)

func main() {
	var (
		src        = flag.String("src", "", "源文件夹路径（必填）")
		kw         = flag.String("kw", "", "关键词，逗号/空格/顿号分隔（必填）")
		strategy   = flag.String("strategy", "per_keyword", "输出策略：per_keyword | merged | per_source")
		outDir     = flag.String("out", "", "输出目录（默认 <src>/提取结果_<时间戳>）")
		headerRow  = flag.Int("header", 1, "表头行号（1-based，0 表示无表头）")
		noImage    = flag.Bool("no-image", false, "不保留图片（加快速度）")
		searchCols = flag.String("cols", "", "限定搜索列名（逗号分隔，默认全列搜索）")
		modeStr    = flag.String("mode", "all", "匹配模式：exact | contains | all（默认两种全开）")
		dedupCol   = flag.String("dedup", "", "去重列名（空 = 不去重，按该列 strict 比较去重，保留首次出现）")
	)
	flag.Parse()

	if *src == "" || *kw == "" {
		flag.Usage()
		os.Exit(2)
	}

	keywords := matcher.ParseKeywords(*kw)
	if len(keywords) == 0 {
		exitWith("关键词解析为空，请检查 -kw 参数")
	}

	mode, err := parseMode(*modeStr)
	if err != nil {
		exitWith(err.Error())
	}

	output, err := parseStrategy(*strategy)
	if err != nil {
		exitWith(err.Error())
	}

	finalOut := *outDir
	if finalOut == "" {
		finalOut = filepath.Join(*src, "提取结果_"+time.Now().Format("20060102_150405"))
	}
	if err := os.MkdirAll(finalOut, 0o755); err != nil {
		exitWith("创建输出目录失败: " + err.Error())
	}

	var cols []string
	if strings.TrimSpace(*searchCols) != "" {
		for _, c := range strings.Split(*searchCols, ",") {
			if c = strings.TrimSpace(c); c != "" {
				cols = append(cols, c)
			}
		}
	}

	task := core.ExtractTask{
		FolderPath:     *src,
		Keywords:       keywords,
		MatchMode:      mode,
		SearchAllCols:  len(cols) == 0,
		SearchColumns:  cols,
		Output:         output,
		OutputDir:      finalOut,
		HeaderRow:      *headerRow,
		PreserveImages: !*noImage,
		DedupColumn:    strings.TrimSpace(*dedupCol),
	}

	// 支持 Ctrl+C 取消
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Println("\n[取消] 收到中断信号，正在停止...")
		cancel()
	}()

	fmt.Println("=== Excel 大文件工具 · 批量提取（命令行版） ===")
	fmt.Printf("  源文件夹   : %s\n", task.FolderPath)
	fmt.Printf("  关键词     : %v\n", task.Keywords)
	fmt.Printf("  匹配模式   : %s\n", formatMode(task.MatchMode))
	fmt.Printf("  搜索范围   : %s\n", formatSearch(task))
	fmt.Printf("  输出策略   : %s\n", task.Output)
	fmt.Printf("  输出目录   : %s\n", task.OutputDir)
	fmt.Printf("  保留图片   : %v\n", task.PreserveImages)
	if task.DedupColumn != "" {
		fmt.Printf("  去重列     : %s\n", task.DedupColumn)
	}
	fmt.Println("---")

	start := time.Now()
	result, err := extractor.Extract(ctx, task, pipeline.LogEmitter{})
	elapsed := time.Since(start)
	if err != nil {
		exitWith("提取失败: " + err.Error())
	}

	fmt.Println("---")
	fmt.Printf("[完成] 耗时 %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  扫描文件   : %d\n", result.FilesScanned)
	fmt.Printf("  命中文件   : %d\n", result.FilesMatched)
	fmt.Printf("  命中行数   : %d\n", result.RowsMatched)
	fmt.Printf("  迁移图片   : %d\n", result.ImagesMigrated)
	fmt.Printf("  输出文件   : %d 个\n", len(result.OutputFiles))
	for _, p := range result.OutputFiles {
		fmt.Printf("    - %s\n", p)
	}
}

func parseMode(s string) (core.MatchMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "exact":
		return core.MatchExact, nil
	case "contains":
		return core.MatchContains, nil
	case "all", "":
		return core.MatchExact | core.MatchContains, nil
	default:
		return 0, fmt.Errorf("未知匹配模式: %s（可选：exact/contains/all）", s)
	}
}

func parseStrategy(s string) (core.OutputStrategy, error) {
	switch core.OutputStrategy(strings.ToLower(strings.TrimSpace(s))) {
	case core.OutputPerKeyword:
		return core.OutputPerKeyword, nil
	case core.OutputMerged:
		return core.OutputMerged, nil
	case core.OutputPerSource:
		return core.OutputPerSource, nil
	default:
		return "", fmt.Errorf("未知输出策略: %s（可选：per_keyword/merged/per_source）", s)
	}
}

func formatMode(m core.MatchMode) string {
	var parts []string
	if m.Has(core.MatchExact) {
		parts = append(parts, "精准")
	}
	if m.Has(core.MatchContains) {
		parts = append(parts, "包含")
	}
	if len(parts) == 0 {
		return "(未指定)"
	}
	return strings.Join(parts, "+")
}

func formatSearch(t core.ExtractTask) string {
	if t.SearchAllCols || len(t.SearchColumns) == 0 {
		return "全列"
	}
	return "指定列: " + strings.Join(t.SearchColumns, ", ")
}

func exitWith(msg string) {
	fmt.Fprintln(os.Stderr, "[错误] "+msg)
	os.Exit(1)
}
