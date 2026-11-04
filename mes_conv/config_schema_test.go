package main

import (
	"encoding/json"
	"strings"
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

// TestOrdersReconcileFillsTagPrefixesElementWise 验证打标前缀按「元素」补齐：
// 内置默认里有、盘上缺的都补上，用户自己加的前缀与已有顺序不动，且迁移收敛。
func TestOrdersReconcileFillsTagPrefixesElementWise(t *testing.T) {
	resetConfigNotes()
	raw := []byte(`{
		"schema": 1,
		"field_mapping": {"work_order": {"製令編號": "工单号"}},
		"tag_materials": {"enabled": true, "column": "是否需要打标",
		                  "source_field": "产品编码",
		                  "prefixes": ["ZZ01", "61CS"], "yes": "是", "no": "否"}
	}`)
	var c OrdersConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if !reconcileConfig(&c, raw) {
		t.Fatal("存量配置缺默认前缀，应发生补齐")
	}
	defs := defaultOrdersConfig().TagMaterials.Prefixes
	for _, p := range defs {
		if !hasTagPrefix(c.TagMaterials.Prefixes, p) {
			t.Fatalf("默认前缀 %s 应被补齐，实际：%v", p, c.TagMaterials.Prefixes)
		}
	}
	if len(c.TagMaterials.Prefixes) != len(defs)+1 {
		t.Fatalf("应补齐 %d 条默认前缀并保留 1 条自定义，实际：%v", len(defs), c.TagMaterials.Prefixes)
	}
	// 用户原有的条目与顺序保持在前，新增的追加在后面
	if c.TagMaterials.Prefixes[0] != "ZZ01" || c.TagMaterials.Prefixes[1] != "61CS" {
		t.Fatalf("不应打乱用户已有的前缀顺序：%v", c.TagMaterials.Prefixes)
	}

	// 收敛：写回后再读一遍不能再有改动，否则每次启动都会备份 + 重写一遍配置
	b, err := json.Marshal(&c)
	if err != nil {
		t.Fatal(err)
	}
	var c2 OrdersConfig
	if err := json.Unmarshal(b, &c2); err != nil {
		t.Fatal(err)
	}
	if reconcileConfig(&c2, b) {
		t.Fatal("第二次 reconcile 不应再产生改动（迁移必须收敛）")
	}
}

// TestOrdersReconcileRespectsPrefixesOff 验证「本机停用」的前缀不会被逐元素补全加回来，
// 以及填回 prefixes 之后停用记录会自动清掉。
func TestOrdersReconcileRespectsPrefixesOff(t *testing.T) {
	resetConfigNotes()
	defs := defaultOrdersConfig().TagMaterials.Prefixes
	allBut := func(skip string) string {
		out := make([]string, 0, len(defs))
		for _, p := range defs {
			if !strings.EqualFold(p, skip) {
				out = append(out, `"`+p+`"`)
			}
		}
		return strings.Join(out, ",")
	}

	// 已经齐全、只差一条主动停用的：不该补回，也不该产生任何其它改动
	raw := []byte(`{
		"schema": 1,
		"field_mapping": {"work_order": {"製令編號": "工单号"}},
		"tag_materials": {"enabled": true, "column": "是否需要打标",
		                  "source_field": "产品编码",
		                  "prefixes": [` + allBut("6CR") + `],
		                  "prefixes_off": ["6cr"], "yes": "是", "no": "否"}
	}`)
	var c OrdersConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	before := append([]string(nil), c.TagMaterials.Prefixes...)
	reconcileConfig(&c, raw) // 其余小节（列顺序等）也会被补齐，这里只看前缀
	if !sameStringSlice(before, c.TagMaterials.Prefixes) {
		t.Fatalf("停用的前缀不该被补回：before=%v after=%v", before, c.TagMaterials.Prefixes)
	}
	if hasTagPrefix(c.TagMaterials.Prefixes, "6CR") {
		t.Fatal("prefixes_off 里的前缀被补回来了")
	}

	// 缺多条时：除了停用的，其余都补；停用的仍然缺席
	raw2 := []byte(`{
		"schema": 1,
		"field_mapping": {"work_order": {"製令編號": "工单号"}},
		"tag_materials": {"enabled": true, "column": "是否需要打标",
		                  "source_field": "产品编码",
		                  "prefixes": ["61CS"], "prefixes_off": ["6CR"], "yes": "是", "no": "否"}
	}`)
	var c2 OrdersConfig
	if err := json.Unmarshal(raw2, &c2); err != nil {
		t.Fatal(err)
	}
	if !reconcileConfig(&c2, raw2) {
		t.Fatal("缺默认前缀应补齐")
	}
	if hasTagPrefix(c2.TagMaterials.Prefixes, "6CR") {
		t.Fatalf("停用的 6CR 不该被补回：%v", c2.TagMaterials.Prefixes)
	}
	for _, p := range defs {
		if strings.EqualFold(p, "6CR") {
			continue
		}
		if !hasTagPrefix(c2.TagMaterials.Prefixes, p) {
			t.Fatalf("默认前缀 %s 应被补齐：%v", p, c2.TagMaterials.Prefixes)
		}
	}

	// 填回 prefixes = 恢复启用：停用记录被清掉，配置文件只改这一处
	var c3 OrdersConfig
	if err := json.Unmarshal(raw2, &c3); err != nil {
		t.Fatal(err)
	}
	c3.TagMaterials.Prefixes = append(c3.TagMaterials.Prefixes, "6CR")
	if !reconcileConfig(&c3, raw2) {
		t.Fatal("恢复启用后应清掉停用记录")
	}
	if len(c3.TagMaterials.PrefixesOff) != 0 {
		t.Fatalf("已启用的前缀不该留在停用表：%v", c3.TagMaterials.PrefixesOff)
	}
	n := 0
	for _, p := range c3.TagMaterials.Prefixes {
		if strings.EqualFold(p, "6CR") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("恢复启用后 6CR 应正好出现一次，实际 %d 次：%v", n, c3.TagMaterials.Prefixes)
	}
}

// TestMergePrefixesOff 验证保存配置时「删掉哪个内置前缀就记哪个」：
// 这是「暂时不用」的唯一入口 —— 不做记录的话，逐元素补全会在下次启动时加回来。
func TestMergePrefixesOff(t *testing.T) {
	old := exeDirOverride
	exeDirOverride = t.TempDir()
	t.Cleanup(func() { exeDirOverride = old })

	disk := defaultOrdersConfig()
	disk.TagMaterials.Enabled = true
	if err := saveOrdersConfig(disk); err != nil {
		t.Fatal(err)
	}

	// 删掉 6CR 后保存 → 记进停用表
	sub := defaultOrdersConfig()
	sub.TagMaterials.Enabled = true
	kept := make([]string, 0, len(sub.TagMaterials.Prefixes))
	for _, p := range sub.TagMaterials.Prefixes {
		if !strings.EqualFold(p, "6CR") {
			kept = append(kept, p)
		}
	}
	sub.TagMaterials.Prefixes = kept
	mergePrefixesOff(sub)
	if len(sub.TagMaterials.PrefixesOff) != 1 || !hasTagPrefix(sub.TagMaterials.PrefixesOff, "6CR") {
		t.Fatalf("删掉的内置前缀应记入停用表，实际：%v", sub.TagMaterials.PrefixesOff)
	}

	// 把 6CR 填回来再保存 → 停用记录清掉
	back := defaultOrdersConfig()
	back.TagMaterials.Enabled = true
	back.TagMaterials.PrefixesOff = []string{"6CR"}
	mergePrefixesOff(back)
	if len(back.TagMaterials.PrefixesOff) != 0 {
		t.Fatalf("恢复启用后停用表应为空，实际：%v", back.TagMaterials.PrefixesOff)
	}

	// 打标整组关掉 → 停用表没有意义，清空
	off := defaultOrdersConfig()
	off.TagMaterials.Enabled = false
	off.TagMaterials.PrefixesOff = []string{"6CR"}
	mergePrefixesOff(off)
	if off.TagMaterials.PrefixesOff != nil {
		t.Fatalf("关掉打标后停用表应清空，实际：%v", off.TagMaterials.PrefixesOff)
	}
}
