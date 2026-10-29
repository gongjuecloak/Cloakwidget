package main

// ============================================================================
// 内置更新日志
//
// 以前「这个版本改了什么」只存在于 GitHub Release 的正文里：内网镜像环境连不上
// GitHub 就看不到，升级完重启更不会有人回头去翻发布页。于是把更新日志编译进 exe：
//   · 更新弹窗里显示本次新版本的说明（清单里带 notes 时优先用清单，其次 GitHub）；
//   · 「关于」里可以翻历史版本的更新内容；
//   · 版本变了之后首次启动，主动弹一次「这个版本改了什么」。
//
// 数据源是 changelog.json（go:embed），发版时在 versions 顶部加一条即可。
// ============================================================================

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

//go:embed changelog.json
var changelogJSON []byte

// changelogEntry 一个版本的更新说明
type changelogEntry struct {
	Version string              `json:"version"`
	Date    string              `json:"date"`
	Notes   map[string][]string `json:"notes"`
}

type changelogFile struct {
	Versions []changelogEntry `json:"versions"`
}

var (
	changelogOnce sync.Once
	changelogData changelogFile
)

func loadChangelog() *changelogFile {
	changelogOnce.Do(func() {
		if err := json.Unmarshal(changelogJSON, &changelogData); err != nil {
			appLog("内置更新日志解析失败：%v", err)
		}
	})
	return &changelogData
}

// pickLang 从多语言条目里取指定语言，缺了回退简体中文，再缺就回退第一条。
func pickLang(m map[string][]string, lang string) []string {
	if len(m) == 0 {
		return nil
	}
	if v, ok := m[lang]; ok && len(v) > 0 {
		return v
	}
	if v, ok := m["zh"]; ok && len(v) > 0 {
		return v
	}
	for _, v := range m { // map 顺序随机，但只在「连中文都没有」时才会走到这里
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

// changelogForVersion 取某个版本的说明（本地化后）；找不到返回 nil
func changelogForVersion(v, lang string) []string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	for _, e := range loadChangelog().Versions {
		if strings.TrimPrefix(e.Version, "v") == v {
			return pickLang(e.Notes, lang)
		}
	}
	return nil
}

// ---------- 「这次是从哪个版本升上来的」 ----------

// updatedFrom 本次启动时的「上一个版本」；与当前版本相同或首次运行则为空。
// 由 main 在启动早期调用 trackVersion 赋值。
var updatedFrom string

func lastVersionPath() string { return filepath.Join(exeDir(), "last_version.txt") }

// trackVersion 记录本次运行的版本，返回读到「上一个版本」（首次运行为空）。
// 只在版本变化时写盘，避免每次启动都动文件。
func trackVersion() string {
	p := lastVersionPath()
	prev := ""
	if b, err := os.ReadFile(p); err == nil {
		prev = strings.TrimSpace(string(b))
	}
	cur := strings.TrimPrefix(strings.TrimSpace(appVersion), "v")
	if strings.TrimPrefix(prev, "v") != cur {
		_ = os.WriteFile(p, []byte(cur), 0644)
	}
	return strings.TrimPrefix(prev, "v")
}

// ---------- 接口 ----------

// handlerChangelog GET /api/changelog?lang=zh&limit=10
// 返回本地化后的版本说明列表；limit 默认 10，最多 50。
func handlerChangelog(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	limit := 10
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 50 {
		limit = 50
	}

	all := loadChangelog().Versions
	out := make([]map[string]interface{}, 0, limit)
	for _, e := range all {
		if len(out) >= limit {
			break
		}
		items := pickLang(e.Notes, lang)
		if len(items) == 0 {
			continue
		}
		out = append(out, map[string]interface{}{
			"version": e.Version,
			"date":    e.Date,
			"items":   items,
		})
	}

	writeJSON(w, map[string]interface{}{
		"ok":           true,
		"current":      appVersion,
		"updated_from": updatedFrom,
		"versions":     out,
	})
}
