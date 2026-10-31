package main

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestConvertBOMWritesCodeNameAndRowIssues(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.xlsx")
	outPath := filepath.Join(dir, "out.xlsx")

	src := excelize.NewFile()
	defer src.Close()
	sh := src.GetSheetName(0)
	_ = src.SetSheetRow(sh, "A1", &[]interface{}{"主件品号", "主件品名", "", "项次", "子件品号", "", "", "单位", "", "组成用量", "底数", "损耗率"})
	_ = src.SetSheetRow(sh, "A2", &[]interface{}{"P001", "测试主件", "", "", "", "", "", "PCS", "", "2", "1", "0"})
	if err := src.SaveAs(srcPath); err != nil {
		t.Fatal(err)
	}

	rep, err := ConvertBOM(srcPath, filepath.Join("templates", "bom.xlsx"), "", outPath,
		BomOptions{Cfg: defaultBomConfig()}, bomMsgs("zh"), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Issues) != 2 || rep.Issues[0].Row != 2 || rep.Issues[1].Row != 2 {
		t.Fatalf("unexpected row issues: %#v", rep.Issues)
	}

	out, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	rows, err := out.GetRows("BOM主表")
	if err != nil || len(rows) < 2 || len(rows[1]) < 2 {
		t.Fatalf("read BOM master: rows=%#v err=%v", rows, err)
	}
	if rows[1][0] != "BOM-P001" || rows[1][1] != "测试主件" {
		t.Fatalf("BOM code/name = %q/%q", rows[1][0], rows[1][1])
	}
}
