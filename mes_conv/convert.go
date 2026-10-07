package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// maxIssues 预检明细最多保留的条数（超大文件不至于把 JSON 撑爆）
const maxIssues = 300

// Issue 预检发现的一条问题，直接对应到「第几行 / 哪一列 / 什么原因」，
// 方便用户对照 MES 的导入报错（Số dòng / Trường / Nguyên nhân lỗi）。
type Issue struct {
	Row   int    `json:"row"`
	Col   string `json:"col"`
	Msg   string `json:"msg"`
	Level string `json:"level"` // error | warn
}

// Report 转换统计 + 预检结果
type Report struct {
	Rows       int            `json:"rows"`
	Types      map[string]int `json:"types"`
	Sources    map[string]int `json:"sources"`
	Issues     []Issue        `json:"issues"`      // 明细（最多 maxIssues 条）
	IssueTotal int            `json:"issue_total"` // 明细总数（可能大于 len(Issues)）
	Errors     []string       `json:"errors"`      // 汇总：必填为空 / 编码重复
	Warnings   []string       `json:"warnings"`    // 汇总：字典未匹配 / 单位未识别 / 值不在白名单
	Notes      []string       `json:"notes"`       // 中性说明：源档格式、批量文件数、模板变化、日期还原
}

// cleanHeader 去掉模板表头的 "* " 必填前缀，得到干净列名
func cleanHeader(h string) string {
	s := strings.TrimSpace(h)
	s = strings.TrimPrefix(s, "*")
	return strings.TrimSpace(s)
}

// isBlank 判断写出去的值是否可视为空
func isBlank(v interface{}) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

func sortedCountKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Convert 按配置把源 xlsx 转成模板格式，写到 outPath，返回统计与预检结果。
// lang 决定预检提示 / 汇总告警 / 错误信息的语言（zh / zht / vi / en），未知语言回退中文。
func Convert(cfg *Config, srcPath, tplPath, outPath, lang string) (*Report, error) {
	L := msgs(lang)
	const colCode = "物料编码" // 模板列名（数据本身，不随界面语言变化）

	// ---- 模板表头 ----
	twb, err := excelize.OpenFile(tplPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["err_open_tpl"], err)
	}
	tSheet := twb.GetSheetName(cfg.TemplateSheet)
	trows, err := twb.GetRows(tSheet)
	if err != nil || len(trows) == 0 {
		twb.Close()
		return nil, fmt.Errorf("%s", L["err_tpl_header"])
	}
	theaders := trows[0]
	idx := make(map[string]int, len(theaders))
	var required []int // 带 * 的必填列下标
	for i, h := range theaders {
		clean := cleanHeader(h)
		if clean == "" {
			continue
		}
		idx[clean] = i
		if strings.HasPrefix(strings.TrimSpace(h), "*") {
			required = append(required, i)
		}
	}
	twb.Close()

	// ---- 源数据 ----
	// 统一读取层：xlsx / 老 .xls(BIFF) / Excel 2003 XML / CSV 都走这里，真身按魔数判断。
	// 物料档案沿用「第 0 行就是表头」的口径（与既有产出对齐，不改动已验证的转换规则）。
	srows, sSheet, srcDesc, _, err := readSourceGrid(srcPath, cfg.SourceSheet)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["err_open_src"], err)
	}
	if len(srows) < 2 {
		return nil, fmt.Errorf("%s", L["err_no_data"])
	}
	_ = sSheet
	sheader := srows[0]
	mbIdx := make(map[string]int, len(sheader))
	for i, h := range sheader {
		if h != "" {
			mbIdx[strings.TrimSpace(h)] = i
		}
	}

	get := func(row []string, mb string) string {
		j, ok := mbIdx[mb]
		if !ok || j >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[j])
	}
	getRaw := func(row []string, mb string) string {
		j, ok := mbIdx[mb]
		if !ok || j >= len(row) {
			return ""
		}
		return row[j]
	}
	lookup := func(dictName, key string) (string, bool) {
		d, ok := cfg.Dicts[dictName]
		if !ok {
			return "", false
		}
		if v, ok := d[key]; ok {
			return v, true
		}
		if n, e := strconv.Atoi(key); e == nil {
			if v, ok := d[strconv.Itoa(n)]; ok {
				return v, true
			}
		}
		return "", false
	}

	// ---- 构建输出 ----
	owb := excelize.NewFile()
	oSheet := cfg.OutputSheet
	if oSheet == "" {
		oSheet = owb.GetSheetName(0)
	}
	owb.SetSheetName(owb.GetSheetName(0), oSheet)
	if err := owb.SetSheetRow(oSheet, "A1", &theaders); err != nil {
		return nil, err
	}

	rep := &Report{Types: map[string]int{}, Sources: map[string]int{}}
	rep.Notes = append(rep.Notes, fmt.Sprintf(L["note_src_fmt"], srcDesc))
	// 模板列变化检测：与上次记录的基准比对，MES 那边悄悄换了模板也能立刻看见
	{
		tNotes, tWarns := checkTemplate(tSheet, theaders, cfg, L)
		rep.Notes = append(rep.Notes, tNotes...)
		rep.Warnings = append(rep.Warnings, tWarns...)
	}
	var issues []Issue
	issueTotal := 0
	addIssue := func(row int, col, msg, level string) {
		issueTotal++
		if len(issues) < maxIssues {
			issues = append(issues, Issue{Row: row, Col: col, Msg: msg, Level: level})
		}
	}
	unknownDict := map[string]int{}  // "字典名 = 原值" -> 行数
	unknownUnit := map[string]int{}  // 未识别单位 -> 行数
	notInList := map[string]int{}    // "目标列 = 值" -> 行数（值不在白名单，如 MES 单位档案）
	errCellByCol := map[string]int{} // 源列 -> Excel 错误值（#NAME? 等）格数

	// 重复检查：关键列来自配置（留空默认「物料编码」），每一列各自成组
	dupCols := cfg.DuplicateKeys
	if len(dupCols) == 0 {
		dupCols = []string{colCode}
	}
	dups := map[string]*dupTracker{}
	var dupActive []string
	for _, c := range dupCols {
		if _, ok := idx[c]; !ok {
			continue
		}
		dups[c] = newDupTracker([]string{c})
		dupActive = append(dupActive, c)
	}

	wrote := 0
	typeIdx, srcIdx := -1, -1
	if i, ok := idx["物料类型"]; ok {
		typeIdx = i
	}
	if i, ok := idx["物料来源"]; ok {
		srcIdx = i
	}

	totalRows := len(srows) - 1
	progressSet(0, totalRows)
	for ri, row := range srows[1:] {
		if ri%100 == 0 { // 每 100 行上报一次，避免频繁加锁
			progressSet(ri, totalRows)
		}
		empty := true
		for _, c := range row {
			if c != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}

		out := make([]interface{}, len(theaders))
		for _, f := range cfg.Fields {
			ci, ok := idx[f.Target]
			if !ok {
				continue
			}
			switch f.Op {
			case "copy":
				var v string
				if f.Raw {
					v = getRaw(row, f.Source)
				} else {
					v = get(row, f.Source)
				}
				if f.Trim {
					v = strings.TrimSpace(v)
				}
				if v == "" && f.Default != "" {
					v = f.Default
				}
				out[ci] = v
			case "case":
				outv := f.DefaultOut
				for _, c := range f.Cases {
					if c.Match == "eq" {
						if strings.EqualFold(get(row, c.Source), c.Value) {
							outv = c.Then
							break
						}
					} else { // contains
						s := get(row, c.Source)
						hit := false
						if c.Value != "" && strings.Contains(s, c.Value) {
							hit = true
						}
						for _, kw := range c.Keywords {
							if kw != "" && strings.Contains(s, kw) {
								hit = true
								break
							}
						}
						if hit {
							outv = c.Then
							break
						}
					}
				}
				out[ci] = outv
			case "dict":
				key := get(row, f.Source)
				if key != "" {
					if v, ok := lookup(f.Dict, key); ok {
						out[ci] = v
					} else {
						out[ci] = key // 未命中字典则保留原值，不丢数据
						unknownDict[fmt.Sprintf("%s = %s", f.Dict, key)]++
					}
				}
			case "num":
				out[ci] = num(get(row, f.Source))
			case "fixed":
				out[ci] = num(f.Value)
			case "rownum":
				out[ci] = ri + 1
			case "cond":
				s := get(row, f.Source)
				if strings.EqualFold(s, f.If) {
					out[ci] = f.Then
				} else {
					out[ci] = f.Else
				}
			case "kwbool":
				s := get(row, f.Source)
				hit := false
				for _, kw := range f.Keywords {
					if kw != "" && strings.Contains(s, kw) {
						hit = true
						break
					}
				}
				if hit {
					out[ci] = f.Then
				} else {
					out[ci] = f.Else
				}
			case "kwmap":
				s := get(row, f.Source)
				val := f.Else
				for _, kw := range f.Keywords {
					if kw != "" && strings.Contains(s, kw) {
						if v, ok := f.KWMap[kw]; ok {
							val = v
							break
						}
					}
				}
				out[ci] = val
			case "unit":
				raw := get(row, f.Source)
				v, hit := translateUnit(raw, cfg.UnitMap, cfg.ValueWhitelist[f.Target])
				out[ci] = v
				if !hit && strings.TrimSpace(raw) != "" {
					unknownUnit[strings.TrimSpace(raw)]++
				}
			}
		}

		if err := owb.SetSheetRow(oSheet, fmt.Sprintf("A%d", wrote+2), &out); err != nil {
			return nil, err
		}

		// ---------- 预检（针对刚写入的这一行） ----------
		rowNo := wrote + 1
		// 1) 必填列为空
		for _, ci := range required {
			if ci >= len(out) || isBlank(out[ci]) {
				addIssue(rowNo, cleanHeader(theaders[ci]), L["chk_required"], "error")
			}
		}
		// 2) 值不在白名单（如 MES 单位档案）
		for target, list := range cfg.ValueWhitelist {
			ci, ok := idx[target]
			if !ok || ci >= len(out) || len(list) == 0 {
				continue
			}
			s, ok := out[ci].(string)
			if !ok || strings.TrimSpace(s) == "" {
				continue
			}
			if !inList(list, s) {
				notInList[target+" = "+s]++
			}
		}
		// 3) 关键列重复（配置驱动，可多列；每列各自成组）
		for _, c := range dupActive {
			ci, ok := idx[c]
			if !ok || ci >= len(out) {
				continue
			}
			cv, _ := out[ci].(string)
			if dup, key, first := dups[c].addValues(rowNo, cv); dup {
				addIssue(rowNo, c, fmt.Sprintf(L["warn_dup"], c, key, first, rowNo), "error")
			}
		}
		// 4) Excel 错误值：源档这一格是坏公式（#NAME? / #REF! …），会原样搬进 MES
		for si, cellv := range row {
			if !excelErrValue(cellv) {
				continue
			}
			scol := ""
			if si < len(sheader) {
				scol = cleanHeader(sheader[si])
			}
			if scol == "" {
				scol = fmt.Sprintf("#%d", si+1)
			}
			errCellByCol[scol]++
			addIssue(rowNo, scol, fmt.Sprintf(L["warn_excel_err"], strings.TrimSpace(cellv)), "warning")
		}

		if typeIdx >= 0 {
			if v, ok := out[typeIdx].(string); ok {
				rep.Types[v]++
			}
		}
		if srcIdx >= 0 {
			if v, ok := out[srcIdx].(string); ok {
				rep.Sources[v]++
			}
		}
		wrote++
	}
	progressSet(totalRows, totalRows) // 补满：空行会被跳过，进度未必自然到 100%

	if err := owb.SaveAs(outPath); err != nil {
		return nil, fmt.Errorf("%s: %w", L["err_save"], err)
	}

	// ---------- 汇总 ----------
	rep.Rows = wrote
	rep.Issues = issues
	rep.IssueTotal = issueTotal
	// 重复检查汇总：每列各报一条「有/无」说明，重复组进 Errors（不阻断，仍需业务判断）
	for _, c := range dupActive {
		d := dups[c]
		lab := keyLabel(d.cols)
		if d.dupGroupCount() == 0 {
			rep.Notes = append(rep.Notes, fmt.Sprintf(L["note_dup_none"], lab))
			continue
		}
		rep.Notes = append(rep.Notes, fmt.Sprintf(L["note_dup_total"], lab, d.dupGroupCount(), d.dupRowCount()))
		for _, it := range d.dupItems() {
			rep.Errors = append(rep.Errors, fmt.Sprintf(L["sum_dup"], lab, it.Key, it.N))
		}
	}
	for _, k := range sortedCountKeys(errCellByCol) {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(L["sum_excel_err"], k, errCellByCol[k]))
	}
	for _, k := range sortedCountKeys(unknownDict) {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(L["sum_dict_unknown"], k, unknownDict[k]))
	}
	for _, k := range sortedCountKeys(unknownUnit) {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(L["sum_unit_unknown"], k, unknownUnit[k]))
	}
	for _, k := range sortedCountKeys(notInList) {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(L["sum_not_in_list"], k, notInList[k]))
	}
	return rep, nil
}

// num 把字符串转成数值（整数去小数，空串返回 nil）
func num(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	if f == math.Trunc(f) {
		return int(f)
	}
	return f
}

// translateUnit 基本单位翻译：
//  1. 源值本身已在目标列白名单（即 MES 档案中真实存在的单位名，如 PCS/SET/套/支/桶/组/卷）→ 原样保留；
//  2. 否则查表精确匹配；
//  3. 再否则「最接近」子串匹配（长键优先，避免 mm 被 m 误中）；
//  4. 已是中文(含 CJK)则原样保留；其余原样保留并视为未识别。
//
// 第二个返回值表示是否命中（即该值是否为 MES 认可的合法单位）。
func translateUnit(s string, m map[string]string, allow []string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	// 1) 档案里已有的单位名，直通（对齐参考文件，避免 PCS 被误译成「件」等）
	if inList(allow, s) {
		return s, true
	}
	low := strings.ToLower(s)
	if v, ok := m[low]; ok {
		return v, true
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		if k != "" && strings.Contains(low, k) {
			return m[k], true
		}
	}
	// 本身就是中文单位名（如 套/支/桶/卷/组），原样保留，视为已识别
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return s, true
		}
	}
	return s, false
}
