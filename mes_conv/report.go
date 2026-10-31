package main

// 转换报告落盘。
//
// 背景：预检问题原先只在转换响应里返回，页面一刷新或服务一重启就没了——
// 用户回头想问「上次到底哪几行有问题」时查不到。
// 现在每次转换把完整报告写一份到 out/reports/<输出文件名>.json，
// 「最近转换」列表里可以随时把任何一次的问题明细翻出来看。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConvertReport 一次转换的完整记录（统计 + 预检明细 + 汇总）
type ConvertReport struct {
	ID           string         `json:"id"`       // 报告文件名（不含目录），如 MES物料档案_20261007_112514.json
	Time         string         `json:"time"`     // 转换时间
	Module       string         `json:"module"`   // 所属模块：空/materials = 物料档案，orders = 订单工单，bom = BOM
	Source       string         `json:"source"`   // 源文件名
	Template     string         `json:"template"` // 模板文件名
	Output       string         `json:"output"`   // 输出文件名
	Rows         int            `json:"rows"`
	ElapsedMS    int64          `json:"elapsed_ms"`
	IssueTotal   int            `json:"issue_total"`
	WarningTotal int            `json:"warning_total,omitempty"`
	Errors       []string       `json:"errors"`
	Warnings     []string       `json:"warnings"`
	Issues       []Issue        `json:"issues"`
	Types        map[string]int `json:"types"`
	Sources      map[string]int `json:"sources"`
}

func reportsDir() string { return filepath.Join(exeDir(), "out", "reports") }

// saveReport 把报告写到 out/reports/，返回报告文件名；失败只记日志，绝不影响转换本身。
func saveReport(r *ConvertReport) string {
	dir := reportsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		appLog("报告保存失败（建目录）：%v", err)
		return ""
	}
	if r.Errors == nil {
		r.Errors = []string{}
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	if r.Issues == nil {
		r.Issues = []Issue{}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		appLog("报告保存失败（序列化）：%v", err)
		return ""
	}
	if err := os.WriteFile(filepath.Join(dir, r.ID), b, 0644); err != nil {
		appLog("报告保存失败（写文件）：%v", err)
		return ""
	}
	pruneReports(100)
	return r.ID
}

// pruneReports 只保留最近 keep 份报告（文件名含时间戳，字典序即时间序）
func pruneReports(keep int) {
	ents, err := os.ReadDir(reportsDir())
	if err != nil {
		return
	}
	type reportFile struct {
		name string
		mod  int64
	}
	files := make([]reportFile, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			info, err := e.Info()
			if err == nil {
				files = append(files, reportFile{name: e.Name(), mod: info.ModTime().UnixNano()})
			}
		}
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mod == files[j].mod {
			return files[i].name < files[j].name
		}
		return files[i].mod < files[j].mod
	})
	for _, f := range files[:len(files)-keep] {
		_ = os.Remove(filepath.Join(reportsDir(), f.name))
	}
}

// loadReport 读一份报告。id 只取文件名部分，避免目录穿越。
func loadReport(id string) (*ConvertReport, error) {
	name := filepath.Base(strings.TrimSpace(id))
	if name == "" || name == "." || !strings.HasSuffix(name, ".json") {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(reportsDir(), name))
	if err != nil {
		return nil, err
	}
	var r ConvertReport
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func restoreLegacyBomWarning(r *ConvertReport, history []HistoryItem) {
	if r.Module != "bom" || r.WarningTotal > 0 || len(r.Warnings) > 0 {
		return
	}
	for _, it := range history {
		if it.Report == r.ID && it.Warnings > 0 {
			r.WarningTotal = it.Warnings
			r.Warnings = []string{fmt.Sprintf("旧版记录只保存了 %d 行单位告警，未保存具体单位明细；请重新转换以查看详情", it.Warnings)}
			return
		}
	}
}

// historyFromReports 让新增的模块能从既有报告恢复历史，不因旧版本漏写 history 而丢记录。
func historyFromReports(dir, module string) []HistoryItem {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	items := make([]HistoryItem, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r ConvertReport
		if json.Unmarshal(b, &r) != nil || r.Module != module {
			continue
		}
		report := r.ID
		if report == "" {
			report = e.Name()
		}
		warningTotal := r.WarningTotal
		if warningTotal == 0 {
			warningTotal = len(r.Warnings)
		}
		items = append(items, HistoryItem{
			Time: r.Time, File: r.Output, Rows: r.Rows, Source: r.Source, Template: r.Template,
			Issues: r.IssueTotal, Warnings: warningTotal, Report: report, Module: r.Module,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	return items
}

func mergeHistories(groups ...[]HistoryItem) []HistoryItem {
	seen := map[string]bool{}
	var out []HistoryItem
	for _, group := range groups {
		for _, it := range group {
			key := it.Report
			if key == "" {
				key = it.Module + "\x00" + it.Time + "\x00" + it.File
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out
}
