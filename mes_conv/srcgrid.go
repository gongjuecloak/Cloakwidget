package main

// ============================================================================
// 源文件统一读取层
//
// 扩展名完全不可信 —— 鼎新/ERP 导出的「.xls」至少有三种真身（实测都见过）：
//   1) OLE2 复合文档   → 真正的 BIFF 老格式（Excel 97-2003）
//   2) ZIP/OOXML       → 其实是 xlsx 改了个名（如「计量单位 (2).xls」）
//   3) SpreadsheetML 2003 XML → 单文件 XML（如「採購單頭檔.xls」，excelize 也不认）
// 所以一律按**文件头魔数**判断，扩展名只用来给文本类文件取个默认分隔符。
//
// 读出的一律是 [][]string（UTF-8 文本网格），后续表头探测 / 列匹配两个模块共用。
// ============================================================================

import (
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xlsReader "github.com/shakinm/xlsReader/xls"
	"github.com/shakinm/xlsReader/xls/structure"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	xunicode "golang.org/x/text/encoding/unicode"
)

// ---------- 格式嗅探 ----------

type srcKind int

const (
	srcXlsx    srcKind = iota // OOXML（ZIP 容器）
	srcBiff                   // 老的 .xls（OLE2 复合文档）
	srcXML2003                // Excel 2003 XML（SpreadsheetML）
	srcText                   // CSV / TSV / 其它文本
)

func (k srcKind) label() string {
	switch k {
	case srcXlsx:
		return "OOXML(.xlsx)"
	case srcBiff:
		return "BIFF(.xls 老格式)"
	case srcXML2003:
		return "SpreadsheetML 2003(.xml)"
	default:
		return "文本(CSV)"
	}
}

var (
	magicOLE2     = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	magicZIP      = []byte{0x50, 0x4B, 0x03, 0x04}
	magicZIPEmpty = []byte{0x50, 0x4B, 0x05, 0x06}
)

// sniffSource 只看文件头。ZIP 还可能是 .docx 之类，交给 excelize 报错即可 ——
// 这里的目标是「别把 BIFF 当成 xlsx 打开」，那是用户最常见的困惑。
func sniffSource(raw []byte) srcKind {
	switch {
	case bytes.HasPrefix(raw, magicOLE2):
		return srcBiff
	case bytes.HasPrefix(raw, magicZIP), bytes.HasPrefix(raw, magicZIPEmpty):
		return srcXlsx
	}
	if looksLikeXMLSpreadsheet(raw) {
		return srcXML2003
	}
	return srcText
}

// ---------- 文本解码（编码嗅探） ----------

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// decodeText 把任意文本字节解成 UTF-8，并返回识别到的编码名（写进日志）。
// 顺序：BOM → 合法 UTF-8 → 在 GB18030 / Big5 之间按「像不像中文」挑一个。
func decodeText(raw []byte) (string, string) {
	switch {
	case bytes.HasPrefix(raw, utf8BOM):
		return string(raw[len(utf8BOM):]), "UTF-8(BOM)"
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		if s, err := decBytes(xunicode.UTF16(xunicode.LittleEndian, xunicode.IgnoreBOM), raw[2:]); err == nil {
			return s, "UTF-16LE"
		}
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		if s, err := decBytes(xunicode.UTF16(xunicode.BigEndian, xunicode.IgnoreBOM), raw[2:]); err == nil {
			return s, "UTF-16BE"
		}
	}
	if utf8.Valid(raw) {
		return string(raw), "UTF-8"
	}
	// 非 UTF-8：繁体 ERP 导出可能是 Big5，大陆多为 GBK/GB18030。
	// GB18030 是 GBK 的超集，两个都试，用「解码后像不像正常中文」打分取高者。
	gb, e1 := decBytes(simplifiedchinese.GB18030, raw)
	b5, e2 := decBytes(traditionalchinese.Big5, raw)
	if e1 != nil && e2 != nil {
		return string(raw), "未知(按原样)"
	}
	if e2 == nil && (e1 != nil || cjkScore(b5) > cjkScore(gb)) {
		return b5, "Big5"
	}
	return gb, "GB18030"
}

