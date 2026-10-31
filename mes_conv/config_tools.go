package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type configProbe struct {
	Group   string   `json:"group,omitempty"`
	Target  string   `json:"target"`
	Source  string   `json:"source"`
	Found   bool     `json:"found"`
	Samples []string `json:"samples"`
}

func probeSamples(g *sheetGrid, col int) []string {
	if col < 0 {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, row := range g.Rows {
		v := strings.TrimSpace(cell(row, col))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) == 5 {
			break
		}
	}
	return out
}

func sourceSpecIndex(g *sheetGrid, spec string) int {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return -1
	}
	if n, err := strconv.Atoi(spec); err == nil && n > 0 {
		if n <= len(g.Headers) {
			return n - 1
		}
		return -1
	}
	if bomColLetterRe.MatchString(spec) {
		i := bomColToNum(strings.ToUpper(spec)) - 1
		if i >= 0 && i < len(g.Headers) {
			return i
		}
		return -1
	}
	want := bomNormKey(spec)
	for i, h := range g.Headers {
		if bomNormKey(h) == want {
			return i
		}
	}
	return -1
}

func probeBomConfig(g *sheetGrid, c *BomConfig) []configProbe {
	keys := make([]string, 0, len(c.Columns))
	for k := range c.Columns {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]configProbe, 0, len(keys))
	for _, role := range keys {
		spec := c.Columns[role]
		col := sourceSpecIndex(g, spec)
		out = append(out, configProbe{Group: "bom", Target: role, Source: spec, Found: col >= 0, Samples: probeSamples(g, col)})
	}
	return out
}

func probeOrdersConfig(g *sheetGrid, c *OrdersConfig) []configProbe {
	cats := make([]string, 0, len(c.FieldMapping))
	for cat := range c.FieldMapping {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	var out []configProbe
	for _, cat := range cats {
		sources := make([]string, 0, len(c.FieldMapping[cat]))
		for source := range c.FieldMapping[cat] {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			col := resolveColIndex(g, source, c.ColumnAliases)
			out = append(out, configProbe{Group: cat, Target: c.FieldMapping[cat][source], Source: source, Found: col >= 0, Samples: probeSamples(g, col)})
		}
	}
	return out
}

func probeMaterialConfig(g *sheetGrid, c *Config) []configProbe {
	var out []configProbe
	for _, f := range c.Fields {
		if strings.TrimSpace(f.Source) == "" {
			continue
		}
		col := -1
		if i, ok := g.normIndex()[normCol(f.Source)]; ok {
			col = i
		}
		out = append(out, configProbe{Group: "materials", Target: f.Target, Source: f.Source, Found: col >= 0, Samples: probeSamples(g, col)})
	}
	return out
}

func handlerConfigTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		writeUploadParseError(w, err)
		return
	}
	head, fileHeader, err := r.FormFile("source")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "缺少源文件"})
		return
	}
	head.Close()
	dir, err := os.MkdirTemp("", "mes-config-test-*")
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer os.RemoveAll(dir)
	ext := filepath.Ext(fileHeader.Filename)
	if ext == "" {
		ext = ".xlsx"
	}
	path := filepath.Join(dir, "source"+ext)
	f, err := fileHeader.Open()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	err = saveUpload(f, path)
	f.Close()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	g, err := readGridAuto(path)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	module := r.FormValue("module")
	configJSON := []byte(r.FormValue("config"))
	var items []configProbe
	switch module {
	case "bom":
		c := defaultBomConfig()
		if len(configJSON) > 0 && json.Unmarshal(configJSON, c) != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "BOM 配置格式错误"})
			return
		}
		items = probeBomConfig(g, c)
	case "ord", "orders":
		c := defaultOrdersConfig()
		if len(configJSON) > 0 && json.Unmarshal(configJSON, c) != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "订单配置格式错误"})
			return
		}
		c.fillDefaults()
		items = probeOrdersConfig(g, c)
	default:
		c := defaultConfig()
		if len(configJSON) > 0 && json.Unmarshal(configJSON, c) != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "物料配置格式错误"})
			return
		}
		items = probeMaterialConfig(g, c)
	}
	matched := 0
	for _, item := range items {
		if item.Found {
			matched++
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "sheet": g.SheetName, "rows": len(g.Rows), "headers": g.Headers,
		"items": items, "matched": matched, "missing": len(items) - matched,
		"summary": fmt.Sprintf("匹配 %d / 缺失 %d", matched, len(items)-matched),
	})
}
