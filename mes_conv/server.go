package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
)

func ensureConfig() *Config {
	c, err := loadConfig()
	if err != nil {
		c = defaultConfig()
		_ = saveConfig(c)
	}
	return c
}

// logI18n 转换日志 + 预检提示 + 错误信息的四语文案（键为固定标识，值为带 %s/%d 的模板）
var logI18n = map[string]map[string]string{
	"zh": {
		"start":    "开始转换",
		"src":      "源文件：%s",
		"tpl":      "模板文件：%s",
		"conv":     "正在按映射规则转换…",
		"precheck": "预检：发现 %d 处需要关注的问题（见下方明细）",
		"wrote":    "已写入：%s",
		"rows":     "数据行数：%d",
		"type":     "物料类型分布：%s",
		"source":   "物料来源分布：%s",
		"done":     "转换完成 ✓",
		"elapsed":  "耗时：%d 毫秒",
		"failed":   "转换失败：%s",

		"chk_required":     "必填列为空",
		"chk_code_dup":     "与第 %d 行重复（%s）",
		"sum_code_dup":     "物料编码重复：%s（共 %d 行）",
		"sum_dict_unknown": "字典未匹配，已保留原值：%s（%d 行）",
		"sum_unit_unknown": "单位未识别，已原样保留：%s（%d 行）",
		"sum_not_in_list":  "值不在允许清单中：%s（%d 行）",

		"err_open_tpl":   "打开模板失败",
		"err_tpl_header": "读模板表头失败",
		"err_open_src":   "打开源文件失败",
		"err_no_data":    "源文件无数据行",
		"err_save":       "保存输出文件失败",
	},
	"zht": {
		"start":    "開始轉換",
		"src":      "來源檔案：%s",
		"tpl":      "範本檔案：%s",
		"conv":     "正在依對應規則轉換…",
		"precheck": "預檢：發現 %d 處需要關注的問題（見下方明細）",
		"wrote":    "已寫入：%s",
		"rows":     "資料筆數：%d",
		"type":     "物料類型分布：%s",
		"source":   "物料來源分布：%s",
		"done":     "轉換完成 ✓",
		"elapsed":  "耗時：%d 毫秒",
		"failed":   "轉換失敗：%s",

		"chk_required":     "必填欄為空",
		"chk_code_dup":     "與第 %d 行重複（%s）",
		"sum_code_dup":     "物料編號重複：%s（共 %d 行）",
		"sum_dict_unknown": "字典未匹配，已保留原值：%s（%d 行）",
		"sum_unit_unknown": "單位未識別，已原樣保留：%s（%d 行）",
		"sum_not_in_list":  "值不在允許清單中：%s（%d 行）",

		"err_open_tpl":   "開啟範本失敗",
		"err_tpl_header": "讀取範本表頭失敗",
		"err_open_src":   "開啟來源檔案失敗",
		"err_no_data":    "來源檔案無資料列",
		"err_save":       "儲存輸出檔案失敗",
	},
	"vi": {
		"start":    "Bắt đầu chuyển đổi",
		"src":      "Tệp nguồn: %s",
		"tpl":      "Tệp mẫu: %s",
		"conv":     "Đang chuyển đổi theo quy tắc ánh xạ…",
		"precheck": "Kiểm tra trước: phát hiện %d vấn đề cần lưu ý (xem chi tiết bên dưới)",
		"wrote":    "Đã ghi: %s",
		"rows":     "Số dòng dữ liệu: %d",
		"type":     "Phân bố loại vật liệu: %s",
		"source":   "Phân bố nguồn vật liệu: %s",
		"done":     "Chuyển đổi hoàn tất ✓",
		"elapsed":  "Thời gian: %d ms",
		"failed":   "Chuyển đổi thất bại: %s",

		"chk_required":     "Cột bắt buộc bị trống",
		"chk_code_dup":     "Trùng với dòng %d (%s)",
		"sum_code_dup":     "Mã vật liệu bị trùng: %s (%d dòng)",
		"sum_dict_unknown": "Không khớp từ điển, giữ nguyên giá trị: %s (%d dòng)",
		"sum_unit_unknown": "Không nhận diện đơn vị, giữ nguyên: %s (%d dòng)",
		"sum_not_in_list":  "Giá trị không có trong danh sách cho phép: %s (%d dòng)",

		"err_open_tpl":   "Mở tệp mẫu thất bại",
		"err_tpl_header": "Đọc tiêu đề tệp mẫu thất bại",
		"err_open_src":   "Mở tệp nguồn thất bại",
		"err_no_data":    "Tệp nguồn không có dòng dữ liệu",
		"err_save":       "Lưu tệp kết quả thất bại",
	},
	"en": {
		"start":    "Conversion started",
		"src":      "Source file: %s",
		"tpl":      "Template file: %s",
		"conv":     "Converting according to mapping rules…",
		"precheck": "Pre-check: %d item(s) need attention (see details below)",
		"wrote":    "Written to: %s",
		"rows":     "Data rows: %d",
		"type":     "Material type distribution: %s",
		"source":   "Material source distribution: %s",
		"done":     "Conversion complete ✓",
		"elapsed":  "Elapsed: %d ms",
		"failed":   "Conversion failed: %s",

		"chk_required":     "Required column is empty",
		"chk_code_dup":     "Duplicate of row %d (%s)",
		"sum_code_dup":     "Duplicate material code: %s (%d rows)",
		"sum_dict_unknown": "Dictionary miss, original value kept: %s (%d rows)",
		"sum_unit_unknown": "Unit not recognized, kept as-is: %s (%d rows)",
		"sum_not_in_list":  "Value not in allowed list: %s (%d rows)",

		"err_open_tpl":   "Failed to open template",
		"err_tpl_header": "Failed to read template header",
		"err_open_src":   "Failed to open source file",
		"err_no_data":    "No data rows in source file",
		"err_save":       "Failed to save output file",
	},
}

