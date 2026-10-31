package main

// ============================================================================
// 检查更新 + 自动更新
//
// 信任模型（v1.9.0 起）：
//   更新来源 = GitHub Releases。
//   version.json 清单描述版本：含版本号、exe 文件名、
//   SHA256 与大小。客户端拉到清单后，先比对版本，再下载 exe，下载完必须校验
//   SHA256 与清单一致才允许替换 —— 不一致一律中止，绝不拿来历不明的文件覆盖自己。
//
// 供应链签名（v1.10.0 起）：SHA256 只是「内容一致性」，不是「来源可信」——
// 若攻击者能同时改 exe 与清单里的哈希，仍能骗过校验。故 CI 用 Ed25519 私钥对
// version.json 签名（version.sig），客户端用内置公钥验签：
//   验签通过 → 清单可信 → 其中的 SHA256 才是真正的信任锚点 → 下载 exe 后校验哈希。
//   验签失败或无签名 → 拒绝更新（宁可留在旧版，也不装来路不明的包）。
//
// 网络/限流/无外网：任一环节失败都只是「查不到更新」，绝不能影响工具本身使用。
// ============================================================================

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
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

// manifestPubKeyB64 Ed25519 公钥（base64，32 字节 raw）。CI 用对应私钥签 version.json，
// 客户端据此验签，建立供应链信任根。私钥存于仓库 secret MES_SIGN_PRIVATE_KEY，勿入库。
const manifestPubKeyB64 = "89ymVon//tfWP9d+KzKZxg3oCBUT+w31nUqZN5LBML0="

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

// runningExeSHA256 计算当前运行中的 exe 文件的 SHA256。
// 用于「防误报升级」：若发布清单描述的 exe 哈希与正在运行的文件完全一致，
// 说明两者其实是同一个构建，即便版本号比对异常也绝不提示升级。
func runningExeSHA256() string {
	p, err := os.Executable()
	if err != nil || p == "" {
		return ""
	}
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
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
	// Notes 更新说明（多语言，可选）。构建时从 mes_conv/changelog.json 里取该版本的条目。
	// 客户端优先用它显示更新内容，避免再发一次 GitHub API 请求。
	Notes map[string][]string `json:"notes,omitempty"`
	Exe   struct {
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

// fetchBytes GET 一个 URL 的原始字节，最多读取 1 MiB。
func fetchBytes(rawURL string, timeout time.Duration) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "MESConverter/"+appVersion)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, err
}

// fetchManifest 拉取并解析 version.json。
// 先取清单原始字节与同目录的 version.sig，用内置 Ed25519 公钥验签：
// 验签通过才解析使用；无签名或验签失败都拒绝更新。
func fetchManifest(rawURL string) (*updateManifest, error) {
	raw, _, err := fetchBytes(rawURL, 6*time.Second)
	if err != nil {
		return nil, err
	}
	// 签名与清单必须同时存在。
	sig, _, _ := fetchBytes(manifestSigURL(rawURL), 4*time.Second)
	verified, hasSig := verifyManifestSig(raw, sig)
	if hasSig && !verified {
		return nil, errors.New("更新清单签名校验失败，已拒绝更新（来源可能被篡改）")
	}
	if !hasSig {
		return nil, errors.New("更新清单无签名，已拒绝更新")
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

// resolveUpdate 决定 GitHub Releases 上有没有新版本。
func resolveUpdate() (*updateManifest, bool) {
	// reachable 表示 GitHub 正常应答。
	// 调用方靠它区分「确实没有新版」和「网络没打通」——后者不该写 24h 节流，
	// 否则一次抖动就让机器一天内不再检查更新。
	rel, err := fetchLatestRelease()
	if err != nil {
		return nil, false
	}
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "version.json") {
			if m, e := fetchManifest(a.URL); e == nil && versionLess(appVersion, m.Version) {
				if m.Exe.SHA256 != "" && strings.EqualFold(m.Exe.SHA256, runningExeSHA256()) {
					continue
				}
				return m, true
			}
		}
	}
	return nil, true
}