// decBytes 用给定编码把字节解成 UTF-8 字符串
func decBytes(e encoding.Encoding, raw []byte) (string, error) {
	out, err := e.NewDecoder().Bytes(raw)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------- 分隔符嗅探 ----------

var delimCandidates = []rune{',', '\t', ';', '|'}

func delimName(r rune) string {
	switch r {
	case ',', '，':
		return "逗号"
	case '\t':
		return "制表符"
	case ';':
		return "分号"
	case '|':
		return "竖线"
	}
	return string(r)
}

// sniffDelimiter 用「按该分隔符解析出的字段数是否稳定且大于 1」来打分。
// 比单纯数字符更准：能自动避开引号里的逗号。
func sniffDelimiter(txt string) rune {
	lines := sampleLines(txt, 30)
	if len(lines) == 0 {
		return ','
	}
	best, bestScore := ',', -1.0
	for _, d := range delimCandidates {
		score, total, ok := 0.0, 0, 0
		counts := map[int]int{}
		for _, ln := range lines {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			r := csv.NewReader(strings.NewReader(ln))
			r.Comma = d
			r.FieldsPerRecord = -1
			r.LazyQuotes = true
			recs, err := r.Read()
			if err != nil {
				continue
			}
			total++
			counts[len(recs)]++
			if len(recs) > 1 {
				ok++
			}
		}
		if total == 0 || ok == 0 {
			continue
		}
		// 字段数众数越高、越稳定，得分越高
		mode, modeN := 0, 0
		for n, c := range counts {
			if c > modeN || (c == modeN && n > mode) {
				mode, modeN = n, c
			}
		}
		score = float64(ok)/float64(total) + float64(mode)/100.0
		if score > bestScore {
			best, bestScore = d, score
		}
	}
	return best
}

// ---------- 统一入口 ----------

// readSourceGrid 读一个工作表，返回二维字符串网格、工作表名、识别说明、以及格式真身。
// sheetIdx < 0 或越界时取第一个工作表。
func readSourceGrid(path string, sheetIdx int) (rows [][]string, sheetName, desc string, kind srcKind, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", srcText, err
	}
	switch sniffSource(raw) {
	case srcXlsx:
		rows, sheetName, err = readXlsxGrid(path, sheetIdx)
		return rows, sheetName, srcXlsx.label(), srcXlsx, err
	case srcBiff:
		rows, sheetName, err = readBiffGrid(path, sheetIdx)
		return rows, sheetName, srcBiff.label(), srcBiff, err
	case srcXML2003:
		txt, enc := decodeText(raw)
		rows, sheetName, err = readXML2003Grid(txt, sheetIdx)
		if err != nil {
			return nil, "", "", srcXML2003, err
		}
		return rows, sheetName, fmt.Sprintf("%s, %s", srcXML2003.label(), enc), srcXML2003, nil
	default:
		txt, enc := decodeText(raw)
		d := sniffDelimiter(txt)
		if strings.EqualFold(filepath.Ext(path), ".tsv") {
			d = '\t'
		}
		rows, err = parseCSV(txt, d)
		return rows, "CSV", fmt.Sprintf("%s, %s, 分隔符 %s", srcText.label(), enc, delimName(d)), srcText, err
	}
}

