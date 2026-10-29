package main

// 订单 / 工单模块的配置。
//
// 与「物料档案」模块（mapping.json）刻意分开：两者的字段体系、目标系统表结构
// 完全不同，混在一份配置里既难维护也容易改错。这里单独一份 orders.json，
// 默认值内嵌在 exe 里，磁盘上有同名文件则覆盖 —— 和物料档案模块同一套「磁盘优先」策略。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TagMaterials 「是否需要打标」列的判定规则：产品编码以任一前缀开头 → 是
type TagMaterials struct {
	Enabled     bool     `json:"enabled"`
	Column      string   `json:"column"`       // 目标列名，如「是否需要打标」
	SourceField string   `json:"source_field"` // 取哪个字段判断，如「产品编码」
	Prefixes    []string `json:"prefixes"`     // 命中前缀
	Yes         string   `json:"yes"`
	No          string   `json:"no"`
}

// OrdersSettings 订单模块的运行参数
type OrdersSettings struct {
	UseTemplateHeader  bool `json:"use_template_header"`  // 输出表头带 "* " 必填前缀
	AutoFillOrderLine  bool `json:"auto_fill_order_line"` // 工单的销售订单行号从订单明细里匹配
	FilterSummaryRows  bool `json:"filter_summary_rows"`  // 过滤小计/合计行
	EnableValidation   bool `json:"enable_validation"`    // 转换前做数据校验
	TemplateSheetIndex int  `json:"source_sheet"`         // 源数据工作表索引，-1 = 第一个
}

// OrdersConfig 订单模块的唯一数据源（界面与命令行都读它）
type OrdersConfig struct {
	// Schema 配置结构版本。0 = 老版本写的（无此字段），加载时按 v0 迁移到 schemaOrders。
	Schema int `json:"schema,omitempty"`
	// category（order_master / order_detail / work_order）-> 源字段 -> 目标列
	FieldMapping map[string]map[string]string `json:"field_mapping"`
	// category -> 目标列 -> 源空时填入的默认值
	DefaultValues map[string]map[string]string `json:"default_values"`
	// category -> 目标列 -> 派生来源列（目标列为空时用来源列补）
	DerivedColumns map[string]map[string]string `json:"derived_columns"`
	// category -> 必填目标列
	RequiredColumns map[string][]string `json:"required_columns"`
	// 标准列名 -> 同义列名（含繁体/英文），用于识别源档五花八门的表头
	ColumnAliases map[string][]string `json:"column_aliases"`
	// category -> 输出列顺序
	ColumnOrder map[string][]string `json:"column_order"`
	// category -> 目标列里哪些是日期列。这些列会写成真正的日期单元格（格式 YYYY-MM-DD），
	// 而不是文本 —— 与 Python 原版写出的单元格类型保持一致，避免 MES 按日期字段解析时出岔子。
	DateColumns map[string][]string `json:"date_columns"`
	// 输出表头（模板）：干净列名 -> 带 "* " 前缀的表头
	TemplateHeaders map[string]string `json:"template_headers"`
	TagMaterials    TagMaterials      `json:"tag_materials"`
	Settings        OrdersSettings    `json:"settings"`
	// category -> 组合键列（输出列名）。这些列拼起来应当唯一，重复即告警。
	// 留空则该表用内置默认（明细＝订单编号+产品编码，工单＝工单号）；显式给空数组表示关闭该表检查。
	DuplicateKeys map[string][]string `json:"duplicate_keys,omitempty"`
}

func ordersConfigPath() string {
	return filepath.Join(exeDir(), "orders.json")
}

