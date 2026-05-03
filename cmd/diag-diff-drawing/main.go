// 逐 anchor 对比源与输出 drawing1.xml，把源里 sheet row 2 的 anchor 和
// 输出里 new row 2 对应的 anchor 做字段级 diff，找出所有不同属性。
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

func readEntry(path, entry string) string {
	z, err := zip.OpenReader(path)
	if err != nil {
		panic(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == entry {
			rc, _ := f.Open()
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return string(b)
		}
	}
	return ""
}

func firstAnchor(xml string) string {
	re := regexp.MustCompile(`(?s)<xdr:twoCellAnchor\b[^>]*>.*?</xdr:twoCellAnchor>`)
	ms := re.FindAllString(xml, 2)
	if len(ms) == 0 {
		return ""
	}
	return ms[0]
}

func main() {
	src := flag.String("src", "", "源 xlsx")
	out := flag.String("out", "", "输出 xlsx")
	flag.Parse()
	srcXml := readEntry(*src, "xl/drawings/drawing1.xml")
	outXml := readEntry(*out, "xl/drawings/drawing1.xml")
	srcA := firstAnchor(srcXml)
	outA := firstAnchor(outXml)
	fmt.Println("=== 源文件第 1 个 anchor ===")
	fmt.Println(pretty(srcA))
	fmt.Println("=== 输出第 1 个 anchor ===")
	fmt.Println(pretty(outA))
	if srcA == outA {
		fmt.Println(">>> 完全一致")
	} else {
		fmt.Printf(">>> 不同（源 %d 字节 vs 输出 %d 字节）\n", len(srcA), len(outA))
	}
	_ = os.Stdout
}

func pretty(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "><", ">\n<"), "><", ">\n<")
}
