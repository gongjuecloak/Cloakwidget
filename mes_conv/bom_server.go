package main

// BOM 模块的四语文案与 HTTP 处理。
// 与订单模块同一策略：输出 Excel 里的表名/固定值（BOM主表、BOM明细、量产BOM、定额发料…）
// 是下游 MES 的数据标识，刻意保持中文不翻译；这里只翻译日志与提示。

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type bomMsg = map[string]string

// bomMsgs 取 BOM 模块文案；语言未知时回退中文。
func bomMsgs(lang string) map[string]string {
	mod, ok := bomI18n[lang]
	if !ok {
		lang, mod = "zh", bomI18n["zh"]
	}
	return mergeMsgs("bom:"+lang, mod, commonMsgs(lang))
}

var bomI18n = map[string]bomMsg{
	"zh": {
		"b_start":            "开始处理 BOM 数据…",
		"b_src":              "源文件：%s",
		"b_tpl":              "目标模板：%s",
		"b_map":              "单位映射表：%s",
		"b_conv":             "转换中…",
		"b_wrote":            "✅ 已生成输出文件：%s",
		"b_sum":              "BOM主表 %d / BOM明细 %d",
		"b_diag":             "诊断：空项次号 %d / 非数字项次号 %d / 空子件 %d",
		"b_rule":             "基础用量口径：%s（归一化 %d 行）",
		"b_units":            "源單位核对：%d 种",
		"b_elapsed":          "耗时 %d ms",
		"b_done":             "✅ 转换完成",
		"b_failed":           "❌ 转换失败：%s",
		"b_err_map":          "读取单位映射表失败",
		"b_err_src":          "打开源文件失败",
		"b_err_tpl":          "打开目标模板失败",
		"b_err_rows":         "读取源数据行失败",
		"b_err_cols":         "读取行失败",
		"b_err_iter":         "遍历源数据失败",
		"b_err_write_detail": "写入明细行失败",
		"b_err_write_master": "写入主表行失败",
		"b_err_save":         "保存输出文件失败",
		"b_err_dim":          "修复维度失败",
		"b_err_no_header":    "没有找到表头",
		"b_map_loaded":       "映射表载入：names %d / syms %d / ens %d",
		"b_progress":         "已写入 %d 行明细…",
		"b_rule_div":         "組成用量÷底數",
		"b_rule_raw":         "組成用量原值",
		"b_warn_col_fallback": "⚠ 角色 %s 的源列「%s」没找到，已回退默认列位",
	},
	"zht": {
		"b_start":            "開始處理 BOM 資料…",
		"b_src":              "源檔案：%s",
		"b_tpl":              "目標模板：%s",
		"b_map":              "單位映射表：%s",
		"b_conv":             "轉換中…",
		"b_wrote":            "✅ 已生成輸出檔案：%s",
		"b_sum":              "BOM主表 %d / BOM明細 %d",
		"b_diag":             "診斷：空項次號 %d / 非數字項次號 %d / 空子件 %d",
		"b_rule":             "基礎用量口徑：%s（歸一化 %d 行）",
		"b_units":            "源單位核對：%d 種",
		"b_elapsed":          "耗時 %d ms",
		"b_done":             "✅ 轉換完成",
		"b_failed":           "❌ 轉換失敗：%s",
		"b_err_map":          "讀取單位映射表失敗",
		"b_err_src":          "開啟源檔案失敗",
		"b_err_tpl":          "開啟目標模板失敗",
		"b_err_rows":         "讀取源資料行失敗",
		"b_err_cols":         "讀取行失敗",
		"b_err_iter":         "遍歷源資料失敗",
		"b_err_write_detail": "寫入明細行失敗",
		"b_err_write_master": "寫入主表行失敗",
		"b_err_save":         "儲存輸出檔案失敗",
		"b_err_dim":          "修復維度失敗",
		"b_err_no_header":    "沒有找到表頭",
		"b_map_loaded":       "映射表載入：names %d / syms %d / ens %d",
		"b_progress":         "已寫入 %d 行明細…",
		"b_rule_div":         "組成用量÷底數",
		"b_rule_raw":         "組成用量原值",
		"b_warn_col_fallback": "⚠ 角色 %s 的源欄「%s」沒找到，已回退預設欄位",
	},
	"vi": {
		"b_start":            "Bắt đầu xử lý dữ liệu BOM…",
		"b_src":              "Tệp nguồn: %s",
		"b_tpl":              "Mẫu đích: %s",
		"b_map":              "Bảng ánh xạ đơn vị: %s",
		"b_conv":             "Đang chuyển đổi…",
		"b_wrote":            "✅ Đã tạo tệp kết quả: %s",
		"b_sum":              "BOM chính %d / BOM chi tiết %d",
		"b_diag":             "Chẩn đoán: số thứ tự trống %d / số thứ tự không phải số %d / vật tư con trống %d",
		"b_rule":             "Quy tắc định mức: %s (chuẩn hóa %d dòng)",
		"b_units":            "Đối chiếu đơn vị nguồn: %d loại",
		"b_elapsed":          "Thời gian %d ms",
		"b_done":             "✅ Chuyển đổi hoàn tất",
		"b_failed":           "❌ Chuyển đổi thất bại: %s",
		"b_err_map":          "Đọc bảng ánh xạ đơn vị thất bại",
		"b_err_src":          "Mở tệp nguồn thất bại",
		"b_err_tpl":          "Mở mẫu đích thất bại",
		"b_err_rows":         "Đọc dữ liệu nguồn thất bại",
		"b_err_cols":         "Đọc hàng thất bại",
		"b_err_iter":         "Duyệt dữ liệu nguồn thất bại",
		"b_err_write_detail": "Ghi dòng chi tiết thất bại",
		"b_err_write_master": "Ghi dòng bảng chính thất bại",
		"b_err_save":         "Lưu tệp kết quả thất bại",
		"b_err_dim":          "Sửa chiều dữ liệu thất bại",
		"b_err_no_header":    "Không tìm thấy dòng tiêu đề",
		"b_map_loaded":       "Đã nạp bảng ánh xạ: names %d / syms %d / ens %d",
		"b_progress":         "Đã ghi %d dòng chi tiết…",
		"b_rule_div":         "組成用量÷底數",
		"b_rule_raw":         "Giá trị 組成用量 gốc",
		"b_warn_col_fallback": "⚠ Không tìm thấy cột nguồn「%s」của vai trò %s, đã dùng vị trí cột mặc định",
	},
	"en": {
		"b_start":            "Starting BOM conversion…",
		"b_src":              "Source: %s",
		"b_tpl":              "Template: %s",
		"b_map":              "Unit mapping: %s",
		"b_conv":             "Converting…",
		"b_wrote":            "✅ Output file created: %s",
		"b_sum":              "BOM master %d / BOM detail %d",
		"b_diag":             "Diagnostics: empty seq %d / non-numeric seq %d / empty child %d",
		"b_rule":             "Base qty rule: %s (normalized %d rows)",
		"b_units":            "Source unit check: %d distinct",
		"b_elapsed":          "Elapsed %d ms",
		"b_done":             "✅ Conversion finished",
		"b_failed":           "❌ Conversion failed: %s",
		"b_err_map":          "Failed to read unit mapping",
		"b_err_src":          "Failed to open source file",
		"b_err_tpl":          "Failed to open template",
		"b_err_rows":         "Failed to read source rows",
		"b_err_cols":         "Failed to read row",
		"b_err_iter":         "Failed to iterate source",
		"b_err_write_detail": "Failed to write detail row",
		"b_err_write_master": "Failed to write master row",
		"b_err_save":         "Failed to save output",
		"b_err_dim":          "Failed to fix dimensions",
		"b_err_no_header":    "Header row not found",
		"b_map_loaded":       "Mapping loaded: names %d / syms %d / ens %d",
		"b_progress":         "%d detail rows written…",
		"b_rule_div":         "usage ÷ base",
		"b_rule_raw":         "usage as-is",
		"b_warn_col_fallback": "⚠ Column「%s」for role %s not found, fell back to default position",
	},
}