// msgs 取指定语言的文案表；语言未知时回退中文，保证任何情况都有文案可用。
func msgs(lang string) map[string]string {
	if m, ok := logI18n[lang]; ok {
		return m
	}
	return logI18n["zh"]
}

// formatDist 把分布 map 格式化为 "k: v, k: v"
func formatDist(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// ---------- 界面 ----------

func handlerIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// 界面禁止缓存：否则改了 webui.html 后浏览器仍显示旧版本，极易误判为「没生效」
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	// 磁盘优先（临时改界面免重编），缺失则回退内嵌版本（只发一个 exe 也能用）
	if b, err := os.ReadFile(filepath.Join(exeDir(), "webui.html")); err == nil && len(b) > 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	if len(embeddedHTML) == 0 {
		http.Error(w, "找不到 webui.html（请确保它与本程序在同一目录）", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(embeddedHTML)
}

// ---------- 配置 ----------

func handlerGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, ensureConfig())
}

func handlerSaveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var c Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	// 覆盖前先备份旧配置，改坏了可回滚
	backupName, _ := backupConfig()
	if err := saveConfig(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "backup": backupName})
}

func backupRoot() string { return filepath.Join(exeDir(), "backup") }

// backupConfig 把当前 mapping.json 复制到 backup/ 下（时间戳命名），只保留最近 20 份
func backupConfig() (string, error) {
	return backupFile(configPath(), backupRoot())
}

// backupFile 把 src 复制到 dir 下，文件名 = 原文件名 + 时间戳，只保留最近 20 份。
// 物料档案的 mapping.json 与订单模块的 orders.json 共用这套备份策略。
func backupFile(src, dir string) (string, error) {
	if !fileExists(src) {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(filepath.Base(src), ".json")
	name := stem + "_" + time.Now().Format("20060102_150405") + ".json"
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
		return "", err
	}
	pruneBackups(dir, 20)
	return name, nil
}

