package main

// ============================================================================
// 订单 / 工单转换引擎
//
// 业务来源：鼎新 ERP 导出的「订单档」与「製令（工单）档」→ MES 可导入的
// 「销售订单主表 / 销售订单明细 / 工单」三张表。
//
// 本文件是 Python 版工具（project-001/code-file/015/39/2.py，6672 行）里
// DataImporter 的业务重写：业务口径原样保留（含繁简混用表头识别、报表标题
// 前缀跳过、合计行过滤、销售订单行号回填、打标判定），去掉了 Tkinter / pandas 依赖。
// ============================================================================

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// ---------- 繁体 → 简体 归一（158 字，源自 Python 版 _CN_VARIANT_MAP） ----------

var cnVariant = map[rune]rune{
	'預': '预', '開': '开', '單': '单', '號': '号', '編': '编', '產': '产', '線': '线', '別': '别',
	'製': '制', '訂': '订', '細': '细', '計': '计', '務': '务', '員': '员', '處': '处', '備': '备',
	'註': '注', '狀': '状', '領': '领', '歷': '历', '類': '类', '稱': '称', '戶': '户', '應': '应',
	'優': '优', '緊': '紧', '級': '级', '價': '价', '錢': '钱', '額': '额', '項': '项', '總': '总',
	'結': '结', '關': '关', '來': '来', '進': '进', '車': '车', '庫': '库', '據': '据', '運': '运',
	'輸': '输', '認': '认', '證': '证', '負': '负', '責': '责', '資': '资', '訊': '讯', '點': '点',
	'質': '质', '規': '规', '劃': '划', '則': '则', '對': '对', '準': '准', '確': '确', '驗': '验',
	'檢': '检', '隨': '随', '評': '评', '試': '试', '間': '间', '題': '题', '順': '顺', '復': '复',
	'雜': '杂', '體': '体', '統': '统', '說': '说', '話': '话', '語': '语', '辦': '办', '動': '动',
	'機': '机', '當': '当', '後': '后', '職': '职', '場': '场', '區': '区', '專': '专', '導': '导',
	'條': '条', '顯': '显', '隱': '隐', '種': '种', '觀': '观', '視': '视', '圖': '图', '團': '团',
	'鐵': '铁', '鋼': '钢', '頁': '页', '層': '层', '發': '发', '張': '张', '強': '强', '費': '费',
	'財': '财', '會': '会', '電': '电', '腦': '脑', '萬': '万', '與': '与', '興': '兴', '盡': '尽',
	'監': '监', '護': '护', '實': '实', '圓': '圆', '壇': '坛', '網': '网', '紅': '红', '綠': '绿',
	'藍': '蓝', '顏': '颜', '兩': '两', '東': '东', '協': '协', '議': '议', '調': '调', '變': '变',
	'響': '响', '麗': '丽', '麼': '么', '這': '这', '們': '们', '問': '问', '請': '请', '謝': '谢',
	'許': '许', '講': '讲', '錯': '错', '設': '设', '訴': '诉', '識': '识', '讀': '读', '買': '买',
	'賣': '卖', '貴': '贵', '賬': '账', '貨': '货', '銀': '银', '銅': '铜', '錶': '表', '鍵': '键',
	'鎖': '锁', '鏈': '链', '長': '长', '門': '门', '隊': '队', '際': '际', '雲': '云', '幾': '几',
	'燈': '灯', '魚': '鱼', '鳥': '鸟', '馬': '马', '讓': '让', '詞': '词',
}

