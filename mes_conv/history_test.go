package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendHistoryFileKeepsNewestFifty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	for i := 0; i < 52; i++ {
		it := HistoryItem{Time: fmt.Sprintf("2026-10-31 10:%02d:00", i), File: fmt.Sprintf("bom-%02d.xlsx", i)}
		if err := appendHistoryFile(path, it); err != nil {
			t.Fatal(err)
		}
	}

	got := loadHistoryFile(path)
	if len(got) != 50 {
		t.Fatalf("history length = %d, want 50", len(got))
	}
	if got[0].File != "bom-51.xlsx" || got[49].File != "bom-02.xlsx" {
		t.Fatalf("history order/cap wrong: first=%q last=%q", got[0].File, got[49].File)
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
		Output: "old-bom.xlsx", Rows: 18, IssueTotal: 2,
	})
	writeReport("material.json", ConvertReport{
		ID: "material.json", Time: "2026-10-31 09:00:00", Module: "materials", Output: "material.xlsx",
	})

	recovered := historyFromReports(reports, "bom")
	if len(recovered) != 1 || recovered[0].File != "old-bom.xlsx" || recovered[0].Issues != 2 {
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
			{Status: "OK", Count: 4},
			{Status: "AMBIG_RESOLVED", Count: 5},
			{Status: "unmapped", Count: 6},
			{Status: "AMBIG_FALLBACK", Count: 7},
		},
	}
	issues, warnings := bomHistoryCounts(rep)
	if issues != 6 || warnings != 13 {
		t.Fatalf("counts = (%d, %d), want (6, 13)", issues, warnings)
	}
}
