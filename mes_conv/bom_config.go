package main

// BOM 模块的配置。
//
// 与物料档案（mapping.json）、订单（orders.json）同一策略：模块字段体系完全不同，
// 各自一份配置文件。BOM 源是 ERP 固定格式的「BOM 抓取」大表，所以映射的最小单位是
// 「角色 -> 源列」：值可以填源表表头名（优先，按表头匹配，不依赖列序），
// 也可以填列号（如 A / J / 10），留空用内置默认列位。
//
// bom_mapping.json，默认值内嵌在 exe 里，磁盘上有同名文件则覆盖。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
)

// BomConfig BOM 模块唯一数据源（界面与转换都读它）
type BomConfig struct {
	SrcSheet    string            `json:"src_sheet"`    // 源工作表名，空用内置默认
	MasterSheet string            `json:"master_sheet"` // 输出主表名，空用内置默认
	DetailSheet string            `json:"detail_sheet"` // 输出明细表名，空用内置默认
	Columns     map[string]string `json:"columns"`      // 角色 -> 源表头名或列号（A/J/10）
	// MasterFixed 主表固定值：name_prefix（BOM 编码前缀）、bom_type、kind、unit
	MasterFixed map[string]string `json:"master_fixed"`
	// DetailFixed 明细固定值：layer（层级）、issue_type（发料方式）、allow_sub（允许替代）、main_flag（主体）
	DetailFixed map[string]string `json:"detail_fixed"`
	// UnitAliases 源单位别名（匹配时忽略大小写）-> 归一后的单位名，如 PCSQ -> PCS。
	// 优先级高于单位映射表。
	UnitAliases map[string]string `json:"unit_aliases"`
	// Basediv 转换页「基础用量归一化」复选框的默认值
	Basediv bool `json:"basediv"`
}

func bomConfigPath() string {
	return filepath.Join(exeDir(), "bom_mapping.json")
}

// bomColumnDefaults 各角色的内置列位（0 基，对应 ERP「BOM 抓取 V1.1」导出格式）：
// A 主件品号 / B 主件品名 / D 项次 / E 子件品号 / H 源單位 / J 組成用量 / K 底數 / L 損耗率
var bomColumnDefaults = map[string]int{
	"parent": 0, "parent_name": 1, "seq": 3, "child": 4,
	"unit": 7, "base": 9, "base_div": 10, "loss": 11,
}

func defaultBomConfig() *BomConfig {
	return &BomConfig{
		SrcSheet:    bomSrcSheetDefault,
		MasterSheet: bomMasterSheetDefault,
		DetailSheet: bomDetailSheetDefault,
		Columns: map[string]string{
			"parent": "A", "parent_name": "B", "seq": "D", "child": "E",
			"unit": "H", "base": "J", "base_div": "K", "loss": "L",
		},
		MasterFixed: map[string]string{
			"name_prefix": "BOM-", "bom_type": "量产BOM", "kind": "production", "unit": "支",
		},
		DetailFixed: map[string]string{
			"layer": "1", "issue_type": "定额发料", "allow_sub": "是", "main_flag": "否",
		},
		UnitAliases: map[string]string{
			"PCSQ": "PCS",
		},
		Basediv: false,
	}
}

// loadBomConfig 磁盘优先；没有或解析失败则回退内置默认配置。
func loadBomConfig() *BomConfig {
	if b, err := os.ReadFile(bomConfigPath()); err == nil && len(b) > 0 {
		var c BomConfig
		if json.Unmarshal(b, &c) == nil {
			c.fillDefaults()
			return &c
		}
	}
	return defaultBomConfig()
}

func saveBomConfig(c *BomConfig) error {
	c.fillDefaults()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(bomConfigPath(), b, 0644)
}

// ensureBomConfig 首次运行写出配置文件；返回当前生效的配置
func ensureBomConfig() *BomConfig {
	c := loadBomConfig()
	if !fileExists(bomConfigPath()) {
		_ = saveBomConfig(c)
	}
	return c
}

