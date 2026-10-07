package main

// ============================================================================
// 检查更新（GitHub Releases）
//
// 仓库是公开库，匿名调 api.github.com 即可，不需要 token；也没有自动下载 ——
// 只告诉用户「有新版本了、在哪下载」，装不装、什么时候装由人决定。
// 网络不通/没发布过 release 都只是「查不到」，不能影响工具本身的使用。
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// releaseRepo 发布页所在仓库
const releaseRepo = "gongjuecloak/Cloakwidget"

// parseVer 把 "v1.5.0" / "1.5" / "1.6.0-beta" 解析成三段数字
func parseVer(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		out[i] = n
	}
	return out
}

// versionLess latest 是否比 cur 新
func versionLess(cur, latest string) bool {
	c, l := parseVer(cur), parseVer(latest)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// ghGet 调 GitHub 公开 API。网络不通 / 限流 / 仓库不存在都会以 error 返回，
// 由调用方决定怎么降级 —— 检查更新失败绝不能影响工具本身。
func ghGet(path string, v interface{}) error {
	req, err := http.NewRequest(http.MethodGet,
		"https://api.github.com/repos/"+releaseRepo+"/"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MESConverter/"+appVersion)
	cli := &http.Client{Timeout: 8 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// fetchLatestRelease 先看 releases/latest；仓库还处于「打了 tag 但没发 release」
// 的阶段时退回看 tags —— 否则检查更新会一直报错，其实新版本早就推上去了。
func fetchLatestRelease() (*ghRelease, error) {
	var rel ghRelease
	if err := ghGet("releases/latest", &rel); err == nil && rel.TagName != "" {
		return &rel, nil
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := ghGet("tags?per_page=10", &tags); err != nil {
		return nil, err
	}
	best := ""
	for _, t := range tags {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		if best == "" || versionLess(best, t.Name) {
			best = t.Name
		}
	}
	if best == "" {
		return nil, fmt.Errorf("仓库尚未发布版本")
	}
	return &ghRelease{
		TagName: best,
		HTMLURL: "https://github.com/" + releaseRepo + "/tags",
	}, nil
}

// handlerUpdate 检查更新
func handlerUpdate(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)

	rel, err := fetchLatestRelease()
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"ok": false, "current": appVersion,
			"error": fmt.Sprintf(L["msg_update_fail"], err.Error()),
		})
		return
	}
	latest := rel.TagName
	has := versionLess(appVersion, latest)
	msg := fmt.Sprintf(L["msg_update_latest"], appVersion)
	if has {
		msg = fmt.Sprintf(L["msg_update_new"], latest, appVersion)
	}
	// 优先给 zip（分享版是 zip 分发的），没有再退回 release 页
	dl := rel.HTMLURL
	for _, a := range rel.Assets {
		n := strings.ToLower(a.Name)
		if strings.HasSuffix(n, ".zip") || strings.HasSuffix(n, ".exe") {
			dl = a.URL
			break
		}
	}
	body := rel.Body
	if len(body) > 4000 {
		body = body[:4000]
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "current": appVersion, "latest": latest, "has_update": has,
		"url": rel.HTMLURL, "download": dl, "notes": body, "message": msg,
	})
}