func pruneBackups(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 时间戳命名，字典序即时间序
	if len(names) <= keep {
		return
	}
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

func handlerConfigBackups(w http.ResponseWriter, r *http.Request) {
	dir := backupRoot()
	ents, _ := os.ReadDir(dir)
	type Item struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	out := []Item{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Item{Name: e.Name(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name }) // 新的在前
	writeJSON(w, map[string]interface{}{"ok": true, "items": out})
}

func handlerConfigRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	name := filepath.Base(r.URL.Query().Get("name")) // 防目录穿越
	if !strings.HasSuffix(name, ".json") {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "非法备份名"})
		return
	}
	src := filepath.Join(backupRoot(), name)
	b, err := os.ReadFile(src)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "备份不存在: " + err.Error()})
		return
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "备份内容不是合法配置: " + err.Error()})
		return
	}
	_, _ = backupConfig() // 还原前也备份当前，避免「还原」变成不可逆
	if err := saveConfig(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "config": c})
}

// ---------- 源文件表头（供界面下拉） ----------

// ---------- 值域表（字典 / 单位 / 允许值）导出与导入 ----------
//
// 目的：字典表、单位表的维护常常要交给「懂业务但不懂本工具」的人。
// 导出成 CSV（Excel 双击即可打开、编辑），改完再导回来，全程不用碰 JSON。
//
// 导入语义：**按类型整体替换**——CSV 里有多少条就覆盖成多少条。
// 所以必须先「导出」拿到全量再改。前端导入时会把当前配置一起发上来，
// 保证「表单里还没保存的其它改动」不会被这一步冲掉。

// valuesCSV 把配置里的字典表 / 单位表 / 允许值清单序列化为 CSV 文本（含 UTF-8 BOM）。
func valuesCSV(cfg *Config) string {
	var buf bytes.Buffer
	buf.WriteString("\ufeff") // BOM：让 Excel 正确识别 UTF-8
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"类型", "分组", "键", "值"})

	names := make([]string, 0, len(cfg.Dicts))
	for n := range cfg.Dicts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		keys := make([]string, 0, len(cfg.Dicts[n]))
		for k := range cfg.Dicts[n] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_ = cw.Write([]string{"字典", n, k, cfg.Dicts[n][k]})
		}
	}

	uks := make([]string, 0, len(cfg.UnitMap))
	for k := range cfg.UnitMap {
		uks = append(uks, k)
	}
	sort.Strings(uks)
	for _, k := range uks {
		_ = cw.Write([]string{"单位", "基本单位", k, cfg.UnitMap[k]})
	}

	cols := make([]string, 0, len(cfg.ValueWhitelist))
	for c := range cfg.ValueWhitelist {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	for _, c := range cols {
		for _, v := range cfg.ValueWhitelist[c] {
			_ = cw.Write([]string{"允许值", c, v, v})
		}
	}
	cw.Flush()
	return buf.String()
}

func handlerValuesExport(w http.ResponseWriter, r *http.Request) {
	cfg := ensureConfig()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="values.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, valuesCSV(cfg))
}

func handlerValuesImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Config json.RawMessage `json:"config"`
		CSV    string          `json:"csv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "请求解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(body.CSV) == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "CSV 内容为空"})
		return
	}
	// 基础配置：优先用前端传来的（保留表单里未保存的其它改动），否则读磁盘
	cfg := &Config{}
	if len(body.Config) > 0 {
		if err := json.Unmarshal(body.Config, cfg); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "配置解析失败: " + err.Error()})
			return
		}
	}
	if cfg.OutputSheet == "" || len(cfg.Fields) == 0 {
		cfg = ensureConfig()
	}

	rd := csv.NewReader(strings.NewReader(strings.TrimPrefix(body.CSV, "\ufeff")))
	rd.FieldsPerRecord = -1 // 容忍人工编辑造成的列数不一致
	rows, err := rd.ReadAll()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "CSV 解析失败: " + err.Error()})
		return
	}

	dicts := map[string]map[string]string{}
	units := map[string]string{}
	whitelist := map[string][]string{}
	seenWL := map[string]map[string]bool{}
	nDict, nUnit, nWL, skipped := 0, 0, 0, 0
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		kind := strings.TrimSpace(row[0])
		if i == 0 && kind == "类型" { // 表头
			continue
		}
		cell := func(j int) string {
			if j < len(row) {
				return strings.TrimSpace(row[j])
			}
			return ""
		}
		switch kind {
		case "字典":
			name, k, v := cell(1), cell(2), cell(3)
			if name == "" || k == "" {
				skipped++
				continue
			}
			if dicts[name] == nil {
				dicts[name] = map[string]string{}
			}
			dicts[name][k] = v
			nDict++
		case "单位":
			k, v := cell(2), cell(3)
			if k == "" {
				skipped++
				continue
			}
			units[k] = v
			nUnit++
		case "允许值":
			col, v := cell(1), cell(2)
			if col == "" || v == "" {
				skipped++
				continue
			}
			if seenWL[col] == nil {
				seenWL[col] = map[string]bool{}
			}
			if !seenWL[col][v] {
				seenWL[col][v] = true
				whitelist[col] = append(whitelist[col], v)
			}
			nWL++
		default:
			skipped++
		}
	}
	if nDict == 0 && nUnit == 0 && nWL == 0 {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "没有解析到有效行（第一列应为「字典」「单位」或「允许值」）"})
		return
	}
	cfg.Dicts = dicts
	cfg.UnitMap = units
	if len(whitelist) > 0 { // CSV 里没写允许值时保持原样，避免误清空
		cfg.ValueWhitelist = whitelist
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"config": cfg,
		"stats":  map[string]int{"dict": nDict, "unit": nUnit, "whitelist": nWL, "skipped": skipped},
	})
}

// ---------- 多套配置档 ----------
//
// 场景：同一台机器要对不同工厂 / 不同模板做转换，每次手改配置太麻烦。
// 把一套 mapping 存成「命名配置档」（configs/<名字>.json），用时一键载入。
//
// 语义：载入只把内容填回界面表单，**不直接落盘**；用户在表单里确认后再点「保存配置」。
// 当前生效的配置始终是 exe 同级的 mapping.json（CLI 与分享版都依赖它，保持不变）。

func profilesDir() string { return filepath.Join(exeDir(), "configs") }

// safeProfileName 只保留中英文、数字、-、_、空格，防止目录穿越与非法文件名。
func safeProfileName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff: // 允许中文
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	if rs := []rune(s); len(rs) > 40 {
		s = string(rs[:40])
	}
	return s
}

func handlerProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		dir := profilesDir()
		_ = os.MkdirAll(dir, 0755)
		ents, _ := os.ReadDir(dir)
		list := []map[string]interface{}{}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			list = append(list, map[string]interface{}{
				"name":  strings.TrimSuffix(e.Name(), ".json"),
				"size":  info.Size(),
				"mtime": info.ModTime().Format("2006-01-02 15:04:05"),
			})
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i]["name"].(string) < list[j]["name"].(string)
		})
		writeJSON(w, map[string]interface{}{"ok": true, "items": list, "dir": dir})

	case http.MethodPost:
		var body struct {
			Action string          `json:"action"` // save | load | delete
			Name   string          `json:"name"`
			Config json.RawMessage `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "请求解析失败: " + err.Error()})
			return
		}
		name := safeProfileName(body.Name)
		if name == "" {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "配置档名称不能为空（可用中英文、数字、-、_、空格）"})
			return
		}
		p := filepath.Join(profilesDir(), name+".json")
		switch body.Action {
		case "save":
			var c Config
			if err := json.Unmarshal(body.Config, &c); err != nil {
				writeJSON(w, map[string]interface{}{"ok": false, "error": "配置内容无效: " + err.Error()})
				return
			}
			if c.OutputSheet == "" || len(c.Fields) == 0 {
				writeJSON(w, map[string]interface{}{"ok": false, "error": "配置内容不完整（缺少输出工作表或字段）"})
				return
			}
			if err := os.MkdirAll(profilesDir(), 0755); err != nil {
				writeJSON(w, map[string]interface{}{"ok": false, "error": "创建配置档目录失败: " + err.Error()})
				return
			}
			b, _ := json.MarshalIndent(c, "", "  ")
			if err := os.WriteFile(p, b, 0644); err != nil {
				writeJSON(w, map[string]interface{}{"ok": false, "error": "写入失败: " + err.Error()})
				return
			}
			appLog("配置档已保存：%s", p)
			writeJSON(w, map[string]interface{}{"ok": true, "name": name})

		case "load":
			b, err := os.ReadFile(p)
			if err != nil {
				writeJSON(w, map[string]interface{}{"ok": false, "error": "配置档不存在: " + name})
				return
			}
			var c Config
			if err := json.Unmarshal(b, &c); err != nil {
				writeJSON(w, map[string]interface{}{"ok": false, "error": "配置档内容损坏: " + err.Error()})
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "name": name, "config": c})

		case "delete":
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, map[string]interface{}{"ok": true, "name": name})

		default:
			writeJSON(w, map[string]interface{}{"ok": false, "error": "未知操作: " + body.Action})
		}

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handlerSourceHeaders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "解析上传失败: " + err.Error()})
		return
	}
	f, _, err := r.FormFile("source")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "缺少源文件"})
		return
	}
	defer f.Close()

	dir, err := os.MkdirTemp("", "messrc-*")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "src.xlsx")
	if err := saveUpload(f, p); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	wb, err := excelize.OpenFile(p)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "打开源文件失败: " + err.Error()})
		return
	}
	defer wb.Close()
	sheets := wb.GetSheetList()
	if len(sheets) == 0 {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "源文件没有工作表"})
		return
	}
	sheetIdx := 0
	if s := r.FormValue("sheet"); s != "" {
		_, _ = fmt.Sscanf(s, "%d", &sheetIdx)
	}
	if sheetIdx < 0 || sheetIdx >= len(sheets) {
		sheetIdx = 0
	}
	sheet := sheets[sheetIdx]
	rows, err := wb.GetRows(sheet)
	if err != nil || len(rows) == 0 {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "读不到表头"})
		return
	}

	type Hdr struct {
		Code  string `json:"code"`
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	headers := []Hdr{}
	for _, h := range rows[0] {
		c := strings.TrimSpace(h)
		if c == "" {
			continue
		}
		n := mbFieldNames[c]
		label := c
		if n != "" {
			label = c + " " + n
		}
		headers = append(headers, Hdr{Code: c, Name: n, Label: label})
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"sheet":  sheet,
		"sheets": sheets,
		"count":  len(headers),
		"headers": headers,
	})
}