// fillDefaults 补齐缺失分支，避免配置里没写的键到处判空
func (c *BomConfig) fillDefaults() {
	def := defaultBomConfig()
	if c.SrcSheet == "" {
		c.SrcSheet = def.SrcSheet
	}
	if c.MasterSheet == "" {
		c.MasterSheet = def.MasterSheet
	}
	if c.DetailSheet == "" {
		c.DetailSheet = def.DetailSheet
	}
	if c.Columns == nil {
		c.Columns = def.Columns
	}
	if c.MasterFixed == nil {
		c.MasterFixed = def.MasterFixed
	}
	if c.DetailFixed == nil {
		c.DetailFixed = def.DetailFixed
	}
	if c.UnitAliases == nil {
		c.UnitAliases = def.UnitAliases
	}
}

// bomFixedVal 取固定值，空回退默认
func bomFixedVal(m map[string]string, key, def string) string {
	if v := strings.TrimSpace(m[key]); v != "" {
		return v
	}
	return def
}

var bomColLetterRe = regexp.MustCompile(`^[A-Za-z]{1,3}$`)

// bomNormKey 表头归一：去所有空白 + 转大写（繁简不做转换——ERP 导出表头稳定，写配置时照抄即可）
func bomNormKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r == ' ' || r == '\t' || r == '　' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// bomResolveColumns 把配置里的「角色 -> 表头名/列号」解析成实际列下标（0 基）。
// 规则：纯数字按 1 基列号；1-3 位字母按列标（A=第一列）；否则当表头名在源表第一行找
// （忽略空白与大小写）；找不到或配置为空时回退内置默认列位。
// 第二个返回值是「配置值没识别出来、回退了默认列位」的角色列表，供日志提示。
func bomResolveColumns(f *excelize.File, sheet string, cols map[string]string) (map[string]int, []string) {
	res := map[string]int{}
	for role, def := range bomColumnDefaults {
		res[role] = def
	}
	if len(cols) == 0 {
		return res, nil
	}
	// 读源表第一行表头，建归一化下标表
	header := map[string]int{}
	if f != nil {
		rows, err := f.GetRows(sheet)
		if err == nil && len(rows) > 0 {
			for i, h := range rows[0] {
				k := bomNormKey(h)
				if k != "" {
					if _, dup := header[k]; !dup {
						header[k] = i
					}
				}
			}
		}
	}
	var fallback []string
	for role := range bomColumnDefaults {
		v := strings.TrimSpace(cols[role])
		if v == "" {
			continue
		}
		if n, err := parsePositiveInt(v); err == nil && n >= 1 {
			res[role] = n - 1
			continue
		}
		if bomColLetterRe.MatchString(v) {
			res[role] = bomColToNum(strings.ToUpper(v)) - 1
			continue
		}
		if idx, ok := header[bomNormKey(v)]; ok {
			res[role] = idx
			continue
		}
		fallback = append(fallback, role)
	}
	return res, fallback
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, errBomNotNumber
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBomNotNumber
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, errBomNotNumber
	}
	return n, nil
}

type bomError string

func (e bomError) Error() string { return string(e) }

const errBomNotNumber = bomError("not a number")

// handlerBomGetConfig GET /api/bom/config
func handlerBomGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "config": ensureBomConfig()})
}

// handlerBomSaveConfig POST /api/bom/config（覆盖前自动备份）
func handlerBomSaveConfig(w http.ResponseWriter, r *http.Request) {
	var c BomConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "配置格式错误：" + err.Error()})
		return
	}
	if len(c.Columns) == 0 {
		writeJSON(w, map[string]interface{}{"ok": false, "error": "配置里没有源列映射，拒绝保存"})
		return
	}
	c.fillDefaults()
	if fileExists(bomConfigPath()) {
		_, _ = backupFile(bomConfigPath(), backupRoot())
	}
	if err := saveBomConfig(&c); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	appLog("BOM 模块配置已保存：%s", bomConfigPath())
	writeJSON(w, map[string]interface{}{"ok": true})
}