// fixSerialDates 把指定源列里的「日期序列号」还原成日期字符串。
//
// 只用在一处：老 .xls（BIFF）里自定义日期格式的单元格，其 XF 格式号不在内置列表里，
// 单看单元格判不出来；但如果这一列映射到了配置里声明的日期目标列，那就是日期无疑。
// 返回还原了多少个单元格（写进转换日志，便于确认没白改）。
func fixSerialDates(g *sheetGrid, cols []int) int {
	if len(cols) == 0 {
		return 0
	}
	seen := map[int]bool{}
	fixed := 0
	for _, ci := range cols {
		if ci < 0 || seen[ci] {
			continue
		}
		seen[ci] = true
		for ri, row := range g.Rows {
			if ci >= len(row) || !looksNumeric(row[ci]) {
				continue
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(row[ci]), 64)
			// 序列号落在 1900-01-01 ~ 2200-01-01 之外的不动（避免把数量、金额当日期）
			if err != nil || f < 2 || f > 109600 {
				continue
			}
			row[ci] = excelSerialToDate(f)
			g.Rows[ri] = row
			fixed++
		}
	}
	return fixed
}

func parseCSV(txt string, delim rune) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(txt))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = false
	recs, err := r.ReadAll()
	if err != nil && len(recs) == 0 {
		return nil, err
	}
	return recs, nil
}

// ---------- OOXML ----------

func readXlsxGrid(path string, sheetIdx int) ([][]string, string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	list := f.GetSheetList()
	if len(list) == 0 {
		return nil, "", fmt.Errorf("no-sheet")
	}
	if sheetIdx < 0 || sheetIdx >= len(list) {
		sheetIdx = 0
	}
	name := list[sheetIdx]
	rows, err := f.GetRows(name)
	if err != nil {
		return nil, name, err
	}
	return rows, name, nil
}

// ---------- BIFF（老的 .xls） ----------

// readBiffGrid 用纯 Go 的 xls 库读 BIFF8。
// 库内部对 XF 索引的越界守卫不严（某些精简文件会 panic），所以这里兜一层 recover，
// 把 panic 转成可读错误 —— 宁可提示「这个老文件读不了」，也不能让整个程序崩掉。
func readBiffGrid(path string, sheetIdx int) (rows [][]string, sheetName string, err error) {
	defer func() {
		if r := recover(); r != nil {
			rows, sheetName = nil, ""
			err = fmt.Errorf("biff-panic: %v", r)
		}
	}()

	wb, e := xlsReader.OpenFile(path)
	if e != nil {
		return nil, "", e
	}
	sheets := wb.GetSheets() // 注意：不要走 wb.GetSheet(i)，它的越界守卫有 bug（i == len 时会 panic）
	if len(sheets) == 0 {
		return nil, "", fmt.Errorf("no-sheet")
	}
	if sheetIdx < 0 || sheetIdx >= len(sheets) {
		sheetIdx = 0
	}
	sh := &sheets[sheetIdx]
	name := sh.GetName()

	raw := sh.GetRows()
	out := make([][]string, 0, len(raw))
	for _, r := range raw {
		cols := r.GetCols() // 已按列下标补齐（空洞返回空串）
		if len(cols) == 0 {
			out = append(out, nil)
			continue
		}
		row := make([]string, len(cols))
		for i, cd := range cols {
			row[i] = biffCellString(cd, &wb)
		}
		out = append(out, trimTrailingEmpty(row))
	}
	return out, name, nil
}

// biffCellString 取单元格文本。数字若带日期格式，换成 YYYY-MM-DD
// —— 否则老 .xls 里的日期会变成 45678 这种序列号，静默污染数据。
func biffCellString(cd structure.CellData, wb *xlsReader.Workbook) string {
	if cd == nil {
		return ""
	}
	s := cd.GetString()
	if s == "" || !looksNumeric(s) {
		return s
	}
	if isBuiltinDateFormat(biffFormatIndex(cd, wb)) {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
			return excelSerialToDate(f)
		}
	}
	return s
}

