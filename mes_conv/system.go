package main

// ============================================================================
// 系统设置：口令锁 + 局域网访问 + 开机自启开关
//
// 前两个是「把工具从本机搬到局域网」时绕不开的事：
//   · 局域网访问 —— 同事的浏览器能打开界面，但如果没有口令，谁都能点「转换」
//   · 口令锁     —— 设了口令之后，除探活与认证入口外所有接口都要带 token
//
// 设置落在 exe 同目录的 sys_settings.json：{"password_hash":"salt$hash","lan":false}
// 口令只存 salt + sha256，不存明文；token 只存在内存里（重启即失效）。
// ============================================================================

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
)

// SysSettings 落盘的系统设置
type SysSettings struct {
	// PasswordHash salt$sha256hex(salt+pwd)；空串 = 未启用口令
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

func hashPassword(pwd string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	h := sha256.Sum256(append(append([]byte{}, salt...), []byte(pwd)...))
	return hex.EncodeToString(salt) + "$" + hex.EncodeToString(h[:])
}

func verifyPassword(stored, pwd string) bool {
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

// authMiddleware 口令锁。未设口令时完全透传；设了口令后白名单之外全要 token。
// token 可以放在 X-Token 头、query（?token=，给 /download 这类直链用）或表单里。
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
			tok = r.URL.Query().Get("token")
		}
		if tok == "" {
			tok = r.FormValue("token")
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
	writeJSON(w, map[string]interface{}{"ok": true, "token": issueToken(), "need_auth": true})
}

// handlerAuthState 界面启动时问一句：需要口令吗？当前 token 还有效吗？
func handlerAuthState(w http.ResponseWriter, r *http.Request) {
	s := sysSettings()
	need := s.PasswordHash != ""
	tok := r.Header.Get("X-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
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
func handlerSetLAN(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	on := r.FormValue("on") == "1"
	s := sysSettings()
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
