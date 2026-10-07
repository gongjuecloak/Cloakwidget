package main

import (
	"os"
	"strings"
	"testing"
)

// 这三种「.xls」在真实数据里都出现过（路径是用户机器上的样本，缺失就跳过）
var realSamples = []struct {
	name string
	path string
	want srcKind
}{
	{"BIFF 老格式", `D:/User/Cloak_Zeng/Doc/WXWork/1688857941777689/Cache/File/2026-09/物料档案.xls`, srcBiff},
	{"xlsx 伪装成 .xls", `D:/User/Cloak_Zeng/Download/计量单位 (2).xls`, srcXlsx},
	{"SpreadsheetML 2003", `D:/User/Cloak_Zeng/Doc/WXWork/1688857941777689/Cache/File/2026-08/採購單頭檔.xls`, srcXML2003},
}

func TestSniffSourceRealSamples(t *testing.T) {
	for _, c := range realSamples {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Logf("跳过（样本不存在）：%s", c.name)
			continue
		}
		if got := sniffSource(raw); got != c.want {
			t.Errorf("%s: 嗅探结果 %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestReadSourceGridRealSamples(t *testing.T) {
	for _, c := range realSamples {
		if _, err := os.Stat(c.path); err != nil {
			t.Logf("跳过（样本不存在）：%s", c.name)
			continue
		}
		rows, sheet, desc, _, err := readSourceGrid(c.path, 0)
		if err != nil {
			t.Errorf("%s: 读取失败 %v", c.name, err)
			continue
		}
		if len(rows) == 0 {
			t.Errorf("%s: 读到 0 行", c.name)
			continue
		}
		// 表头行必须能找到：非空列数 > 1
		hdr := 0
		for i, r := range rows {
			if len(r) > 1 {
				hdr = i
				break
			}
		}
		nonEmpty := 0
		for _, v := range rows[hdr] {
			if strings.TrimSpace(v) != "" {
				nonEmpty++
			}
		}
		t.Logf("%s | 工作表=%q 行数=%d 格式=%s 首个多列表头行=%d 列数=%d",
			c.name, sheet, len(rows), desc, hdr, nonEmpty)
		t.Logf("    表头样例: %v", firstN(rows[hdr], 8))
		if len(rows) > hdr+1 {
			t.Logf("    数据样例: %v", firstN(rows[hdr+1], 8))
		}
		if nonEmpty < 2 {
			t.Errorf("%s: 表头不像表头（非空列 %d）", c.name, nonEmpty)
		}
	}
}

// TestBiffDates 老 .xls 里日期是序列号，必须还原成日期字符串而不是 46302
func TestBiffDates(t *testing.T) {
	// 46302 = 2026-10-07（Excel 序列号，基数 1899-12-30）
	if got := excelSerialToDate(46302); got != "2026-10-07" {
		t.Errorf("excelSerialToDate(46302) = %q，期望 2026-10-07", got)
	}
	// 带小数的序列号 = 日期 + 时刻
	if got := excelSerialToDate(46302.5); got != "2026-10-07 12:00" {
		t.Errorf("excelSerialToDate(46302.5) = %q，期望 2026-10-07 12:00", got)
	}
	if got := xml2003Date("2026-10-07T00:00:00.000"); got != "2026-10-07" {
		t.Errorf("xml2003Date = %q，期望 2026-10-07", got)
	}
	if got := xml2003Date("2026-10-07T08:30:00.000"); got != "2026-10-07 08:30" {
		t.Errorf("xml2003Date = %q，期望 2026-10-07 08:30", got)
	}
}

// TestDecodeTextEncoding GBK / Big5 / BOM 三种编码都能解出来
func TestDecodeTextEncoding(t *testing.T) {
	// 0xB2C9 0xB9BA 是 GBK 的「采购」
	if s, enc := decodeText([]byte{0xB2, 0xC9, 0xB9, 0xBA, ',', '1'}); !strings.Contains(s, "采购") {
		t.Errorf("GBK 解码失败：%q (%s)", s, enc)
	}
	if s, enc := decodeText([]byte{0xEF, 0xBB, 0xBF, 'a', ',', 'b'}); s != "a,b" || enc != "UTF-8(BOM)" {
		t.Errorf("UTF-8 BOM 解码失败：%q (%s)", s, enc)
	}
	// UTF-16LE 的 "a,b"
	if s, _ := decodeText([]byte{0xFF, 0xFE, 'a', 0, ',', 0, 'b', 0}); s != "a,b" {
		t.Errorf("UTF-16LE 解码失败：%q", s)
	}
	// 已是合法 UTF-8 时不能被误判成 GBK
	if s, enc := decodeText([]byte("物料编码,1")); s != "物料编码,1" || enc != "UTF-8" {
		t.Errorf("UTF-8 误判：%q (%s)", s, enc)
	}
}

func TestSniffDelimiter(t *testing.T) {
	cases := []struct {
		in   string
		want rune
	}{
		{"a,b,c\n1,2,3\n", ','},
		{"a\tb\tc\n1\t2\t3\n", '\t'},
		{"a;b;c\n1;2;3\n", ';'},
		{"a|b|c\n1|2|3\n", '|'},
		{`"x,y",b,c` + "\n" + `"p,q",r,s` + "\n", ','}, // 引号里的逗号不能被误判
	}
	for _, c := range cases {
		if got := sniffDelimiter(c.in); got != c.want {
			t.Errorf("sniffDelimiter(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestParseCSVQuoted(t *testing.T) {
	rows, err := parseCSV("a,b\n\"x,y\",2\n", ',')
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][0] != "x,y" {
		t.Errorf("CSV 解析失败：%v", rows)
	}
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
