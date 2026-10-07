package main

// ============================================================================
// 转换历史统计
//
// 「最近转换」列表能回答「上一次怎么样」，回答不了「这一阵子怎么样」：
// 一共转了多少次、踩过多少问题、告警集中在哪天。面板要的数字在这里汇总。
//
// 数据源就是两本 history（物料 + 订单），不额外落盘任何东西。
// ============================================================================

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
)

type statsDay struct {
	Day  string `json:"day"`
	Runs int    `json:"runs"`
	Rows int    `json:"rows"`
}

type statsResp struct {
	Runs       int           `json:"runs"`        // 总转换次数
	Rows       int           `json:"rows"`        // 累计输出行数
	Clean      int           `json:"clean"`       // 零问题零告警的次数
	WithIssues int           `json:"with_issues"` // 至少有一个问题的次数
	IssueTotal int           `json:"issue_total"` // 问题总数（含重复计数）
	WarnTotal  int           `json:"warn_total"`  // 告警总数
	Materials  int           `json:"materials"`   // 物料档案次数
	Orders     int           `json:"orders"`      // 订单工单次数
	ByDay      []statsDay    `json:"by_day"`
	Recent     []HistoryItem `json:"recent"`
}

func allHistory() []HistoryItem {
	out := append([]HistoryItem{}, loadHistory()...)
	if b, err := os.ReadFile(ordersHistoryPath()); err == nil {
		var h []HistoryItem
		if json.Unmarshal(b, &h) == nil {
			out = append(out, h...)
		}
	}
	return out
}

func computeStats() statsResp {
	var s statsResp
	items := allHistory()
	byDay := map[string]*statsDay{}

	for _, it := range items {
		s.Runs++
		s.Rows += it.Rows
		s.IssueTotal += it.Issues
		s.WarnTotal += it.Warnings
		if it.Issues == 0 && it.Warnings == 0 {
			s.Clean++
		} else {
			s.WithIssues++
		}
		if it.Module == "orders" {
			s.Orders++
		} else {
			s.Materials++
		}
		if len(it.Time) >= 10 {
			d := it.Time[:10]
			x, ok := byDay[d]
			if !ok {
				x = &statsDay{Day: d}
				byDay[d] = x
			}
			x.Runs++
			x.Rows += it.Rows
		}
	}

	for _, d := range byDay {
		s.ByDay = append(s.ByDay, *d)
	}
	sort.Slice(s.ByDay, func(i, j int) bool { return s.ByDay[i].Day < s.ByDay[j].Day })
	if len(s.ByDay) > 14 { // 只留最近 14 个有记录的日子
		s.ByDay = s.ByDay[len(s.ByDay)-14:]
	}

	// 最近 10 条（两本 history 混在一起按时间倒序）
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	n := len(items)
	if n > 10 {
		n = 10
	}
	s.Recent = items[:n]
	if s.Recent == nil {
		s.Recent = []HistoryItem{}
	}
	if s.ByDay == nil {
		s.ByDay = []statsDay{}
	}
	return s
}

// handlerStats 转换历史统计面板的数据
func handlerStats(w http.ResponseWriter, r *http.Request) {
	s := computeStats()
	writeJSON(w, map[string]interface{}{
		"ok":          true,
		"runs":        s.Runs,
		"rows":        s.Rows,
		"clean":       s.Clean,
		"with_issues": s.WithIssues,
		"issue_total": s.IssueTotal,
		"warn_total":  s.WarnTotal,
		"materials":   s.Materials,
		"orders":      s.Orders,
		"by_day":      s.ByDay,
		"recent":      s.Recent,
	})
}
