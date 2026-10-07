package main

// ============================================================================
// 数据质量检查
//
// 两件在真实数据里反复出现、但以前不会被告知的事：
//   1) 关键列重复 —— 同一物料/同一订单行出现两次，导进 MES 会冲突或覆盖
//   2) Excel 错误值 —— 源档里有人用「=」开头写备注、引用被删的单元格……
//      单元格显示 #NAME? / #REF!，这些值会原样被搬进 MES 导入表
//
// 两者都只「告警」不阻断：数据怎么处理由业务判断，工具只负责让人看见。
// ============================================================================

import (
	"fmt"
	"regexp"
	"strings"
)

// excelErrRe Excel 错误值。带不带问号都算（#N/A 与 #N/A? 都见过）。
var excelErrRe = regexp.MustCompile(`^#(NAME|REF|VALUE|DIV/0|N/A|NULL|NUM|SPILL|CALC|GETTING_DATA)\??$`)

// excelErrValue 判断一个单元格是不是 Excel 错误值
func excelErrValue(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return false
	}
	return excelErrRe.MatchString(s)
}

// dupTracker 按一组关键列统计重复。key 由多个列拼接而成（单列就是只给一列）。
//
// 用法：逐行调用 add()，返回这条是不是重复、以及首次出现在第几行。
type dupTracker struct {
	cols  []string
	seen  map[string]int // 键 -> 首次出现的行号
	count map[string]int // 键 -> 出现次数
	max   int            // 最多报几组，避免刷屏
}

func newDupTracker(cols []string) *dupTracker {
	clean := make([]string, 0, len(cols))
	for _, c := range cols {
		if strings.TrimSpace(c) != "" {
			clean = append(clean, c)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return &dupTracker{cols: clean, seen: map[string]int{}, count: map[string]int{}, max: 50}
}

// add 记一行。get 用来按列名取值（会做 TrimSpace）。
// 返回：是否重复、键的展示值、首次出现的行号。
func (d *dupTracker) add(rowNo int, get func(string) string) (bool, string, int) {
	if d == nil {
		return false, "", 0
	}
	vals := make([]string, 0, len(d.cols))
	for _, c := range d.cols {
		vals = append(vals, get(c))
	}
	return d.addValues(rowNo, vals...)
}

// addValues 按列序直接给值（物料档案那边是按下标取，走这个入口更直接）
func (d *dupTracker) addValues(rowNo int, vals ...string) (bool, string, int) {
	if d == nil {
		return false, "", 0
	}
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			return false, "", 0 // 关键列为空的行不算重复（另有必填检查负责）
		}
		parts = append(parts, v)
	}
	key := strings.Join(parts, "\x1f")
	if first, ok := d.seen[key]; ok {
		d.count[key]++
		return true, strings.Join(parts, " / "), first
	}
	d.seen[key] = rowNo
	d.count[key] = 1
	return false, "", 0
}

// dupItem 一组重复
type dupItem struct {
	Key string // 展示用的键（多列用 " / " 连接）
	N   int    // 出现次数
}

// dupItems 返回所有重复组（次数多的在前，同次数按键排序保证输出稳定）
func (d *dupTracker) dupItems() []dupItem {
	if d == nil {
		return nil
	}
	var all []dupItem
	for k, n := range d.count {
		if n > 1 {
			all = append(all, dupItem{Key: strings.ReplaceAll(k, "\x1f", " / "), N: n})
		}
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && (all[j].N > all[j-1].N ||
			(all[j].N == all[j-1].N && all[j].Key < all[j-1].Key)); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	if len(all) > d.max {
		all = all[:d.max]
	}
	return all
}

// summary 只返回前几组的可读文本（日志里用）
func (d *dupTracker) summary() []string {
	items := d.dupItems()
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s ×%d", it.Key, it.N))
	}
	return out
}

