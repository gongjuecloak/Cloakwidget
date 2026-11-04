package main

// ============================================================================
// 配置 schema：让「新版程序 + 旧版配置」不再静默失配
//
// 三份配置文件各带一个 schema 号：
//   mapping.json     物料档案映射（Config）
//   orders.json      订单 / 工单映射（OrdersConfig）
//   bom_mapping.json BOM 映射（BomConfig）
//
// 加载时的口径：
//   schema == 0        老版本写的配置（没有 schema 字段）→ 按 v0 处理，逐级迁移
//   schema <  current  逐级迁移：先备份到 backup/，补齐新增字段，再写回磁盘
//   schema == current  原样使用
//   schema >  current  配置由更高版本程序写过 → 只读不写，避免旧版把新键抹掉
//
// 为什么要这套东西：配置以前是「整份快照」——盘上有什么就用什么，
// 新版本新增的列 / 默认值 / 开关不会出现在旧配置里，输出就少一列或空一列，
// 而且完全没有提示（v1.15.2 的「工艺路线编码」就是这么丢的）。
// 有了 schema 之后，补什么、补了几项、什么时候补的，都能在日志与排障包里查到。
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// 当前程序支持的配置 schema。**新增「需要写到旧配置里」的结构时，这个数字 +1**，
// 并在对应的 migrate*Config 里加一段 `if c.Schema < N { ... }` 的迁移步骤。
const (
	schemaMaterials = 1
	schemaOrders    = 1
	schemaBom       = 1
)

// configNotes 配置加载过程中的「需要让用户知道」的说明（迁移了、损坏了、比程序新）。
// 由启动阶段的 ensure*Config 写入，界面通过 /api/about 读取展示。
var (
	configNotesMu sync.Mutex
	configNotes   []string
)

func addConfigNote(format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	configNotesMu.Lock()
	for _, old := range configNotes {
		if old == msg { // 同一条说明只留一次（ensureConfig 每次取配置都会走一遍）
			configNotesMu.Unlock()
			return
		}
	}
	configNotes = append(configNotes, msg)
	configNotesMu.Unlock()
	appLog("%s", msg)
}

// configNotesSnapshot 返回当前累积的配置说明（副本，供接口使用）
func configNotesSnapshot() []string {
	configNotesMu.Lock()
	defer configNotesMu.Unlock()
	out := make([]string, len(configNotes))
	copy(out, configNotes)
	return out
}

// resetConfigNotes 仅供测试使用：清空累积说明
func resetConfigNotes() {
	configNotesMu.Lock()
	configNotes = nil
	configNotesMu.Unlock()
}

// backupBeforeMigrate 迁移前把旧配置复制到 backup/ 下，返回备份文件名（失败返回空串）。
// 迁移本身只做「补」，理论上不会丢东西；但用户可能手工删过键，
// 有个迁移前的快照，出问题时能一眼看出程序动了什么。
func backupBeforeMigrate(path string) string {
	if !fileExists(path) {
		return ""
	}
	name, err := backupFile(path, backupRoot())
	if err != nil {
		addConfigNote("警告：迁移前备份 %s 失败（%v），仍继续迁移", pathBase(path), err)
		return ""
	}
	return name
}

