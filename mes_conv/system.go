package main

// ============================================================================
// 系统设置：口令锁 + 局域网访问 + 开机自启开关
//
// 前两个是「把工具从本机搬到局域网」时绕不开的事：
//   · 局域网访问 —— 同事的浏览器能打开界面，但如果没有口令，谁都能点「转换」
//   · 口令锁     —— 设了口令之后，除探活与认证入口外所有接口都要带 token
//
// 设置落在 exe 同目录的 sys_settings.json：{"password_hash":"$argon2id$...","lan":false}
// 口令用 Argon2id 加盐慢哈希，只存哈希不存明文；token 只存在内存里（重启即失效）。
// ============================================================================

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// SysSettings 落盘的系统设置
type SysSettings struct {
	// PasswordHash 口令哈希；空串 = 未启用口令。
	// 现为 Argon2id 自描述串（$argon2id$...），仍兼容旧 sha256 格式并在登录时升级。
	PasswordHash string `json:"password_hash,omitempty"`
	// LAN 是否允许局域网访问。改动需重启才生效（监听地址在启动时确定）
	LAN bool `json:"lan"`
}

func sysSettingsPath() string { return filepath.Join(exeDir(), "sys_settings.json") }

var (
	sysMu    sync.RWMutex
	sysCache *SysSettings
)

// sysSettings 读设置（带进程内缓存）
func sysSettings() SysSettings {
	sysMu.RLock()
	if sysCache != nil {
		s := *sysCache
		sysMu.RUnlock()
		return s
	}
	sysMu.RUnlock()

	sysMu.Lock()
	defer sysMu.Unlock()
	if sysCache != nil {
		return *sysCache
	}
	s := &SysSettings{}
	if b, err := os.ReadFile(sysSettingsPath()); err == nil && len(b) > 0 {
		_ = json.Unmarshal(b, s)
	}
	sysCache = s
	return *s
}

func saveSysSettings(s SysSettings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(sysSettingsPath(), b, 0o644); err != nil {
		return err
	}
	sysMu.Lock()
	cp := s
	sysCache = &cp
	sysMu.Unlock()
	return nil
}

// ---------- 口令 ----------
//
// 口令用 Argon2id 存储（自带盐、慢、内存硬，适合口令而非通用哈希）。
// 编码为自描述字符串，便于以后调参或换算法而不动数据结构：
//
//	$argon2id$v=19$m=65536,t=3,p=2$<base64(salt)>$<base64(hash)>
//
// 老版本存的是 "hex(salt)$hex(sha256(salt+pwd))"，仍可校验通过；
// 用户一旦用老口令登录成功，就地升级为 Argon2id（见 handlerAuth）。

const (
	argon2Time    uint32 = 3
	argon2Memory  uint32 = 64 * 1024 // 64 MiB
	argon2Threads uint8  = 2
	argon2KeyLen  uint32 = 32
	argon2SaltLen        = 16
)

// argon2Encode 生成一条 Argon2id 记录
func argon2Encode(pwd string) string {
	salt := make([]byte, argon2SaltLen)
	_, _ = rand.Read(salt)
	sum := argon2.IDKey([]byte(pwd), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum))
}

// argon2Verify 校验一条 Argon2id 记录
func argon2Verify(stored, pwd string) bool {
	parts := strings.Split(stored, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var ver int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil || ver != argon2.Version {
		return false
	}
	var mem, tim uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &tim, &par); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pwd), salt, tim, mem, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func hashPassword(pwd string) string { return argon2Encode(pwd) }

// needsRehash 判断存量口令是否还是旧的 sha256 格式（登录成功后顺手升级）
func needsRehash(stored string) bool {
	return !strings.HasPrefix(stored, "$argon2id$")
}

// verifyPassword 兼容两种格式：优先 Argon2id，旧记录回退 sha256。
func verifyPassword(stored, pwd string) bool {
	if stored == "" {
		return false
	}
	if strings.HasPrefix(stored, "$argon2id$") {
		return argon2Verify(stored, pwd)
	}
	// 旧格式：hex(salt)$hex(sha256(salt+pwd))
	i := strings.IndexByte(stored, '$')
	if i <= 0 || i == len(stored)-1 {
		return false
	}
	salt, err := hex.DecodeString(stored[:i])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(stored[i+1:])
	if err != nil {
		return false
	}
	got := sha256.Sum256(append(append([]byte{}, salt...), []byte(pwd)...))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// ---------- token ----------

var (
	authMu    sync.Mutex
	authToken string
	authExp   time.Time
)

const authTTL = 8 * time.Hour

func issueToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	authMu.Lock()
	authToken = tok
	authExp = time.Now().Add(authTTL)
	authMu.Unlock()
	return tok
}