// handlerUpdate 检查更新（手动点按钮）
func handlerUpdate(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)

	m, reachable := resolveUpdate()
	if !reachable {
		appLog("检查更新：GitHub 不可达（当前 v%s），按已是最新处理", appVersion)
	}
	latest := appVersion
	has := false
	var dl string
	if m != nil {
		latest = m.Version
		has = true
		dl = m.Exe.URL
	}
	msg := fmt.Sprintf(L["msg_update_latest"], appVersion)
	if has {
		msg = fmt.Sprintf(L["msg_update_new"], latest, appVersion)
	}
	// 更新说明优先用清单里的，没有再读 GitHub Release 正文。
	notes := ""
	if m != nil {
		if ns := pickLang(m.Notes, lang); len(ns) > 0 {
			notes = strings.Join(ns, "\n")
		}
	}
	if notes == "" {
		if rel, e := fetchLatestRelease(); e == nil {
			notes = rel.Body
			if len(notes) > 4000 {
				notes = notes[:4000]
			}
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok": true, "current": appVersion, "latest": latest, "has_update": has,
		"url":      "https://github.com/" + releaseRepo + "/releases",
		"download": dl, "notes": notes, "message": msg,
		// updated_from 不为空表示「这次是从哪个版本升上来的」，界面据此主动弹一次更新日志
		"updated_from": updatedFrom,
	})
}

// ============================================================================
// 下载 + 校验 + 就地替换
// ============================================================================

// downloadFile 下载 GitHub Release 资产到 dest，可选校验内容长度。
func downloadFile(url, dest string, expectSize int64) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MESConverter/"+appVersion)
	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
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

// applyUpdateFromManifest 下载 exe 并就地替换当前 exe。返回最终 exe 路径。
// 目标文件名一律以清单 m.Exe.Name 为准（通用：每个应用自定自己的 exe 名，不再硬编码前缀）。
// 用「并存式替换」绕开「不能覆盖正在运行的 exe」的限制：
//   - 版本化文件名（新名 != 运行中 exe）：新版本落成新文件名，旧文件留到下次启动由 cleanupOldExe 删除；
//   - 稳定文件名（新名 == 运行中 exe）：先把运行中 exe 改名 .old，再把新文件落到原名，最后重启新进程。
//
// 关键：下载完先校验 SHA256 与清单一致，不一致立即放弃，绝不替换。
// pendingUpdateFile 记录「上次更新遗留、需在下次启动时清理的旧文件名」。
func pendingUpdateFile() string { return filepath.Join(exeDir(), "pending_update.txt") }

// pendingApplyFile 记录「上次落位失败、需要重试」的更新。
// 没有它的话，就地替换一旦被占用挡下，这条更新就彻底丢了：
// 下次启动还是旧版，而且 24h 节流内也不会再检查，用户以为「已经是最新」。
func pendingApplyFile() string { return filepath.Join(exeDir(), "pending_apply.json") }

// cleanupOldExe 处理上一轮更新遗留的旧 exe：归档到 previous/ 而不是直接删除。
// **只在启动自检通过之后调用**（main.go 里在 serveAsync 成功后才调）——
// 旧 exe 是新版本起不来时唯一的回退副本，删早了就退无可退。
// 归档失败就跳过（下次启动还会再试），绝不影响启动。
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
		if err := archiveOldExe(old); err != nil {
			appLog("旧版本文件暂无法归档（可能被占用），下次启动再试：%s", name)
			return // 保留 pending 文件，等下次
		}
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