// ---------- 转换 ----------

func handlerConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "解析上传失败: " + err.Error()})
		return
	}
	srcF, srcH, err := r.FormFile("source")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "缺少源文件"})
		return
	}
	defer srcF.Close()
	tplF, tplH, err := r.FormFile("template")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "缺少模板文件"})
		return
	}
	defer tplF.Close()

	lang := r.FormValue("lang")
	if _, ok := logI18n[lang]; !ok {
		lang = "zh"
	}
	L := logI18n[lang]

	var cfg *Config
	if cs := r.FormValue("config"); cs != "" {
		var c Config
		if err := json.Unmarshal([]byte(cs), &c); err == nil && c.OutputSheet != "" && len(c.Fields) > 0 {
			cfg = &c
		}
	}
	if cfg == nil {
		cfg = ensureConfig()
	}

	dir, err := os.MkdirTemp("", "mesconv-*")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer os.RemoveAll(dir)
	srcPath := filepath.Join(dir, "src.xlsx")
	tplPath := filepath.Join(dir, "tpl.xlsx")
	if err := saveUpload(srcF, srcPath); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if err := saveUpload(tplF, tplPath); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	// 输出目录：exe 同级的 out/，文件名带时间戳且保证唯一
	outDir := filepath.Join(exeDir(), "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "创建输出目录失败: " + err.Error()})
		return
	}
	ts := time.Now().Format("20060102_150405")
	base := "MES物料档案_" + ts + ".xlsx"
	outPath := filepath.Join(outDir, base)
	n := 1
	for fileExists(outPath) {
		n++
		base = fmt.Sprintf("MES物料档案_%s_%d.xlsx", ts, n)
		outPath = filepath.Join(outDir, base)
	}
	logPath := filepath.Join(outDir, strings.TrimSuffix(base, ".xlsx")+".log")

	logs := []string{}
	add := func(s string) { logs = append(logs, time.Now().Format("2006-01-02 15:04:05")+"  "+s) }
	start := time.Now()
	add(L["start"])
	add(fmt.Sprintf(L["src"], srcH.Filename))
	add(fmt.Sprintf(L["tpl"], tplH.Filename))
	add(L["conv"])

	progressStart(srcH.Filename)
	rep, err := Convert(cfg, srcPath, tplPath, outPath, lang)
	progressEnd()
	if err != nil {
		add(fmt.Sprintf(L["failed"], err.Error()))
		_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")), 0644)
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "log": logs})
		return
	}
	add(fmt.Sprintf(L["wrote"], base))
	add(fmt.Sprintf(L["rows"], rep.Rows))
	add(fmt.Sprintf(L["type"], formatDist(rep.Types)))
	add(fmt.Sprintf(L["source"], formatDist(rep.Sources)))
	if rep.IssueTotal > 0 {
		add(fmt.Sprintf(L["precheck"], rep.IssueTotal))
	}
	for _, e := range rep.Errors {
		add("  ! " + e)
	}
	for _, x := range rep.Warnings {
		add("  * " + x)
	}
	add(L["done"])
	add(fmt.Sprintf(L["elapsed"], time.Since(start).Milliseconds()))
	_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")), 0644)

	// 落盘完整报告：页面刷新 / 服务重启后，仍能从「最近转换」里把这次的问题明细翻出来看
	elapsed := time.Since(start).Milliseconds()
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	reportID := strings.TrimSuffix(base, ".xlsx") + ".json"
	saveReport(&ConvertReport{
		ID:         reportID,
		Time:       nowStr,
		Source:     srcH.Filename,
		Template:   tplH.Filename,
		Output:     base,
		Rows:       rep.Rows,
		ElapsedMS:  elapsed,
		IssueTotal: rep.IssueTotal,
		Errors:     rep.Errors,
		Warnings:   rep.Warnings,
		Issues:     rep.Issues,
		Types:      rep.Types,
		Sources:    rep.Sources,
	})

	appendHistory(HistoryItem{
		Time:     nowStr,
		File:     base,
		Rows:     rep.Rows,
		Source:   srcH.Filename,
		Template: tplH.Filename,
		Issues:   rep.IssueTotal,
		Warnings: len(rep.Warnings),
		Report:   reportID,
	})

	issues, errs, warns := rep.Issues, rep.Errors, rep.Warnings
	if issues == nil {
		issues = []Issue{}
	}
	if errs == nil {
		errs = []string{}
	}
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, map[string]interface{}{
		"ok":          true,
		"rows":        rep.Rows,
		"file":        base,
		"report_id":   reportID,
		"log":         logs,
		"types":       rep.Types,
		"sources":     rep.Sources,
		"issues":      issues,
		"issue_total": rep.IssueTotal,
		"errors":      errs,
		"warnings":    warns,
		"elapsed":     elapsed,
		"outDir":      "out",
	})
}

