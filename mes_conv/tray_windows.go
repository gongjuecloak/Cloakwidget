//go:build windows

package main

// 系统托盘：程序在后台运行时常驻右下角，右键菜单可
// 「最近转换摘要 / 打开界面 / 打开输出文件夹 / 重启程序 / 检查更新 / 退出」，
// 双击图标直接打开界面。
//
// 实现方式：纯 Go 调用 Win32 API（不引入 CGO、不加第三方依赖），
// 建一个不可见的隐藏窗口挂消息循环，用 Shell_NotifyIcon 把图标挂到托盘。
// 退出走「销毁窗口 → WM_DESTROY → 清除托盘图标 → 退出消息循环 → os.Exit」。

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// ---------- Win32 常量 ----------

const (
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmApp           = 0x8000

	nimAdd    = 0 // NIM_ADD
	nimDelete = 2 // NIM_DELETE

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04

	mfString    = 0x0000
	mfGrayed    = 0x0001 // 灰掉的项：看得见、点不动
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	idiApplication = 32512 // IDI_APPLICATION

	trayCmdOpen    = 1
	trayCmdFolder  = 2
	trayCmdQuit    = 3
	trayCmdRestart = 4
	trayCmdUpdate  = 5
)

// 托盘回调消息（WM_APP+1），图标上的鼠标事件都会以这个消息送到窗口
const trayCallbackMsg = wmApp + 1

// ---------- Win32 结构体（字段顺序/对齐须与 SDK 一致） ----------

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type notifyIconDataW struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type point struct {
	x int32
	y int32
}

type msgW struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// ---------- DLL 过程 ----------

var (
	user32Proc   = syscall.NewLazyDLL("user32.dll")
	shell32Proc  = syscall.NewLazyDLL("shell32.dll")
	kernel32Proc = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW    = user32Proc.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32Proc.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32Proc.NewProc("DefWindowProcW")
	procGetMessageW         = user32Proc.NewProc("GetMessageW")
	procTranslateMessage    = user32Proc.NewProc("TranslateMessage")
	procDispatchMessageW    = user32Proc.NewProc("DispatchMessageW")
	procPostMessageW        = user32Proc.NewProc("PostMessageW")
	procPostQuitMessage     = user32Proc.NewProc("PostQuitMessage")
	procDestroyWindow       = user32Proc.NewProc("DestroyWindow")
	procCreatePopupMenu     = user32Proc.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32Proc.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32Proc.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32Proc.NewProc("DestroyMenu")
	procGetCursorPos        = user32Proc.NewProc("GetCursorPos")
	procSetForegroundWindow = user32Proc.NewProc("SetForegroundWindow")
	procLoadIconW           = user32Proc.NewProc("LoadIconW")
	procGetMenuItemCount    = user32Proc.NewProc("GetMenuItemCount")
	procGetMenuStringW      = user32Proc.NewProc("GetMenuStringW")
	procGetMenuState        = user32Proc.NewProc("GetMenuState")
	procShellNotifyIconW    = shell32Proc.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW    = kernel32Proc.NewProc("GetModuleHandleW")
)

// ---------- 状态 ----------

var (
	trayHwnd  uintptr
	trayData  notifyIconDataW
	trayURL   string
	trayLang  string
	trayAlive bool
)

// trayStrings 托盘文案（跟随界面语言）
type trayStrings struct {
	Tip      string
	Open     string
	Folder   string
	Recent   string // 带一个 %s：最近一次转换摘要
	NoRecent string
	Restart  string
	Update   string
	Quit     string
	// 摘要里的量词
	Rows     string
	Problems string
	Warnings string
}

