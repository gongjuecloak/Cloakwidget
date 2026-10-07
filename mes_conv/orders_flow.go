package main

// ============================================================================
// 订单 / 工单 的具体转换流程（ConvertOrders 的两个阶段）
// ============================================================================

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// importOrder 订单文件 → 销售订单主表 + 销售订单明细
func (c *orderCtx) importOrder(owb *excelize.File, firstSheet string, created *bool, srcPath string) error {
	g, err := c.openGrid(srcPath)
	if err != nil {
		return err
	}
	aliases := c.cfg.ColumnAliases
	c.log(fmt.Sprintf(c.L["o_read"], g.SheetName, len(g.Rows), g.HeaderRow+1))

	// 必需列（源档侧）：没有订单号和品号，整件事无从谈起
	if missing := ensureColumns(g, []string{"訂單單號", "品號"}, aliases); len(missing) > 0 {
		return fmt.Errorf(c.L["o_missing_cols"], c.L["label_order"], strings.Join(missing, "、"))
	}
	if c.cfg.Settings.EnableValidation {
		c.validateNumeric(g, "訂單數量", "订单数量")
	}

	iOrderNo := g.colIdx("訂單單號", aliases)
	iProduct := g.colIdx("品號", aliases)

	// ---- 过滤：订单号为空的行 + 小计 / 合计行 ----
	clean := make([][]string, 0, len(g.Rows))
	noKey := 0
	for _, row := range g.Rows {
		if isBlankStr(cell(row, iOrderNo)) {
			noKey++
			continue
		}
		clean = append(clean, row)
	}
	if len(clean) == 0 {
		return fmt.Errorf(c.L["o_no_valid"], c.L["label_order"])
	}
	sumRows := 0
	if c.cfg.Settings.FilterSummaryRows {
		clean, sumRows = c.filterSummary(g, clean, "品號")
	}
	if dropped := noKey + sumRows; dropped > 0 {
		c.rep.Filtered[c.L["label_order"]] = dropped
		c.log(fmt.Sprintf(c.L["o_filtered"], c.L["label_order"], dropped))
	}
	c.orderClean = &sheetGrid{Path: g.Path, SheetName: g.SheetName, Headers: g.Headers, Rows: clean}

	masterBinds := bindMapping(g, resolveMapping(g, c.cfg.FieldMapping["order_master"], aliases))
	detailBinds := bindMapping(g, resolveMapping(g, c.cfg.FieldMapping["order_detail"], aliases))
	masterDefaults := c.cfg.DefaultValues["order_master"]
	detailDefaults := c.cfg.DefaultValues["order_detail"]
	masterDates := c.dateSet("order_master")
	detailDates := c.dateSet("order_detail")

	// ---- 主表：按订单号分组，每组取第一行 ----
	masterOrder := c.cfg.ColumnOrder["order_master"]
	masterRows := make([][]interface{}, 0, 64)
	seen := map[string]bool{}
	for _, row := range clean {
		no := strings.TrimSpace(cell(row, iOrderNo))
		if seen[no] {
			continue
		}
		seen[no] = true
		values := applyBinds(masterBinds, row)
		applyDerived(values, c.cfg.DerivedColumns["order_master"])
		applyDefaults(values, masterDefaults)
		for _, col := range []string{"订单日期", "交货日期"} {
			if !isBlankStr(values[col]) {
				values[col] = parseDateCell(values[col])
			}
		}
		masterRows = append(masterRows, rowToCells(masterOrder, values, masterDates))
	}
	if err := c.writeSheet(owb, firstSheet, created, "销售订单主表",
		c.templateHeader(masterOrder), masterRows, masterDates); err != nil {
		return err
	}
	c.rep.OrderMaster = len(masterRows)
	c.log(fmt.Sprintf(c.L["o_master_count"], len(masterRows)))

	// ---- 明细：逐行；行号按订单号在「品号非空」的行上累计 ----
	lineSeq := map[string]int{}
	detailOrder := c.cfg.ColumnOrder["order_detail"]
	detailRows := make([][]interface{}, 0, len(clean))
	progressSet(0, len(clean))
	for ri, row := range clean {
		progressSet(ri+1, len(clean))
		product := strings.TrimSpace(cell(row, iProduct))
		if product == "" {
			continue // 没有品号的明细行不导出（与 Python 版口径一致）
		}
		values := applyBinds(detailBinds, row)
		applyDerived(values, c.cfg.DerivedColumns["order_detail"])
		applyDefaults(values, detailDefaults)

		no := strings.TrimSpace(cell(row, iOrderNo))
		lineSeq[no]++
		line := lineSeq[no]
		values["行号"] = strconv.Itoa(line)
		// 工单回填用：同一 (订单号, 品号) 记首次出现的行号
		if key := detailKey(no, product); c.detailLine[key] == 0 {
			c.detailLine[key] = line
		}

		if !isBlankStr(values["交货日期"]) {
			values["交货日期"] = parseDateCell(values["交货日期"])
		}
		c.checkDetailRequired(values)
		detailRows = append(detailRows, rowToCells(detailOrder, values, detailDates))
	}
	if err := c.writeSheet(owb, firstSheet, created, "销售订单明细",
		c.templateHeader(detailOrder), detailRows, detailDates); err != nil {
		return err
	}
	c.rep.OrderDetail = len(detailRows)
	c.log(fmt.Sprintf(c.L["o_detail_count"], len(detailRows)))
	return nil
}

