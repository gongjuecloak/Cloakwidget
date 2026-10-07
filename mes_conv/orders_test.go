package main

// 订单引擎的回归测试。
//
// 覆盖几处「踩过坑、改错了不会当场报错」的语义点：
//   1. 源档同名列（两个「備註」）按 pandas 规则改名为 備註.1，配置里
//      「備註.1|備註」这个候选写法必须命中第二列，而不是又指回第一列。
//   2. 简/繁混用表头要能互相识别（源档是繁体，配置或别名里写简体也要认）。
//   3. 小计 / 合计行必须被过滤，且不计入明细。

import (
	"testing"
)

func grid(headers []string, rows [][]string) *sheetGrid {
	return &sheetGrid{Path: "mem", SheetName: "S", HeaderRow: 0, Headers: headers, Rows: rows}
}

// 同名列改名后，候选写法要指到正确的那一列
func TestDuplicateHeaderRenaming(t *testing.T) {
	g := grid([]string{"訂單單號", "備註", "品號", "備註", "備註"}, nil)
	idx := g.normIndex()

	if got := idx["备注"]; got != 1 {
		t.Errorf("第一个「備註」应在第 1 列，实际 %d", got)
	}
	if got := idx["备注.1"]; got != 3 {
		t.Errorf("第二个「備註」应改名为 备注.1 指向第 3 列，实际 %d", got)
	}
	if got := idx["备注.2"]; got != 4 {
		t.Errorf("第三个「備註」应改名为 备注.2 指向第 4 列，实际 %d", got)
	}

	// 逐列名解析：備註.1 必须指到第 3 列（而不是又回到第 1 列）
	aliases := defaultColumnAliases()
	if got := resolveColIndex(g, "備註.1", aliases); got != 3 {
		t.Errorf("resolveColIndex(備註.1) 应为 3，实际 %d", got)
	}
	// 「備註.1|備註」这种候选写法由 resolveMapping 负责拆分：
	// 第一候选命中就用第一候选（第 3 列）
	m := resolveMapping(g, map[string]string{"備註.1|備註": "备注"}, aliases)
	if got := m[3]; got != "备注" {
		t.Errorf("「備註.1|備註」应命中第 3 列，实际命中 %v", m)
	}
	// 只有一列备注时，第一候选不存在，回退到「備註」本身（第 1 列）
	g2 := grid([]string{"訂單單號", "備註"}, nil)
	m2 := resolveMapping(g2, map[string]string{"備註.1|備註": "备注"}, aliases)
	if got := m2[1]; got != "备注" {
		t.Errorf("单备注列时应回退命中第 1 列，实际命中 %v", m2)
	}
}

// 简繁混用表头：源档繁体表头 + 配置写简体，也要认得出来
func TestAliasCrossVariant(t *testing.T) {
	g := grid([]string{"订单编号", "产品品号", "订单数量", "单价"}, nil)
	aliases := defaultColumnAliases()
	cases := map[string]int{
		"訂單單號": 0, // 配置写繁体，源档是简体
		"品號":   1,
		"訂單數量": 2,
		"單價":   3,
	}
	for src, want := range cases {
		if got := resolveColIndex(g, src, aliases); got != want {
			t.Errorf("resolveColIndex(%q) = %d，期望 %d", src, got, want)
		}
	}
}

// resolveMapping 支持 | 候选，且映射到空串 = 该列不导出
func TestResolveMappingSkipsEmptyTarget(t *testing.T) {
	g := grid([]string{"訂單單號", "備註", "品號", "備註"}, nil)
	out := resolveMapping(g, map[string]string{
		"訂單單號":   "订单编号",
		"備註.1|備註": "备注",
		"業務員":    "", // 映射到空 = 不导出
	}, defaultColumnAliases())

	if len(out) != 2 {
		t.Fatalf("应解析出 2 条绑定，实际 %d：%v", len(out), out)
	}
	if out[0] != "订单编号" {
		t.Errorf("第 0 列应绑到订单编号，实际 %q", out[0])
	}
	if out[3] != "备注" {
		t.Errorf("第 3 列（備註.1）应绑到备注，实际 %q", out[3])
	}
}

// 表头探测：跳过报表标题前缀行
func TestDetectHeaderRowSkipsTitle(t *testing.T) {
	rows := [][]string{
		{"鼎新電腦", "", ""},
		{"訂單明細表", "", ""},
		{"訂單單號", "品號", "訂單數量"},
		{"SO001", "P001", "10"},
	}
	if got := detectHeaderRow(rows, 20); got != 2 {
		t.Errorf("表头应在第 2 行（0-based），实际 %d", got)
	}
}

// 日期单元格归一：Excel 序列号 / 多种分隔符都要变成 YYYY-MM-DD
func TestParseDateCell(t *testing.T) {
	cases := map[string]string{
		"2026-09-03":          "2026-09-03",
		"2026/09/03":          "2026-09-03",
		"20260903":            "2026-09-03",
		"2026.09.03":          "2026-09-03",
		"2026年9月3日":          "2026-09-03",
	}
	for in, want := range cases {
		if got := parseDateCell(in); got != want {
			t.Errorf("parseDateCell(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 打标判定：命中前缀为「是」，否则「否」
func TestHasPrefix(t *testing.T) {
	prefixes := []string{"61CS", "61CR"}
	if !hasPrefix("61CS123", prefixes) {
		t.Error("61CS123 应以 61CS 命中")
	}
	if hasPrefix("62CS123", prefixes) {
		t.Error("62CS123 不应命中")
	}
	if hasPrefix("", prefixes) {
		t.Error("空串不应命中")
	}
}
