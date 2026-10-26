package main

// BOM 模块：把 ERP 导出的「BOM 抓取」大表转成 MES 的 BOM 导入模板（BOM主表 + BOM明细）。
// 逻辑与独立版 bomconv 一致：固定列号的源格式（ERP 导出格式稳定），
// 按主件去重生成主表、逐行生成明细；计量单位映射表仅用于核对诊断（明细不写单位列）。
// 输出文件最后做一次 <dimension> 修复：excelize 追加行后不刷新 dimension，
// 陈旧的 dimension 会让「信任 dimension 的读取器」（openpyxl read_only、MES 导入器）只看到前几行。

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	bomSrcSheetDefault    = ``
	bomMasterSheetDefault = `BOM主表`
	bomDetailSheetDefault = `BOM明细`
)

// BomOptions 转换选项
type BomOptions struct {
	Basediv     bool       // true: 底數≠1 时基础用量 = 組成用量÷底數；false: 直接取組成用量原值
	SrcSheet    string     // 源工作表名，空用默认
	MasterSheet string     // 目标主表名，空用默认
	DetailSheet string     // 目标明细表名，空用默认
	Cfg         *BomConfig // 非空时按配置解析源列位 / 固定值 / 单位别名；空用内置默认
}

// BomUnitStat 源單位出现次数与映射结果（诊断用）
type BomUnitStat struct {
	Raw    string `json:"raw"`
	Count  int    `json:"count"`
	Mapped string `json:"mapped"`
	Status string `json:"status"` // OK / EMPTY / UNMAPPED / AMBIG_RESOLVED / AMBIG_FALLBACK / NO_MAP
}

// BomReport 转换结果
type BomReport struct {
	MasterRows int           `json:"master_rows"`
	DetailRows int           `json:"detail_rows"`
	EmptySeq   int           `json:"empty_seq"`
	NonNumSeq  int           `json:"non_numeric_seq"`
	EmptyChild int           `json:"empty_child"`
	DivApplied int           `json:"div_applied"`
	BaseRule   string        `json:"base_rule"`
	UnitStats  []BomUnitStat `json:"unit_stats"`
	DimFixes   []string      `json:"dim_fixes"`
}

// ---------------- 通用小工具（bomconv 移植，包内无同名冲突） ----------------

func bomClean(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "\""))
}

func bomGet(cols []string, i int) string {
	if i < len(cols) {
		return bomClean(cols[i])
	}
	return ""
}

func bomToNum(s string) interface{} {
	if s == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func bomToInt(s string) interface{} {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return int(f)
	}
	return s
}

// ---------------- dimension 修复 ----------------

var (
	bomRowRe  = regexp.MustCompile(`<row[^>]*\br="(\d+)"`)
	bomCellRe = regexp.MustCompile(`<c[^>]*\br="([A-Z]+)\d+"`)
	bomDimRe  = regexp.MustCompile(`<dimension\b[^>]*>(?:</dimension>)?`)
)

func bomColToNum(s string) int {
	n := 0
	for _, c := range s {
		n = n*26 + int(c-'A'+1)
	}
	return n
}

func bomColName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

// bomFixDimensions 重写每个工作表的 <dimension> 为真实使用范围。
func bomFixDimensions(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var report []string

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			maxRow, maxCol := 0, 0
			for _, m := range bomRowRe.FindAllSubmatch(data, -1) {
				if n, e := strconv.Atoi(string(m[1])); e == nil && n > maxRow {
					maxRow = n
				}
			}
			for _, m := range bomCellRe.FindAllSubmatch(data, -1) {
				if n := bomColToNum(string(m[1])); n > maxCol {
					maxCol = n
				}
			}
			if maxRow > 0 && maxCol > 0 {
				nd := fmt.Sprintf(`<dimension ref="A1:%s%d"/>`, bomColName(maxCol), maxRow)
				if bomDimRe.Match(data) {
					data = bomDimRe.ReplaceAll(data, []byte(nd))
				} else if i := bytes.Index(data, []byte("<sheetData")); i >= 0 {
					data = append(append(append([]byte{}, data[:i]...), []byte(nd)...), data[i:]...)
				}
				report = append(report, fmt.Sprintf("%s -> A1:%s%d", f.Name, bomColName(maxCol), maxRow))
			}
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return nil, err
	}
	return report, nil
}

// ---------------- 计量单位映射（仅诊断） ----------------

type bomUnitMaps struct {
	names, syms, ens map[string]string
}

