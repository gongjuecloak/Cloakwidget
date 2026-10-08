package main

// ============================================================================
// 检查更新 + 自动更新
//
// 信任模型（v1.9.0 起）：
//   更新来源 = 「内网镜像」优先，GitHub Releases 兜底。
//   两个来源都通过同一个 version.json 清单来描述版本：含版本号、exe 文件名、
//   SHA256 与大小。客户端拉到清单后，先比对版本，再下载 exe，下载完必须校验
//   SHA256 与清单一致才允许替换 —— 不一致一律中止，绝不拿来历不明的文件覆盖自己。
//   这样即使镜像 / GitHub 任一环节被投毒，只要清单里的哈希没被篡改（源来自 CI 构建时
//   对产物的真实哈希），就不会被静默装进恶意 exe。
//
// 网络/限流/无外网：任一环节失败都只是「查不到更新」，绝不能影响工具本身使用。
// ============================================================================

import (
	"crypto/sha256"
	"encoding/hex"
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

// updateMirrorBase 更新镜像基址（工厂工位机优先从这里拉）。
// 可用环境变量 MES_UPDATE_MIRROR 覆盖（例如内网 HTTP 或共享盘映射）。
// 默认指向自有服务器上的镜像服务；若该地址不可达，自动回退 GitHub。
var updateMirrorBase = "https://update.lzplus.top"

func init() {
	if v := strings.TrimSpace(os.Getenv("MES_UPDATE_MIRROR")); v != "" {
		updateMirrorBase = strings.TrimRight(v, "/")
	}
}

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

// ============================================================================
// 版本清单（version.json）
// ============================================================================

// updateManifest 描述一次发布的更新包；由 CI 在构建时生成并随 release 一起上传。
type updateManifest struct {
	Version string `json:"version"`
	Exe     struct {
		Name   string `json:"name"`   // 如 MES-Converter-v1.9.0.exe
		URL    string `json:"url"`    // GitHub 上的下载地址（兜底用）
		SHA256 string `json:"sha256"` // 构建时算出的真实哈希，校验锚点
		Size   int64  `json:"size"`
	} `json:"exe"`
}

// fetchManifest 拉取并解析 version.json。带较短超时（镜像不可达要快点回退）。
func fetchManifest(rawURL string) (*updateManifest, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MESConverter/"+appVersion)
	cli := &http.Client{Timeout: 6 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	if m.Version == "" || m.Exe.Name == "" || m.Exe.SHA256 == "" {
		return nil, fmt.Errorf("清单字段不完整")
	}
	return &m, nil
}

// resolveUpdate 决定有没有新版本、从哪下。
// 返回 (清单, 下载基址)；基址为空表示用清单里的 GitHub URL，非空表示用该基址 + "/" + exe.Name。
// 镜像优先：镜像可达且更新则直接采用；否则回退 GitHub 的 version.json 资产。
func resolveUpdate() (*updateManifest, string) {
	if updateMirrorBase != "" {
		if m, err := fetchManifest(updateMirrorBase + "/version.json"); err == nil &&
			versionLess(appVersion, m.Version) {
			return m, updateMirrorBase
		}
	}
	rel, err := fetchLatestRelease()
	if err == nil {
		for _, a := range rel.Assets {
			if strings.EqualFold(a.Name, "version.json") {
				if m, e := fetchManifest(a.URL); e == nil &&
					versionLess(appVersion, m.Version) {
					return m, "" // 用清单里的 GitHub 下载地址
				}
			}
		}
	}
	return nil, ""
}

// handlerUpdate 检查更新（手动点按钮）
func handlerUpdate(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)

	m, base := resolveUpdate()
	latest := appVersion
	has := false
	var dl string
	if m != nil {
		latest = m.Version
		has = true
		if base != "" {
			dl = base + "/" + m.Exe.Name
		} else {
			dl = m.Exe.URL
		}
	}
	msg := fmt.Sprintf(L["msg_update_latest"], appVersion)
	if has {
		msg = fmt.Sprintf(L["msg_update_new"], latest, appVersion)
	}
	// 尽量给更新说明（best-effort）
	notes := ""
	if rel, e := fetchLatestRelease(); e == nil {
		notes = rel.Body
		if len(notes) > 4000 {
			notes = notes[:4000]
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "current": appVersion, "latest": latest, "has_update": has,
		"url":      "https://github.com/" + releaseRepo + "/releases",
		"download": dl, "notes": notes, "message": msg,
	})
}

// ============================================================================
// 下载 + 校验 + 就地替换
// ============================================================================

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

// sha256File 计算文件 SHA256（十六进制小写）
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// applyUpdateFromManifest 下载 exe 并就地替换当前 exe。
// 用「改名旧 exe → 移动新 exe 落位」的方式，绕开「不能覆盖正在运行的 exe」的限制。
// 关键：下载完先校验 SHA256 与清单一致，不一致立即放弃，绝不替换。
func applyUpdateFromManifest(m *updateManifest, base string) error {
	var dlURL string
	if base != "" {
		dlURL = base + "/" + m.Exe.Name
	} else {
		dlURL = m.Exe.URL
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = filepath.Clean(exe)
	tmp := exe + ".new"
	_ = os.Remove(tmp)
	if err := downloadFile(dlURL, tmp, m.Exe.Size); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// 校验 SHA256 —— 不通过则中止，绝不替换（防投毒）
	sum, err := sha256File(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if !strings.EqualFold(sum, m.Exe.SHA256) {
		_ = os.Remove(tmp)
		return errors.New("更新包校验失败（SHA256 不匹配），已放弃更新以保证安全")
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

// autoUpdateStatePath 记录上次检查时间，避免每次启动都打网络
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
	m, base := resolveUpdate()
	if m == nil {
		return
	}
	if err := applyUpdateFromManifest(m, base); err != nil {
		appLog("后台自动更新失败：%v", err)
		return
	}
	appLog("已自动更新到 %s，下次启动生效（或重启程序立即生效）。", m.Version)
}

// handlerUpdateApply 手动「检查并更新」：下载替换后立刻重启生效
func handlerUpdateApply(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	m, base := resolveUpdate()
	if m == nil {
		writeJSON(w, map[string]interface{}{
			"ok": true, "restart": false,
			"message": fmt.Sprintf(L["msg_update_latest"], appVersion),
		})
		return
	}
	if err := applyUpdateFromManifest(m, base); err != nil {
		writeJSON(w, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf(L["msg_update_apply_fail"], err.Error()),
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "restart": true,
		"message": fmt.Sprintf(L["msg_update_applied"], m.Version),
	})
	// 先让响应发出去，再重启，确保前端能收到「已更新」提示
	go func() {
		time.Sleep(600 * time.Millisecond)
		relaunchSelf()
	}()
}
