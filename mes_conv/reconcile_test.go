package main

import "testing"

// TestReconcileConfigAppendsNewColumns 验证盘上旧配置加载后，内置默认里新增的模板列
// （如 工艺路线编码）会被补回 ColumnOrder 末尾，且不会丢失用户已有的列顺序。
func TestReconcileConfigAppendsNewColumns(t *testing.T) {
	c := &OrdersConfig{
		FieldMapping: map[string]map[string]string{
			"work_order": {"製令編號": "工单号"},
		},
		ColumnOrder: map[string][]string{
			"work_order": {
				"工单号", "工单生产批次号", "来源单号", "销售订单", "销售订单行号",
				"计划数量", "计划开始", "计划完成", "优先级别", "备注信息",
				"紧急标识", "是否需要打标", "产品编码", "物料编码", "产线",
			},
		},
	}
	reconcileConfig(c)
	wo := c.ColumnOrder["work_order"]
	if len(wo) != 16 {
		t.Fatalf("work_order 应为 16 列, 实际 %d: %v", len(wo), wo)
	}
	if wo[len(wo)-1] != "工艺路线编码" {
		t.Fatalf("新增列应追加到末尾, 实际末列=%q", wo[len(wo)-1])
	}
	// 用户已有列顺序应保持（工艺路线编码插入前的顺序不变）
	wantPrefix := []string{
		"工单号", "工单生产批次号", "来源单号", "销售订单", "销售订单行号",
		"计划数量", "计划开始", "计划完成", "优先级别", "备注信息",
		"紧急标识", "是否需要打标", "产品编码", "物料编码", "产线",
	}
	for i, w := range wantPrefix {
		if wo[i] != w {
			t.Fatalf("列顺序错位 @%d: want=%q got=%q", i, w, wo[i])
		}
	}
	// TemplateHeaders 缺口也应补齐（工艺路线编码不在必填映射里，故不强制，但其他默认项应存在）
	if c.TemplateHeaders["工单号"] != "* 工单号" {
		t.Fatalf("TemplateHeaders 缺口未补齐: 工单号=%q", c.TemplateHeaders["工单号"])
	}
}
