// Command diag-zipdiff 对比两个 xlsx（zip）的内部条目：列出各自文件清单、按条目大小对比。
// 对指定 xml 文件可以打印其前 N 行用于快速定位差异。
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func main() {
	a := flag.String("a", "", "文件 A (通常是源文件)")
	b := flag.String("b", "", "文件 B (通常是输出文件)")
	print := flag.String("print", "", "打印指定 zip 条目内容（逗号分隔）")
	side := flag.String("side", "b", "print 来源: a | b")
	head := flag.Int("head", 120, "打印时显示前多少行")
	flag.Parse()
	if *a == "" || *b == "" {
		fmt.Fprintln(os.Stderr, "用法: diag-zipdiff -a src.xlsx -b out.xlsx [-print xl/drawings/drawing1.xml -side a -head 200]")
		os.Exit(2)
	}

	za, err := zip.OpenReader(*a)
	check(err)
	defer za.Close()
	zb, err := zip.OpenReader(*b)
	check(err)
	defer zb.Close()

	ma := indexZip(za)
	mb := indexZip(zb)

	// 列出所有条目名 union
	all := map[string]struct{}{}
	for k := range ma {
		all[k] = struct{}{}
	}
	for k := range mb {
		all[k] = struct{}{}
	}
	names := make([]string, 0, len(all))
	for k := range all {
		names = append(names, k)
	}
	sort.Strings(names)

	fmt.Printf("%-60s %10s %10s %s\n", "条目", "A(size)", "B(size)", "状态")
	fmt.Println(strings.Repeat("-", 100))
	for _, n := range names {
		sa, okA := ma[n]
		sb, okB := mb[n]
		status := ""
		switch {
		case okA && !okB:
			status = "仅A (B丢失)"
		case !okA && okB:
			status = "仅B (B多出)"
		case sa != sb:
			status = "大小不同"
		default:
			status = "一致"
		}
		aSize := "-"
		bSize := "-"
		if okA {
			aSize = fmt.Sprintf("%d", sa)
		}
		if okB {
			bSize = fmt.Sprintf("%d", sb)
		}
		fmt.Printf("%-60s %10s %10s %s\n", n, aSize, bSize, status)
	}

	// 可选打印指定条目
	if strings.TrimSpace(*print) != "" {
		var src *zip.ReadCloser
		if *side == "a" {
			src = za
		} else {
			src = zb
		}
		for _, name := range strings.Split(*print, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			fmt.Printf("\n====== [%s] %s (前 %d 行) ======\n", *side, name, *head)
			printEntry(src, name, *head)
		}
	}
}

func indexZip(z *zip.ReadCloser) map[string]int64 {
	out := make(map[string]int64, len(z.File))
	for _, f := range z.File {
		out[f.Name] = int64(f.UncompressedSize64)
	}
	return out
}

func printEntry(z *zip.ReadCloser, name string, head int) {
	for _, f := range z.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				fmt.Println("打开失败:", err)
				return
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				fmt.Println("读取失败:", err)
				return
			}
			lines := strings.SplitN(string(data), "\n", head+1)
			for i, l := range lines {
				if i >= head {
					fmt.Printf("... (剩余 %d 行省略)\n", len(lines)-head)
					break
				}
				fmt.Println(l)
			}
			return
		}
	}
	fmt.Println("未找到:", name)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
