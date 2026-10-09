package main

// ============================================================================
// 检查更新 + 自动更新
//
// 信任模型（v1.9.0 起）：
//   更新来源 = 「内网镜像」优先，GitHub Releases 兜底。
//   两个来源都通过同一个 version.json 清单来描述版本：含版本号、exe 文件名、
//   SHA256 与大小。客户端拉到清单后，先比对版本，再下载 exe，下载完必须校验
//   SHA256 与清单一致才允许替换 —— 不一致一律中止，绝不拿来历不明的文件覆盖自己。
//
// 供应链签名（v1.10.0 起）：SHA256 只是「内容一致性」，不是「来源可信」——
// 若攻击者能同时改 exe 与清单里的哈希，仍能骗过校验。故 CI 用 Ed25519 私钥对
// version.json 签名（version.sig），客户端用内置公钥验签：
//   验签通过 → 清单可信 → 其中的 SHA256 才是真正的信任锚点 → 下载 exe 后校验哈希。
//   验签失败/无签名且要求强制 → 拒绝更新（宁可留在旧版，也不装来路不明的包）。
//   默认「验签通过才更新；无签名时按未签名处理但记警告」，可用环境变量
//   MES_UPDATE_REQUIRE_SIG=1 强制「必须有签名」，进一步收口。
//
// 网络/限流/无外网：任一环节失败都只是「查不到更新」，绝不能影响工具本身使用。
// ============================================================================

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// releaseRepo 发布页所在仓库
const releaseRepo = "gongjuecloak/Cloakwidget"

// updateMirrorBase 更新镜像基址（工厂工位机优先从这里拉）。
// 可用环境变量 MES_UPDATE_MIRROR 覆盖（例如内网 HTTP 或共享盘映射，或带子路径 /mirror）。
// 默认指向自有服务器上的镜像服务（经 Cloudflare 反代）；若该地址不可达，自动回退 GitHub。
// x.lzplus.top 现已不再托管 Mastodon，镜像可直接走根路径反代（x.lzplus.top → 127.0.0.1:18080），无需子路径。
// 若仍想用子路径或内网地址，可用环境变量 MES_UPDATE_MIRROR 覆盖。
var updateMirrorBase = "https://x.lzplus.top"

// mirrorAppPath 本应用在镜像上的命名空间（通用更新平台按 app_id 隔离多应用）。
// 新客户端走 /mes-converter/version.json；镜像同时保留根路径默认应用兜底（老客户端兼容）。
// 要让另一个工具也用这个镜像自助更新，只需把它的 app_id 配进镜像 apps.json，
// 并在该工具里复制下面这组 per-app 旋钮（releaseRepo / updateMirrorBase / mirrorAppPath / manifestPubKeyB64）。
const mirrorAppPath = "/mes-converter"

// mirrorAppBase 镜像上「本应用」的基址：基址 + 命名空间。
func mirrorAppBase() string {
	return strings.TrimRight(updateMirrorBase, "/") + mirrorAppPath
}

// mirrorAppID 本应用在镜像上的 app_id（由命名空间推导，与镜像 apps.json 的键一致）。
func mirrorAppID() string {
	return strings.Trim(mirrorAppPath, "/")
}

// manifestPubKeyB64 Ed25519 公钥（base64，32 字节 raw）。CI 用对应私钥签 version.json，
// 客户端据此验签，建立供应链信任根。私钥存于仓库 secret MES_SIGN_PRIVATE_KEY，勿入库。
const manifestPubKeyB64 = "89ymVon//tfWP9d+KzKZxg3oCBUT+w31nUqZN5LBML0="

// requireSig 是否强制「清单必须有签名」。默认否（无签名清单放行但记警告）；
// 置 MES_UPDATE_REQUIRE_SIG=1 则无签名直接拒更新，进一步收口。
func requireSig() bool {
	return strings.TrimSpace(os.Getenv("MES_UPDATE_REQUIRE_SIG")) == "1"
}