func trayTexts(lang string) trayStrings {
	switch lang {
	case "zht":
		return trayStrings{
			Tip:      "MES 物料檔案轉換工具（執行中，右鍵可結束）",
			Open:     "開啟介面",
			Folder:   "開啟輸出資料夾",
			Recent:   "最近轉換：%s",
			NoRecent: "最近轉換：（暫無記錄）",
			Restart:  "重新啟動程式",
			Update:   "檢查更新…",
			Quit:     "結束",
			Rows:     "列", Problems: "問題", Warnings: "告警",
		}
	case "vi":
		return trayStrings{
			Tip:      "Cong cu chuyen doi ho so vat lieu MES (dang chay, chuot phai de thoat)",
			Open:     "Mo giao dien",
			Folder:   "Mo thu muc ket qua",
			Recent:   "Chuyen gan nhat: %s",
			NoRecent: "Chuyen gan nhat: (chua co)",
			Restart:  "Khoi dong lai chuong trinh",
			Update:   "Kiem tra cap nhat…",
			Quit:     "Thoat",
			Rows:     "dong", Problems: "loi", Warnings: "canh bao",
		}
	case "en":
		return trayStrings{
			Tip:      "MES Material Converter (running, right-click to quit)",
			Open:     "Open interface",
			Folder:   "Open output folder",
			Recent:   "Last conversion: %s",
			NoRecent: "Last conversion: (none yet)",
			Restart:  "Restart program",
			Update:   "Check for updates…",
			Quit:     "Quit",
			Rows:     "rows", Problems: "issues", Warnings: "warnings",
		}
	default:
		return trayStrings{
			Tip:      "MES 物料档案转换工具（运行中，右键可退出）",
			Open:     "打开界面",
			Folder:   "打开输出文件夹",
			Recent:   "最近转换：%s",
			NoRecent: "最近转换：（暂无记录）",
			Restart:  "重启程序",
			Update:   "检查更新…",
			Quit:     "退出",
			Rows:     "行", Problems: "问题", Warnings: "告警",
		}
	}
}

// recentSummary 托盘菜单顶部那行「最近转换」摘要。没有记录时给一句「暂无记录」。
// 只放数字 + 量词，不塞路径 —— 菜单宽度有限，路径会把整行撑爆。
func recentSummary(lang string) string {
	T := trayTexts(lang)
	h := loadHistory()
	if len(h) == 0 {
		return T.NoRecent
	}
	it := h[0]
	when := it.Time
	if len(when) >= 16 { // "2026-10-07 14:32:05" -> "10-07 14:32"
		when = when[5:16]
	}
	parts := []string{fmt.Sprintf("%d %s", it.Rows, T.Rows)}
	if it.Issues > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", it.Issues, T.Problems))
	}
	if it.Warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", it.Warnings, T.Warnings))
	}
	return fmt.Sprintf(T.Recent, strings.Join(append([]string{when}, parts...), " · "))
}

func utf16z(s string) []uint16 {
	p, err := syscall.UTF16FromString(s)
	if err != nil {
		p, _ = syscall.UTF16FromString("MES")
	}
	return p
}

func appendMenuText(hMenu uintptr, flags uintptr, id uintptr, text string) {
	var buf []uint16
	var p uintptr
	if text != "" {
		buf = utf16z(text)
		p = uintptr(unsafe.Pointer(&buf[0]))
	}
	_, _, _ = procAppendMenuW.Call(hMenu, flags, id, p)
	runtime.KeepAlive(buf)
}

// runTray 在主 goroutine 跑托盘消息循环（阻塞直到用户选择退出）。
// HTTP 服务已在别的 goroutine 里跑，两者互不阻塞。
func runTray(port int, lang string) {
	runtime.LockOSThread()
	trayLang = lang
	trayURL = fmt.Sprintf("http://127.0.0.1:%d/", port)
	tip := trayTexts(lang).Tip

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := utf16z("MesMaterialConvTrayWnd")

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(trayWndProc),
		hInstance:     hInst,
		lpszClassName: &className[0],
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		appLog("托盘初始化失败（注册窗口类：%v），程序仍在后台运行，可用任务管理器结束。", err)
		select {}
	}

	title := utf16z("MES 物料档案转换工具")
	hwnd, _, err := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(&className[0])), uintptr(unsafe.Pointer(&title[0])),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		appLog("托盘初始化失败（创建隐藏窗口：%v），程序仍在后台运行。", err)
		select {}
	}
	trayHwnd = hwnd

	// 优先用 exe 自带的图标资源（ID 1），失败退回系统默认图标
	hIcon, _, _ := procLoadIconW.Call(hInst, 1)
	if hIcon == 0 {
		hIcon, _, _ = procLoadIconW.Call(0, idiApplication)
	}

	trayData = notifyIconDataW{
		cbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: trayCallbackMsg,
		hIcon:            hIcon,
	}
	copy(trayData.szTip[:], utf16z(tip))

	if ret, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&trayData))); ret == 0 {
		appLog("托盘图标添加失败（%v），程序仍在后台运行。", err)
		select {}
	}
	trayAlive = true
	appLog("已在右下角显示托盘图标：右键可「打开界面 / 打开输出文件夹 / 重启 / 检查更新 / 退出」，双击可打开界面。")

	var m msgW
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 { // 0 = WM_QUIT，-1 = 出错
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	removeTrayIcon()
	appLog("程序已退出。")
	os.Exit(0)
}

