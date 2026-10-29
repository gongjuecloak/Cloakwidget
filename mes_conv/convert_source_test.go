package main

import "testing"

// 源表指纹：选错文件时要能识别出「一个映射源列都没对上」。
func TestSourceHit(t *testing.T) {
	cfg := &Config{Fields: []FieldRule{
		{Target: "物料编码", Source: "MB001"},
		{Target: "物料名称", Source: "MB002"},
		{Target: "物料规格", Source: "MB001"}, // 重复引用只算一次
		{Target: "显示顺序", Op: "rownum"},   // 不依赖源列
	}}

	want, hit := sourceHit(cfg, map[string]int{"MB001": 0, "MB002": 1, "MB003": 2})
	if want != 2 || hit != 2 {
		t.Fatalf("全命中：want=2 hit=2，实际 want=%d hit=%d", want, hit)
	}

	want, hit = sourceHit(cfg, map[string]int{"品号": 0, "品名": 1, "规格": 2})
	if want != 2 || hit != 0 {
		t.Fatalf("全不命中：want=2 hit=0，实际 want=%d hit=%d", want, hit)
	}

	// 配置里没有任何源列（全靠 fixed/rownum）时无从判断，不能拦
	plain := &Config{Fields: []FieldRule{{Target: "显示顺序", Op: "rownum"}}}
	if want, hit := sourceHit(plain, map[string]int{"X": 0}); want != 0 || hit != 0 {
		t.Fatalf("无源列应为 0/0，实际 %d/%d", want, hit)
	}
}

func TestHeaderPreview(t *testing.T) {
	if got := headerPreview([]string{"品号", "", "品名", "规格", "数量", "单位", "备注", "额外"}, 6); got != "品号、品名、规格、数量、单位、备注" {
		t.Fatalf("前 6 个非空表头，实际 %q", got)
	}
	if got := headerPreview([]string{"", "  "}, 6); got != "-" {
		t.Fatalf("空表头应返回占位符，实际 %q", got)
	}
	if got := headerPreview([]string{"A"}, 6); got != "A" {
		t.Fatalf("只有一个表头，实际 %q", got)
	}
}