// bomLoadUnits 读映射表。注意「计量单位 (3).xls」实际是 xlsx 容器，
// 用 excelize.OpenReader 按内容嗅探打开（对真实 xls 也能走 OpenReader 之外的分支）。
func bomLoadUnits(path string) (*bomUnitMaps, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		return nil, err
	}
	m := &bomUnitMaps{map[string]string{}, map[string]string{}, map[string]string{}}
	for i, r := range rows {
		if i == 0 {
			continue
		}
		name, en, sym := bomGet(r, 2), bomGet(r, 3), bomGet(r, 4)
		if name != "" {
			m.names[strings.ToUpper(name)] = name
		}
		if en != "" {
			m.ens[strings.ToUpper(en)] = name
		}
		if sym != "" {
			m.syms[strings.ToUpper(sym)] = name
		}
	}
	return m, nil
}

// bomMapUnit 把源單位映射到 MES 单位名。
func bomMapUnit(m *bomUnitMaps, u string) (string, string) {
	u2 := bomClean(u)
	if u2 == "" {
		return "", "EMPTY"
	}
	if m == nil {
		return u2, "NO_MAP"
	}
	k := strings.ToUpper(u2)
	// 手工别名：源表出现的 PCSQ 归到 PCS
	if k == "PCSQ" {
		k = "PCS"
	}
	cands := map[string]bool{}
	if v, ok := m.names[k]; ok {
		cands[v] = true
	}
	if v, ok := m.syms[k]; ok {
		cands[v] = true
	}
	if v, ok := m.ens[k]; ok {
		cands[v] = true
	}
	if len(cands) == 0 {
		return u2, "UNMAPPED"
	}
	if len(cands) == 1 {
		for v := range cands {
			return v, "OK"
		}
	}
	for v := range cands {
		if strings.ToUpper(v) == k {
			return v, "AMBIG_RESOLVED"
		}
	}
	keys := make([]string, 0, len(cands))
	for v := range cands {
		keys = append(keys, v)
	}
	sort.Strings(keys)
	return keys[0], "AMBIG_FALLBACK"
}

// ---------------- 主转换 ----------------

