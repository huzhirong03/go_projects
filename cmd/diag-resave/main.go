// Command diag-resave 诊断：把源 xlsx 原封不动复制到目标，然后仅仅用 excelize.OpenFile + Save，
// 看会不会丢失条件格式/图片锚点/样式。用于定位"原汁原味"失败的根因。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/xuri/excelize/v2"
)

func main() {
	src := flag.String("src", "", "源 xlsx")
	dst := flag.String("dst", "", "目标 xlsx（原样二进制复制 + 不修改地 excelize save）")
	flag.Parse()
	if *src == "" || *dst == "" {
		fmt.Fprintln(os.Stderr, "用法: diag-resave -src <src.xlsx> -dst <dst.xlsx>")
		os.Exit(2)
	}
	if err := copyFile(*src, *dst); err != nil {
		fmt.Fprintln(os.Stderr, "复制失败:", err)
		os.Exit(1)
	}
	fmt.Println("[1] 二进制复制完成")

	f, err := excelize.OpenFile(*dst)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开失败:", err)
		os.Exit(1)
	}
	fmt.Println("[2] excelize.OpenFile 成功")

	if err := f.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "保存失败:", err)
		os.Exit(1)
	}
	fmt.Println("[3] excelize.Save 成功（整个文件被重写）")
	_ = f.Close()
	fmt.Println("[4] 请用 Excel 打开", *dst, "对比源文件差异：条件格式/图片锚点/样式等")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
