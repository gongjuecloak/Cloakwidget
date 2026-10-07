package main

// 订单 / 工单模块的 HTTP 接口。
//
// 与物料档案模块共用同一套基础设施（托盘、日志、进度、报告、多语言），
// 但配置、历史、输出命名各自独立，互不干扰：
//   物料档案  mapping.json  →  MES物料档案_<时间戳>.xlsx
//   订单工单  orders.json   →  订单工单_<时间戳>.xlsx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------- 配置 ----------

func handlerOrdersGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "config": ensureOrdersConfig()})
}

func handlerOrdersSaveConfig(w http.ResponseWriter, r *http.Request) {
	var c OrdersConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "配置格式错误：" + err.Error()})
		return
	}
	if len(c.FieldMapping) == 0 {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "配置里没有字段映射，拒绝保存"})
		return
	}
	c.fillDefaults()
	if fileExists(ordersConfigPath()) {
		_, _ = backupFile(ordersConfigPath(), backupRoot())
	}
	if err := saveOrdersConfig(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	appLog("订单模块配置已保存：%s", ordersConfigPath())
	writeJSON(w, map[string]interface{}{"ok": true})
}

// ---------- 转换 ----------

func handlerOrdersConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "解析上传失败: " + err.Error()})
		return
	}

	lang := r.FormValue("lang")
	if _, ok := ordersI18n[lang]; !ok {
		lang = "zh"
	}
	L := ordersMsgs(lang)

	// 订单文件与工单文件至少给一个
	dir, err := os.MkdirTemp("", "mesorders-*")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer os.RemoveAll(dir)

	orderPath, orderName := "", ""
	if f, h, err := r.FormFile("order"); err == nil {
		defer f.Close()
		orderPath = filepath.Join(dir, "order.xlsx")
		if err := saveUpload(f, orderPath); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		orderName = h.Filename
	}
	workPath, workName := "", ""
	if f, h, err := r.FormFile("work"); err == nil {
		defer f.Close()
		workPath = filepath.Join(dir, "work.xlsx")
		if err := saveUpload(f, workPath); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		workName = h.Filename
	}
	if orderPath == "" && workPath == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["e_no_file"]})
		return
	}

	var cfg *OrdersConfig
	if cs := r.FormValue("config"); cs != "" {
		var c OrdersConfig
		if err := json.Unmarshal([]byte(cs), &c); err == nil && len(c.FieldMapping) > 0 {
			c.fillDefaults()
			cfg = &c
		}
	}
	if cfg == nil {
		cfg = ensureOrdersConfig()
	}

	// 输出：out/订单工单_<时间戳>.xlsx，同秒冲突则加序号
	outDir := filepath.Join(exeDir(), "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "创建输出目录失败: " + err.Error()})
		return
	}
	ts := time.Now().Format("20060102_150405")
	base := "订单工单_" + ts + ".xlsx"
	outPath := filepath.Join(outDir, base)
	for n := 2; fileExists(outPath); n++ {
		base = fmt.Sprintf("订单工单_%s_%d.xlsx", ts, n)
		outPath = filepath.Join(outDir, base)
	}
	logPath := filepath.Join(outDir, strings.TrimSuffix(base, ".xlsx")+".log")

	logs := []string{}
	add := func(s string) { logs = append(logs, time.Now().Format("2006-01-02 15:04:05")+"  "+s) }
	start := time.Now()

	// 引擎自己的日志回调：既进界面日志，也进进度条的文件名提示
	logName := orderName
	if logName == "" {
		logName = workName
	}
	progressStart(logName)
	rep, err := ConvertOrders(cfg, orderPath, workPath, outPath, lang, func(s string) { add(s) })
	progressEnd()

	if err != nil {
		add("✗ " + err.Error())
		_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")), 0644)
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "log": logs})
		return
	}
	add(fmt.Sprintf(L["o_wrote"], base))
	add(fmt.Sprintf(L["o_sum"], rep.OrderMaster, rep.OrderDetail, rep.WorkOrders))
	add(fmt.Sprintf(L["o_elapsed"], time.Since(start).Milliseconds()))
	_ = os.WriteFile(logPath, []byte(strings.Join(logs, "\n")), 0644)

	elapsed := time.Since(start).Milliseconds()
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	reportID := strings.TrimSuffix(base, ".xlsx") + ".json"
	srcLabel := orderName
	if workName != "" {
		if srcLabel != "" {
			srcLabel += " + " + workName
		} else {
			srcLabel = workName
		}
	}
	saveReport(&ConvertReport{
		ID:         reportID,
		Time:       nowStr,
		Source:     srcLabel,
		Template:   L["label_work"],
		Output:     base,
		Rows:       rep.OrderMaster + rep.OrderDetail + rep.WorkOrders,
		ElapsedMS:  elapsed,
		IssueTotal: rep.IssueTotal,
		Errors:     rep.Errors,
		Warnings:   rep.Warnings,
		Issues:     rep.Issues,
		Types: map[string]int{
			L["sheet_master"]: rep.OrderMaster,
			L["sheet_detail"]: rep.OrderDetail,
			L["sheet_work"]:   rep.WorkOrders,
		},
		Module: "orders",
	})

	appendOrdersHistory(HistoryItem{
		Time:     nowStr,
		File:     base,
		Rows:     rep.OrderMaster + rep.OrderDetail + rep.WorkOrders,
		Source:   srcLabel,
		Template: L["label_work"],
		Issues:   rep.IssueTotal,
		Warnings: len(rep.Warnings),
		Report:   reportID,
		Module:   "orders",
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
		"rows":        rep.OrderMaster + rep.OrderDetail + rep.WorkOrders,
		"file":        base,
		"report_id":   reportID,
		"log":         logs,
		"types":       map[string]int{L["sheet_master"]: rep.OrderMaster, L["sheet_detail"]: rep.OrderDetail, L["sheet_work"]: rep.WorkOrders},
		"sources":     rep.Filtered,
		"line_match":  rep.LineMatch,
		"issues":      issues,
		"issue_total": rep.IssueTotal,
		"errors":      errs,
		"warnings":    warns,
		"elapsed":     elapsed,
		"outDir":      "out",
	})
}

// ---------- 订单模块专属历史 ----------

// orders historyPath 与物料档案分开存，两个模块的「最近转换」互不污染
func ordersHistoryPath() string { return filepath.Join(exeDir(), "out", "orders_history.json") }

func appendOrdersHistory(it HistoryItem) {
	histMu.Lock()
	defer histMu.Unlock()
	var old []HistoryItem
	if b, err := os.ReadFile(ordersHistoryPath()); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	h := append([]HistoryItem{it}, old...)
	if len(h) > 50 {
		h = h[:50]
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(ordersHistoryPath()), 0755)
	_ = os.WriteFile(ordersHistoryPath(), b, 0644)
}

func handlerOrdersHistory(w http.ResponseWriter, r *http.Request) {
	var h []HistoryItem
	if b, err := os.ReadFile(ordersHistoryPath()); err == nil {
		_ = json.Unmarshal(b, &h)
	}
	if h == nil {
		h = []HistoryItem{}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "items": h})
}
