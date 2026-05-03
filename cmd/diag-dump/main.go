// Command diag-dump 把 xlsx 内某个 zip 条目内容原样 dump 到标准输出。
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	path := flag.String("file", "", "xlsx 路径")
	entry := flag.String("entry", "", "zip 条目名")
	flag.Parse()
	if *path == "" || *entry == "" {
		fmt.Fprintln(os.Stderr, "用法: diag-dump -file x.xlsx -entry xl/drawings/drawing1.xml")
		os.Exit(2)
	}
	z, err := zip.OpenReader(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == *entry {
			rc, err := f.Open()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			defer rc.Close()
			_, _ = io.Copy(os.Stdout, rc)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "未找到 entry:", *entry)
	os.Exit(1)
}
