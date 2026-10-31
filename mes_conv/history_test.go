package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendHistoryFileKeepsAllForCumulativeStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	for i := 0; i < 52; i++ {
		it := HistoryItem{Time: fmt.Sprintf("2026-10-31 10:%02d:00", i), File: fmt.Sprintf("bom-%02d.xlsx", i)}
		if err := appendHistoryFile(path, it); err != nil {
			t.Fatal(err)
		}
	}

	got := loadHistoryFile(path)
	if len(got) != 52 {
		t.Fatalf("history length = %d, want 52", len(got))
	}
	if got[0].File != "bom-51.xlsx" || got[51].File != "bom-00.xlsx" {
		t.Fatalf("history order wrong: first=%q last=%q", got[0].File, got[51].File)
	}
}

func TestBackupRetentionIsPerModuleAndNamesAreUnique(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mapping.json")
	if err := os.WriteFile(src, []byte(`{"schema":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	b1, err := backupFile(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := backupFile(src, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b1 == b2 {
		t.Fatalf("同一秒备份名必须唯一：%q", b1)
	}
	for _, name := range []string{"orders_1.json", "orders_2.json", "orders_3.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	pruneBackups(dir, "mapping_", 1)
	if _, err := os.Stat(filepath.Join(dir, "orders_1.json")); err != nil {
		t.Fatalf("清理 mapping 备份不应删除 orders 备份：%v", err)
	}
}

func TestOutputIsExternal(t *testing.T) {
	old := exeDirOverride
	exeDirOverride = t.TempDir()
	t.Cleanup(func() { exeDirOverride = old })
	if outputIsExternal(filepath.Join(exeDir(), "out", "ok.xlsx")) {
		t.Fatal("默认 out 目录内的 CLI 输出应可下载")
	}
	if !outputIsExternal(filepath.Join(exeDir(), "custom", "result.xlsx")) {
		t.Fatal("自定义目录输出应标记为 external")
	}
}

func TestPruneReportsUsesModificationTime(t *testing.T) {
	old := exeDirOverride
	exeDirOverride = t.TempDir()
	t.Cleanup(func() { exeDirOverride = old })
	if err := os.MkdirAll(reportsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range []string{"z-old.json", "a-middle.json", "b-new.json"} {
		path := filepath.Join(reportsDir(), name)
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	pruneReports(2)
	if _, err := os.Stat(filepath.Join(reportsDir(), "z-old.json")); !os.IsNotExist(err) {
		t.Fatalf("应按修改时间删除最旧报告，got=%v", err)
	}
}

func TestBomHistoryRecoversExistingReportsWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	reports := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reports, 0755); err != nil {
		t.Fatal(err)
	}
	writeReport := func(name string, r ConvertReport) {
		t.Helper()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(reports, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeReport("old-bom.json", ConvertReport{
		ID: "old-bom.json", Time: "2026-10-30 09:00:00", Module: "bom",
		Output: "old-bom.xlsx", Rows: 18, IssueTotal: 2, WarningTotal: 7, Warnings: []string{"单位告警"},
	})
	writeReport("material.json", ConvertReport{
		ID: "material.json", Time: "2026-10-31 09:00:00", Module: "materials", Output: "material.xlsx",
	})

	recovered := historyFromReports(reports, "bom")
	if len(recovered) != 1 || recovered[0].File != "old-bom.xlsx" || recovered[0].Issues != 2 || recovered[0].Warnings != 7 {
		t.Fatalf("unexpected recovered history: %#v", recovered)
	}
	merged := mergeHistories([]HistoryItem{{Report: "old-bom.json", File: "from-history.xlsx", Module: "bom"}}, recovered)
	if len(merged) != 1 || merged[0].File != "from-history.xlsx" {
		t.Fatalf("explicit history should win and deduplicate report: %#v", merged)
	}
}

func TestBomHistoryCounts(t *testing.T) {
	rep := &BomReport{
		EmptySeq: 1, NonNumSeq: 2, EmptyChild: 3,
		UnitStats: []BomUnitStat{
			{Raw: "PCS", Status: "OK", Count: 4},
			{Raw: "KG", Status: "AMBIG_RESOLVED", Count: 5},
			{Raw: "unmapped", Status: "unmapped", Count: 6},
			{Raw: "AMBIG", Mapped: "MES-A", Status: "AMBIG_FALLBACK", Count: 7},
		},
	}
	issues, warnings := bomHistoryCounts(rep)
	if issues != 6 || warnings != 13 {
		t.Fatalf("counts = (%d, %d), want (6, 13)", issues, warnings)
	}
	messages, total := bomUnitWarnings(rep, bomMsgs("zh"))
	if total != 13 || len(messages) != 2 {
		t.Fatalf("unit warnings = (%d, %#v), want total 13 and 2 messages", total, messages)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "unmapped") || !strings.Contains(joined, "MES-A") {
		t.Fatalf("单位告警应说明原单位与实际采用值：%s", joined)
	}
}
