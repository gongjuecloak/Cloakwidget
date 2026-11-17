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
	if rows[1][0] != "P001" || rows[1][1] != "BOM-P001" {
		t.Fatalf("BOM code/name = %q/%q，应为 品号 / BOM-品号", rows[1][0], rows[1][1])
	}
	if len(rows[1]) < 6 || rows[1][4] != "P001" || rows[1][5] != "测试主件" {
		t.Fatalf("物料编码/名称 = %v，应为 品号 / 主件品名", rows[1][4:6])
	}
	// 明细表 A 列必须与主表 A 列一致，否则 MES 导入时主明细对不上
	dRows, err := out.GetRows("BOM明细")
	if err != nil || len(dRows) < 2 || len(dRows[1]) < 1 {
		t.Fatalf("read BOM detail: rows=%#v err=%v", dRows, err)
	}
	if dRows[1][0] != rows[1][0] {
		t.Fatalf("明细 BOM编码 %q ≠ 主表 %q", dRows[1][0], rows[1][0])
	}
}

// 主体判定：主件品名含「膠合」且该 BOM 有「前三角」子件时，那一行「是否主体」= 是。
// 其余行保持默认「否」；非膠合 BOM 即使子件带「前三角」字样也不标。
func TestConvertBOMMainFlagRule(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.xlsx")
	outPath := filepath.Join(dir, "out.xlsx")

	src := excelize.NewFile()
	defer src.Close()
	sh := src.GetSheetName(0)
	_ = src.SetSheetRow(sh, "A1", &[]interface{}{"主件品号", "主件品名", "", "项次", "子件品号", "子件品名", "", "单位", "", "组成用量", "底数", "损耗率"})
	rows := [][]interface{}{
		{"P100", "膠合車架 A", "", "0010", "C1", "成型前三角 A", "", "PCS", "", "1", "1", "0"},
		{"P100", "膠合車架 A", "", "0020", "C2", "螺絲 M5", "", "PCS", "", "2", "1", "0"},
		{"P200", "車架 B", "", "0010", "C3", "前三角透明保護貼", "", "PCS", "", "1", "1", "0"},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		_ = src.SetSheetRow(sh, cell, &r)
	}
	if err := src.SaveAs(srcPath); err != nil {
		t.Fatal(err)
	}

	if _, err := ConvertBOM(srcPath, filepath.Join("templates", "bom.xlsx"), "", outPath,
		BomOptions{Cfg: defaultBomConfig()}, bomMsgs("zh"), func(string) {}); err != nil {
		t.Fatal(err)
	}

	out, err := excelize.OpenFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	dRows, err := out.GetRows("BOM明细")
	if err != nil || len(dRows) < 4 {
		t.Fatalf("read detail: rows=%d err=%v", len(dRows), err)
	}
	// 列位：A 编码 B 项次 C 层级 D 父级 E 物料编码 F 基础用量 G 损耗 H 报废 I 发料 J 替代 K 主体
	flag := func(r []string) string {
		if len(r) > 10 {
			return r[10]
		}
		return ""
	}
	if got := flag(dRows[1]); got != "是" {
		t.Fatalf("膠合 BOM 的成型前三角行 主体 = %q，应为「是」", got)
	}
	if got := flag(dRows[2]); got != "否" {
		t.Fatalf("膠合 BOM 的普通子件行 主体 = %q，应为「否」", got)
	}
	if got := flag(dRows[3]); got != "否" {
		t.Fatalf("非膠合 BOM 的前三角行 主体 = %q，应为「否」", got)
	}
}