// biffFormatIndex 安全地取单元格的格式索引，任何异常都退回默认格式 0
func biffFormatIndex(cd structure.CellData, wb *xlsReader.Workbook) int {
	defer func() { _ = recover() }()
	xi := cd.GetXFIndex()
	if xi < 0 {
		return 0
	}
	xf := wb.GetXFbyIndex(xi) // 先落到变量上：GetFormatIndex 是指针方法，不能直接链式调用
	return xf.GetFormatIndex()
}

// isBuiltinDateFormat BIFF 内置格式号里属于日期/日期时间的那些（0x0E~0x11、0x16、0x2D~0x2F）
func isBuiltinDateFormat(idx int) bool {
	switch {
	case idx >= 14 && idx <= 17: // m/d/yy、d-mmm-yy、d-mmm、mmm-yy
		return true
	case idx == 22: // m/d/yy h:mm
		return true
	case idx >= 45 && idx <= 47: // mm:ss、[h]:mm:ss、mm:ss.0
		return false // 纯时间，保持原样更安全
	}
	return false
}

// excelSerialToDate 序列号 → 日期字符串。基数取 1899-12-30（吸收 Excel 的 1900 闰年 bug）。
func excelSerialToDate(f float64) string {
	days := int(f)
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	t := base.AddDate(0, 0, days)
	if frac := f - float64(days); frac > 1e-9 {
		t = t.Add(time.Duration(frac * 24 * float64(time.Hour)))
	}
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04")
}

func looksNumeric(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

// ---------- SpreadsheetML 2003 ----------

func looksLikeXMLSpreadsheet(raw []byte) bool {
	head := raw
	if len(head) > 4096 {
		head = head[:4096]
	}
	s, _ := decodeText(head)
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '<' {
		return false
	}
	return strings.Contains(s, "spreadsheet") || strings.Contains(s, "<?mso-application")
}

type xml2003Workbook struct {
	Worksheets []xml2003Sheet `xml:"Worksheet"`
}

type xml2003Sheet struct {
	Name  string       `xml:"Name,attr"`
	Table xml2003Table `xml:"Table"`
}

type xml2003Table struct {
	Rows []xml2003Row `xml:"Row"`
}

type xml2003Row struct {
	Index int           `xml:"Index,attr"`
	Cells []xml2003Cell `xml:"Cell"`
}

type xml2003Cell struct {
	Index       int         `xml:"Index,attr"`
	MergeAcross int         `xml:"MergeAcross,attr"`
	Data        xml2003Data `xml:"Data"`
}

type xml2003Data struct {
	Type  string `xml:"Type,attr"`
	Value string `xml:",chardata"`
}

// readXML2003Grid 解析 Excel 2003 XML。注意：
//   - ss:Index 是 1-based 的列号，跳号处要补空列（鼎新导出大量使用）
//   - ss:MergeAcross 表示横向合并，值只在第一格，后续补空
//   - ss:Type=DateTime 形如 2026-10-07T00:00:00.000 → 截成日期
//
// Go 的 xml 解码在标签未写命名空间时按「本地名」匹配，正好适配 ss: 前缀。
func readXML2003Grid(txt string, sheetIdx int) ([][]string, string, error) {
	dec := xml.NewDecoder(strings.NewReader(txt))
	dec.CharsetReader = charsetReader
	var wb xml2003Workbook
	if err := dec.Decode(&wb); err != nil {
		return nil, "", fmt.Errorf("xml-2003: %w", err)
	}
	if len(wb.Worksheets) == 0 {
		return nil, "", fmt.Errorf("no-sheet")
	}
	if sheetIdx < 0 || sheetIdx >= len(wb.Worksheets) {
		sheetIdx = 0
	}
	sh := wb.Worksheets[sheetIdx]

	var out [][]string
	for ri, r := range sh.Table.Rows {
		if r.Index > 0 { // 跳行时补空行，保证行号与 Excel 里看到的一致
			for len(out)+1 < r.Index {
				out = append(out, nil)
			}
		}
		row := make([]string, 0, len(r.Cells))
		col := 0
		for _, c := range r.Cells {
			if c.Index > 0 { // 1-based
				for col+1 < c.Index {
					row = append(row, "")
					col++
				}
			}
			v := c.Data.Value
			if strings.EqualFold(c.Data.Type, "DateTime") {
				v = xml2003Date(v)
			}
			row = append(row, v)
			col++
			for i := 0; i < c.MergeAcross; i++ {
				row = append(row, "")
				col++
			}
		}
		_ = ri
		out = append(out, trimTrailingEmpty(row))
	}
	return out, sh.Name, nil
}

// xml2003Date 2026-10-07T00:00:00.000 → 2026-10-07（保留时间时用空格分隔）
func xml2003Date(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 10 && v[4] == '-' && v[7] == '-' {
		datePart := v[:10]
		if len(v) >= 16 && (v[10] == 'T' || v[10] == ' ') {
			timePart := v[11:16]
			if timePart != "00:00" {
				return datePart + " " + timePart
			}
		}
		return datePart
	}
	return v
}

// charsetReader 让 encoding/xml 能处理声明为 GB2312/Big5 的文件（老 ERP 常见）
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "gb2312", "gbk", "gb18030", "cp936":
		return simplifiedchinese.GB18030.NewDecoder().Reader(input), nil
	case "big5", "cp950":
		return traditionalchinese.Big5.NewDecoder().Reader(input), nil
	case "utf-16", "utf-16le":
		return xunicode.UTF16(xunicode.LittleEndian, xunicode.IgnoreBOM).NewDecoder().Reader(input), nil
	}
	return input, nil
}