// cnUnify 把繁体常用字统一为简体，使简/繁混用的表头也能正确匹配
func cnUnify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if v, ok := cnVariant[r]; ok {
			b.WriteRune(v)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normCol 列名归一化：去换行/制表/所有空白、全角括号转半角、去掉 "* " 必填前缀、
// 繁→简、转小写。源档表头与配置里的标准列名都过这一遍，再比对。
func normCol(name string) string {
	s := strings.NewReplacer("\n", "", "\r", "", "\t", "", "（", "(", "）", ")", "　", "").Replace(name)
	s = strings.Join(strings.Fields(s), "")
	s = strings.TrimSpace(strings.TrimLeft(s, "*"))
	return strings.ToLower(cnUnify(s))
}

// ---------- 读表 ----------

// sheetGrid 一个工作表的内容，表头行已定位
type sheetGrid struct {
	Path      string
	SheetName string
	HeaderRow int // 0-based，指向真实表头那一行
	Headers   []string
	Rows      [][]string
	SrcDesc   string         // 「读到了什么格式/编码/分隔符」，写进转换日志
	SrcKind   srcKind        // 源档真身（.xls 伪装成 xlsx 之类都在这层被识破）
	normIdx   map[string]int // 归一化列名 -> 列下标
	stdIdx    map[string]int // 标准列名 -> 列下标（-1 表示源档里没有），带缓存避免逐行重解析
}

func (g *sheetGrid) normIndex() map[string]int {
	if g.normIdx == nil {
		g.normIdx = make(map[string]int, len(g.Headers))
		used := map[string]int{}
		for i, h := range g.Headers {
			k := normCol(h)
			if k == "" {
				continue
			}
			// 源档常有同名列（如两个「備註」）。pandas 会自动改名为 備註.1 / 備註.2，
			// 配置里的候选写法（「備註.1|備註」）正是依赖这个规则，这里复刻一遍。
			if n, dup := used[k]; dup {
				used[k] = n + 1
				k = fmt.Sprintf("%s.%d", k, n)
			} else {
				used[k] = 1
			}
			if _, dup := g.normIdx[k]; !dup {
				g.normIdx[k] = i
			}
		}
	}
	return g.normIdx
}

// cell 取某行某列的值，越界返回空串
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// colIdx 返回标准列名对应的列下标，-1 表示源档里没有这一列。
// 结果按列名缓存 —— 逐行取值时（几万行 × 十几列）不能每次重跑一遍别名匹配。
func (g *sheetGrid) colIdx(std string, aliases map[string][]string) int {
	if v, ok := g.stdIdx[std]; ok {
		return v
	}
	if g.stdIdx == nil {
		g.stdIdx = map[string]int{}
	}
	g.stdIdx[std] = resolveColIndex(g, std, aliases)
	return g.stdIdx[std]
}

// isBlankStr 空串或纯空白视为空
func isBlankStr(s string) bool { return strings.TrimSpace(s) == "" }

// readGridAuto 打开源档，自动定位真实表头行（跳过公司名/报表名等标题前缀），
// 返回表头与数据行。xlsx / 老 .xls(BIFF) / Excel 2003 XML / CSV 都能读 ——
// 真身按文件头魔数判断，不看扩展名（ERP 导出的「.xls」常是 xlsx 或 XML 改名）。
func readGridAuto(path string) (*sheetGrid, error) {
	rows, name, desc, kind, err := readSourceGrid(path, 0)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty-sheet")
	}
	hr := detectHeaderRow(rows, 20)
	if hr < 0 || hr >= len(rows) {
		hr = 0
	}
	g := &sheetGrid{Path: path, SheetName: name, HeaderRow: hr, Headers: rows[hr], SrcDesc: desc, SrcKind: kind}
	for _, r := range rows[hr+1:] {
		if len(r) == 0 {
			continue
		}
		g.Rows = append(g.Rows, r)
	}
	return g, nil
}

// headerStrongKeys 出现即认为「这行是表头」的强特征列名
var headerStrongKeys = []string{
	"製令編號", "工單編號", "工单号", "訂單單號", "订单编号", "訂單編號", "品號", "產品品號", "產品品名",
}

