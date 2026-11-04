package main

import (
	"encoding/json"
	"testing"
)

// snapshotOf 从审计快照里取某份配置的结果
func snapshotOf(t *testing.T, file string) configDiff {
	t.Helper()
	for _, d := range configDiffSnapshot() {
		if d.File == file {
			return d
		}
	}
	t.Fatalf("审计快照里没有 %s", file)
	return configDiff{}
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestMaterialAuditFillsNewRules 验证旧版 mapping.json 里缺的两类新配置能被补齐：
// 跨字段校验（cross_checks，以前完全没补）与逐键单位映射（以前整表为空才补）；
// 用户改过的字段规则不能被默认值覆盖，且差异要能分类记进审计。
func TestMaterialAuditFillsNewRules(t *testing.T) {
	resetConfigNotes()
	resetConfigDiffs()

	raw := []byte(`{
		"source_sheet": 0,
		"template_sheet": 0,
		"output_sheet": "物料档案",
		"fields": [
			{"target": "物料编码", "source": "MB001", "op": "copy", "trim": true},
			{"target": "物料名称", "source": "MB002", "op": "copy"}
		],
		"dicts": {"matType": {"110": "成品"}},
		"unit_map": {"kg": "千克"},
		"value_whitelist": {"基本单位": ["千克"]}
	}`)
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if !migrateMaterialConfig(&c, raw) {
		t.Fatal("旧配置应发生补齐")
	}

	// 以前完全没补的两类：跨字段校验、逐键单位映射
	if len(c.CrossChecks) == 0 {
		t.Fatal("cross_checks 应被补齐（默认的「序列管理=是 → 序列号规则编码」）")
	}
	if c.UnitMap["m"] != "米" {
		t.Fatalf("unit_map 里缺的键应逐条补齐，实际：%v", c.UnitMap)
	}
	if c.UnitMap["kg"] != "千克" {
		t.Fatal("已有的单位映射被改动了")
	}
	if len(c.ValueWhitelist["基本单位"]) <= 1 {
		t.Fatalf("白名单应补入默认单位，实际：%v", c.ValueWhitelist["基本单位"])
	}

	// 用户改过的规则保持原样：这里「物料名称」故意没写 raw，默认是 raw=true
	for _, f := range c.Fields {
		if f.Target == "物料名称" && f.Raw {
			t.Fatal("用户改过的字段规则被默认值覆盖了（raw 被改回 true）")
		}
	}

	// 缺的字段规则要补，已有的不动
	if len(c.Fields) != len(defaultConfig().Fields) {
		t.Fatalf("字段规则应补到默认条数，实际 %d", len(c.Fields))
	}

	// 审计分类：补齐项进 Filled，用户改过的进 Custom
	d := snapshotOf(t, "mapping.json")
	if !containsStr(d.Filled, "cross_checks") {
		t.Fatalf("审计里应记录 cross_checks 已补齐，实际：%v", d.Filled)
	}
	if !containsStr(d.Custom, "fields.物料名称") {
		t.Fatalf("审计里应把用户改过的字段规则记为 custom，实际：%v", d.Custom)
	}

	// 收敛：写回再读一遍不能再有改动（否则每次启动都备份 + 重写）
	b, err := json.Marshal(&c)
	if err != nil {
		t.Fatal(err)
	}
	var c2 Config
	if err := json.Unmarshal(b, &c2); err != nil {
		t.Fatal(err)
	}
	if migrateMaterialConfig(&c2, b) {
		t.Fatal("第二次迁移不应再有改动（必须收敛）")
	}
}

// TestMaterialAuditKeepsExplicitEmpty 验证「用户显式写空数组关掉检查」不会被默认值重新打开。
func TestMaterialAuditKeepsExplicitEmpty(t *testing.T) {
	resetConfigNotes()
	resetConfigDiffs()

	raw := []byte(`{
		"output_sheet": "物料档案",
		"fields": [{"target": "物料编码", "source": "MB001", "op": "copy"}],
		"duplicate_keys": [],
		"cross_checks": []
	}`)
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	migrateMaterialConfig(&c, raw)
	if len(c.DuplicateKeys) != 0 {
		t.Fatalf("用户写空数组关掉的查重被重新打开了：%v", c.DuplicateKeys)
	}
	if len(c.CrossChecks) != 0 {
		t.Fatalf("用户写空数组关掉的跨字段校验被重新打开了：%v", c.CrossChecks)
	}
}

// TestOrdersAuditFillsAliasesAndDuplicateKeys 验证同义词组只补组内缺项、duplicate_keys 补齐，
// 并确认第二次 reconcile 不再产生改动。
func TestOrdersAuditFillsAliasesAndDuplicateKeys(t *testing.T) {
	resetConfigNotes()
	resetConfigDiffs()

	raw := []byte(`{
		"schema": 1,
		"field_mapping": {"work_order": {"製令編號": "工单号"}},
		"column_aliases": {"工单号": ["製令編號"]},
		"column_order": {"work_order": ["工单号"]}
	}`)
	var c OrdersConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if !reconcileConfig(&c, raw) {
		t.Fatal("旧配置应发生补齐")
	}

	def := defaultOrdersConfig()
	for _, want := range def.ColumnAliases["工单号"] {
		if !containsStr(c.ColumnAliases["工单号"], want) {
			t.Fatalf("同义词组里的 %q 应被补齐，实际：%v", want, c.ColumnAliases["工单号"])
		}
	}
	if !containsStr(c.ColumnAliases["工单号"], "製令編號") {
		t.Fatal("用户原有的同义词被删掉了")
	}
	// 默认配置没有显式查重键（留空 = 用代码内置默认），不该往用户配置里塞空对象
	if c.DuplicateKeys != nil && len(c.DuplicateKeys) == 0 {
		t.Fatal("默认没有查重键时不应写入空对象")
	}

	d := snapshotOf(t, "orders.json")
	if len(d.Filled) == 0 {
		t.Fatal("审计里应记录补齐项")
	}

	b, err := json.Marshal(&c)
	if err != nil {
		t.Fatal(err)
	}
	var c2 OrdersConfig
	if err := json.Unmarshal(b, &c2); err != nil {
		t.Fatal(err)
	}
	if reconcileConfig(&c2, b) {
		t.Fatal("第二次 reconcile 不应再产生改动（必须收敛）")
	}
}

// TestBomAuditFillsMissingRole 验证 BOM 配置只补缺失角色，不动已有列位。
func TestBomAuditFillsMissingRole(t *testing.T) {
	resetConfigNotes()
	resetConfigDiffs()

	c := &BomConfig{Columns: map[string]string{"parent": "A", "child": "E"}}
	if !reconcileBomConfig(c) {
		t.Fatal("缺角色的配置应发生补齐")
	}
	if c.Columns["child"] != "E" {
		t.Fatalf("用户已有的列位被改了：%v", c.Columns)
	}
	if c.Columns["unit"] != "H" {
		t.Fatalf("缺失的角色应补默认列位，实际：%v", c.Columns)
	}
	if len(c.MasterFixed) == 0 || len(c.DetailFixed) == 0 || len(c.UnitAliases) == 0 {
		t.Fatal("固定值 / 单位别名应一并补齐")
	}

	d := snapshotOf(t, "bom_mapping.json")
	if !containsStr(d.Filled, "columns") {
		t.Fatalf("审计里应记录 columns 已补齐，实际：%v", d.Filled)
	}

	if reconcileBomConfig(c) {
		t.Fatal("第二次 reconcile 不应再产生改动（必须收敛）")
	}
}

// TestConfigDiffClassifiesLocalCustom 验证审计能把「本地自定义」与「本地独有」分开报出来。
func TestConfigDiffClassifiesLocalCustom(t *testing.T) {
	resetConfigDiffs()

	c := defaultConfig()
	c.OutputSheet = "我的物料档案"                                                        // 与默认不同 → custom
	c.Fields = append(c.Fields, FieldRule{Target: "自定义列", Op: "fixed", Value: "X"}) // 默认没有 → only_local
	recordConfigDiff(materialConfigDiff(c, nil))

	d := snapshotOf(t, "mapping.json")
	if !containsStr(d.Custom, "output_sheet") {
		t.Fatalf("本地改过的 output_sheet 应记为 custom，实际：%v", d.Custom)
	}
	if !containsStr(d.OnlyLocal, "fields.自定义列") {
		t.Fatalf("本地独有的字段规则应记为 only_local，实际：%v", d.OnlyLocal)
	}
	if len(d.Filled) != 0 {
		t.Fatalf("没补齐任何项时 Filled 应为空，实际：%v", d.Filled)
	}
}