// bomCopyFile 把模板文件整份拷贝为输出文件：styles.xml / theme 原样保留，
// 表头样式（含主题色）因此天然有效——这是「跨工作簿复制样式定义」做不到的
// （GetStyle→NewStyle 会把主题色写成非法 RGB，openpyxl 直接打不开）。
func bomCopyFile(src, dst string) error {
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

// bomHeaderShell 读模板某张表的表头：值、每格样式 ID（同文件内有效）与列宽。
func bomHeaderShell(f *excelize.File, sh string, L map[string]string) (vals []string, sty []int, widths []float64, err error) {
	for c := 1; c <= 256; c++ {
		cell, _ := excelize.CoordinatesToCellName(c, 1)
		v, e := f.GetCellValue(sh, cell)
		if e != nil {
			return nil, nil, nil, fmt.Errorf("%s %s: %w", L["b_err_tpl"], sh, e)
		}
		if v == "" {
			break
		}
		vals = append(vals, v)
		sid, _ := f.GetCellStyle(sh, cell)
		sty = append(sty, sid)
		col, _ := excelize.ColumnNumberToName(c)
		w, _ := f.GetColWidth(sh, col)
		widths = append(widths, w)
	}
	if len(vals) == 0 {
		return nil, nil, nil, fmt.Errorf("%s %s: %s", L["b_err_tpl"], sh, L["b_err_no_header"])
	}
	return vals, sty, widths, nil
}

// bomWriteHeaderRow 用 StreamWriter 写表头行：每格带模板原样式 ID（同一文件内有效），
// 并回填列宽。必须在任何数据行写入前调用（SetColWidth 要求先于行写入）。
func bomWriteHeaderRow(sw *excelize.StreamWriter, vals []string, sty []int, widths []float64) error {
	for c, w := range widths {
		if w <= 0 {
			continue
		}
		if e := sw.SetColWidth(c+1, c+1, w); e != nil {
			return e
		}
	}
	cells := make([]interface{}, len(vals))
	for c, v := range vals {
		cell := excelize.Cell{Value: v}
		if c < len(sty) && sty[c] > 0 {
			cell.StyleID = sty[c]
		}
		cells[c] = cell
	}
	return sw.SetRow("A1", cells)
}

// ConvertBOM 把源 ERP BOM 抓取表写进目标模板（BOM主表 + BOM明细），输出到 outPath。
// mapPath 允许为空（不做单位核对诊断）。
func ConvertBOM(srcPath, tplPath, mapPath, outPath string, opt BomOptions, L map[string]string, logf func(string)) (*BomReport, error) {
	if opt.SrcSheet == "" {
		opt.SrcSheet = bomSrcSheetDefault
	}
	if opt.MasterSheet == "" {
		opt.MasterSheet = bomMasterSheetDefault
	}
	if opt.DetailSheet == "" {
		opt.DetailSheet = bomDetailSheetDefault
	}

	var um *bomUnitMaps
	if mapPath != "" {
		var err error
		um, err = bomLoadUnits(mapPath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", L["b_err_map"], err)
		}
		logf(fmt.Sprintf(L["b_map_loaded"], len(um.names), len(um.syms), len(um.ens)))
	}

	src, err := excelize.OpenFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_src"], err)
	}
	defer src.Close()

	// 源工作表：未指定时用第一个；指定了但不存在时回退第一个并写日志告警（与列回退同一理念）
	if sheets := src.GetSheetList(); len(sheets) > 0 {
		found := opt.SrcSheet == ""
		for _, s := range sheets {
			if s == opt.SrcSheet {
				found = true
				break
			}
		}
		if !found {
			logf(fmt.Sprintf(L["b_warn_sheet_fallback"], opt.SrcSheet, sheets[0]))
		}
		if !found || opt.SrcSheet == "" {
			opt.SrcSheet = sheets[0]
		}
	}

	// 输出 = 模板整份拷贝（styles/theme/表头样式原样保留），再用 StreamWriter
	// 把两张表的数据区整表重写。不在模板上原地清行——excelize v2.11 的
	// RemoveRow 只支持单行删除，模板带几万行旧数据时逐行删是 O(n²)。
	if err := bomCopyFile(tplPath, outPath); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_save"], err)
	}
	outF, err := excelize.OpenFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_tpl"], err)
	}
	defer outF.Close()

	mHdr, mSty, mW, err := bomHeaderShell(outF, opt.MasterSheet, L)
	if err != nil {
		return nil, err
	}
	dHdr, dSty, dW, err := bomHeaderShell(outF, opt.DetailSheet, L)
	if err != nil {
		return nil, err
	}

	// 明细表流式写入：表头（带原样式）+ 数据行按行号递增直写
	dSw, err := outF.NewStreamWriter(opt.DetailSheet)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_write_detail"], err)
	}
	if err := bomWriteHeaderRow(dSw, dHdr, dSty, dW); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_write_detail"], err)
	}

	iter, err := src.Rows(opt.SrcSheet)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_rows"], err)
	}

	// 源列位：按配置解析（表头名优先 / 列号次之），失败回退内置默认列位并写日志提示
	cfg := opt.Cfg
	if cfg == nil {
		cfg = defaultBomConfig()
	}
	colsIdx, colFallback := bomResolveColumns(src, opt.SrcSheet, cfg.Columns)
	for _, role := range colFallback {
		logf(fmt.Sprintf(L["b_warn_col_fallback"], role, cfg.Columns[role]))
	}

	masterName := map[string]string{}
	masterOrder := []string{}
	unitRawStats := map[string]int{}
	emptySeq, emptyChild, nonNumSeq := 0, 0, 0
	divApplied := 0
	detailCount := 0
	detailRow := 2
	headerSkipped := false

	// 固定值：配置可覆盖内置默认
	mfPrefix := bomFixedVal(cfg.MasterFixed, "name_prefix", "BOM-")
	mfType := bomFixedVal(cfg.MasterFixed, "bom_type", "量产BOM")
	mfKind := bomFixedVal(cfg.MasterFixed, "kind", "production")
	mfUnit := bomFixedVal(cfg.MasterFixed, "unit", "支")
	dfLayer := bomFixedVal(cfg.DetailFixed, "layer", "1")
	dfIssue := bomFixedVal(cfg.DetailFixed, "issue_type", "定额发料")
	dfAllowSub := bomFixedVal(cfg.DetailFixed, "allow_sub", "是")
	dfMain := bomFixedVal(cfg.DetailFixed, "main_flag", "否")

	for iter.Next() {
		if !headerSkipped {
			headerSkipped = true
			continue
		}
		cols, err := iter.Columns()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", L["b_err_cols"], err)
		}
		parent := bomGet(cols, colsIdx["parent"])
		if parent == "" {
			continue
		}
		child := bomGet(cols, colsIdx["child"])
		seqRaw := bomGet(cols, colsIdx["seq"])
		base := bomGet(cols, colsIdx["base"]) // 組成用量
		if opt.Basediv {
			// 底數≠1 时归一化为「每 1 支主件用量」
			if cu, e1 := strconv.ParseFloat(base, 64); e1 == nil {
				if d, e2 := strconv.ParseFloat(bomGet(cols, colsIdx["base_div"]), 64); e2 == nil && d != 0 && d != 1 {
					base = strconv.FormatFloat(cu/d, 'f', -1, 64)
					divApplied++
				}
			}
		}
		loss := bomGet(cols, colsIdx["loss"])
		unitRaw := bomGet(cols, colsIdx["unit"])

		if child == "" {
			emptyChild++
		}
		if seqRaw == "" {
			emptySeq++
		} else if _, e := strconv.Atoi(seqRaw); e != nil {
			nonNumSeq++
		}
		unitRawStats[unitRaw]++

		if _, ok := masterName[parent]; !ok {
			masterName[parent] = bomGet(cols, colsIdx["parent_name"])
			masterOrder = append(masterOrder, parent)
		}

		dRow := []interface{}{
			parent, bomToInt(seqRaw), bomToInt(dfLayer), nil, child, bomToNum(base), bomToNum(loss),
			nil, dfIssue, dfAllowSub, dfMain, nil, nil, nil,
		}
		cell, _ := excelize.CoordinatesToCellName(1, detailRow)
		if err := dSw.SetRow(cell, dRow); err != nil {
			return nil, fmt.Errorf("%s %d: %w", L["b_err_write_detail"], detailRow, err)
		}
		detailRow++
		detailCount++
		if detailCount%5000 == 0 {
			progressSet(detailCount, 0) // 总行数未知，做不定进度：UI 显示已耗时与活动状态
			logf(fmt.Sprintf(L["b_progress"], detailCount))
		}
	}
	iter.Close()
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_iter"], err)
	}
	if err := dSw.Flush(); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_write_detail"], err)
	}

	// 主表流式写入
	mSw, err := outF.NewStreamWriter(opt.MasterSheet)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_write_master"], err)
	}
	if err := bomWriteHeaderRow(mSw, mHdr, mSty, mW); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_write_master"], err)
	}
	masterRow := 2
	for _, parent := range masterOrder {
		name := mfPrefix + parent
		mRow := []interface{}{
			parent, name, nil, nil, parent, nil,
			mfType, mfKind, nil, mfUnit, nil, nil, nil, nil,
		}
		cell, _ := excelize.CoordinatesToCellName(1, masterRow)
		if err := mSw.SetRow(cell, mRow); err != nil {
			return nil, fmt.Errorf("%s %d: %w", L["b_err_write_master"], masterRow, err)
		}
		masterRow++
	}
	if err := mSw.Flush(); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_write_master"], err)
	}

	if err := outF.Save(); err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_save"], err)
	}

	reps, err := bomFixDimensions(outPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", L["b_err_dim"], err)
	}

	rep := &BomReport{
		MasterRows: len(masterOrder),
		DetailRows: detailCount,
		EmptySeq:   emptySeq,
		NonNumSeq:  nonNumSeq,
		EmptyChild: emptyChild,
		DivApplied: divApplied,
		DimFixes:   reps,
	}
	if opt.Basediv {
		rep.BaseRule = L["b_rule_div"]
	} else {
		rep.BaseRule = L["b_rule_raw"]
	}

	// 单位统计（按次数降序、同次数按名称升序）
	type kv struct {
		k string
		c int
	}
	a1 := make([]kv, 0, len(unitRawStats))
	for k, c := range unitRawStats {
		a1 = append(a1, kv{k, c})
	}
	sort.Slice(a1, func(i, j int) bool {
		if a1[i].c != a1[j].c {
			return a1[i].c > a1[j].c
		}
		return a1[i].k < a1[j].k
	})
	rep.UnitStats = make([]BomUnitStat, 0, len(a1))
	for _, e := range a1 {
		mu, st := bomMapUnit(um, e.k)
		// 配置里的单位别名优先于映射表：命中即取别名目标（如 PCSQ -> PCS）
		if k := strings.ToUpper(bomClean(e.k)); k != "" {
			if alias, ok := cfg.UnitAliases[k]; ok && strings.TrimSpace(alias) != "" {
				mu, st = alias, "ALIAS"
			}
		}
		rep.UnitStats = append(rep.UnitStats, BomUnitStat{Raw: e.k, Count: e.c, Mapped: mu, Status: st})
	}
	return rep, nil
}
