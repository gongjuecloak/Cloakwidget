package main

import (
	"encoding/json"
	"testing"
)

// TestMaterialConfigMigrationFillsNewRules 验证「旧 mapping.json（没有 schema、少几条字段规则）」
// 加载后会被补齐：缺的字段规则按 Target 追加，用户改过的规则原样保留。
func TestMaterialConfigMigrationFillsNewRules(t *testing.T) {
	resetConfigNotes()
	legacy := &Config{
		Fields: []FieldRule{
			{Target: "物料编码", Source: "MB001", Op: "copy", Trim: true},
			{Target: "物料名称", Source: "MB002", Op: "copy", Raw: true},
			// 用户把「物料分类」的默认值改成了自己的口径 —— 迁移不能覆盖
			{Target: "物料分类", Source: "MB142", Op: "copy", Default: "自定义分类"},
		},
	}
	raw, _ := json.Marshal(legacy)
	changed := migrateMaterialConfig(legacy, raw)
	if !changed {
		t.Fatal("旧配置应被判定为「发生了迁移」")
	}
	if legacy.Schema != schemaMaterials {
		t.Fatalf("schema 应升到 %d, 实际 %d", schemaMaterials, legacy.Schema)
	}
	// 缺的规则应补回（工艺路线名称是 v1 之前就有的默认规则，测试用较稳的锚点）
	var got map[string]FieldRule
	got = map[string]FieldRule{}
	for _, f := range legacy.Fields {
		got[f.Target] = f
	}
	for _, want := range []string{"物料编码", "物料名称", "物料规格", "物料类型", "工艺路线名称"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("迁移后缺少字段规则：%s", want)
		}
	}
	// 用户改过的值不能被默认覆盖
	if got["物料分类"].Default != "自定义分类" {
		t.Fatalf("用户改过的默认值被覆盖了：%q", got["物料分类"].Default)
	}
	if _, ok := legacy.Dicts["matType"]; !ok {
		t.Fatal("缺失的字典应整组补回")
	}
	if len(legacy.UnitMap) == 0 {
		t.Fatal("缺失的单位映射应整表补回")
	}
}

// TestMaterialConfigTooNewIsReadOnly 验证「配置由更高版本程序写入」时不做任何改动。
func TestMaterialConfigTooNewIsReadOnly(t *testing.T) {
	resetConfigNotes()
	c := &Config{
		Schema: schemaMaterials + 5,
		Fields: []FieldRule{{Target: "物料编码", Op: "copy", Source: "MB001"}},
	}
	before := len(c.Fields)
	if changed := migrateMaterialConfig(c, nil); changed {
		t.Fatal("schema 比程序新时不应报「已迁移」")
	}
	if c.Schema != schemaMaterials+5 {
		t.Fatalf("schema 不应被改写, 实际 %d", c.Schema)
	}
	if len(c.Fields) != before {
		t.Fatal("schema 比程序新时不应补齐字段")
	}
}

// TestOrdersReconcileFillsFieldLevel 验证订单配置的字段级补齐：
// DefaultValues（新版本加的列默认值）、FieldMapping（新映射）、Settings 开关、打标规则。
func TestOrdersReconcileFillsFieldLevel(t *testing.T) {
	resetConfigNotes()
	// 模拟一份「v1.15.x 写的」旧配置：只有列顺序和部分映射，没有 settings / tag_materials，
	// DefaultValues 里也没有后加的「工艺路线编码」。
	legacy := map[string]interface{}{
		"field_mapping": map[string]map[string]string{
			"work_order": {"製令編號": "工单号", "產品品號": "产品编码"},
		},
		"column_order": map[string][]string{
			"work_order": {"工单号", "计划数量", "产品编码"},
		},
	}
	raw, _ := json.Marshal(legacy)
	var c OrdersConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	c.fillDefaults()
	if !reconcileConfig(&c, raw) {
		t.Fatal("旧订单配置应判定为发生了补齐")
	}

	if _, ok := c.DefaultValues["work_order"]["工艺路线编码"]; !ok {
		t.Fatal("新版默认值（工艺路线编码）应补进旧配置")
	}
	if !c.Settings.EnableValidation || !c.Settings.FilterSummaryRows || !c.Settings.AutoFillOrderLine {
		t.Fatalf("缺失的开关应按默认打开，实际 %+v", c.Settings)
	}
	if !c.TagMaterials.Enabled || len(c.TagMaterials.Prefixes) == 0 {
		t.Fatal("缺失的打标规则应整组补回")
	}
	if c.FieldMapping["work_order"]["預計產量"] != "计划数量" {
		t.Fatal("缺失的源字段映射应补回")
	}
	// 列顺序：用户已有列保持在前，缺的列追加到末尾
	wo := c.ColumnOrder["work_order"]
	if len(wo) <= 3 || wo[0] != "工单号" {
		t.Fatalf("列顺序被破坏: %v", wo)
	}
}

// TestOrdersReconcileKeepsExplicitOff 验证「用户显式关掉的开关」不会被默认值重新打开。
func TestOrdersReconcileKeepsExplicitOff(t *testing.T) {
	resetConfigNotes()
	raw := []byte(`{
		"field_mapping": {"work_order": {"製令編號": "工单号"}},
		"settings": {"enable_validation": false, "filter_summary_rows": true,
		             "auto_fill_order_line": true, "use_template_header": true},
		"tag_materials": {"enabled": false, "column": "是否需要打标",
		                  "source_field": "产品编码", "prefixes": ["61CS"], "yes": "是", "no": "否"}
	}`)
	var c OrdersConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	reconcileConfig(&c, raw)
	if c.Settings.EnableValidation {
		t.Fatal("用户显式关掉的 enable_validation 被重新打开了")
	}
	if c.TagMaterials.Enabled {
		t.Fatal("用户显式关掉的打标规则被重新打开了")
	}
}

// TestOrdersCoverageFindsUnmappedRequired 验证「必填列没有数据来源」能被识别出来。
func TestOrdersCoverageFindsUnmappedRequired(t *testing.T) {
	cfg := defaultOrdersConfig()
	req, _ := orderConfigCoverage(cfg, "work_order")
	if len(req) != 0 {
		t.Fatalf("内置默认配置不应有缺来源的必填列，实际：%v", req)
	}

	// 删掉「产线」等来源，并把某一列标成必填且无来源
	cfg.ColumnOrder["work_order"] = append(cfg.ColumnOrder["work_order"], "新增必填列")
	cfg.TemplateHeaders["新增必填列"] = "* 新增必填列"
	req, _ = orderConfigCoverage(cfg, "work_order")
	if len(req) != 1 || req[0] != "新增必填列" {
		t.Fatalf("应识别出缺来源的必填列，实际：%v", req)
	}
}

// TestMaterialUnmappedColumns 验证物料模板里「必填且没配规则」的列能被挑出来。
func TestMaterialUnmappedColumns(t *testing.T) {
	headers := []string{"* 物料编码", "* 物料名称", "英文名称", "* 新必填列"}
	required := []int{0, 1, 3}
	cfg := defaultConfig()
	req, opt := unmappedTemplateColumns(headers, required, cfg.Fields)
	if len(req) != 1 || req[0] != "新必填列" {
		t.Fatalf("应识别出未覆盖的必填列，实际：%v", req)
	}
	if len(opt) != 1 || opt[0] != "英文名称" {
		t.Fatalf("非必填未覆盖列应单独归类，实际：%v", opt)
	}
}