func removeTrayIcon() {
	if trayAlive && trayHwnd != 0 {
		_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&trayData)))
		trayAlive = false
	}
}

func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case trayCallbackMsg:
		switch uint32(lParam) {
		case wmRButtonUp:
			showTrayMenu(hwnd)
		case wmLButtonDblClk:
			go openBrowser(trayURL)
		}
		return 0
	case wmDestroy:
		removeTrayIcon()
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// buildTrayMenu 把托盘右键菜单的项全部挂上，返回菜单句柄。
// 单独抽出来是为了能被测试直接读回菜单文本做断言（不必真的弹出菜单）。
// 结构：摘要（灰显） · 打开界面 / 打开输出文件夹 · 重启程序 / 检查更新 · 退出
func buildTrayMenu(lang string) uintptr {
	T := trayTexts(lang)
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return 0
	}

	// 顶部一条「最近转换」摘要：只读信息，灰掉不可点
	appendMenuText(hMenu, mfString|mfGrayed, 0, recentSummary(lang))
	appendMenuText(hMenu, mfSeparator, 0, "")

	appendMenuText(hMenu, mfString, trayCmdOpen, T.Open)
	appendMenuText(hMenu, mfString, trayCmdFolder, T.Folder)
	appendMenuText(hMenu, mfSeparator, 0, "")
	appendMenuText(hMenu, mfString, trayCmdRestart, T.Restart)
	appendMenuText(hMenu, mfString, trayCmdUpdate, T.Update)
	appendMenuText(hMenu, mfSeparator, 0, "")
	appendMenuText(hMenu, mfString, trayCmdQuit, T.Quit)
	return hMenu
}

func showTrayMenu(hwnd uintptr) {
	hMenu := buildTrayMenu(trayLang)
	if hMenu == 0 {
		return
	}
	defer func() { _, _, _ = procDestroyMenu.Call(hMenu) }()

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 先置前台，否则点击别处时菜单不会消失
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(hMenu,
		tpmRightButton|tpmReturnCmd,
		uintptr(int(pt.x)), uintptr(int(pt.y)), 0, hwnd, 0)
	_, _, _ = procPostMessageW.Call(hwnd, wmNull, 0, 0)

	switch cmd {
	case trayCmdOpen:
		go openBrowser(trayURL)
	case trayCmdFolder:
		openOutFolder()
	case trayCmdRestart:
		restartSelf()
		_, _, _ = procDestroyWindow.Call(hwnd) // 触发 WM_DESTROY → 清理并退出
	case trayCmdUpdate:
		go openBrowser("https://github.com/" + releaseRepo + "/releases")
	case trayCmdQuit:
		_, _, _ = procDestroyWindow.Call(hwnd) // 触发 WM_DESTROY → 清理并退出
	}
}

// restartSelf 重启程序：交给 cmd 延迟 2 秒再拉起新进程，
// 留出时间让本进程退出、把监听端口让出来（否则新进程会以为「已有实例在跑」而只开浏览器）。
func restartSelf() {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return
	}
	line := `"` + exe + `"`
	for _, a := range os.Args[1:] {
		line += ` "` + a + `"`
	}
	c := exec.Command("cmd", "/c", "timeout /t 2 /nobreak >nul & start \"\" "+line)
	c.Dir = exeDir()
	_ = c.Start()
	appLog("正在重启程序…")
}