// dateSourceCols 从「源列下标 -> 目标列名」的映射里挑出映射到日期目标列的源列，
// 供老 .xls 的序列号兜底还原使用。
func dateSourceCols(resolved map[int]string, dateTargets map[string]bool) []int {
	var out []int
	for ci, target := range resolved {
		if dateTargets[target] {
			out = append(out, ci)
		}
	}
	return out
}

// ---------- 批量：多份源档合并 ----------

// mergedGrid 多文件合并的结果
type mergedGrid struct {
	Rows  [][]string // 第 0 行是合并后的表头
	Notes []string   // 每个文件的行数说明，直接进转换日志
	Files int
}

// mergeSourceGrids 把多份源档按「列名」合并成一份：
//   - 表头取并集：以第一个文件出现的顺序为准，后面文件新出现的列追加到右边
//   - 同名列（如两个「備註」）按出现次序配对，与 pandas 的 備註 / 備註.1 规则一致
//   - 数据行按列名归位，所以各文件列顺序不同也能对齐
//
// displayNames 用来在日志里显示「用户上传时的文件名」——落盘用的是临时名，
// 只写临时名用户认不出来是哪个文件。
// detectHeader=true 时走表头自动探测（订单/工单档常有报表标题前缀行）；
// 物料档案沿用「第 0 行就是表头」的口径。
func mergeSourceGrids(paths, displayNames []string, sheetIdx int, detectHeader bool) (*mergedGrid, error) {
	type part struct {
		name string
		head []string
		rows [][]string
	}
	var parts []part
	mg := &mergedGrid{}
	for i, p := range paths {
		name := filepath.Base(p)
		if i < len(displayNames) && strings.TrimSpace(displayNames[i]) != "" {
			name = displayNames[i]
		}
		rows, _, _, _, err := readSourceGrid(p, sheetIdx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if len(rows) == 0 {
			mg.Notes = append(mg.Notes, fmt.Sprintf("· %s：0 行（已跳过）", name))
			continue
		}
		hr := 0
		if detectHeader {
			hr = detectHeaderRow(rows, 20)
		}
		if hr < 0 || hr >= len(rows) {
			hr = 0
		}
		parts = append(parts, part{name: name, head: rows[hr], rows: rows[hr+1:]})
		mg.Notes = append(mg.Notes, fmt.Sprintf("· %s：%d 行", name, len(rows)-hr-1))
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no-usable-source")
	}
	mg.Files = len(parts)

	// ---- 表头并集 ----
	unionHead := []string{}
	keyIdx := map[string]int{}
	for _, pt := range parts {
		seen := map[string]int{}
		for _, h := range pt.head {
			k := normCol(h)
			if k == "" {
				continue
			}
			seen[k]++
			key := k
			if seen[k] > 1 {
				key = fmt.Sprintf("%s#%d", k, seen[k])
			}
			if _, ok := keyIdx[key]; !ok {
				keyIdx[key] = len(unionHead)
				unionHead = append(unionHead, strings.TrimSpace(h))
			}
		}
	}
	if len(unionHead) == 0 {
		return nil, fmt.Errorf("no-header")
	}

	// ---- 数据行按列名归位 ----
	out := make([][]string, 0, 1024)
	for _, pt := range parts {
		colMap := make([]int, len(pt.head))
		seen := map[string]int{}
		for i, h := range pt.head {
			k := normCol(h)
			if k == "" {
				colMap[i] = -1
				continue
			}
			seen[k]++
			key := k
			if seen[k] > 1 {
				key = fmt.Sprintf("%s#%d", k, seen[k])
			}
			colMap[i] = keyIdx[key]
		}
		for _, row := range pt.rows {
			if len(row) == 0 {
				continue
			}
			blank := true
			line := make([]string, len(unionHead))
			for i, v := range row {
				if i >= len(colMap) || colMap[i] < 0 {
					continue
				}
				line[colMap[i]] = v
				if strings.TrimSpace(v) != "" {
					blank = false
				}
			}
			if blank {
				continue
			}
			out = append(out, line)
		}
	}
	mg.Rows = append([][]string{unionHead}, out...)
	return mg, nil
}

// writeGridXlsx 把二维网格落成一个临时 xlsx，好让转换主流程原样复用
// （写出去的单元格是文本，读回来还是原样字符串，不会被 Excel 的类型推断改动）
func writeGridXlsx(rows [][]string, path string) error {
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	for i, row := range rows {
		vals := make([]interface{}, len(row))
		for j, v := range row {
			vals[j] = v
		}
		if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+1), &vals); err != nil {
			return err
		}
	}
	return f.SaveAs(path)
}