func applyUpdateFromManifest(m *updateManifest) (string, error) {
	// 同进程内只有一个更新流程（自动更新与手动「立即更新」可能同时触发）；
	// 目录锁再挡住共享盘上其他机器。两者都在最外层，任何一步失败都不会碰到现有文件。
	if !updateGateMu.TryLock() {
		return "", errors.New("已有更新正在进行中，请稍候再试")
	}
	defer updateGateMu.Unlock()
	releaseLock, err := acquireUpdateFileLock()
	if err != nil {
		return "", err
	}
	defer releaseLock()

	cur, err := os.Executable()
	if err != nil {
		return "", err
	}
	cur = filepath.Clean(cur)
	dir := exeDir()

	// 临时文件：隐藏、唯一，绝不与原文件名冲突（避免"同名锁文件"类问题）。
	tmp := filepath.Join(dir, "."+strings.TrimSuffix(m.Exe.Name, ".exe")+".part")
	_ = os.Remove(tmp)
	if err := downloadFile(m.Exe.URL, tmp, m.Exe.Size); err != nil {
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
		// 上一次更新可能留下同名的 .old（还没归档）：先归档而不是删掉，
		// 那份才是「万一这次也不行」时能退回去的版本。
		if fileExists(backup) {
			_ = archiveOldExe(backup)
		}
		if rerr := os.Rename(cur, backup); rerr != nil {
			// 极端情况（被占用 / 特殊路径解析异常）无法挪动运行时 exe：
			// 退回「下次启动再替换」，本次不破坏当前进程，提示用户重启即可生效。
			_ = os.Remove(tmp)
			// 留下待重试标记：否则下次启动还是旧版，而且再也不会提示更新
			_ = os.WriteFile(pendingApplyFile(), []byte(m.Version), 0644)
			return "", fmt.Errorf("无法就地替换正在运行的程序（%v）；已记为待重试，下次启动会自动再试一次", rerr)
		}
		if err := moveFile(tmp, target); err != nil {
			_ = os.Rename(backup, cur) // 落位失败：把 .old 还原，避免程序缺失
			_ = os.Remove(tmp)
			return "", fmt.Errorf("新版本落位失败：%w", err)
		}
		_ = os.WriteFile(pendingUpdateFile(), []byte(filepath.Base(backup)), 0644)
		recordUpdatePending("inplace", filepath.Base(target), filepath.Base(backup), m.Version)
		_ = os.Remove(pendingApplyFile())
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
		recordUpdatePending("newfile", filepath.Base(target), curName, m.Version)
		_ = os.Remove(pendingApplyFile())
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

// relaunchFn 供测试替换（生产环境就是 relaunchSelf —— 它会 os.Exit，没法在单测里跑）
var relaunchFn = relaunchSelf

// relaunchSelf 启动新 exe 并退出当前进程。
// 先释放单实例锁，让新进程能立即拿到锁；用一次性环境变量告知新进程「这是重启，别把自己当重复实例」。
// target 为空时回退到 os.Executable()。
// extraEnv 用于追加场景标记（如回滚时的 MES_UPDATE_ROLLBACK=1），便于新进程在日志里区分。
func relaunchSelf(target string, extraEnv ...string) {
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
	cmd.Env = append(cmd.Env, extraEnv...)
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

// updateCheckState 自动检查更新的节流状态。
// Last 只在「真的拿到了应答」时才写；连不上更新源时只写 Next（短退避），
// 这样一次网络抖动不会让机器 24 小时内不再检查更新。
type updateCheckState struct {
	Last  int64 `json:"last"`
	Fails int   `json:"fails,omitempty"`
	Next  int64 `json:"next,omitempty"`
}

func loadUpdateCheckState() updateCheckState {
	var s updateCheckState
	if b, err := os.ReadFile(autoUpdateStatePath()); err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveUpdateCheckState(s updateCheckState) {
	_ = os.WriteFile(autoUpdateStatePath(),
		[]byte(fmt.Sprintf(`{"last":%d,"fails":%d,"next":%d}`, s.Last, s.Fails, s.Next)), 0644)
}

func autoUpdateCheckedRecently() bool {
	s := loadUpdateCheckState()
	now := time.Now().Unix()
	if s.Next > now {
		return true // 失败后的退避窗口内
	}
	return s.Last > 0 && now-s.Last < 24*3600
}

// autoUpdateMarkChecked 检查成功（无论有没有新版），重置退避并进入 24h 节流
func autoUpdateMarkChecked() {
	s := loadUpdateCheckState()
	s.Last = time.Now().Unix()
	s.Fails = 0
	s.Next = 0
	saveUpdateCheckState(s)
}

// autoUpdateMarkFailed 更新源不可达：按 5 / 15 / 60 分钟退避，不算「检查过」
func autoUpdateMarkFailed() int {
	backoff := []int64{300, 900, 3600}
	s := loadUpdateCheckState()
	s.Fails++
	step := backoff[len(backoff)-1]
	if s.Fails-1 < len(backoff) {
		step = backoff[s.Fails-1]
	}
	s.Next = time.Now().Unix() + step
	saveUpdateCheckState(s)
	return s.Fails
}

// autoUpdate 后台静默检查并就地更新 exe；更新在下次启动生效（不强制重启，避免打断正在用的用户）。
// 任何异常都吞掉——自动更新失败绝不能影响工具本身。
func autoUpdate() {
	defer func() { _ = recover() }()
	time.Sleep(3 * time.Second)
	// 上一次落位失败过（exe 被占用等）：这次不看 24h 节流，直接再试一次
	retryPending := fileExists(pendingApplyFile())
	if !retryPending && autoUpdateCheckedRecently() {
		return
	}
	m, reachable := resolveUpdate()
	if !reachable {
		// GitHub 没应答：不能记成「已检查」，否则一次网络抖动就是一天不再查
		n := autoUpdateMarkFailed()
		appLog("自动更新：更新源暂时不可达（第 %d 次），稍后重试", n)
		return
	}
	autoUpdateMarkChecked()
	if m == nil {
		return
	}
	if _, err := applyUpdateFromManifest(m); err != nil {
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
	m, reachable := resolveUpdate()
	if m == nil {
		msg := fmt.Sprintf(L["msg_update_latest"], appVersion)
		if !reachable {
			appLog("手动更新：GitHub 不可达，按已是最新处理")
		}
		writeJSON(w, map[string]interface{}{
			"ok": true, "restart": false, "reachable": reachable,
			"message": msg,
		})
		return
	}
	final, err := applyUpdateFromManifest(m)
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
