package main

// gen07_csv_smoke_test.go：fixture 07 生成后的自检。
// 用项目真实的 excelio.OpenCSV（走自动编码嗅探 + 分隔符推断）读一遍 5 个变体，
// 断言表头能正常识别、总行数 = 10001（表头 + 10000 数据）、关键词计数符合预期。
//
// 这是一个**手动运行**的冒烟测试（不进 CI），因为依赖 testdata_smoke/ 下的真实
// fixture 文件。本地验证命令：
//
//	go run ./cmd/gen-smoke-fixture -only 07
//	go test ./cmd/gen-smoke-fixture/ -run TestGen07_CSVFixtures -v
//
// fixture 不存在时自动 t.Skip，不会让 CI 误 fail。

import (
	"path/filepath"
	"testing"

	"excel-master/internal/excelio"
)

func TestGen07_CSVFixtures(t *testing.T) {
	cases := []struct {
		file             string
		wantDelim        rune // 期望自动嗅探出的分隔符（验证嗅探正确性）
		wantVIP, wantRet int
		totalRowsIncHead int
	}{
		// 编码 + 分隔符全靠自动嗅探（CSVOptions 全空），覆盖 4 大场景：
		{"07_CSV_UTF8_逗号_1万行.csv", ',', 2700, 1446, 10001},
		{"07_CSV_UTF8BOM_逗号_1万行.csv", ',', 2700, 1446, 10001},
		{"07_CSV_GBK_逗号_1万行.csv", ',', 2700, 1446, 10001},
		{"07_CSV_UTF8_分号_1万行.csv", ';', 2700, 1446, 10001},
		{"07_CSV_UTF8_Tab_1万行.csv", '\t', 2700, 1446, 10001},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata_smoke", tc.file)
			// 编码 + 分隔符 全部空 → 走完整自动嗅探路径
			r, err := excelio.OpenCSV(path, excelio.CSVOptions{})
			if err != nil {
				t.Skipf("fixture 不存在或打开失败（跑 `go run ./cmd/gen-smoke-fixture -only 07` 先生成）: %v", err)
				return
			}
			defer r.Close()

			vip, ret, total := 0, 0, 0
			for r.Next() {
				total++
				if total == 1 {
					continue // 表头
				}
				rec := r.Record()
				if len(rec) < 6 {
					t.Fatalf("row %d 字段数 %d < 6（分隔符嗅探可能失败）: %v", total, len(rec), rec)
				}
				if rec[1] == "VIP 客户" {
					vip++
				}
				if rec[5] == "退货" {
					ret++
				}
			}
			if err := r.Err(); err != nil {
				t.Fatalf("读 CSV 失败: %v", err)
			}
			if total != tc.totalRowsIncHead {
				t.Errorf("总行数 %d 期望 %d (含表头)", total, tc.totalRowsIncHead)
			}
			if vip != tc.wantVIP {
				t.Errorf("VIP 客户计数 %d 期望 %d", vip, tc.wantVIP)
			}
			if ret != tc.wantRet {
				t.Errorf("退货计数 %d 期望 %d", ret, tc.wantRet)
			}
			if r.Delimiter() != tc.wantDelim {
				t.Errorf("嗅探分隔符 %q 期望 %q", r.Delimiter(), tc.wantDelim)
			}
			t.Logf("encoding=%s, delimiter=%q, total=%d, vip=%d, ret=%d",
				r.Encoding(), r.Delimiter(), total, vip, ret)
		})
	}
}