// detectHeaderRow 扫描前 maxScan 行，挑最像表头的一行。
// 兼容「报表标题占前几行、真实表头在下方」的导出行式（如 工单--002.xlsx）。
// 没有任何命中时回退 0，保持对无前缀文件的兼容。
func detectHeaderRow(rows [][]string, maxScan int) int {
	keys := map[string]bool{}
	for _, k := range headerStrongKeys {
		keys[normCol(k)] = true
	}
	strong := map[string]bool{}
	for _, k := range headerStrongKeys {
		strong[normCol(k)] = true
	}
	for _, alist := range defaultColumnAliases() {
		for _, a := range alist {
			keys[normCol(a)] = true
		}
	}
	bestIdx, bestScore, bestStrong := 0, 0, false
	limit := maxScan
	if len(rows) < limit {
		limit = len(rows)
	}
	for i := 0; i < limit; i++ {
		score := 0
		sStrong := false
		for _, v := range rows[i] {
			s := strings.TrimSpace(v)
			if s == "" {
				continue
			}
			n := normCol(s)
			if n == "" {
				continue
			}
			if keys[n] {
				score++
			}
			if strong[n] {
				sStrong = true
			}
		}
		if score == 0 && !sStrong {
			continue
		}
		// 强特征行优先；同级再比命中数量
		if sStrong && !bestStrong {
			bestIdx, bestScore, bestStrong = i, score, true
		} else if sStrong == bestStrong && score > bestScore {
			bestIdx, bestScore = i, score
		}
	}
	return bestIdx
}

// ---------- 别名解析 ----------

// resolveColIndex 在源档里找出 srcField（或它的任一别名）对应的「列下标」，
// -1 表示源档里没有这一列。
//
// 注意必须返回下标而不是列名：源档常有重名列（三个「備註」），归一化后列名
// 记作 備註 / 備註.1 / 備註.2，但它们的原始表头文本是同一个字符串 —— 只传列名
// 会把它指回第一列。
func resolveColIndex(g *sheetGrid, srcField string, aliases map[string][]string) int {
	if strings.TrimSpace(srcField) == "" {
		return -1
	}
	if i, ok := g.normIndex()[normCol(srcField)]; ok {
		return i
	}
	// 别名表以原始（可能含繁体）名为键，需按归一化后比对才能兼容简/繁混用表头
	key := normCol(srcField)
	var matched []string
	for std := range aliases {
		if normCol(std) == key {
			matched = append(matched, std)
		}
	}
	sortStrings(matched)
	for _, std := range matched {
		for _, a := range aliases[std] {
			if i, ok := g.normIndex()[normCol(a)]; ok {
				return i
			}
		}
	}
	return -1
}

// resolveMapping 把配置里的「源字段 -> 目标列」解析成「列下标 -> 目标列」。
// 源字段支持用 | 写多个候选（如「備註.1|備註」），按顺序取第一个存在的。
func resolveMapping(g *sheetGrid, mapping map[string]string, aliases map[string][]string) map[int]string {
	out := map[int]string{}
	// 排序遍历，保证同一份配置每次解析结果一致（Go 的 map 遍历是无序的）
	for _, srcField := range sortedKeys(mapping) {
		target := mapping[srcField]
		if strings.TrimSpace(target) == "" {
			continue // 映射到空 = 该列不导出
		}
		for _, part := range strings.Split(srcField, "|") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if i := resolveColIndex(g, part, aliases); i >= 0 {
				out[i] = target
				break
			}
		}
	}
	return out
}

// ensureColumns 确保这些标准列在表里能取到（借别名找回），返回找不到的列名。
func ensureColumns(g *sheetGrid, required []string, aliases map[string][]string) []string {
	var missing []string
	for _, std := range required {
		if resolveColIndex(g, std, aliases) < 0 {
			missing = append(missing, std)
		}
	}
	return missing
}

// colBind 「列下标 -> 目标列」的绑定。字段映射先解析成下标，
// 逐行套用时就只剩一次数组取值。
type colBind struct {
	idx    int
	target string
}

