package main

// 订单 / 工单模块的配置。
//
// 与「物料档案」模块（mapping.json）刻意分开：两者的字段体系、目标系统表结构
// 完全不同，混在一份配置里既难维护也容易改错。这里单独一份 orders.json，
// 默认值内嵌在 exe 里，磁盘上有同名文件则覆盖 —— 和物料档案模块同一套「磁盘优先」策略。

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

func ordersConfigPath() string {
	return filepath.Join(exeDir(), "orders.json")
}

// loadOrdersConfig 磁盘优先；没有或解析失败则回退内置默认配置。
// 和物料档案模块同一策略：默认配置编译在 exe 里，首次运行落一份到磁盘供维护。
func loadOrdersConfig() *OrdersConfig {
	if b, err := os.ReadFile(ordersConfigPath()); err == nil && len(b) > 0 {
		var c OrdersConfig
		if json.Unmarshal(b, &c) == nil && len(c.FieldMapping) > 0 {
			c.fillDefaults()
			return &c
		}
	}
	c := defaultOrdersConfig()
	return c
}

// saveOrdersConfig 覆盖前由调用方负责备份
func saveOrdersConfig(c *OrdersConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ordersConfigPath(), b, 0644)
}

// ensureOrdersConfig 首次运行写出配置文件；返回当前生效的配置
func ensureOrdersConfig() *OrdersConfig {
	c := loadOrdersConfig()
	if !fileExists(ordersConfigPath()) {
		_ = saveOrdersConfig(c)
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

// defaultOrdersConfig 内置默认配置（与 Python 版 config.json 业务口径一致）
func defaultOrdersConfig() *OrdersConfig {
	c := &OrdersConfig{
		FieldMapping: map[string]map[string]string{
			"order_master": {
				"訂單單號":   "订单编号",
				"客戶簡稱":   "客户名称",
				"客戶單號":   "客户订单号",
				"訂單日期":   "订单日期",
				"預交日":    "交货日期",
				"業務員":    "",
				"送貨地址(一)": "送货地址",
				"付款條件名稱": "付款条件",
				"備註":     "备注",
			},
			"order_detail": {
				"訂單單號":   "订单编号",
				"品號":     "产品编码",
				"客戶品號":   "客户件号",
				"訂單數量":   "订单数量",
				"單價":     "单价",
				"預交日":    "交货日期",
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
