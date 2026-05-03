package excelio

import (
	"strings"
	"testing"
)

// 关键回归：Override / Relationship 标签的属性值含 "/"，
// 早期实现用 [^/>]* 导致正则匹配失败，dropSet 命中却没真正删除。
// 这里用真实 xlsx 风格的 XML 验证修复后能正确删除 sheet2 相关条目。

func TestRewriteContentTypes_DropsSheetPathWithSlash(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
		`<Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
		`</Types>`

	out, err := rewriteContentTypes([]byte(src), []string{"xl/worksheets/sheet2.xml"})
	if err != nil {
		t.Fatalf("rewriteContentTypes err=%v", err)
	}
	got := string(out)
	if strings.Contains(got, "/xl/worksheets/sheet2.xml") {
		t.Fatalf("sheet2 override should be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "/xl/worksheets/sheet1.xml") {
		t.Fatalf("sheet1 override must remain, got:\n%s", got)
	}
	if !strings.Contains(got, "/xl/styles.xml") {
		t.Fatalf("styles override must remain, got:\n%s", got)
	}
}

func TestRewriteContentTypes_PairedOverrideTag(t *testing.T) {
	// 兼容成对标签写法 <Override ...></Override>
	src := `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="x"></Override>` +
		`<Override PartName="/xl/worksheets/sheet2.xml" ContentType="x"></Override>` +
		`</Types>`
	out, err := rewriteContentTypes([]byte(src), []string{"xl/worksheets/sheet2.xml"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	got := string(out)
	if strings.Contains(got, "sheet2.xml") {
		t.Fatalf("sheet2 paired Override should be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "sheet1.xml") {
		t.Fatalf("sheet1 paired Override must remain, got:\n%s", got)
	}
}

func TestRewriteWorkbookRels_DropsRIDWithSlashTarget(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>` +
		`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>` +
		`</Relationships>`

	out, err := rewriteWorkbookRels([]byte(src), "rId1", []string{"rId2"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	got := string(out)
	if strings.Contains(got, `Id="rId2"`) {
		t.Fatalf("rId2 should be removed, got:\n%s", got)
	}
	for _, must := range []string{`Id="rId1"`, `Id="rId3"`, `Id="rId4"`, "styles.xml", "theme/theme1.xml"} {
		if !strings.Contains(got, must) {
			t.Fatalf("missing required relation %q in:\n%s", must, got)
		}
	}
}

func TestRewriteWorkbookRels_PairedRelationshipTag(t *testing.T) {
	src := `<Relationships xmlns="x">` +
		`<Relationship Id="rId1" Target="worksheets/sheet1.xml"></Relationship>` +
		`<Relationship Id="rId2" Target="worksheets/sheet2.xml"></Relationship>` +
		`</Relationships>`
	out, err := rewriteWorkbookRels([]byte(src), "rId1", []string{"rId2"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	got := string(out)
	if strings.Contains(got, `Id="rId2"`) {
		t.Fatalf("rId2 paired tag should be removed, got:\n%s", got)
	}
	if !strings.Contains(got, `Id="rId1"`) {
		t.Fatalf("rId1 paired tag must remain, got:\n%s", got)
	}
}