// bomSaveOne 落盘同名表单字段下的第一个上传文件；字段缺失时返回空路径。
func bomSaveOne(r *http.Request, dir, field string) (string, string, error) {
	heads := r.MultipartForm.File[field]
	if len(heads) == 0 {
		return "", "", nil
	}
	path := filepath.Join(dir, field+".xlsx")
	f, err := heads[0].Open()
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if err := saveUpload(f, path); err != nil {
		return "", "", err
	}
	return path, heads[0].Filename, nil
}

// handlerBomConvert POST /api/bom/convert
// 表单：source（必填）、template（必填）、unitmap（可选）、basediv、lang
func handlerBomConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "解析上传失败: " + err.Error()})
		return
	}

	lang := r.FormValue("lang")
	L := bomMsgs(lang)

	dir, err := os.MkdirTemp("", "mesbom-*")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer os.RemoveAll(dir)

	srcPath, srcName, err := bomSaveOne(r, dir, "source")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["b_err_src"] + ": " + err.Error()})
		return
	}
	if srcPath == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["b_err_src"]})
		return
	}
	tplPath, tplName, err := bomSaveOne(r, dir, "template")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["b_err_tpl"] + ": " + err.Error()})
		return
	}
	if tplPath == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["b_err_tpl"]})
		return
	}
	mapPath, mapName, err := bomSaveOne(r, dir, "unitmap")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["b_err_map"] + ": " + err.Error()})
		return
	}

	// 配置：磁盘上的 bom_mapping.json（无则用内置默认并落盘）。
	// 工作表名、源列位、固定值、单位别名都从配置来。
	bc := ensureBomConfig()

	// basediv：表单带了就按表单，没带用配置默认
	basediv := bc.Basediv
	if v := r.FormValue("basediv"); v != "" {
		basediv = v == "1" || v == "true"
	}
	opt := BomOptions{
		Basediv:     basediv,
		SrcSheet:    bc.SrcSheet,
		MasterSheet: bc.MasterSheet,
		DetailSheet: bc.DetailSheet,
		Cfg:         bc,
	}

	// 输出目录：exe 同级的 out/，文件名带时间戳且保证唯一
	outDir := filepath.Join(exeDir(), "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "创建输出目录失败: " + err.Error()})
		return
	}
	ts := time.Now().Format("20060102_150405")
	base := "MESBOM_" + ts + ".xlsx"
	outPath := filepath.Join(outDir, base)
	n := 1
	for fileExists(outPath) {
		n++
		base = fmt.Sprintf("MESBOM_%s_%d.xlsx", ts, n)
		outPath = filepath.Join(outDir, base)
	}
	logPath := filepath.Join(outDir, strings.TrimSuffix(base, ".xlsx")+".log")

	logs := []string{}
	add := func(s string) { logs = append(logs, time.Now().Format("2006-01-02 15:04:05")+"  "+s) }
	start := time.Now()
	add(L["b_start"])
	add(fmt.Sprintf(L["b_src"], srcName))
	add(fmt.Sprintf(L["b_tpl"], tplName))
	if mapPath != "" {
		add(fmt.Sprintf(L["b_map"], mapName))
	} else {
		add(strings.TrimSuffix(L["b_map"], "%s") + "-")
	}
	add(L["b_conv"])

	progressStart(srcName)
	rep, err := ConvertBOM(srcPath, tplPath, mapPath, outPath, opt, L, func(s string) { add(s) })
	progressEnd()

	if err != nil {
		add(fmt.Sprintf(L["b_failed"], err.Error()))
		_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")), 0644)
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "log": logs})
		return
	}
	add(fmt.Sprintf(L["b_wrote"], base))
	add(fmt.Sprintf(L["b_sum"], rep.MasterRows, rep.DetailRows))
	add(fmt.Sprintf(L["b_diag"], rep.EmptySeq, rep.NonNumSeq, rep.EmptyChild))
	add(fmt.Sprintf(L["b_rule"], rep.BaseRule, rep.DivApplied))
	if len(rep.UnitStats) > 0 {
		add(fmt.Sprintf(L["b_units"], len(rep.UnitStats)))
	}
	add(L["b_done"])
	add(fmt.Sprintf(L["b_elapsed"], time.Since(start).Milliseconds()))
	_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")), 0644)

	// 落盘完整报告（与物料 / 订单模块共用「最近转换」的报表存储）
	elapsed := time.Since(start).Milliseconds()
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	reportID := strings.TrimSuffix(base, ".xlsx") + ".json"
	saveReport(&ConvertReport{
		ID:        reportID,
		Time:      nowStr,
		Source:    srcName,
		Template:  tplName,
		Output:    base,
		Rows:      rep.MasterRows + rep.DetailRows,
		ElapsedMS: elapsed,
		Module:    "bom",
		Types: map[string]int{
			"BOM主表": rep.MasterRows,
			"BOM明细": rep.DetailRows,
		},
	})

	if rep.UnitStats == nil {
		rep.UnitStats = []BomUnitStat{}
	}
	if rep.DimFixes == nil {
		rep.DimFixes = []string{}
	}
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"rows":            rep.MasterRows + rep.DetailRows,
		"master":          rep.MasterRows,
		"detail":          rep.DetailRows,
		"file":            base,
		"report_id":       reportID,
		"base_rule":       rep.BaseRule,
		"empty_seq":       rep.EmptySeq,
		"non_numeric_seq": rep.NonNumSeq,
		"empty_child":     rep.EmptyChild,
		"div_applied":     rep.DivApplied,
		"unit_stats":      rep.UnitStats,
		"dim_fixes":       rep.DimFixes,
		"log":             logs,
		"elapsed":         elapsed,
	})
}
