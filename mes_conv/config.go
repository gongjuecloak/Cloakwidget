package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FieldRule 描述一个目标列的取值规则
type FieldRule struct {
	Target  string            `json:"target"`             // 目标列（模板干净列名，不带 "* "）
	Source  string            `json:"source,omitempty"`   // 源 MB 列，如 "MB001"；fixed/rownum 时为空
	Op      string            `json:"op"`                 // copy | dict | num | fixed | rownum | cond | kwbool | kwmap | unit | case
	Trim    bool              `json:"trim,omitempty"`     // copy 时是否去首尾空格
	Raw     bool              `json:"raw,omitempty"`      // copy 时保留原始首尾空格/换行(不 trim)
	Default string            `json:"default,omitempty"`  // copy 时源为空则填此默认值
	Dict    string            `json:"dict,omitempty"`     // op=dict 时使用的字典名
	Value   string            `json:"value,omitempty"`    // op=fixed 时的常量
	If      string            `json:"if,omitempty"`       // op=cond：源值等于 If（忽略大小写）则 Then，否则 Else
	Then    string            `json:"then,omitempty"`
	Else    string            `json:"else,omitempty"`
	Keywords   []string            `json:"keywords,omitempty"`    // op=kwbool/kwmap：按此顺序匹配关键词
	KWMap      map[string]string   `json:"kwmap,omitempty"`       // op=kwmap：关键词 -> 写入值
	Cases      []WhenCase          `json:"cases,omitempty"`       // op=case：多条件，按顺序首命中
	DefaultOut string              `json:"default_out,omitempty"` // op=case：全不命中时写入
}

// WhenCase 描述 op=case 的一个分支条件
type WhenCase struct {
	Source   string   `json:"source,omitempty"`   // 源 MB 列
	Match    string   `json:"match,omitempty"`    // "eq" 等于(忽略大小写) | "contains" 包含
	Value    string   `json:"value,omitempty"`    // match=eq 时的比较值；或 contains 时的单关键词
	Keywords []string `json:"keywords,omitempty"` // match=contains 时的关键词列表(任一命中即可)
	Then     string   `json:"then,omitempty"`     // 命中后写入值
}

// Config 是工具的唯一数据源：UI 与 CLI 都读它
type Config struct {
	SourceSheet   int                          `json:"source_sheet"`   // 源数据工作表索引，-1 表示第一个
	TemplateSheet int                          `json:"template_sheet"` // 模板工作表索引
	OutputSheet   string                       `json:"output_sheet"`   // 输出工作表名
	Fields        []FieldRule                  `json:"fields"`
	Dicts         map[string]map[string]string `json:"dicts"`
	UnitMap       map[string]string            `json:"unit_map"` // 基本单位翻译：源符号(小写) -> 单位名
	// ValueWhitelist 目标列 -> 允许值清单。写出的值不在清单中时只「告警」(不影响输出)，
	// 用于提前发现会导致 MES 导入失败的值（例如基本单位不在 MES 单位档案里）。
	ValueWhitelist map[string][]string `json:"value_whitelist,omitempty"`
}

// exeDir 返回可执行文件所在目录（配置文件放在这里）
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func configPath() string {
	return filepath.Join(exeDir(), "mapping.json")
}

func loadConfig() (*Config, error) {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveConfig(c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0644)
}