// dupGroupCount 重复组数（用于「发现 N 组重复」这类汇总）
func (d *dupTracker) dupGroupCount() int {
	if d == nil {
		return 0
	}
	n := 0
	for _, c := range d.count {
		if c > 1 {
			n++
		}
	}
	return n
}

// dupRowCount 处于重复组里的总行数
func (d *dupTracker) dupRowCount() int {
	if d == nil {
		return 0
	}
	n := 0
	for _, c := range d.count {
		if c > 1 {
			n += c
		}
	}
	return n
}

// keyLabel 把关键列名拼成可读的标签，如「订单号 + 品号」
func keyLabel(cols []string) string {
	return strings.Join(cols, " + ")
}

// ============================================================================
// 订单 / 工单侧的重复检查接入
//
// 物料档案是「一列一个唯一键」，订单侧则是组合键：明细的（订单编号 + 产品编码）
// 才唯一，工单号本身唯一。所以默认值按表分开给，也可由 orders.json 覆盖。
// ============================================================================

// defaultOrderDupKeys 输出表名 -> 组成唯一键的输出列（组合键，按列序）
var defaultOrderDupKeys = map[string][]string{
	"order_detail": {"订单编号", "产品编码"},
	"work_order":   {"工单号"},
}

// orderDupCols 取某张输出表的重复检查列。
// 配置里显式给了就用配置的（给空数组＝关闭该表检查）；没给则用内置默认。
func orderDupCols(cfg *OrdersConfig, sheet string) []string {
	if cfg != nil && cfg.DuplicateKeys != nil {
		if cols, ok := cfg.DuplicateKeys[sheet]; ok {
			return cols
		}
	}
	return defaultOrderDupKeys[sheet]
}

// dupTrackerFor 取/建某张输出表的重复跟踪器；该表未配置检查时返回 nil。
func (c *orderCtx) dupTrackerFor(sheet string) *dupTracker {
	cols := orderDupCols(c.cfg, sheet)
	if len(cols) == 0 {
		return nil
	}
	if c.dups == nil {
		c.dups = map[string]*dupTracker{}
	}
	if d, ok := c.dups[sheet]; ok {
		return d
	}
	d := newDupTracker(cols)
	c.dups[sheet] = d
	return d
}

// reportDups 把某张输出表的重复检查结果写进 notes（有/无）与 errors（重复组明细）。
func (c *orderCtx) reportDups(sheet string) {
	d := c.dups[sheet]
	if d == nil {
		return
	}
	lab := keyLabel(d.cols)
	if d.dupGroupCount() == 0 {
		c.info(c.L["note_dup_none"], lab)
		return
	}
	c.info(c.L["note_dup_total"], lab, d.dupGroupCount(), d.dupRowCount())
	for _, it := range d.dupItems() {
		c.rep.Errors = append(c.rep.Errors, fmt.Sprintf(c.L["sum_dup"], lab, it.Key, it.N))
	}
}

// scanExcelErrors 扫源档里的 Excel 错误值（#NAME? / #REF! …）。
// 这类格子是「有人用 = 开头写备注 / 引用了已删除的单元格」，值会被原样搬进 MES，
// 所以按源列汇总成告警 —— 只提示，不阻断转换。
func (c *orderCtx) scanExcelErrors(g *sheetGrid) {
	byCol := map[string]int{}
	for ri, row := range g.Rows {
		for si, v := range row {
			if !excelErrValue(v) {
				continue
			}
			col := ""
			if si < len(g.Headers) {
				col = strings.TrimSpace(g.Headers[si])
			}
			if col == "" {
				col = fmt.Sprintf("#%d", si+1)
			}
			byCol[col]++
			c.addIssue(g.HeaderRow+1+ri+1, col,
				fmt.Sprintf(c.L["warn_excel_err"], strings.TrimSpace(v)), "warn")
		}
	}
	for _, k := range sortedCountKeys(byCol) {
		c.rep.Warnings = append(c.rep.Warnings, fmt.Sprintf(c.L["sum_excel_err"], k, byCol[k]))
	}
}