// ---------- 小工具 ----------

// trimTrailingEmpty 去掉行尾空列（源档常有一堆空白列，白占内存）
func trimTrailingEmpty(row []string) []string {
	last := -1
	for i, v := range row {
		if strings.TrimSpace(v) != "" {
			last = i
		}
	}
	if last < 0 {
		return nil
	}
	return row[:last+1]
}

func sampleLines(txt string, n int) []string {
	var out []string
	for _, ln := range strings.Split(txt, "\n") {
		out = append(out, strings.TrimRight(ln, "\r"))
		if len(out) >= n {
			break
		}
	}
	return out
}

// cjkScore 给解码结果打分：越像「正常中文文本」越高。
// 乱码通常表现为替换符、私用区或罕用扩展汉字，所以对它们重罚。
func cjkScore(s string) float64 {
	good, bad := 0.0, 0.0
	for _, r := range s {
		switch {
		case r == '\uFFFD':
			bad += 5
		case r >= 0xE000 && r <= 0xF8FF: // 私用区
			bad += 3
		case r >= 0x20000: // CJK 扩展 B 及以后
			bad += 1
		case r >= 0x4E00 && r <= 0x9FFF:
			good += 2
		case r == '\n' || r == '\r' || r == '\t':
			good += 0.1
		case r >= 0x20 && r < 0x7F:
			good += 0.2
		case r >= 0x3000 && r <= 0x303F: // 中文标点
			good += 1
		case r >= 0xFF00 && r <= 0xFFEF: // 全角
			good += 0.5
		}
	}
	return good - bad
}