func saveUpload(f io.ReadCloser, path string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, f)
	return err
}

// ---------- 转换历史 ----------

// HistoryItem 一次成功转换的记录
type HistoryItem struct {
	Time     string `json:"time"`
	File     string `json:"file"`
	Rows     int    `json:"rows"`
	Source   string `json:"source"`
	Template string `json:"template"`
	Issues   int    `json:"issues"`
	Warnings int    `json:"warnings"`
	Report   string `json:"report"` // 报告文件名（null/空表示那次没留下报告），用于事后查看问题明细
	Module   string `json:"module"` // 空/materials = 物料档案，orders = 订单工单
}

// noBrowser 由 -nobrowser 设置：启动时不自动打开浏览器（开机自启 / 托盘常驻场景）
var noBrowser bool

var histMu sync.Mutex

func historyPath() string { return filepath.Join(exeDir(), "out", "history.json") }

func loadHistory() []HistoryItem {
	b, err := os.ReadFile(historyPath())
	if err != nil {
		return nil
	}
	var h []HistoryItem
	if json.Unmarshal(b, &h) != nil {
		return nil
	}
	return h
}

func appendHistory(it HistoryItem) {
	histMu.Lock()
	defer histMu.Unlock()
	h := append([]HistoryItem{it}, loadHistory()...)
	if len(h) > 50 {
		h = h[:50]
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(historyPath()), 0755)
	_ = os.WriteFile(historyPath(), b, 0644)
}

