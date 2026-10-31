package main

import "testing"

func TestSourceSpecIndex(t *testing.T) {
	g := &sheetGrid{Headers: []string{"父件品号", "子件品号", "用量"}}
	for spec, want := range map[string]int{"父件品号": 0, "B": 1, "3": 2, "不存在": -1, "9": -1} {
		if got := sourceSpecIndex(g, spec); got != want {
			t.Errorf("sourceSpecIndex(%q) = %d, want %d", spec, got, want)
		}
	}
}

func TestProbeBomConfigReportsMissingAndSamples(t *testing.T) {
	g := &sheetGrid{Headers: []string{"父件", "子件"}, Rows: [][]string{{"P1", "C1"}, {"P1", "C2"}}}
	items := probeBomConfig(g, &BomConfig{Columns: map[string]string{"parent": "父件", "unit": "单位"}})
	if len(items) != 2 || !items[0].Found || len(items[0].Samples) != 1 || items[1].Found {
		t.Fatalf("unexpected probes: %#v", items)
	}
}

func TestRowHasValueTreatsWhitespaceAsBlank(t *testing.T) {
	if rowHasValue([]interface{}{nil, " \t "}) {
		t.Fatal("whitespace-only row must be blank")
	}
	if !rowHasValue([]interface{}{nil, 0}) {
		t.Fatal("numeric zero is a value")
	}
}

func TestConfigModulePathUsesSeparateBackupPrefixes(t *testing.T) {
	wants := map[string]string{"mat": "mapping_", "ord": "orders_", "bom": "bom_mapping_"}
	for module, want := range wants {
		_, got, err := configModulePath(module)
		if err != nil || got != want {
			t.Fatalf("configModulePath(%q) = %q, %v; want %q", module, got, err, want)
		}
	}
}
