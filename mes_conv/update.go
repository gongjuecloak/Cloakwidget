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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
		Size int64  `json:"size"`
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

// ============================================================================
// 自动更新（直连 GitHub Releases）
//
// 思路：启动后静默检查 GitHub 上的最新 release；若有新版本，后台下载 exe 并就地
// 替换当前 exe（Windows 允许重命名正在运行的 exe），下次启动生效。点「检查并更新」
// 则会下载替换并立即重启生效。
//
// 信任边界：未签名的 exe 无论是手动还是自动下载，首次运行都会被 SmartScreen 拦一次，
// 这一层不在本功能范围内（需要 EV/OV 证书或内网组策略豁免）。这里只解决「拉取+替换」，
// 让用户免去手动下载解压。
// ============================================================================

// pickExeAsset 从 release 里挑 .exe 更新包
func pickExeAsset(rel *ghRelease) string {
	for _, a := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(a.Name), ".exe") {
			return a.URL
		}
	}
	return ""
}

// assetSize 取某个下载地址对应的资源大小（用于下载后校验）
func assetSize(rel *ghRelease, url string) int64 {
	for _, a := range rel.Assets {
		if a.URL == url {
			return a.Size
		}
	}
	return 0
}

// downloadFile 下载 url 到 dest，可选校验内容长度
func downloadFile(url, dest string, expectSize int64) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MESConverter/"+appVersion)
	cli := &http.Client{Timeout: 180 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新失败：HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	if expectSize > 0 && n != expectSize {
		return fmt.Errorf("更新包大小不符（期望 %d，实际 %d）", expectSize, n)
	}
	return nil
}

// applyUpdate 下载新 exe 并就地替换当前 exe。
// 用「改名旧 exe → 移动新 exe 落位」的方式，绕开「不能覆盖正在运行的 exe」的限制。
func applyUpdate(rel *ghRelease) error {
	url := pickExeAsset(rel)
	if url == "" {
		return errors.New("找不到 exe 更新包")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = filepath.Clean(exe)
	tmp := exe + ".new"
	_ = os.Remove(tmp)
	if err := downloadFile(url, tmp, assetSize(rel, url)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // 回滚，尽量不影响正在用的进程
		return err
	}
	_ = os.Remove(old)
	return nil
}

// relaunchSelf 启动新 exe 并退出当前进程。
// 先释放单实例锁，让新进程能立即拿到锁；用一次性环境变量告知新进程「这是重启，别把自己当重复实例」。
func relaunchSelf() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	if wd, e := os.Getwd(); e == nil {
		cmd.Dir = wd
	}
	cmd.Env = append(os.Environ(), "MES_UPDATE_RESTART=1")
	releaseSingleInstance()
	if err := cmd.Start(); err != nil {
		appLog("重启失败：%v（保留当前进程）", err)
		return
	}
	os.Exit(0)
}

// autoUpdateStatePath 记录上次检查时间，避免每次启动都打 GitHub
func autoUpdateStatePath() string {
	return filepath.Join(exeDir(), "autoupdate.json")
}

func autoUpdateCheckedRecently() bool {
	b, err := os.ReadFile(autoUpdateStatePath())
	if err != nil {
		return false
	}
	var s struct {
		Last int64 `json:"last"`
	}
	if json.Unmarshal(b, &s) != nil {
		return false
	}
	return time.Now().Unix()-s.Last < 24*3600
}

func autoUpdateMarkChecked() {
	_ = os.WriteFile(autoUpdateStatePath(),
		[]byte(fmt.Sprintf(`{"last":%d}`, time.Now().Unix())), 0644)
}

// autoUpdate 后台静默检查并就地更新 exe；更新在下次启动生效（不强制重启，避免打断正在用的用户）。
// 任何异常都吞掉——自动更新失败绝不能影响工具本身。
func autoUpdate() {
	defer func() { _ = recover() }()
	time.Sleep(3 * time.Second)
	if autoUpdateCheckedRecently() {
		return
	}
	autoUpdateMarkChecked()
	rel, err := fetchLatestRelease()
	if err != nil {
		return
	}
	if !versionLess(appVersion, rel.TagName) {
		return
	}
	if err := applyUpdate(rel); err != nil {
		appLog("后台自动更新失败：%v", err)
		return
	}
	appLog("已自动更新到 %s，下次启动生效（或重启程序立即生效）。", rel.TagName)
}

// handlerUpdateApply 手动「检查并更新」：下载替换后立刻重启生效
func handlerUpdateApply(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	rel, err := fetchLatestRelease()
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf(L["msg_update_fail"], err.Error()),
		})
		return
	}
	if !versionLess(appVersion, rel.TagName) {
		writeJSON(w, map[string]interface{}{
			"ok": true, "restart": false,
			"message": fmt.Sprintf(L["msg_update_latest"], appVersion),
		})
		return
	}
	if err := applyUpdate(rel); err != nil {
		writeJSON(w, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf(L["msg_update_apply_fail"], err.Error()),
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "restart": true,
		"message": fmt.Sprintf(L["msg_update_applied"], rel.TagName),
	})
	// 先让响应发出去，再重启，确保前端能收到「已更新」提示
	go func() {
		time.Sleep(600 * time.Millisecond)
		relaunchSelf()
	}()
}