func handlerHistory(w http.ResponseWriter, r *http.Request) {
	h := loadHistory()
	if h == nil {
		h = []HistoryItem{}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "items": h})
}

// handlerReport 返回某一次转换的完整报告（「最近转换」里点「查看问题」时调用）
func handlerReport(w http.ResponseWriter, r *http.Request) {
	rep, err := loadReport(r.URL.Query().Get("id"))
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "report": rep})
}

// ---------- 打开输出文件夹 ----------

func handlerOpenFolder(w http.ResponseWriter, r *http.Request) {
	dir, err := openOutFolder()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "dir": dir})
}

// openOutFolder 确保 out/ 目录存在，并用系统文件管理器打开它（界面按钮与托盘菜单共用）。
func openOutFolder() (string, error) {
	dir := filepath.Join(exeDir(), "out")
	if !fileExists(dir) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return dir, err
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	// explorer 的退出码不可靠（常常非 0），忽略
	_ = cmd.Start()
	return dir, nil
}

// ---------- 下载 ----------

func handlerDownload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("file")) // 防目录穿越
	ok := strings.HasSuffix(name, ".xlsx") || strings.HasSuffix(name, ".log")
	if !ok {
		http.NotFound(w, r)
		return
	}
	candidates := []string{
		filepath.Join(exeDir(), "out", name),
		filepath.Join(exeDir(), name),
	}
	var path string
	for _, p := range candidates {
		if fileExists(p) {
			path = p
			break
		}
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".xlsx") {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", name))
	http.ServeFile(w, r, path)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ---------- 本地服务日志 ----------

// appLogPath 服务运行日志路径：exe 同级 logs/server_YYYYMMDD.log（按天切分）
func appLogPath() string {
	dir := filepath.Join(exeDir(), "logs")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "server_"+time.Now().Format("20060102")+".log")
}

var appLogMu sync.Mutex

// appLog 同时输出到控制台与本地日志文件，保证「本地留痕」。
func appLog(format string, args ...interface{}) {
	line := time.Now().Format("2006-01-02 15:04:05") + "  " + fmt.Sprintf(format, args...)
	fmt.Println(line)
	appLogMu.Lock()
	defer appLogMu.Unlock()
	f, err := os.OpenFile(appLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// logMiddleware 记录 API 调用与下载请求；静态首页不记（噪声大）。
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/download" {
			appLog("%s %s (%d ms)", r.Method, r.URL.Path, time.Since(start).Milliseconds())
		}
	})
}

// ---------- 启动 ----------

// appTag 用于识别「本程序」的已运行实例，避免重复启动出第二个服务
const appTag = "mes-material-converter"

// 版本号与 buildTime 见 version.go（集中管理）

// currentPort 记录本进程实际监听端口，供「关于」弹窗展示。
var currentPort int

// probeExisting 探测某端口上是否已经有本程序在运行
func probeExisting(port int) bool {
	client := &http.Client{Timeout: 900 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var m map[string]interface{}
	if json.NewDecoder(resp.Body).Decode(&m) != nil {
		return false
	}
	v, _ := m["app"].(string)
	return v == appTag
}

// handlerProgress 返回当前转换进度快照（前端转换期间每 300ms 拉一次）。
func handlerProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, progressSnapshot())
}

// findRunningInstance 在 base..base+19 内查找已有本程序实例，返回其端口；找不到返回 0。
func findRunningInstance(base int) int {
	for p := base; p < base+20; p++ {
		if probeExisting(p) {
			return p
		}
	}
	return 0
}

func handlerPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "app": appTag, "version": appVersion})
}

// handlerAbout 返回运行环境信息，供界面「关于」弹窗展示（出问题时方便定位）。
func handlerAbout(w http.ResponseWriter, r *http.Request) {
	exe, _ := os.Executable()
	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"app":        appTag,
		"version":    appVersion,
		"build_time": buildTime,
		"exe_path":   exe,
		"config":     configPath(),
		"log":        appLogPath(),
		"out":        filepath.Join(exeDir(), "out"),
		"backup":     backupRoot(),
		"port":       currentPort,
		"addr":       fmt.Sprintf("http://127.0.0.1:%d/", currentPort),
		"runtime":    runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handlerIndex)
	mux.HandleFunc("/api/ping", handlerPing)
	mux.HandleFunc("/api/about", handlerAbout)
	mux.HandleFunc("/api/progress", handlerProgress)
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handlerSaveConfig(w, r)
		} else {
			handlerGetConfig(w, r)
		}
	})
	mux.HandleFunc("/api/config-backups", handlerConfigBackups)
	mux.HandleFunc("/api/config-restore", handlerConfigRestore)
	mux.HandleFunc("/api/source-headers", handlerSourceHeaders)
	mux.HandleFunc("/api/values-export", handlerValuesExport)
	mux.HandleFunc("/api/values-import", handlerValuesImport)
	mux.HandleFunc("/api/profiles", handlerProfiles)
	mux.HandleFunc("/api/history", handlerHistory)
	mux.HandleFunc("/api/report", handlerReport)
	mux.HandleFunc("/api/open-folder", handlerOpenFolder)
	mux.HandleFunc("/api/convert", handlerConvert)
	// ---- 订单 / 工单模块 ----
	mux.HandleFunc("/api/orders/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handlerOrdersSaveConfig(w, r)
		} else {
			handlerOrdersGetConfig(w, r)
		}
	})
	mux.HandleFunc("/api/orders/convert", handlerOrdersConvert)
	mux.HandleFunc("/api/orders/history", handlerOrdersHistory)
	mux.HandleFunc("/download", handlerDownload)
	return mux
}

// startServer 在指定端口启动服务。
// 服务跑在独立 goroutine 里（本函数立即返回），主 goroutine 留给系统托盘的消息循环。
// 返回 error 表示「这个端口没起来」（调用方据此顺延）。
// 关键：先 net.Listen 成功、之后再开浏览器——否则端口冲突时会先弹出一个打不开的页面。
func startServer(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	currentPort = port
	url := "http://" + addr + "/"
	appLog("物料档案转换工具 v%s 已启动：%s", appVersion, url)
	appLog("配置文件: %s", configPath())
	appLog("服务日志: %s", appLogPath())
	if !noBrowser {
		openBrowser(url)
	}

	go func() {
		_ = http.Serve(ln, logMiddleware(buildMux()))
	}()
	return nil
}

// serveAsync 找一个可用端口并在后台把服务跑起来。
// 返回 (端口, 是否由本进程提供服务)；第二个返回值为 false 表示
// 「默认端口上已有本程序实例，已打开它的界面，本进程应直接退出」——不会出现两个托盘图标。
func serveAsync(base int) (int, bool) {
	// 1) 默认端口上已有本程序 → 只打开它，绝不起第二个
	if probeExisting(base) {
		appLog("端口 %d 上已有本程序在运行，直接打开该界面（不重复启动服务）", base)
		if !noBrowser {
			openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", base))
		}
		return base, false
	}
	// 2) 依次尝试；每个端口先看是否已有本程序实例，再尝试绑定
	for p := base; p < base+20; p++ {
		if p != base && probeExisting(p) {
			appLog("端口 %d 上已有本程序在运行，直接打开该界面", p)
			if !noBrowser {
				openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", p))
			}
			return p, false
		}
		err := startServer(p)
		if err == nil {
			return p, true
		}
		appLog("端口 %d 不可用（%v），尝试下一个…", p, err)
	}
	appLog("连续 20 个端口都无法启动，请关闭其他程序后重试。")
	return base, false
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