// pathBase 取文件名（避免为了一个 Base 调用把 filepath 灌进本文件的所有分支）
func pathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// rawTopKeys 把配置原始字节解析成「顶层键 -> 原始值」，用于判断某个键「没写」还是「写了零值」。
// JSON 的结构体解码分不清这两者：布尔开关缺省是 false，跟用户真的关掉了长得一样。
func rawTopKeys(raw []byte) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// rawHas 判断顶层键是否存在（m 为 nil 时一律当作「没有」）
func rawHas(m map[string]json.RawMessage, key string) bool {
	if m == nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// mergeMissingStrings 把 def 里有、cur 里没有的字符串追加到 cur 末尾，返回追加条数。
// 只加不删：用户自己加的列、自己删的列都不动，避免「迁移」变成「覆盖」。
func mergeMissingStrings(cur, def []string) ([]string, int) {
	if cur == nil {
		cur = []string{}
	}
	seen := make(map[string]bool, len(cur))
	for _, s := range cur {
		seen[s] = true
	}
	added := 0
	for _, d := range def {
		if !seen[d] {
			cur = append(cur, d)
			seen[d] = true
			added++
		}
	}
	return cur, added
}

// mergeMissingMap 把 def 里有、cur 里没有的键补进 cur，返回补了几项。
// 已有的键（哪怕值是空串）一律保留 —— 空串往往是用户「有意留空」的选择。
func mergeMissingMap(cur, def map[string]string) (map[string]string, int) {
	if cur == nil {
		cur = map[string]string{}
	}
	added := 0
	for k, v := range def {
		if _, ok := cur[k]; !ok {
			cur[k] = v
			added++
		}
	}
	return cur, added
}

// configWriteMu 串行化同进程内的配置写盘。
// 配置是「按需重读」的（/api/config、转换入口都会走 ensure*Config），
// 两个标签页同时保存、或「保存」撞上「启动迁移」时，会真的并发写同一份文件。
var configWriteMu sync.Mutex

// writeConfigFile 原子写配置文件：先写同目录临时文件再 rename，
// 避免「写到一半程序被杀 / 拔电」留下半截 JSON（那会被下一次启动判成损坏）。
//
// 临时文件名必须每次唯一：以前固定写成 path+".tmp"，并发写时两个 goroutine
// 会往同一个临时文件里写，再把半截内容 rename 上线 —— 配置文件就这么坏掉的。
func writeConfigFile(path string, data []byte) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ============================================================================
// 配置一致性审计：本版本内置默认 vs 磁盘配置
//
// 每次加载配置都会做三件事，结果都记进 configDiff：
//   Filled     盘上缺、内置有 → 已自动补（并写回磁盘）
//   Custom     盘上有值且与内置不同 → 本地覆盖生效，**不动**
//   OnlyLocal  盘上有、内置没有 → 用户自己加的，保留
//
// 为什么要单独记一份：补齐动作散落在三个 migrate/reconcile 函数里，日志只有一句
// 「补齐 N 项」。用户真正想知道的是「新版本加的东西到我机器上了吗、我改过的被覆盖了吗」，
// 所以这里把逐项路径记下来，界面上直接列。
// ============================================================================

// configDiff 一份配置与内置默认的差异
type configDiff struct {
	File      string   `json:"file"`       // 配置文件名
	Schema    int      `json:"schema"`     // 磁盘配置的 schema
	SchemaNow int      `json:"schema_now"` // 本程序支持的 schema
	Filled    []string `json:"filled"`     // 本次加载按默认补齐的项（人可读路径）
	Custom    []string `json:"custom"`     // 本地有值与默认不同 → 本地生效，不覆盖
	OnlyLocal []string `json:"only_local"` // 本地独有，默认里没有
}

// consistent 三项都为空才算「和当前版本完全一致」
func (d *configDiff) consistent() bool {
	return len(d.Filled) == 0 && len(d.Custom) == 0 && len(d.OnlyLocal) == 0
}

var (
	configDiffMu sync.Mutex
	configDiffs  = make(map[string]*configDiff, 3)
)

// recordConfigDiff 记下一份配置的审计结果（同名文件后写覆盖先写）
func recordConfigDiff(d *configDiff) {
	if d == nil || d.File == "" {
		return
	}
	if d.Filled == nil {
		d.Filled = []string{}
	}
	if d.Custom == nil {
		d.Custom = []string{}
	}
	if d.OnlyLocal == nil {
		d.OnlyLocal = []string{}
	}
	configDiffMu.Lock()
	// Filled 记「本进程累计补过哪些」，不是「最后一次加载补了什么」：
	// 配置是按需重读的，第二次加载时已经没得补了，直接覆盖会把启动时那次的记录冲掉，
	// 界面上就变成「明明补过却显示 0 项」。Custom / OnlyLocal 用最新值即可。
	if old, ok := configDiffs[d.File]; ok && len(old.Filled) > 0 {
		merged := append([]string(nil), old.Filled...)
		for _, p := range d.Filled {
			dup := false
			for _, q := range merged {
				if q == p {
					dup = true
					break
				}
			}
			if !dup {
				merged = append(merged, p)
			}
		}
		d.Filled = merged
	}
	configDiffs[d.File] = d
	configDiffMu.Unlock()
}

// configDiffSnapshot 按文件名排序返回全部审计结果（副本）
func configDiffSnapshot() []configDiff {
	configDiffMu.Lock()
	out := make([]configDiff, 0, len(configDiffs))
	for _, d := range configDiffs {
		out = append(out, *d)
	}
	configDiffMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// resetConfigDiffs 仅供测试：清空审计结果
func resetConfigDiffs() {
	configDiffMu.Lock()
	configDiffs = make(map[string]*configDiff, 3)
	configDiffMu.Unlock()
}

// diffMapString 逐键比较两个 map[string]string：同键不同值记 Custom，本地多键记 OnlyLocal。
// 只做「比较」，不碰数据 —— 补齐由各配置的 reconcile 负责。
func diffMapString(path string, cur, def map[string]string, d *configDiff) {
	for k, v := range cur {
		dv, ok := def[k]
		if !ok {
			d.OnlyLocal = append(d.OnlyLocal, path+"."+k)
			continue
		}
		if dv != v {
			d.Custom = append(d.Custom, path+"."+k)
		}
	}
}

// diffMapList 逐键比较两个 map[string][]string（列顺序类，顺序有意义）。
// 同键但内容不同记 Custom，本地多键记 OnlyLocal。
func diffMapList(path string, cur, def map[string][]string, d *configDiff) {
	for k, v := range cur {
		dv, ok := def[k]
		if !ok {
			d.OnlyLocal = append(d.OnlyLocal, path+"."+k)
			continue
		}
		if !sameStringSlice(v, dv) {
			d.Custom = append(d.Custom, path+"."+k)
		}
	}
}

// diffStringSet 比较两个字符串集合（忽略顺序），不同记一条 Custom
func diffStringSet(path string, cur, def []string, d *configDiff) {
	if !sameStringSet(cur, def) {
		d.Custom = append(d.Custom, path)
	}
}

// sameStringSlice 顺序与内容都相同
func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameStringSet 忽略顺序比较（用于白名单 / 同义词这类无序集合）
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return sameStringSlice(x, y)
}

// logConfigAudit 启动时把三份配置的一致性结果写进日志。
// 只给一行摘要：补齐几项、保留了几项本地自定义 —— 出问题时不用去翻配置文件。
func logConfigAudit() {
	for _, d := range configDiffSnapshot() {
		if d.consistent() {
			appLog("配置一致性 %s：与本版本内置默认一致（schema %d）", d.File, d.Schema)
			continue
		}
		appLog("配置一致性 %s：自动补齐 %d 项，本地自定义 %d 项（未覆盖），本地独有 %d 项（schema %d / 程序 %d）",
			d.File, len(d.Filled), len(d.Custom), len(d.OnlyLocal), d.Schema, d.SchemaNow)
	}
}
