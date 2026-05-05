// Command gen-smoke-fixture 生成一组冒烟测试 fixture，覆盖：
//   - 大数据量 (10万行 / 流式读性能)
//   - 共享公式 + calcChain (zip surgery bug 回归测)
//   - 大量图片 (图片迁移性能)
//   - 多 sheet + 跨 sheet 公式 (复刻用户实战 case)
//   - 按列值拆分 (拆分模式)
//   - 多源合并 (merged 模式)
//
// 用法：
//
//	go run ./cmd/gen-smoke-fixture
//	go run ./cmd/gen-smoke-fixture -out testdata_smoke -only 01,02
//
// 所有文件用 StreamWriter 生成，本进程内存占用小。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	out := flag.String("out", "testdata_smoke", "输出目录（默认 testdata_smoke/）")
	only := flag.String("only", "", "只生成指定 fixture，逗号分隔（如 01,02,04）；空表示全部")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		exit(err)
	}

	wanted := parseOnly(*only)
	t0 := time.Now()

	type job struct {
		id   string
		desc string
		fn   func(dir string) error
	}
	jobs := []job{
		{"01", "学生信息_10万行（流式读性能）", gen01_StudentBigTable},
		{"02", "订单含公式_3万行（共享公式 + calcChain 回归）", gen02_OrdersWithFormulas},
		{"03", "员工花名册_2万行带照片（图片迁移性能）", gen03_EmployeesWithPhotos},
		{"04", "学校成绩册_多Sheet交叉公式（复刻用户实战 case）", gen04_SchoolMultiSheet},
		{"05", "按列值拆分_30个班级（拆分模式）", gen05_SplitByColumn30Classes},
		{"06", "批量提取_5个供应商文件（merged 多源合并）", gen06_MultiSourceMerged},
		{"07", "CSV 编码与分隔符样例_5 个变体 × 1 万行", gen07_CSVSamples},
	}

	for _, j := range jobs {
		if !wanted[j.id] && len(wanted) > 0 {
			continue
		}
		jobStart := time.Now()
		fmt.Printf("[%s] 开始：%s\n", j.id, j.desc)
		if err := j.fn(*out); err != nil {
			exit(fmt.Errorf("%s: %w", j.id, err))
		}
		fmt.Printf("[%s] 完成 (耗时 %v)\n\n", j.id, time.Since(jobStart).Round(time.Millisecond))
	}

	abs, _ := filepath.Abs(*out)
	fmt.Printf("==== 全部完成 ====\n输出目录：%s\n总耗时：%v\n", abs, time.Since(t0).Round(time.Millisecond))
	fmt.Println("接下来可参考 docs/SMOKE_TEST_CHECKLIST.md 逐项验证。")
}

func parseOnly(s string) map[string]bool {
	if s == "" {
		return nil
	}
	out := map[string]bool{}
	for _, x := range strings.Split(s, ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			out[x] = true
		}
	}
	return out
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, "[错误]", err)
	os.Exit(1)
}
