package main

// ============================================================================
// 一键打包排障包
//
// 出了问题时最麻烦的往往不是修，而是"说不清" —— 用户描述半天，这边还要
// 一样样问：什么版本？日志在哪？配置长什么样？模板变过没有？
//
// 所以干脆一键把「定位问题真正需要的东西」打成一个 zip：
//   日志尾部 / 三份配置 / 模板基准 / 脱敏后的系统设置 / 环境信息 / 最近转换历史
// 口令哈希不进去（脱敏），其余都是本机自己的东西，没有外泄风险。
// ============================================================================

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// readTail 读文件尾部最多 limit 字节；文件比 limit 大时从下一个换行处切开，
// 免得日志第一行是半截的。
func readTail(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	off := int64(0)
	if size > limit {
		off = size - limit
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && len(buf) == 0 {
		return nil, err
	}
	if off > 0 {
		if i := strings.IndexByte(string(buf), '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf, nil
}

func readOrEmpty(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return b
}

func addZip(w *zip.Writer, name string, data []byte) {
	if len(data) == 0 {
		return
	}
	fw, err := w.Create(name)
	if err != nil {
		return
	}
	_, _ = fw.Write(data)
}

// envReport 环境信息（纯文本，方便直接贴给人看）
func envReport() string {
	var b strings.Builder
	s := sysSettings()
	on, cmd := autostartEnabled()
	autoStr := "否"
	if on {
		autoStr = "是（" + cmd + "）"
	}
	lanStr := "关"
	listen := "127.0.0.1（仅本机）"
	if s.LAN {
		lanStr = "开"
		listen = "0.0.0.0（局域网）"
	}
	pwdStr := "未启用"
	if s.PasswordHash != "" {
		pwdStr = "已启用（哈希存储）"
	}
	exe, _ := os.Executable()
	wd, _ := os.Getwd()

	fmt.Fprintf(&b, "MES 物料档案转换工具 —— 排障包\n")
	fmt.Fprintf(&b, "生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "版本    : %s (%s)\n", appVersion, buildTime)
	fmt.Fprintf(&b, "运行时  : %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "程序路径: %s\n", exe)
	fmt.Fprintf(&b, "工作目录: %s\n", wd)
	fmt.Fprintf(&b, "\n-- 服务 --\n")
	fmt.Fprintf(&b, "端口    : %d\n", currentPort)
	fmt.Fprintf(&b, "监听    : %s\n", listen)
	fmt.Fprintf(&b, "访问地址: http://127.0.0.1:%d/\n", currentPort)
	if u := lanURLs(); len(u) > 0 {
		fmt.Fprintf(&b, "局域网地址: %s\n", strings.Join(u, "  "))
	}
	fmt.Fprintf(&b, "\n-- 设置 --\n")
	fmt.Fprintf(&b, "开机自启: %s\n", autoStr)
	fmt.Fprintf(&b, "局域网访问: %s\n", lanStr)
	fmt.Fprintf(&b, "口令保护: %s\n", pwdStr)
	fmt.Fprintf(&b, "\n-- 路径 --\n")
	fmt.Fprintf(&b, "配置文件: %s\n", configPath())
	fmt.Fprintf(&b, "订单配置: %s\n", ordersConfigPath())
	fmt.Fprintf(&b, "系统设置: %s\n", sysSettingsPath())
	fmt.Fprintf(&b, "模板基准: %s\n", tplBaselinePath())
	fmt.Fprintf(&b, "日志目录: %s\n", filepath.Dir(appLogPath()))
	fmt.Fprintf(&b, "输出目录: %s\n", filepath.Join(exeDir(), "out"))
	fmt.Fprintf(&b, "备份目录: %s\n", backupRoot())
	return b.String()
}

// diagJSON 把历史记录之类的结构化数据写成可读 JSON（空则跳过）
func diagJSON(v interface{}) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil
	}
	return b
}

// buildDiagBundle 生成排障包，返回绝对路径
func buildDiagBundle() (string, error) {
	dir := filepath.Join(exeDir(), "out")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("排障包_%s_v%s.zip", time.Now().Format("20060102_150405"), appVersion)
	p := filepath.Join(dir, name)

	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	// 1) 日志尾部（最多 512KB，够看清最近几次转换）
	if b, err := readTail(appLogPath(), 512*1024); err == nil {
		addZip(zw, "logs/"+filepath.Base(appLogPath()), b)
	}
	// 2) 三份配置 + 模板基准：原样，里面没有机密
	addZip(zw, "config/mapping.json", readOrEmpty(configPath()))
	addZip(zw, "config/orders.json", readOrEmpty(ordersConfigPath()))
	addZip(zw, "config/tpl_baseline.json", readOrEmpty(tplBaselinePath()))
	// 3) 系统设置：口令哈希脱敏后再放进去
	s := sysSettings()
	s.PasswordHash = ""
	addZip(zw, "config/sys_settings.json", diagJSON(s))
	// 4) 环境信息
	addZip(zw, "env.txt", []byte(envReport()))
	// 5) 最近转换历史（物料 + 订单各取最近 20 条）
	hist := loadHistory()
	if len(hist) > 20 {
		hist = hist[:20]
	}
	addZip(zw, "history/materials.json", diagJSON(hist))
	if b := readOrEmpty(ordersHistoryPath()); len(b) > 0 {
		addZip(zw, "history/orders_raw.json", b)
	}

	if err := zw.Close(); err != nil {
		return "", err
	}
	return p, nil
}

// handlerDiag 生成排障包并返回它（界面据此给出下载按钮）
func handlerDiag(w http.ResponseWriter, r *http.Request) {
	lang := reqLang(r)
	L := msgs(lang)
	p, err := buildDiagBundle()
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"ok": false, "error": fmt.Sprintf(L["msg_diag_fail"], err.Error()),
		})
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"file":    filepath.Base(p),
		"path":    p,
		"message": fmt.Sprintf(L["msg_diag_ok"], p),
	})
}