// importWorkOrder 工单文件 → 工单表
func (c *orderCtx) importWorkOrder(owb *excelize.File, firstSheet string, created *bool, srcPath string) error {
	g, err := c.openGrid(srcPath)
	if err != nil {
		return err
	}
	aliases := c.cfg.ColumnAliases
	c.log(fmt.Sprintf(c.L["o_read"], g.SheetName, len(g.Rows), g.HeaderRow+1))

	if missing := ensureColumns(g, []string{"製令編號", "產品品號"}, aliases); len(missing) > 0 {
		return fmt.Errorf(c.L["o_missing_cols"], c.L["label_work"], strings.Join(missing, "、"))
	}
	if c.cfg.Settings.EnableValidation {
		c.validateNumeric(g, "預計產量", "计划数量")
	}

	iWorkNo := g.colIdx("製令編號", aliases)

	clean := make([][]string, 0, len(g.Rows))
	noKey := 0
	for _, row := range g.Rows {
		if isBlankStr(cell(row, iWorkNo)) {
			noKey++
			continue
		}
		clean = append(clean, row)
	}
	if len(clean) == 0 {
		return fmt.Errorf(c.L["o_no_valid"], c.L["label_work"])
	}
	sumRows := 0
	if c.cfg.Settings.FilterSummaryRows {
		clean, sumRows = c.filterSummary(g, clean, "產品品號")
	}
	if dropped := noKey + sumRows; dropped > 0 {
		c.rep.Filtered[c.L["label_work"]] = dropped
		c.log(fmt.Sprintf(c.L["o_filtered"], c.L["label_work"], dropped))
	}

	binds := bindMapping(g, resolveMapping(g, c.cfg.FieldMapping["work_order"], aliases))
	workDefaults := c.cfg.DefaultValues["work_order"]
	defaultLine := workDefaults["产线"]
	defaultOrderLine := workDefaults["销售订单行号"]
	if defaultOrderLine == "" {
		defaultOrderLine = "1"
	}
	tags := c.tagValues()

	colOrder := c.cfg.ColumnOrder["work_order"]
	workDates := c.dateSet("work_order")
	rows := make([][]interface{}, 0, len(clean))
	tagOn := 0
	progressSet(0, len(clean))
	for ri, row := range clean {
		progressSet(ri+1, len(clean))
		values := applyBinds(binds, row)
		// 产线固定取默认产线，忽略源档里的生產線別（MES 侧产线编码与 ERP 不同）
		if defaultLine != "" {
			values["产线"] = defaultLine
		}

		// 销售订单行号：先在订单明细里按 (订单号, 品号) 找，找不到用默认值；
		// 没有销售订单的工单留空
		so := values["销售订单"]
		product := values["产品编码"]
		switch {
		case strings.TrimSpace(so) == "":
			values["销售订单行号"] = ""
			c.rep.LineMatch.NoOrder++
		default:
			if line, ok := c.detailLine[detailKey(so, product)]; ok {
				values["销售订单行号"] = strconv.Itoa(line)
				c.rep.LineMatch.Matched++
			} else {
				values["销售订单行号"] = defaultOrderLine
				c.rep.LineMatch.Unmatched++
			}
		}

		// 产线与销售订单行号都有各自的业务口径（前者固定默认线别，后者按订单明细回填，
		// 无销售订单时留空），不参与「空则填默认值」这一步 —— 否则刚留空的会被填回默认值
		applyDefaults(values, defaultsExcept(workDefaults, "产线", "销售订单行号"))
		// 上面已按业务逻辑定过这两列，别让默认值再覆盖一次
		if defaultLine != "" {
			values["产线"] = defaultLine
		}

		if tags != nil {
			if hasPrefix(product, tags.prefixes) {
				values[tags.column] = tags.yes
				tagOn++
			} else {
				values[tags.column] = tags.no
			}
		}
		values["来源单号"] = "ERP-" + values["工单号"]

		for _, col := range []string{"计划开始", "计划完成"} {
			if !isBlankStr(values[col]) {
				values[col] = parseDateCell(values[col])
			}
		}
		applyDerived(values, c.cfg.DerivedColumns["work_order"])
		c.checkWorkRequired(values)
		rows = append(rows, rowToCells(colOrder, values, workDates))
	}
	if err := c.writeSheet(owb, firstSheet, created, "工单",
		c.templateHeader(colOrder), rows, workDates); err != nil {
		return err
	}
	c.rep.WorkOrders = len(rows)
	c.log(fmt.Sprintf(c.L["o_work_count"], len(rows)))
	c.log(fmt.Sprintf(c.L["o_line_match"],
		c.rep.LineMatch.Matched, c.rep.LineMatch.Unmatched, c.rep.LineMatch.NoOrder))
	if tagOn > 0 {
		c.log(fmt.Sprintf(c.L["o_tag_on"], tagOn))
	}
	return nil
}