// loadOrdersConfig 磁盘优先；没有或解析失败则回退内置默认配置。
// 第二个返回值表示「是否发生了 schema 迁移」，写回由 ensureOrdersConfig 负责。
func loadOrdersConfig() (*OrdersConfig, bool) {
	path := ordersConfigPath()
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		var c OrdersConfig
		if json.Unmarshal(b, &c) == nil && len(c.FieldMapping) > 0 {
			c.fillDefaults()
			// 配置由更高版本程序写入：只读不写，避免旧版把新键抹掉
			if c.Schema > schemaOrders {
				addConfigNote("orders.json 由更高版本的程序写入（schema %d > %d）：本版本只读不写，请升级程序",
					c.Schema, schemaOrders)
				return &c, false
			}
			// 盘上配置可能比内置默认旧（缺新增的模板列 / 默认值 / 开关），
			// 先向默认对齐再返回，否则旧配置会整体覆盖内置默认，导致模板改了输出却跟不上。
			changed := reconcileConfig(&c, b)
			if c.Schema != schemaOrders {
				c.Schema = schemaOrders
				changed = true
			}
			return &c, changed
		}
		// 解析失败 / 缺少 field_mapping：先备份再回退默认，绝不静默覆盖用户的自定义映射
		if name, e := backupFile(path, backupRoot()); e == nil && name != "" {
			addConfigNote("orders.json 解析失败或缺少 field_mapping，已备份为 backup/%s，本次改用内置默认配置",
				name)
		}
	}
	c := defaultOrdersConfig()
	c.Schema = schemaOrders
	return c, true
}

