package main

// ============================================================================
// 模板列变化检测
//
// 背景：MES 那边的导入模板不是一成不变的 —— 版本升级、有人手工加了一列、
// 列序被拖过。这些变化以前是「无声的」：转换照样成功，只是新列整列空着，
// 或者配置里映射的列被删了却没人知道。
//
// 做法：第一次转换时把模板表头记为「基准」存到 exe 同目录的 tpl_baseline.json；
// 之后每次转换都跟基准比一比，新增/缺失/顺序变化都报出来。
// 变更了但确实是有意为之（MES 升级模板）时，清除基准即可重新记录。
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tplBaseline 模板表头基准
type tplBaseline struct {
	Sheet   string   `json:"sheet"`    // 记录时的工作表名
	Headers []string `json:"headers"`  // 干净列名（去掉 "* " 前缀），已剔除空列
	SavedAt string   `json:"saved_at"` // 人可读的记录时间
}

func tplBaselinePath() string {
	return filepath.Join(exeDir(), "tpl_baseline.json")
}

// loadTplBaseline 读基准；没有或损坏都返回 nil（调用方按「首次」处理）
func loadTplBaseline() *tplBaseline {
	b, err := os.ReadFile(tplBaselinePath())
	if err != nil || len(b) == 0 {
		return nil
	}
	var t tplBaseline
	if json.Unmarshal(b, &t) != nil || len(t.Headers) == 0 {
		return nil
	}
	return &t
}

func saveTplBaseline(t *tplBaseline) error {
	t.SavedAt = time.Now().Format("2006-01-02 15:04:05")
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tplBaselinePath(), b, 0o644)
}

// clearTplBaseline 删掉基准文件；不存在也算成功
func clearTplBaseline() error {
	err := os.Remove(tplBaselinePath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// tplDiff 表头差异
type tplDiff struct {
	CountFrom int
	CountTo   int
	New       []string // 基准没有、当前有
	Missing   []string // 基准有、当前没有
	Moved     []string // 纯顺序变化（只有集合相同时才计算，避免「中间插一列」刷屏）
}

func (d tplDiff) changed() bool {
	return len(d.New) > 0 || len(d.Missing) > 0 || len(d.Moved) > 0
}

// diffTpl 比对两组表头（都应是干净列名）
func diffTpl(base, cur []string) tplDiff {
	d := tplDiff{CountFrom: len(base), CountTo: len(cur)}
	bm := make(map[string]int, len(base))
	for i, c := range base {
		bm[c] = i
	}
	cm := make(map[string]int, len(cur))
	for i, c := range cur {
		cm[c] = i
	}
	for _, c := range cur {
		if _, ok := bm[c]; !ok {
			d.New = append(d.New, c)
		}
	}
	for _, c := range base {
		if _, ok := cm[c]; !ok {
			d.Missing = append(d.Missing, c)
		}
	}
	// 只有列集合完全一致时才谈「顺序变化」：否则新增一列会把后面所有列都算成移位，
	// 报出来全是噪声，反而盖住真正有用的「新增列」信息。
	if len(d.New) == 0 && len(d.Missing) == 0 {
		for i, c := range cur {
			if j, ok := bm[c]; ok && j != i {
				d.Moved = append(d.Moved, fmt.Sprintf("%s(%d→%d)", c, j+1, i+1))
				if len(d.Moved) >= 5 {
					break
				}
			}
		}
	}
	return d
}

// cleanCols 从原始表头行提取干净列名（去 "* " 前缀、去空白、丢弃空列）
func cleanCols(headers []string) []string {
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		if c := cleanHeader(h); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// checkTemplate 与基准比对模板表头，返回要写进 notes / warnings 的文案。
// 首次运行记下基准并给一条中性说明；之后一致给中性说明，不一致给告警。
func checkTemplate(sheetName string, theaders []string, cfg *Config, L map[string]string) (notes, warns []string) {
	cur := cleanCols(theaders)
	if len(cur) == 0 {
		return nil, nil
	}
	base := loadTplBaseline()
	if base == nil {
		if err := saveTplBaseline(&tplBaseline{Sheet: sheetName, Headers: cur}); err == nil {
			notes = append(notes, fmt.Sprintf(L["note_tpl_first"], len(cur)))
		}
		return notes, nil
	}
	d := diffTpl(base.Headers, cur)
	if !d.changed() {
		notes = append(notes, fmt.Sprintf(L["note_tpl_ok"], len(cur)))
		return notes, nil
	}

	mapped := make(map[string]bool, len(cfg.Fields))
	for _, f := range cfg.Fields {
		mapped[f.Target] = true
	}
	if d.CountFrom != d.CountTo {
		warns = append(warns, fmt.Sprintf(L["warn_tpl_count"], d.CountFrom, d.CountTo))
	}
	// 新增列里只有「配置还没映射」的才值得提醒 —— 已映射的说明配置早就跟着改了
	for _, c := range d.New {
		if !mapped[c] {
			warns = append(warns, fmt.Sprintf(L["warn_tpl_new"], c))
		}
	}
	// 缺失列里只有「配置仍在映射」的才危险 —— 映射到不存在的列 = 整列空着出去
	for _, c := range d.Missing {
		if mapped[c] {
			warns = append(warns, fmt.Sprintf(L["warn_tpl_missing"], c))
		}
	}
	if len(d.Moved) > 0 {
		warns = append(warns, fmt.Sprintf(L["warn_tpl_order"], strings.Join(d.Moved, "、")))
	}
	return notes, warns
}

// tplStatus 给界面用的基准状态
type tplStatus struct {
	Has    bool   `json:"has"`
	Sheet  string `json:"sheet"`
	Count  int    `json:"count"`
	Saved  string `json:"saved_at"`
	File   string `json:"file"`
	Sample string `json:"sample"` // 前 6 列，方便肉眼确认是不是同一个模板
}

func currentTplStatus() tplStatus {
	b := loadTplBaseline()
	st := tplStatus{File: filepath.Base(tplBaselinePath())}
	if b == nil {
		return st
	}
	st.Has = true
	st.Sheet = b.Sheet
	st.Count = len(b.Headers)
	st.Saved = b.SavedAt
	n := len(b.Headers)
	if n > 6 {
		n = 6
	}
	st.Sample = strings.Join(b.Headers[:n], " / ")
	return st
}