// ---------- 内部工具 ----------

// detailKey 订单明细的行号索引键；用 \x00 分隔，避免订单号/品号里出现同名拼接歧义
func detailKey(orderNo, product string) string {
	return strings.TrimSpace(orderNo) + "\x00" + strings.TrimSpace(product)
}

// rowToCells 按列顺序把一行整理成单元格数组；dates 里的列写成真正的日期值
func rowToCells(colOrder []string, values map[string]string, dates map[string]bool) []interface{} {
	out := make([]interface{}, len(colOrder))
	for i, col := range colOrder {
		out[i] = toCell(col, values[col], dates[col])
	}
	return out
}

// openGrid 打开源档并定位表头；把底层错误换成能看懂的话
func (c *orderCtx) openGrid(path string) (*sheetGrid, error) {
	if strings.HasSuffix(strings.ToLower(path), ".xls") {
		return nil, fmt.Errorf("%s", c.L["o_xls_unsupported"])
	}
	g, err := readGridAuto(path)
	if err != nil {
		switch err.Error() {
		case "no-sheet":
			return nil, fmt.Errorf("%s", c.L["o_no_sheet"])
		case "empty-sheet":
			return nil, fmt.Errorf("%s", c.L["o_empty"])
		default:
			return nil, fmt.Errorf(c.L["o_open_failed"], err.Error())
		}
	}
	if len(g.Rows) == 0 {
		return nil, fmt.Errorf("%s", c.L["o_empty"])
	}
	return g, nil
}

func (c *orderCtx) filterSummary(g *sheetGrid, rows [][]string, keyColumn string) ([][]string, int) {
	out := make([][]string, 0, len(rows))
	removed := 0
	for _, row := range rows {
		if isSummaryRow(g, row, keyColumn) {
			removed++
			continue
		}
		out = append(out, row)
	}
	return out, removed
}

type tagRule struct {
	column   string
	prefixes []string
	yes      string
	no       string
}

// tagValues 打标规则（配置关闭或没配前缀时返回 nil）
func (c *orderCtx) tagValues() *tagRule {
	t := c.cfg.TagMaterials
	if !t.Enabled || len(t.Prefixes) == 0 {
		return nil
	}
	column := t.Column
	if column == "" {
		column = "是否需要打标"
	}
	yes, no := t.Yes, t.No
	if yes == "" {
		yes = "是"
	}
	if no == "" {
		no = "否"
	}
	return &tagRule{column: column, prefixes: t.Prefixes, yes: yes, no: no}
}

// hasPrefix 产品编码是否以任一打标前缀开头（忽略大小写）
func hasPrefix(s string, prefixes []string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p == "" || len(s) < len(p) {
			continue
		}
		if strings.EqualFold(s[:len(p)], p) {
			return true
		}
	}
	return false
}

// ---------- 数据校验（预检） ----------

// validateNumeric 数量列里出现非数字 → 预检问题（行号按源档实际行号算，方便对照）
func (c *orderCtx) validateNumeric(g *sheetGrid, srcCol, target string) {
	ci := g.colIdx(srcCol, c.cfg.ColumnAliases)
	if ci < 0 {
		return
	}
	for ri, row := range g.Rows {
		v := strings.TrimSpace(cell(row, ci))
		if v == "" {
			continue
		}
		if _, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", ""), 64); err != nil {
			c.addIssue(g.HeaderRow+1+ri+1, target, fmt.Sprintf(c.L["o_issue_not_number"], v), "warn")
		}
	}
}

// checkDetailRequired 明细行导出的必填列留空检查
func (c *orderCtx) checkDetailRequired(values map[string]string) {
	for _, col := range c.cfg.RequiredColumns["order_detail"] {
		if col == "行号" {
			continue // 行号由程序生成，必然有值
		}
		if isBlankStr(values[col]) {
			c.addIssue(0, col, c.L["o_issue_out_blank"], "error")
		}
	}
}

// checkWorkRequired 工单行导出的必填列留空检查
func (c *orderCtx) checkWorkRequired(values map[string]string) {
	for _, col := range c.cfg.RequiredColumns["work_order"] {
		if isBlankStr(values[col]) {
			c.addIssue(0, col, c.L["o_issue_out_blank"], "error")
		}
	}
}