// bindMapping 把「列下标 -> 目标列」整理成稳定的绑定序列
func bindMapping(g *sheetGrid, resolved map[int]string) []colBind {
	out := make([]colBind, 0, len(resolved))
	for idx, target := range resolved {
		out = append(out, colBind{idx: idx, target: target})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].idx < out[j-1].idx; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// applyBinds 从一行里取出所有绑定的列值
func applyBinds(binds []colBind, row []string) map[string]string {
	values := make(map[string]string, len(binds))
	for _, b := range binds {
		values[b.target] = strings.TrimSpace(cell(row, b.idx))
	}
	return values
}

// sortStrings 就地升序排序（元素量很小，插入排序足够且无额外依赖）
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// ---------- 合计行过滤 ----------

var summaryKeywords = []string{"小計", "小计", "小 計", "合計", "合计", "合 計", "總計", "总计", "總 計", "subtotal"}

var summaryTextHints = []string{
	"名", "品名", "摘要", "備註", "備注", "说明", "說明", "描述",
	"项目", "項目", "备注", "備考", "remark", "memo", "desc",
}

// isSummaryRow 判断某行是否是「小计 / 合计 / 总计」这类汇总行。
// 只在文本类列（列名含 品名/摘要/备注/项目…）上找关键字，避免把数字列里的
// 「合計」字样误判；keyColumn 一定纳入扫描范围。
func isSummaryRow(g *sheetGrid, row []string, keyColumn string) bool {
	scan := map[int]bool{}
	keyNorm := normCol(keyColumn)
	if i, ok := g.normIndex()[keyNorm]; ok {
		scan[i] = true
	}
	for i, h := range g.Headers {
		hl := strings.ToLower(h)
		for _, hint := range summaryTextHints {
			if strings.Contains(hl, hint) {
				scan[i] = true
				break
			}
		}
	}
	for i := range scan {
		v := strings.ToLower(strings.TrimSpace(cell(row, i)))
		if v == "" {
			continue
		}
		for _, kw := range summaryKeywords {
			if strings.Contains(v, strings.ToLower(kw)) {
				return true
			}
		}
	}
	return false
}

// ---------- 日期 ----------

var dateLayouts = []string{
	"2006-01-02", "2006/01/02", "2006.01.02", "20060102",
	"2006-1-2", "2006/1/2", "2006.1.2",
	"2006-01-02 15:04:05", "2006/01/02 15:04:05",
	"02-01-2006", "02/01/2006",
	"2006年1月2日", "2006年01月02日",
}

// excelEpoch 1900 日期系统的起点（含 Excel 的闰年 bug 偏移）
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// parseDateCell 把源档里的日期值统一成 YYYY-MM-DD。
// 支持常见日期字符串、带时间的形式、以及 Excel 日期序列号（纯数字 20000~80000）。
// 解析不出来时原样保留 —— 宁可留个可疑值让人发现，也不静默丢数据。
func parseDateCell(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Excel 日期序列号（1900 系统，1900-01-01 = 1）
	if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64); err == nil {
		if f > 20000 && f < 80000 && f == math.Trunc(f) {
			return excelEpoch.AddDate(0, 0, int(f)).Format("2006-01-02")
		}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	// 带时间戳的再去掉时间部分试一次
	if i := strings.IndexAny(s, " T"); i > 8 {
		head := strings.TrimSpace(s[:i])
		for _, l := range dateLayouts {
			if t, err := time.Parse(l, head); err == nil {
				return t.Format("2006-01-02")
			}
		}
	}
	return s
}

// parseDateValue 与 parseDateCell 同样的识别口径，但返回 time.Time，
// 供「写成真正的日期单元格」使用。第二个返回值表示是否解析成功。
func parseDateValue(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64); err == nil {
		if f > 20000 && f < 80000 && f == math.Trunc(f) {
			return excelEpoch.AddDate(0, 0, int(f)), true
		}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	if i := strings.IndexAny(s, " T"); i > 8 {
		head := strings.TrimSpace(s[:i])
		for _, l := range dateLayouts {
			if t, err := time.Parse(l, head); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// ---------- 文件类型识别 ----------

var orderKeywords = []string{"訂單", "订单", "訂單單號", "客戶單號", "客户单号", "訂單數量", "订单数量", "單價", "单价", "客戶代號", "客户代号", "交貨日期", "交货日期", "付款条件", "業務員"}

var workKeywords = []string{"製令", "工单", "製令編號", "工单号", "產品品號", "产品品号", "預計產量", "计划产量", "開工日", "开工日", "完工日", "生產線別", "产线", "生產庫別", "BOM", "膠合", "塗裝"}

// detectOrderKind 判断源档是订单文件还是工单文件：
// 先看文件名，再看表头命中关键字的数量。
func detectOrderKind(g *sheetGrid, filename string) string {
	low := strings.ToLower(filename)
	if strings.Contains(low, "订单") || strings.Contains(low, "訂單") || strings.Contains(low, "order") {
		return "order"
	}
	if strings.Contains(low, "工单") || strings.Contains(low, "工單") || strings.Contains(low, "製令") ||
		strings.Contains(low, "work") {
		return "work"
	}
	orderScore, workScore := 0, 0
	for _, h := range g.Headers {
		n := normCol(h)
		if n == "" {
			continue
		}
		for _, kw := range orderKeywords {
			if strings.Contains(n, normCol(kw)) {
				orderScore++
				break
			}
		}
		for _, kw := range workKeywords {
			if strings.Contains(n, normCol(kw)) {
				workScore++
				break
			}
		}
	}
	if orderScore > workScore && orderScore >= 2 {
		return "order"
	}
	if workScore > orderScore && workScore >= 2 {
		return "work"
	}
	if orderScore >= 2 {
		return "order"
	}
	if workScore >= 2 {
		return "work"
	}
	for _, h := range g.Headers {
		n := normCol(h)
		if strings.Contains(n, "订单单号") || strings.Contains(n, "订单编号") {
			return "order"
		}
		if strings.Contains(n, "制令编号") || strings.Contains(n, "工单编号") {
			return "work"
		}
	}
	return "unknown"
}

// ---------- 转换结果 ----------

// LineMatchStat 工单「销售订单行号」的回填统计
type LineMatchStat struct {
	Matched   int `json:"matched"`   // 在订单明细里找到了行号
	Unmatched int `json:"unmatched"` // 有销售订单但没匹配上，用默认值
	NoOrder   int `json:"no_order"`  // 没有销售订单，留空
}

// OrdersReport 一次订单/工单转换的结果
type OrdersReport struct {
	OrderMaster int            `json:"order_master"`
	OrderDetail int            `json:"order_detail"`
	WorkOrders  int            `json:"work_orders"`
	Filtered    map[string]int `json:"filtered"` // 表名 -> 被过滤掉的合计/无效行数
	LineMatch   LineMatchStat  `json:"line_match"`
	Sheets      []string       `json:"sheets"`
	Issues      []Issue        `json:"issues"`
	IssueTotal  int            `json:"issue_total"`
	Errors      []string       `json:"errors"`
	Warnings    []string       `json:"warnings"`
	Notes       []string       `json:"notes"` // 中性说明：源档格式、批量文件、日期还原、模板变化
}

// orderCtx 一次转换过程中的中间状态
type orderCtx struct {
	cfg     *OrdersConfig
	lang    string
	L       map[string]string
	logf    func(string)
	rep     *OrdersReport
	issues  []Issue
	problem map[string]int // 问题值 -> 行数（用于汇总去重）

	// 订单明细的 (订单号, 品号) -> 行号，供工单回填销售订单行号
	detailLine map[string]int
	// 通过校验、过滤后的订单明细行，供工单转换复用
	orderClean *sheetGrid
	// 输出表名 -> 重复跟踪器（组合键）；由 quality.go 的 dupTrackerFor 懒创建
	dups map[string]*dupTracker
}

func (c *orderCtx) addIssue(row int, col, msg, level string) {
	c.rep.IssueTotal++
	if len(c.issues) < maxIssues {
		c.issues = append(c.issues, Issue{Row: row, Col: col, Msg: msg, Level: level})
	}
}

func (c *orderCtx) note(key string) { c.problem[key]++ }

// info 记一条中性说明（不是问题）：源档格式、批量文件数、日期还原、模板变化等。
// 它会同时写进转换日志与结果里的 notes，界面上以中性样式单独列出，不与告警混在一起。
func (c *orderCtx) info(format string, args ...interface{}) {
	s := format
	if len(args) > 0 {
		s = fmt.Sprintf(format, args...)
	}
	c.rep.Notes = append(c.rep.Notes, s)
	c.log(s)
}

func (c *orderCtx) log(s string) {
	if c.logf != nil {
		c.logf(s)
	}
}

// ConvertOrders 主入口：把订单 / 工单源档转成 MES 可导入的多 sheet xlsx。
// orderPath / workPath 可只给一个；输出文件里 sheet 按实际转了的表生成。
func ConvertOrders(cfg *OrdersConfig, orderPath, workPath, outPath, lang string, logf func(string)) (*OrdersReport, error) {
	if cfg == nil {
		cfg = defaultOrdersConfig()
	}
	ctx := &orderCtx{
		cfg:        cfg,
		lang:       lang,
		L:          ordersMsgs(lang),
		logf:       logf,
		rep:        &OrdersReport{Filtered: map[string]int{}},
		problem:    map[string]int{},
		detailLine: map[string]int{},
	}
	ctx.log(ctx.L["o_start"])

	owb := excelize.NewFile()
	firstSheet := owb.GetSheetName(0)
	sheetCreated := false

	// ---- 订单：主表 + 明细 ----
	if strings.TrimSpace(orderPath) != "" {
		if err := ctx.importOrder(owb, firstSheet, &sheetCreated, orderPath); err != nil {
			return nil, err
		}
	}
	// ---- 工单 ----
	if strings.TrimSpace(workPath) != "" {
		if err := ctx.importWorkOrder(owb, firstSheet, &sheetCreated, workPath); err != nil {
			return nil, err
		}
	}
	if !sheetCreated {
		return nil, fmt.Errorf("%s", ctx.L["o_no_input"])
	}

	if err := owb.SaveAs(outPath); err != nil {
		return nil, fmt.Errorf("%s: %w", ctx.L["o_save_failed"], err)
	}

	ctx.rep.Issues = ctx.issues
	// 重复检查汇总（有则进 errors，无则只留一条中性说明）
	ctx.reportDups("order_detail")
	ctx.reportDups("work_order")
	for _, k := range sortedKeysInt(ctx.problem) {
		ctx.rep.Warnings = append(ctx.rep.Warnings, fmt.Sprintf("%s × %d", k, ctx.problem[k]))
	}
	ctx.log(fmt.Sprintf(ctx.L["o_done"], outPath))
	return ctx.rep, nil
}

func sortedKeysInt(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// writeSheet 建表并写入表头 + 数据行。返回最终写入的行数。
func (c *orderCtx) writeSheet(owb *excelize.File, firstSheet string, created *bool, name string, headers []string, rows [][]interface{}, dates map[string]bool) error {
	if !*created {
		if err := owb.SetSheetName(firstSheet, name); err != nil {
			return err
		}
		*created = true
	} else {
		if _, err := owb.NewSheet(name); err != nil {
			return err
		}
	}
	hdr := make([]interface{}, len(headers))
	for i, h := range headers {
		hdr[i] = h
	}
	if err := owb.SetSheetRow(name, "A1", &hdr); err != nil {
		return err
	}
	for i, r := range rows {
		rr := r
		if err := owb.SetSheetRow(name, fmt.Sprintf("A%d", i+2), &rr); err != nil {
			return err
		}
	}
	c.applyDateStyle(owb, name, headers, rows, dates)
	c.rep.Sheets = append(c.rep.Sheets, name)
	return nil
}

// applyDateStyle 给日期列套上 YYYY-MM-DD 数字格式。
// 单元格本身已经在 toCell 里写成 time.Time，这里只补格式，
// 保证在 Excel / MES 里显示与解析都当日期看（与 Python 原版一致）。
func (c *orderCtx) applyDateStyle(owb *excelize.File, sheet string, headers []string, rows [][]interface{}, dates map[string]bool) {
	if len(dates) == 0 || len(rows) == 0 {
		return
	}
	var cols []int
	for i, h := range headers {
		key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(h), "*"))
		if dates[key] {
			cols = append(cols, i+1)
		}
	}
	if len(cols) == 0 {
		return
	}
	dateFmt := "YYYY-MM-DD"
	style, err := owb.NewStyle(&excelize.Style{NumFmt: 22, CustomNumFmt: &dateFmt})
	if err != nil {
		return
	}
	for i := range rows {
		for _, col := range cols {
			axis, err := excelize.CoordinatesToCellName(col, i+2)
			if err != nil {
				continue
			}
			_ = owb.SetCellStyle(sheet, axis, axis, style)
		}
	}
}

// alignRow 按 colOrder 把 map 形式的一行整理成有序的值数组
func alignRow(values map[string]string, colOrder []string) []string {
	out := make([]string, len(colOrder))
	for i, col := range colOrder {
		out[i] = values[col]
	}
	return out
}

// applyDerived 目标列为空时用来源列补（如 物料编码 ← 产品编码）
func applyDerived(values map[string]string, rules map[string]string) {
	for _, target := range sortedKeys(rules) {
		source := rules[target]
		if target == "" || source == "" {
			continue
		}
		if isBlankStr(values[target]) {
			values[target] = values[source]
		}
	}
}

// applyDefaults 目标列为空时填默认值
func applyDefaults(values map[string]string, defaults map[string]string) {
	for _, field := range sortedKeys(defaults) {
		if isBlankStr(values[field]) {
			values[field] = defaults[field]
		}
	}
}

// defaultsExcept 复制一份默认值表并剔除指定字段
func defaultsExcept(defaults map[string]string, skip ...string) map[string]string {
	out := make(map[string]string, len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	for _, s := range skip {
		delete(out, s)
	}
	return out
}

// templateHeader 输出表头：模板里带 "* " 必填前缀的列换成带前缀写法
func (c *orderCtx) templateHeader(cols []string) []string {
	out := make([]string, len(cols))
	for i, col := range cols {
		if !c.cfg.Settings.UseTemplateHeader {
			out[i] = col
			continue
		}
		if h, ok := c.cfg.TemplateHeaders[col]; ok {
			out[i] = h
		} else {
			out[i] = col
		}
	}
	return out
}

// toCell 按列名决定单元格类型：数量 / 单价 / 行号等写成数值，其余保持文本
// （订单编号这类长数字串一旦当数字写会被 Excel 变成科学计数法）
var numericTargets = map[string]bool{
	"订单数量": true, "单价": true, "行号": true,
	"计划数量": true, "销售订单行号": true, "税率": true,
}

func toCell(col, v string, isDate bool) interface{} {
	if isDate {
		if t, ok := parseDateValue(v); ok {
			return t // 写真正的日期值，Excel/MES 都按日期识别
		}
		return v // 解析不出来就保持文本，不硬转
	}
	if numericTargets[col] {
		return num(strings.ReplaceAll(v, ",", ""))
	}
	return v
}

// dateSet 把某个 category 的日期列名转成集合（逐行取单元格时只查一次 map）
func (c *orderCtx) dateSet(cat string) map[string]bool {
	m := map[string]bool{}
	for _, col := range c.cfg.DateColumns[cat] {
		m[col] = true
	}
	return m
}