// verifyManifestSig 用内置公钥校验 version.sig 是否为 version.json 原始字节的合法签名。
// 返回 (是否验签通过, 是否有签名文件)。
func verifyManifestSig(manifest, sig []byte) (ok bool, hasSig bool) {
	if len(sig) == 0 {
		return false, false
	}
	pub, err := base64.StdEncoding.DecodeString(manifestPubKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false, true // 公钥坏了，视作有签名但不可信
	}
	if len(sig) != ed25519.SignatureSize {
		return false, true
	}
	return ed25519.Verify(ed25519.PublicKey(pub), manifest, sig), true
}

func init() {
	if v := strings.TrimSpace(os.Getenv("MES_UPDATE_MIRROR")); v != "" {
		updateMirrorBase = strings.TrimRight(v, "/")
	}
	loadMirrorToken()
}

// ---- 镜像访问令牌（一次性旋转） ----
// 客户端本地保存当前令牌；镜像要求拉取带令牌，成功一次即旋转（响应头 X-Next-Token），
// 下次用新令牌。无令牌/失效时以 enroll 密钥自注册，重新拿到令牌。工厂机无外网、只能走镜像，
// 故必须能自注册成功；公网客户端拿不到令牌也会回退 GitHub。
var mirrorToken string

func mirrorTokenPath() string { return filepath.Join(exeDir(), "mes_mirror_token") }

func loadMirrorToken() {
	if b, err := os.ReadFile(mirrorTokenPath()); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			mirrorToken = s
		}
	}
}

func saveMirrorToken(t string) {
	mirrorToken = t
	_ = os.WriteFile(mirrorTokenPath(), []byte(t), 0600)
}

func mirrorEnrollSecret() string {
	if v := strings.TrimSpace(os.Getenv("MES_MIRROR_SECRET")); v != "" {
		return v
	}
	return "mes-mirror-internal"
}

func isMirrorURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	mu, err := url.Parse(updateMirrorBase)
	if err != nil {
		return false
	}
	return u.Host == mu.Host
}

