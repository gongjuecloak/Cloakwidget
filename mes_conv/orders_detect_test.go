package main

// 订单 / 工单自动识别的回归测试：表头归一化（繁简 / 星号 / 空白）后仍能正确分流。

import "testing"

func TestDetectSourceKindOrder(t *testing.T) {
	g := &sheetGrid{Headers: []string{"訂單單號", "品號", "訂單數量", "交貨日期", "單價", "幣別"}}
	if got := detectSourceKind(g, nil); got != "order" {
		t.Fatalf("订单表头 -> %q，期望 order", got)
	}
}

func TestDetectSourceKindWork(t *testing.T) {
	g := &sheetGrid{Headers: []string{"製令編號", "產品品號", "預計產量", "生產線別", "預計開工日"}}
	if got := detectSourceKind(g, nil); got != "work" {
		t.Fatalf("工单表头 -> %q，期望 work", got)
	}
}

func TestDetectSourceKindNormalized(t *testing.T) {
	// 带 * 必填前缀、夹空格、繁体，都要走 normCol 归一后命中
	g := &sheetGrid{Headers: []string{"* 製令編號", "產品 品號", "預計產量"}}
	if got := detectSourceKind(g, nil); got != "work" {
		t.Fatalf("规范化后 -> %q，期望 work", got)
	}
}

func TestDetectSourceKindMixedPrefersStronger(t *testing.T) {
	// 工单档里也带销售订单号时，工单特征更多，仍应判为 work
	g := &sheetGrid{Headers: []string{"訂單單號", "製令編號", "產品品號", "預計產量", "生產線別", "預計完工日"}}
	if got := detectSourceKind(g, nil); got != "work" {
		t.Fatalf("混合表头 -> %q，期望 work", got)
	}
}

func TestDetectSourceKindUnknown(t *testing.T) {
	g := &sheetGrid{Headers: []string{"姓名", "電話", "地址"}}
	if got := detectSourceKind(g, nil); got != "" {
		t.Fatalf("无关表头 -> %q，期望空串", got)
	}
}