// defaultConfig 内置默认映射，首次运行且 mapping.json 不存在时写出
func defaultConfig() *Config {
	return &Config{
		SourceSheet:   0,
		TemplateSheet: 0,
		OutputSheet:   "物料档案",
		Fields: []FieldRule{
			{Target: "物料编码", Source: "MB001", Op: "copy", Trim: true},
			{Target: "物料名称", Source: "MB002", Op: "copy", Raw: true},
			{Target: "物料规格", Source: "MB003", Op: "copy", Raw: true},
			{Target: "显示顺序", Op: "rownum"},
			{Target: "物料分类", Source: "MB142", Op: "copy", Default: "臨時分類"},
			{Target: "物料类型", Source: "MB005", Op: "dict", Dict: "matType"},
			{Target: "基本单位", Source: "MB004", Op: "unit"},
			{Target: "物料来源", Source: "MB025", Op: "dict", Dict: "matSource"},
			{Target: "序列号规则编码", Op: "fixed", Value: "SN001"},
			{Target: "默认发料仓库", Op: "fixed", Value: "虚拟仓"},
			{Target: "默认供应商", Source: "MB032", Op: "copy"},
			{Target: "批次管理", Source: "MB022", Op: "cond", If: "N", Then: "否", Else: "是"},
			{Target: "序列管理", Op: "fixed", Value: "是"},
			{Target: "保质期天数", Source: "MB023", Op: "num"},
			{Target: "时效物料", Op: "fixed", Value: "否"},
			{Target: "危险品标识", Op: "fixed", Value: "否"},
			{Target: "关键物料", Op: "fixed", Value: "否"},
			{Target: "物料状态", Op: "fixed", Value: "正常"},
			{Target: "ABC分类", Source: "MB027", Op: "num"},
			{Target: "虚拟件标识", Op: "fixed", Value: "否"},
			{Target: "虚设件标识", Op: "fixed", Value: "否"},
			{Target: "可采购标识", Op: "fixed", Value: "否"},
			{Target: "可生产标识", Op: "case",
				Cases: []WhenCase{
					{Source: "MB005", Match: "eq", Value: "110", Then: "是"},
					{Source: "MB002", Match: "contains",
						Keywords: []string{"裁切", "預型", "成型", "膠合", "補磨", "塗裝"}, Then: "是"},
				}, DefaultOut: "否"},
			{Target: "可销售标识", Op: "fixed", Value: "是"},
			{Target: "可委外标识", Op: "fixed", Value: "否"},
			{Target: "工艺路线名称", Op: "case",
				Cases: []WhenCase{
					{Source: "MB005", Match: "eq", Value: "110", Then: "包装(đóng gói)"},
					{Source: "MB002", Match: "contains", Keywords: []string{"裁切"}, Then: "裁切(cắt xén)"},
					{Source: "MB002", Match: "contains", Keywords: []string{"預型"}, Then: "预型(tiền định hình)"},
					{Source: "MB002", Match: "contains", Keywords: []string{"成型"}, Then: "成型(Công đoạn tạo hình)"},
					{Source: "MB002", Match: "contains", Keywords: []string{"膠合"}, Then: "胶合(dán keo / ép keo)"},
					{Source: "MB002", Match: "contains", Keywords: []string{"補磨"}, Then: "补土(trét bả)"},
					{Source: "MB002", Match: "contains", Keywords: []string{"塗裝"}, Then: "涂装(Sơn khung xe)"},
				}, DefaultOut: ""},
		},
		Dicts: map[string]map[string]string{
			"matType": {
				"110": "成品", "120": "原材料", "130": "耗材", "140": "客供品",
				"150": "开发试作半成品", "160": "半成品",
				"170": "费用类(五金/模具/开版)", "171": "收入类(模具/开版请款)",
				"180": "管销类(事务性用品)", "190": "托工类(耗用内部材料)",
				"191": "托工类(纯托工)", "210": "房屋建築", "211": "机器设备",
				"212": "运输设备", "213": "办公设备", "214": "其它固资",
			},
			"matSource": {"M": "自制", "P": "外购", "S": "委外"},
		},
		// 源符号(小写) -> MES 单位档案中的「单位名称」。
		// 只保留「目标值确实存在于单位档案」的条目，避免产出档案里没有、导入会失败的单位名。
		// PCS / SET / 套 / 支 / 桶 / 组 / 卷 等本身即档案里的合法单位名，
		// 由 translateUnit 的白名单直通原样保留，不在此处翻译（对齐参考文件 物料档案导入_577筆.xlsx）。
		// 注意大小写与繁简：档案里合法的是「PCS」「卷」（简体），源档若出现 pcs / 捲 需在此归一。
		UnitMap: map[string]string{
			"kg": "千克", "g": "克",
			"m": "米", "mm": "毫米",
			"㎡": "平方米", "m2": "平方米", "m³": "立方米", "m3": "立方米",
			"pcs":  "PCS", // 源档小写 pcs → 档案中的 PCS
			"pcsq": "PCS", // 源档拼写错误 PCSQ → 档案中的 PCS
			"捲":   "卷",  // 源档繁体「捲」→ 档案中的「卷」
		},
		// 来自 MES「单位档案」(计量单位 (2).xls，55 条 U001~U055) 的全部「单位名称」去重（53 个）。
		// 基本单位的输出值若不在此清单中，预检会告警提示「导入 MES 可能失败」，但不阻断转换。
		ValueWhitelist: map[string][]string{
			"基本单位": {
				"千克", "克", "毫米", "米", "平方米", "立方米", "件", "套", "卷", "罐",
				"支", "組", "组", "只", "張", "桶", "條", "臺", "雙", "式",
				"台", "箱", "丸", "片", "盒", "包", "疊", "瓶", "個", "本",
				"尺", "平方毫米",
				"PCS", "SET", "SPC", "PSC", "PC", "PXS", "PCA", "PCD", "P0CS",
				"PCSB02", "PCSA01", "A010", "Bao", "bale", "gal", "barrel", "L", "Lít",
				"Bottle", "Coil", "XẤP",
			},
		},
	}
}