// mirrorURLWith 给镜像请求附加令牌 / enroll 参数；非镜像地址原样返回。
// 令牌优先通过 Authorization: Bearer 头传递（见 fetchBytes），仅当
// useBearerAuth 为 false 时才回退到旧的 ?token= 形式。
func mirrorURLWith(raw, token string, enroll bool) string {
	if !isMirrorURL(raw) {
		return raw
	}
	q := url.Values{}
	// Bearer 模式下令牌不放进 URL
	if token != "" && !useBearerAuth() {
		q.Set("token", token)
	}
	if enroll {
		q.Set("enroll", "1")
		q.Set("secret", mirrorEnrollSecret())
	}
	if len(q) == 0 {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + q.Encode()
}

// useBearerAuth 默认走标准 Authorization 头；设 MES_MIRROR_BEARER=0 可退回旧的
// ?token= 形式（仅用于兼容尚未升级的镜像服务）。
func useBearerAuth() bool {
	return strings.TrimSpace(os.Getenv("MES_MIRROR_BEARER")) != "0"
}

func captureNextToken(resp *http.Response) {
	if nt := resp.Header.Get("X-Next-Token"); nt != "" {
		saveMirrorToken(nt)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
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

// manifestSigURL 由清单 URL 推出同目录的 version.sig URL。
func manifestSigURL(manifestURL string) string {
	return strings.Replace(manifestURL, "version.json", "version.sig", 1)
}

// fetchBytes GET 一个 URL 的原始字节（带令牌 / 401 自注册重试逻辑同 fetchManifest）。
func fetchBytes(rawURL string, timeout time.Duration) ([]byte, int, error) {
	do := func(token string, enroll bool) ([]byte, int, error) {
		req, err := http.NewRequest(http.MethodGet, mirrorURLWith(rawURL, token, enroll), nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", "MESConverter/"+appVersion)
		if isMirrorURL(rawURL) {
			// 令牌走标准 Authorization 头（不再塞进 URL，避免令牌进日志/浏览器历史）
			if enroll {
				req.Header.Set("X-Access-Token", token)
			} else if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
		cli := &http.Client{Timeout: timeout}
		resp, err := cli.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		captureNextToken(resp)
		return b, resp.StatusCode, err
	}
	b, code, err := do(mirrorToken, false)
	if err == nil {
		return b, code, nil
	}
	if code == http.StatusUnauthorized && isMirrorURL(rawURL) {
		newTok := randHex(16)
		saveMirrorToken(newTok)
		if b2, _, e2 := do(newTok, true); e2 == nil {
			return b2, http.StatusOK, nil
		}
		return nil, code, fmt.Errorf("镜像鉴权失败（已尝试自注册）：%v", err)
	}
	return nil, code, err
}

// fetchManifest 拉取并解析 version.json。带较短超时（镜像不可达要快点回退）。
// 先取清单原始字节与同目录的 version.sig，用内置 Ed25519 公钥验签：
// 验签通过才解析使用；无签名时按未签名处理（除非强制要求签名）。
func fetchManifest(rawURL string) (*updateManifest, error) {
	raw, _, err := fetchBytes(rawURL, 6*time.Second)
	if err != nil {
		return nil, err
	}
	// 取签名（best-effort：404/不可达即视为无签名）
	sig, _, _ := fetchBytes(manifestSigURL(rawURL), 4*time.Second)
	verified, hasSig := verifyManifestSig(raw, sig)
	switch {
	case verified:
		// 验签通过，继续
	case hasSig:
		return nil, errors.New("更新清单签名校验失败，已拒绝更新（来源可能被篡改）")
	default:
		if requireSig() {
			return nil, errors.New("更新清单无签名，且已启用强制验签，拒绝更新")
		}
		appLog("更新清单未签名（仍按 SHA256 校验）；如需强制验签请设 MES_UPDATE_REQUIRE_SIG=1")
	}
	var m updateManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m.Version == "" || m.Exe.Name == "" || m.Exe.SHA256 == "" {
		return nil, fmt.Errorf("清单字段不完整")
	}
	return &m, nil
}

// updateChannel 返回本机跟随的发布渠道：stable（默认）/ beta / dev。
// 生产工位机保持 stable；测试机可设 MES_UPDATE_CHANNEL=beta 抢先体验预发布。
func updateChannel() string {
	c := strings.ToLower(strings.TrimSpace(os.Getenv("MES_UPDATE_CHANNEL")))
	switch c {
	case "beta", "dev":
		return c
	}
	return "stable"
}

// clientID 生成本机标识：优先取显式配置，否则用 主机名-用户名 组合。
func clientID() string {
	if v := strings.TrimSpace(os.Getenv("MES_CLIENT_ID")); v != "" {
		return v
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		return host
	}
	return host + "-" + user
}

// reportCheckin 向镜像登记本机（版本 / 渠道 / 主机名），让控制台能看到
// 「现场到底有哪些机器、哪些还没更新」。best-effort：失败静默，绝不影响使用。
func reportCheckin() {
	base := strings.TrimRight(updateMirrorBase, "/")
	if base == "" {
		return
	}
	payload := map[string]string{
		"client_id": clientID(),
		"app":       mirrorAppID(),
		"version":   appVersion,
		"channel":   updateChannel(),
		"os":        runtime.GOOS,
		"hostname":  clientID(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost,
		base+"/api/v1/client/checkin", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MESConverter/"+appVersion)
	if mirrorToken != "" {
		req.Header.Set("Authorization", "Bearer "+mirrorToken)
	}
	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		appLog("客户端签到上报失败（忽略）：%v", err)
		return
	}
	defer resp.Body.Close()
	captureNextToken(resp)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
}

// resolveUpdate 决定有没有新版本、从哪下。
// 返回 (清单, 下载基址)；基址为空表示用清单里的 GitHub URL，非空表示用该基址 + "/" + exe.Name。
// 更新源由 sys_settings.json 的 update_source 决定（界面「系统设置 → 更新源」可改）：
//   - auto   （默认）镜像优先；镜像不可达/无新版时回退 GitHub
//   - github 只查 GitHub（适合能直连外网的机器）
//   - mirror 只查镜像、不回退（适合内网无法访问 GitHub 的机器，避免每次白等超时）
func resolveUpdate() (*updateManifest, string) {
	src := srcAuto
	if s := sysSettings(); s.UpdateSource != "" {
		src = normalizeUpdateSource(s.UpdateSource)
	}
	mirrorOK := updateMirrorBase != "" && src != srcGithub
	githubOK := src != srcMirror

	if mirrorOK {
		if m, err := fetchManifest(mirrorAppBase() + "/version.json?channel=" + url.QueryEscape(updateChannel())); err == nil &&
			versionLess(appVersion, m.Version) {
			return m, mirrorAppBase()
		}
		if src == srcMirror {
			return nil, "" // 只用镜像：镜像没有新版 / 不可达，就不更新了
		}
	}
	if githubOK {
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
	}
	return nil, ""
}

// handlerUpdate 检查更新（手动点按钮）
func handlerUpdate(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)

	// 顺手向镜像登记本机，让控制台能看到现场机器的更新状态（best-effort）
	go reportCheckin()

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

// downloadFile 下载 url 到 dest，可选校验内容长度。
// 命中镜像时附加令牌；若返回 401，则尝试用 enroll 密钥自注册一次再下载。
func downloadFile(url, dest string, expectSize int64) error {
	fetch := func(token string, enroll bool) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodGet, mirrorURLWith(url, token, enroll), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "MESConverter/"+appVersion)
		cli := &http.Client{Timeout: 180 * time.Second}
		return cli.Do(req)
	}
	resp, err := fetch(mirrorToken, false)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized && isMirrorURL(url) {
		resp.Body.Close()
		newTok := randHex(16)
		saveMirrorToken(newTok)
		resp, err = fetch(newTok, true)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新失败：HTTP %d", resp.StatusCode)
	}
	captureNextToken(resp)
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

// applyUpdateFromManifest 下载 exe 并就地替换当前 exe。返回最终 exe 路径。
// 目标文件名一律以清单 m.Exe.Name 为准（通用：每个应用自定自己的 exe 名，不再硬编码前缀）。
// 用「并存式替换」绕开「不能覆盖正在运行的 exe」的限制：
//   - 版本化文件名（新名 != 运行中 exe）：新版本落成新文件名，旧文件留到下次启动由 cleanupOldExe 删除；
//   - 稳定文件名（新名 == 运行中 exe）：先把运行中 exe 改名 .old，再把新文件落到原名，最后重启新进程。
//
// 关键：下载完先校验 SHA256 与清单一致，不一致立即放弃，绝不替换。
// pendingUpdateFile 记录「上次更新遗留、需在下次启动时清理的旧文件名」。
func pendingUpdateFile() string { return filepath.Join(exeDir(), "pending_update.txt") }

// cleanupOldExe 清理上一轮更新遗留的旧 exe。
// 只在启动早期调用——此时本进程是刚启动的，别的旧进程早已退出，文件不再被锁。
// 删不掉就跳过（下次启动还会再试），绝不影响启动。
func cleanupOldExe() {
	f, err := os.Open(pendingUpdateFile())
	if err != nil {
		return
	}
	name := strings.TrimSpace(string(readAllAndClose(f)))
	if name == "" || filepath.Base(name) != name { // 只接受同目录下的纯文件名，防路径穿越
		_ = os.Remove(pendingUpdateFile())
		return
	}
	old := filepath.Join(exeDir(), name)
	if _, err := os.Stat(old); err == nil {
		if err := os.Remove(old); err != nil {
			appLog("旧版本文件暂无法清理（可能被占用），下次启动再试：%s", name)
			return // 保留 pending 文件，等下次
		}
		appLog("已清理上一版本的 exe：%s", name)
	}
	_ = os.Remove(pendingUpdateFile())
}

// readAllAndClose 读出全部内容并关闭（os.ReadFile 需要路径，这里给已打开的句柄用）
func readAllAndClose(f *os.File) []byte {
	defer f.Close()
	b := make([]byte, 0, 256)
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil {
			break
		}
		if len(b) > 4096 {
			break
		}
	}
	return b
}

func applyUpdateFromManifest(m *updateManifest, base string) (string, error) {
	var dlURL string
	if base != "" {
		dlURL = strings.TrimRight(base, "/") + "/" + m.Exe.Name
	} else {
		dlURL = m.Exe.URL
	}
	cur, err := os.Executable()
	if err != nil {
		return "", err
	}
	cur = filepath.Clean(cur)
	dir := exeDir()

	// 临时文件：隐藏、唯一，绝不与原文件名冲突（避免"同名锁文件"类问题）。
	tmp := filepath.Join(dir, "."+strings.TrimSuffix(m.Exe.Name, ".exe")+".part")
	_ = os.Remove(tmp)
	if err := downloadFile(dlURL, tmp, m.Exe.Size); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	// 校验 SHA256 —— 不通过则中止，绝不替换（防投毒）
	sum, err := sha256File(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if !strings.EqualFold(sum, m.Exe.SHA256) {
		_ = os.Remove(tmp)
		return "", errors.New("更新包校验失败（SHA256 不匹配），已放弃更新以保证安全")
	}

	// 目标文件名一律以清单为准（通用：每个应用自定自己的 exe 名，不再硬编码前缀）。
	target := filepath.Join(dir, m.Exe.Name)

	if sameFile(target, cur) {
		// —— 就地更新：目标文件名与当前运行的 exe 完全相同（稳定文件名场景）——
		// Windows 允许重命名正在运行的 exe（仅禁止删除），故先把它挪到 .old，
		// 再把新文件落到原名，最后重启新进程；旧的 .old 下次启动清理。
		backup := target + ".old"
		_ = os.Remove(backup)
		if rerr := os.Rename(cur, backup); rerr != nil {
			// 极端情况（被占用 / 特殊路径解析异常）无法挪动运行时 exe：
			// 退回「下次启动再替换」，本次不破坏当前进程，提示用户重启即可生效。
			_ = os.Remove(tmp)
			return "", fmt.Errorf("无法就地替换正在运行的程序（%v）；请关闭程序后重新启动以完成更新", rerr)
		}
		if err := moveFile(tmp, target); err != nil {
			_ = os.Rename(backup, cur) // 落位失败：把 .old 还原，避免程序缺失
			_ = os.Remove(tmp)
			return "", fmt.Errorf("新版本落位失败：%w", err)
		}
		_ = os.WriteFile(pendingUpdateFile(), []byte(filepath.Base(backup)), 0644)
		appLog("新版本已就位（就地替换）：%s（重启后生效）", m.Exe.Name)
		return target, nil
	}

	// —— 版本化文件名（新文件名 != 运行中的 exe）：并存式替换，最稳 ——
	// 绝不动运行中 exe；新版本落成新文件名，旧文件留到下次启动由 cleanupOldExe 删除。
	if _, err := os.Stat(target); err == nil {
		_ = os.Remove(target) // 上一次更新若留下同名残留
	}
	if err := moveFile(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("新版本落位失败：%w", err)
	}
	if curName := filepath.Base(cur); curName != "" && curName != filepath.Base(target) {
		_ = os.WriteFile(pendingUpdateFile(), []byte(curName), 0644)
		appLog("新版本已就位：%s（下次启动生效，并自动清理旧文件 %s）", m.Exe.Name, curName)
	} else {
		_ = os.Remove(pendingUpdateFile())
	}
	return target, nil
}

// sameFile 判断两个路径是否指向同一个文件（不存在时按字符串比较）
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// relaunchSelf 启动新 exe 并退出当前进程。
// 先释放单实例锁，让新进程能立即拿到锁；用一次性环境变量告知新进程「这是重启，别把自己当重复实例」。
// target 为空时回退到 os.Executable()。
func relaunchSelf(target string) {
	if target == "" {
		if e, err := os.Executable(); err == nil {
			target = e
		}
	}
	if target == "" {
		return
	}
	cmd := exec.Command(target, os.Args[1:]...)
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
	// 启动即向镜像登记本机（best-effort，失败不影响使用）
	reportCheckin()
	if autoUpdateCheckedRecently() {
		return
	}
	autoUpdateMarkChecked()
	m, base := resolveUpdate()
	if m == nil {
		return
	}
	if _, err := applyUpdateFromManifest(m, base); err != nil {
		appLog("后台自动更新失败：%v", err)
		return
	}
	appLog("已自动更新到 %s，下次启动生效（重启程序可立即生效）。", m.Version)
}

// moveFile 移动文件；跨卷（不同磁盘）时先复制再删除。
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return os.Remove(src)
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
	final, err := applyUpdateFromManifest(m, base)
	if err != nil {
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
		relaunchSelf(final)
	}()
}