func tokenValid(tok string) bool {
	if tok == "" {
		return false
	}
	authMu.Lock()
	defer authMu.Unlock()
	return authToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(authToken)) == 1 &&
		time.Now().Before(authExp)
}

func revokeToken() {
	authMu.Lock()
	authToken = ""
	authExp = time.Time{}
	authMu.Unlock()
}

// ---------- 一次性下载令牌 ----------
//
// /download 是浏览器直链（<a download>），没法带自定义请求头。若把长期会话 token
// 拼在 URL 上（?token=…），会进浏览器历史、服务器/代理日志、Referer 与截图。
// 折中做法：先调 /api/download-token 换一个「短时效 + 用一次即废」的下载令牌，
// 再用它下载；泄漏面与危害都远小于长期 token。

var dlMu sync.Mutex
var dlTokens = map[string]time.Time{} // token -> 过期时间

// issueDownloadToken 签发一次性下载令牌（默认 60 秒）
func issueDownloadToken() string {
	tok := issueToken() // 复用随机源
	dlMu.Lock()
	// 顺手清掉过期项，避免长时间运行累积
	now := time.Now()
	for k, exp := range dlTokens {
		if now.After(exp) {
			delete(dlTokens, k)
		}
	}
	dlTokens[tok] = now.Add(60 * time.Second)
	dlMu.Unlock()
	return tok
}

// consumeDownloadToken 校验并立即作废（一次性）。空/过期/已用过都返回 false。
func consumeDownloadToken(tok string) bool {
	if tok == "" {
		return false
	}
	dlMu.Lock()
	defer dlMu.Unlock()
	exp, ok := dlTokens[tok]
	if !ok {
		return false
	}
	delete(dlTokens, tok) // 用一次即废
	return time.Now().Before(exp)
}

// handlerDownloadToken 签发一次性下载令牌（需通过口令校验才能调用）
func handlerDownloadToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "dt": issueDownloadToken()})
}

// authMiddleware 口令锁。未设口令时完全透传；设了口令后白名单之外全要 token。
// token 只接受请求头 X-Token 或 Authorization: Bearer，不再从 URL / 表单读取
// （避免泄漏到日志与历史）。/download 这类直链改用一次性下载令牌 dt。
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sysSettings().PasswordHash == "" {
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/", "/api/ping", "/api/auth", "/api/auth/state":
			next.ServeHTTP(w, r)
			return
		}
		tok := r.Header.Get("X-Token")
		if tok == "" {
			if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
				tok = strings.TrimSpace(ah[len("Bearer "):])
			}
		}
		// 直链下载：接受尚未使用的一次性下载令牌
		if !tokenValid(tok) && r.URL.Path == "/download" {
			if consumeDownloadToken(r.URL.Query().Get("dt")) {
				next.ServeHTTP(w, r)
				return
			}
		}
		if !tokenValid(tok) {
			writeJSON(w, map[string]interface{}{
				"ok":         false,
				"need_auth":  true,
				"error":      msgs(reqLang(r))["err_pwd_needed"],
				"error_code": "need_auth",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- 局域网地址 ----------

// lanURLs 所有非回环 IPv4 上的访问地址，供界面直接粘给同事
func lanURLs() []string {
	var out []string
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := it.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			out = append(out, fmt.Sprintf("http://%s:%d/", ip4.String(), currentPort))
		}
	}
	return out
}

// ---------- 接口 ----------

// handlerSystemInfo 系统设置总览
func handlerSystemInfo(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	s := sysSettings()
	on, cmd := autostartEnabled()
	writeJSON(w, map[string]interface{}{
		"ok":             true,
		"autostart":      on,
		"autostart_cmd":  cmd,
		"lan":            s.LAN,
		"has_password":   s.PasswordHash != "",
		"lan_insecure":   s.LAN && s.PasswordHash == "",
		"auth":           authToken != "",
		"port":           currentPort,
		"addrs":          lanURLs(),
		"lan_msg_on":     fmt.Sprintf(L["msg_lan_on"], strings.Join(lanURLs(), "  ")),
		"lan_msg_off":    L["msg_lan_off"],
		"auto_msg_on":    L["msg_autostart_on"],
		"auto_msg_off":   L["msg_autostart_off"],
		"update_current": appVersion,
		"first_run":      isFirstRun(),
	})
}

// ---------- 首次运行引导 ----------

// welcomeFlagPath 首次运行标记。看过一次引导就写下来，之后不再弹。
// 放在 exe 同级，程序目录整体拷走时标记也跟着走。
func welcomeFlagPath() string {
	return filepath.Join(exeDir(), "welcomed")
}

// isFirstRun 还没有引导过即视为首次运行。
func isFirstRun() bool {
	_, err := os.Stat(welcomeFlagPath())
	return os.IsNotExist(err)
}

// handlerSetWelcomed 前端点过「开始使用」后回调，标记引导已完成。
func handlerSetWelcomed(w http.ResponseWriter, r *http.Request) {
	body := time.Now().Format("2006-01-02 15:04:05") + "\n"
	if err := os.WriteFile(welcomeFlagPath(), []byte(body), 0644); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// handlerAuth 用口令换 token
func handlerAuth(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	s := sysSettings()
	if s.PasswordHash == "" {
		writeJSON(w, map[string]interface{}{"ok": true, "token": "", "need_auth": false})
		return
	}
	pwd := r.FormValue("password")
	if strings.TrimSpace(pwd) == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["err_pwd_empty"]})
		return
	}
	if !verifyPassword(s.PasswordHash, pwd) {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["err_pwd_wrong"]})
		return
	}
	// 存量口令若是旧 sha256 格式，就地升级为 Argon2id（用户无感，只发生一次）
	if needsRehash(s.PasswordHash) {
		s.PasswordHash = hashPassword(pwd)
		if err := saveSysSettings(s); err == nil {
			appLog("口令哈希已升级为 Argon2id")
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "token": issueToken(), "need_auth": true})
}