// saveOrdersConfig 覆盖前由调用方负责备份。
// schema 比程序新时拒绝写入 —— 否则旧版程序会把新版写入的键抹掉。
func saveOrdersConfig(c *OrdersConfig) error {
	if c.Schema > schemaOrders {
		return fmt.Errorf("配置由更高版本的程序写入（schema %d > %d），本版本不回写，以免丢字段；请先升级程序",
			c.Schema, schemaOrders)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(ordersConfigPath(), b)
}

// ensureOrdersConfig 首次运行写出配置文件；返回当前生效的配置。
// 发生迁移时：先备份旧文件，再把迁移结果写回（盘上配置始终是当前 schema）。
func ensureOrdersConfig() *OrdersConfig {
	path := ordersConfigPath()
	c, changed := loadOrdersConfig()
	if !fileExists(path) {
		_ = saveOrdersConfig(c)
		return c
	}
	if changed && c.Schema <= schemaOrders {
		if name := backupBeforeMigrate(path); name != "" {
			addConfigNote("orders.json 迁移前已备份：backup/%s", name)
		}
		if e := saveOrdersConfig(c); e != nil {
			addConfigNote("警告：orders.json 迁移结果写回失败（%v），本次仍按迁移后的配置运行", e)
		}
	}
	return c
}

// fillDefaults 补齐缺失的分支，避免配置里没写某个 category 时到处判空
func (c *OrdersConfig) fillDefaults() {
	if c.FieldMapping == nil {
		c.FieldMapping = map[string]map[string]string{}
	}
	if c.DefaultValues == nil {
		c.DefaultValues = map[string]map[string]string{}
	}
	if c.DerivedColumns == nil {
		c.DerivedColumns = map[string]map[string]string{}
	}
	if c.RequiredColumns == nil {
		c.RequiredColumns = map[string][]string{}
	}
	if c.ColumnAliases == nil {
		c.ColumnAliases = map[string][]string{}
	}
	if c.ColumnOrder == nil {
		c.ColumnOrder = map[string][]string{}
	}
	if c.DateColumns == nil {
		c.DateColumns = map[string][]string{}
	}
	for cat, cols := range defaultDateColumns() {
		if len(c.DateColumns[cat]) == 0 {
			c.DateColumns[cat] = cols
		}
	}
	if c.TemplateHeaders == nil {
		c.TemplateHeaders = map[string]string{}
	}
	// 别名缺失会直接导致源档表头认不出来，兜底用内置别名表
	if len(c.ColumnAliases) == 0 {
		c.ColumnAliases = defaultColumnAliases()
	}
	if c.Settings.TemplateSheetIndex == 0 {
		c.Settings.TemplateSheetIndex = 0
	}
}

// reconcileConfig 让盘上配置的结构与内置默认（= 当前 MES 模板）对齐：
//   - ColumnOrder：把内置默认里有、但盘上缺失的列追加到末尾，保留用户已有列的先后顺序；
//   - TemplateHeaders / RequiredColumns / DateColumns：补齐盘上缺失的键（不覆盖已有值）；
//   - FieldMapping / DefaultValues / DerivedColumns / ColumnAliases：按「源字段 / 目标列」补缺
//     —— 否则新版本加的映射与默认值到不了旧配置，表现是「某列整列为空」，全程无声；
//   - Settings 开关与 TagMaterials：只有「盘上根本没写这个键」才补默认
//     （布尔缺省 false 与用户有意关掉长得一样，靠原始 JSON 的键存在性区分）。
//
// 仅做「追加/补缺」，不删除盘上多出来的项，避免误伤用户合法定制。
// raw 是盘上配置原始字节，用于判断键是否存在；传 nil 表示不判断（单元测试用）。
func reconcileConfig(c *OrdersConfig, raw []byte) bool {
	d := defaultOrdersConfig()
	added := 0

	// 防御：盘上配置若没写这些分支，先初始化再补，避免对 nil map 赋值 panic
	if c.ColumnOrder == nil {
		c.ColumnOrder = map[string][]string{}
	}
	if c.TemplateHeaders == nil {
		c.TemplateHeaders = map[string]string{}
	}
	if c.RequiredColumns == nil {
		c.RequiredColumns = map[string][]string{}
	}
	if c.DateColumns == nil {
		c.DateColumns = map[string][]string{}
	}
	if c.FieldMapping == nil {
		c.FieldMapping = map[string]map[string]string{}
	}
	if c.DefaultValues == nil {
		c.DefaultValues = map[string]map[string]string{}
	}
	if c.DerivedColumns == nil {
		c.DerivedColumns = map[string]map[string]string{}
	}

	// ColumnOrder：追加默认里有、但盘上缺失的列
	for cat, defCols := range d.ColumnOrder {
		cur, n := mergeMissingStrings(c.ColumnOrder[cat], defCols)
		c.ColumnOrder[cat] = cur
		added += n
	}

	// TemplateHeaders：补齐缺失的表头前缀映射
	for k, v := range d.TemplateHeaders {
		if _, ok := c.TemplateHeaders[k]; !ok {
			c.TemplateHeaders[k] = v
			added++
		}
	}

	// RequiredColumns：补齐默认必填列
	for cat, req := range d.RequiredColumns {
		cur, n := mergeMissingStrings(c.RequiredColumns[cat], req)
		c.RequiredColumns[cat] = cur
		added += n
	}

	// DateColumns：补齐默认日期列
	for cat, dc := range d.DateColumns {
		cur, n := mergeMissingStrings(c.DateColumns[cat], dc)
		c.DateColumns[cat] = cur
		added += n
	}

	// FieldMapping：补齐缺失的「源字段 → 目标列」
	for cat, m := range d.FieldMapping {
		cur := c.FieldMapping[cat]
		if cur == nil {
			cur = map[string]string{}
		}
		for src, tgt := range m {
			if _, ok := cur[src]; !ok {
				cur[src] = tgt
				added++
			}
		}
		c.FieldMapping[cat] = cur
	}

	// DefaultValues：补齐缺失的「目标列默认值」。
	// 这一项最容易漏：新版本给某列加了默认值（如 工艺路线编码），旧配置里没有该键，
	// 输出就会整列为空，且不会报错。
	for cat, m := range d.DefaultValues {
		cur := c.DefaultValues[cat]
		if cur == nil {
			cur = map[string]string{}
		}
		for k, v := range m {
			if _, ok := cur[k]; !ok {
				cur[k] = v
				added++
			}
		}
		c.DefaultValues[cat] = cur
	}

	// DerivedColumns：补齐缺失的派生列
	for cat, m := range d.DerivedColumns {
		cur := c.DerivedColumns[cat]
		if cur == nil {
			cur = map[string]string{}
		}
		for k, v := range m {
			if _, ok := cur[k]; !ok {
				cur[k] = v
				added++
			}
		}
		c.DerivedColumns[cat] = cur
	}

	// ColumnAliases：补齐缺失的源表头同义词组（缺了会直接认不出源档表头）
	if c.ColumnAliases == nil {
		c.ColumnAliases = map[string][]string{}
	}
	for k, v := range d.ColumnAliases {
		if _, ok := c.ColumnAliases[k]; !ok {
			c.ColumnAliases[k] = v
			added++
		}
	}

	// Settings 开关：只有「键根本没写」才按默认打开（旧配置缺这些键时全是 false，
	// 会让校验 / 自动回填 / 合计行过滤这些功能静默失效）
	top := rawTopKeys(raw)
	if len(raw) > 0 {
		var probe struct {
			Settings struct {
				UseTemplateHeader *bool `json:"use_template_header"`
				AutoFillOrderLine *bool `json:"auto_fill_order_line"`
				FilterSummaryRows *bool `json:"filter_summary_rows"`
				EnableValidation  *bool `json:"enable_validation"`
			} `json:"settings"`
		}
		if json.Unmarshal(raw, &probe) == nil {
			if probe.Settings.UseTemplateHeader == nil {
				c.Settings.UseTemplateHeader = d.Settings.UseTemplateHeader
				added++
			}
			if probe.Settings.AutoFillOrderLine == nil {
				c.Settings.AutoFillOrderLine = d.Settings.AutoFillOrderLine
				added++
			}
			if probe.Settings.FilterSummaryRows == nil {
				c.Settings.FilterSummaryRows = d.Settings.FilterSummaryRows
				added++
			}
			if probe.Settings.EnableValidation == nil {
				c.Settings.EnableValidation = d.Settings.EnableValidation
				added++
			}
		}
	}

	// TagMaterials：整组缺（旧配置没有这一节）才补。
	// 缺了就等于「永远不判打标」，是否需要打标列整列为空，MES 导入会直接失败。
	if !rawHas(top, "tag_materials") {
		if c.TagMaterials.Column == "" || len(c.TagMaterials.Prefixes) == 0 {
			c.TagMaterials = d.TagMaterials
			added++
		}
	} else if c.TagMaterials.Enabled && len(c.TagMaterials.Prefixes) == 0 {
		// 开了打标却没有任何前缀 = 永远判「否」，按默认前缀补齐
		c.TagMaterials.Prefixes = d.TagMaterials.Prefixes
		added++
	}

	if added > 0 {
		addConfigNote("orders.json 已按 schema v%d 补齐 %d 项（列顺序 / 映射 / 默认值 / 开关）", schemaOrders, added)
		return true
	}
	return false
}

// orderConfigCoverage 检查某张表「模板里有、但没有任何数据来源」的输出列。
// 返回 (必填列缺来源, 非必填列缺来源)，都按 ColumnOrder 的顺序。
//
// 「有来源」= 有源字段映射 / 有默认值 / 有派生列 / 由程序内部生成（明细的 行号、
// 工单的 来源单号 与 是否需要打标）。这些列以前是最容易被忽略的一类问题：
// 模板加了一列、配置没跟上，转换照样「成功」，只是那一列整列为空。
func orderConfigCoverage(cfg *OrdersConfig, cat string) (missingRequired, missingOptional []string) {
	covered := make(map[string]bool, 32)
	for _, tgt := range cfg.FieldMapping[cat] {
		if t := strings.TrimSpace(tgt); t != "" {
			covered[t] = true
		}
	}
	for k := range cfg.DefaultValues[cat] {
		covered[k] = true
	}
	for k := range cfg.DerivedColumns[cat] {
		covered[k] = true
	}
	// 程序内部生成的列（不来自映射，但一定有值）
	switch cat {
	case "order_detail":
		covered["行号"] = true
	case "work_order":
		covered["来源单号"] = true
		covered["销售订单行号"] = true
		covered["产线"] = true
		if cfg.TagMaterials.Enabled && cfg.TagMaterials.Column != "" {
			covered[cfg.TagMaterials.Column] = true
		}
	}

	for _, col := range cfg.ColumnOrder[cat] {
		name := strings.TrimSpace(col)
		if name == "" || covered[name] {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(cfg.TemplateHeaders[name]), "*") {
			missingRequired = append(missingRequired, name)
		} else {
			missingOptional = append(missingOptional, name)
		}
	}
	return missingRequired, missingOptional
}

// defaultOrdersConfig 内置默认配置（与 Python 版 config.json 业务口径一致）
func defaultOrdersConfig() *OrdersConfig {
	c := &OrdersConfig{
		FieldMapping: map[string]map[string]string{
			"order_master": {
				"訂單單號":    "订单编号",
				"客戶簡稱":    "客户名称",
				"客戶單號":    "客户订单号",
				"訂單日期":    "订单日期",
				"預交日":     "交货日期",
				"業務員":     "",
				"送貨地址(一)": "送货地址",
				"付款條件名稱":  "付款条件",
				"備註":      "备注",
			},
			"order_detail": {
				"訂單單號":    "订单编号",
				"品號":      "产品编码",
				"客戶品號":    "客户件号",
				"訂單數量":    "订单数量",
				"單價":      "单价",
				"預交日":     "交货日期",
				"備註.1|備註": "备注",
			},
			"work_order": {
				"製令編號":  "工单号",
				"產品品號":  "产品编码",
				"預計產量":  "计划数量",
				"開 工 日": "计划开始",
				"完 工 日": "计划完成",
				"訂單單號":  "销售订单",
				"客戶代號":  "客户代号",
				"客戶名稱":  "客户名称",
				"客戶單號":  "客户单号",
				"客戶品號":  "客户件号",
				"生產線別":  "产线",
				"備註":    "备注信息",
			},
		},
		DefaultValues: map[string]map[string]string{
			"order_master": {
				"订单类型": "标准订单", "订单来源": "ERP系统",
				"业务员": "", "联系人": "", "联系电话": "",
				"币种": "₫越南盾", "税率": "0", "优先级": "中", "是否紧急": "否",
				"送货地址": "", "付款条件": "月结30天",
			},
			"order_detail": {"客户件号": ""},
			"work_order": {
				"工单生产批次号": "", "销售订单行号": "1", "优先级别": "中",
				"紧急标识": "否", "物料编码": "", "产线": "LINE07",
				"工艺路线编码": "",
			},
		},
		DerivedColumns: map[string]map[string]string{
			"work_order":   {"物料编码": "产品编码"},
			"order_master": {},
			"order_detail": {},
		},
		RequiredColumns: map[string][]string{
			"order_master": {"订单编号", "订单类型", "客户名称", "订单日期", "交货日期"},
			"order_detail": {"订单编号", "行号", "产品编码", "订单数量"},
			"work_order":   {"工单号", "计划数量", "计划开始", "计划完成"},
		},
		ColumnAliases: defaultColumnAliases(),
		DateColumns:   defaultDateColumns(),
		ColumnOrder: map[string][]string{
			"order_master": {
				"订单编号", "订单类型", "客户名称", "客户订单号", "订单日期", "交货日期",
				"订单来源", "业务员", "联系人", "联系电话", "送货地址", "币种",
				"付款条件", "税率", "优先级", "是否紧急", "备注",
			},
			"order_detail": {
				"订单编号", "行号", "产品编码", "客户件号",
				"订单数量", "单价", "交货日期", "备注",
			},
			"work_order": {
				"工单号", "工单生产批次号", "来源单号", "销售订单", "销售订单行号",
				"计划数量", "计划开始", "计划完成", "优先级别", "备注信息",
				"紧急标识", "是否需要打标", "产品编码", "物料编码", "产线",
				"工艺路线编码",
			},
		},
		// 输出表头：带 "* " 的列是 MES 模板的必填列（其余列不带前缀）
		TemplateHeaders: map[string]string{
			"工单号": "* 工单号", "计划数量": "* 计划数量",
			"计划开始": "* 计划开始", "计划完成": "* 计划完成",
			"优先级别": "* 优先级别", "紧急标识": "* 紧急标识",
			"产品编码": "* 产品编码", "物料编码": "* 物料编码",
			"订单编号": "* 订单编号", "订单类型": "* 订单类型",
			"客户名称": "* 客户名称", "订单日期": "* 订单日期",
			"交货日期": "* 交货日期", "行号": "* 行号", "订单数量": "* 订单数量",
		},
		TagMaterials: TagMaterials{
			Enabled:     true,
			Column:      "是否需要打标",
			SourceField: "产品编码",
			Prefixes: []string{
				"61CS", "61CR", "61FC", "6CR", "61FK",
				"61FS", "61HB", "61SP", "61SS",
			},
			Yes: "是",
			No:  "否",
		},
		Settings: OrdersSettings{
			UseTemplateHeader: true,
			AutoFillOrderLine: true,
			FilterSummaryRows: true,
			EnableValidation:  true,
		},
	}
	return c
}

// defaultDateColumns 三类表里哪些列是日期列（写成真正的日期单元格，格式 YYYY-MM-DD）
func defaultDateColumns() map[string][]string {
	return map[string][]string{
		"order_master": {"订单日期", "交货日期"},
		"order_detail": {"交货日期"},
		"work_order":   {"计划开始", "计划完成"},
	}
}

// defaultColumnAliases 源档表头的同义词表。
// 键是标准列名（繁体，与鼎新导出一致），值是可接受的写法（含简体、英文）。
// 匹配时两边都做「繁→简 + 去空白 + 小写」归一化，所以这里写繁写简都不影响识别。
func defaultColumnAliases() map[string][]string {
	return map[string][]string{
		"訂單單號": {"訂單編號", "订单编号", "订单单号", "訂單號", "订单号", "銷售訂單號", "销售订单号", "salesorder", "sono", "orderno"},
		"客戶簡稱": {"客户简称", "客戶名稱", "客户名称", "客戶", "客户", "customername"},
		"客戶名稱": {"客户名称", "客戶簡稱", "客户简称", "客戶", "客户"},
		"訂單日期": {"订单日期", "單據日期", "单据日期", "單據日", "制单日期", "orderdate"},
		"預交日":  {"预交日", "交貨日", "交货日", "交期", "預交日期", "预交日期", "交货日期", "duedate"},
		"品號":   {"品号", "產品品號", "产品品号", "产品编码", "產品編號", "产品编号", "物料編號", "物料编码", "itemno", "partno"},
		"訂單數量": {"订单数量", "數量", "数量", "訂購數量", "订购数量", "qty", "quantity"},
		"客戶品號": {"客户品号", "客戶件號", "客户件号", "customerpartno"},
		"客戶單號": {"客户单号", "客戶訂單號", "客户订单号"},
		"客戶代號": {"客户代号", "客戶代碼", "客户代码", "客戶編號", "客户编号"},
		"單價":   {"单价", "價格", "价格", "unitprice"},
		"備註":   {"备注", "備註.1", "备注.1", "remark", "memo"},
		"製令編號": {"製令编号", "工单号", "工單編號", "工單號", "工单编号", "制令编号", "製令", "mono", "workorder"},
		"產品品號": {"产品品号", "品號", "品号", "产品编码", "產品編號", "产品编号", "物料編號", "物料编码"},
		"預計產量": {"预计产量", "生產數量", "生产数量", "计划数量", "計劃數量", "預計生產量", "預計產", "產量", "产量", "數量", "数量"},
		"開工日":  {"开工日", "開工日期", "开工日期", "计划开始", "預計開工", "計劃開始", "生产开始日", "預計開工日", "预计开工日", "預計开工日", "预計開工日", "startdate", "開 工 日"},
		"完工日":  {"完工日", "完工日期", "计划完成", "計劃完成", "預計完工", "預計完工日", "预计完工日", "預計完工日期", "enddate", "完 工 日"},
		"生產線別": {"生产线别", "生產線別名稱", "生产线别名称", "產線", "产线", "線別", "线别", "生產線", "生产线", "line"},
	}
}