// handlerAuthState 界面启动时问一句：需要口令吗？当前 token 还有效吗？
func handlerAuthState(w http.ResponseWriter, r *http.Request) {
	s := sysSettings()
	need := s.PasswordHash != ""
	tok := r.Header.Get("X-Token")
	if tok == "" {
		if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
			tok = strings.TrimSpace(ah[len("Bearer "):])
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok":        true,
		"need_auth": need,
		"valid":     !need || tokenValid(tok),
	})
}

// handlerSetPassword 设置 / 清除口令
func handlerSetPassword(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	s := sysSettings()
	pwd := r.FormValue("password")
	clear := r.FormValue("clear") == "1"

	if clear {
		s.PasswordHash = ""
		if err := saveSysSettings(s); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		revokeToken()
		writeJSON(w, map[string]interface{}{"ok": true, "has_password": false, "message": L["msg_pwd_cleared"]})
		return
	}

	// 已经设过口令的，改口令要先验旧口令，防止局域网里被人直接改掉
	if s.PasswordHash != "" && !verifyPassword(s.PasswordHash, r.FormValue("old_password")) {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["err_pwd_wrong"]})
		return
	}
	if strings.TrimSpace(pwd) == "" {
		writeJSON(w, map[string]interface{}{"ok": false, "error": L["err_pwd_empty"]})
		return
	}
	s.PasswordHash = hashPassword(pwd)
	if err := saveSysSettings(s); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	// 设完口令立刻给当前会话发一个 token，免得用户被自己刚设的口令挡在门外
	tok := issueToken()
	writeJSON(w, map[string]interface{}{
		"ok": true, "has_password": true, "token": tok, "message": L["msg_pwd_saved"],
	})
}

// handlerSetAutostart 开关开机自启
func handlerSetAutostart(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	on := r.FormValue("on") == "1"
	if err := setAutostart(on); err != nil {
		writeJSON(w, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf(L["msg_autostart_fail"], err.Error()),
		})
		return
	}
	msg := L["msg_autostart_off"]
	if on {
		msg = L["msg_autostart_on"]
	}
	writeJSON(w, map[string]interface{}{"ok": true, "autostart": on, "message": msg})
}

// handlerSetLAN 开关局域网访问（保存后需重启生效）
// 安全约束：开启局域网等于把服务暴露给整个网段，若未设访问口令则拒绝开启，
// 避免「谁都能打开界面点转换」。用户须先设置口令。
func handlerSetLAN(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	on := r.FormValue("on") == "1"
	s := sysSettings()
	if on && s.PasswordHash == "" {
		writeJSON(w, map[string]interface{}{
			"ok": false, "error": L["msg_lan_need_pwd"], "need_password": true,
		})
		return
	}
	s.LAN = on
	if err := saveSysSettings(s); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	msg := L["msg_lan_off"]
	if on {
		msg = fmt.Sprintf(L["msg_lan_on"], strings.Join(lanURLs(), "  "))
	}
	writeJSON(w, map[string]interface{}{"ok": true, "lan": on, "message": msg})
}
