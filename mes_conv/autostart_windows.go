//go:build windows

package main

// ============================================================================
// 开机自启：往 HKCU\Software\Microsoft\Windows\CurrentVersion\Run 写一条启动项
//
// 只影响当前用户（HKCU），不需要管理员权限 —— 这一点很重要，工具是绿色版，
// 装到 HKLM 会因为没提权而静默失败。
//
// 注册的命令带 -nobrowser：开机时安静地驻留右下角托盘，不弹浏览器。
// 直接用 advapi32 的注册表 API（与托盘一样走 syscall），不引第三方依赖。
// ============================================================================

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32             = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002
	regSZ           = 1
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartName   = "MES物料档案转换工具"
)

// regOpenRun 打开（必要时创建）自启所在的注册表键
func regOpenRun(create bool) (syscall.Handle, error) {
	sub, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	if create {
		var disp uint32
		r, _, e := procRegCreateKeyExW.Call(
			uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
			0, 0, 0, uintptr(keySetValue|keyQueryValue), 0,
			uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disp)))
		if r != 0 {
			return 0, fmt.Errorf("RegCreateKeyEx 失败：%v", e)
		}
		return h, nil
	}
	r, _, e := procRegOpenKeyExW.Call(
		uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, uintptr(keySetValue|keyQueryValue), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, fmt.Errorf("RegOpenKeyEx 失败：%v", e)
	}
	return h, nil
}

func regSetString(h syscall.Handle, name, val string) error {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	d, err := syscall.UTF16FromString(val) // 自带结尾 NUL
	if err != nil {
		return err
	}
	r, _, e := procRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0,
		uintptr(regSZ), uintptr(unsafe.Pointer(&d[0])), uintptr(len(d)*2))
	if r != 0 {
		return fmt.Errorf("RegSetValueEx 失败：%v", e)
	}
	return nil
}

func regQueryString(h syscall.Handle, name string) (string, bool) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	var typ uint32
	buf := make([]uint16, 2048)
	size := uint32(len(buf) * 2)
	r, _, _ := procRegQueryValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

// autostartCommand 注册进 Run 的命令行：带引号的 exe 路径 + -nobrowser
func autostartCommand() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return ""
	}
	return `"` + exe + `" -nobrowser`
}

// autostartEnabled 当前是否已设置自启；第二个返回值是注册的命令（便于界面显示实际路径）
func autostartEnabled() (bool, string) {
	h, err := regOpenRun(false)
	if err != nil {
		return false, ""
	}
	defer procRegCloseKey.Call(uintptr(h))
	v, ok := regQueryString(h, autostartName)
	return ok && strings.TrimSpace(v) != "", v
}

// setAutostart 打开 / 关闭自启
func setAutostart(on bool) error {
	h, err := regOpenRun(true)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(uintptr(h))

	if on {
		cmd := autostartCommand()
		if cmd == "" {
			return fmt.Errorf("无法定位程序路径")
		}
		return regSetString(h, autostartName, cmd)
	}
	if _, ok := regQueryString(h, autostartName); !ok {
		return nil // 本来就没有，删除视为成功
	}
	n, err := syscall.UTF16PtrFromString(autostartName)
	if err != nil {
		return err
	}
	r, _, e := procRegDeleteValueW.Call(uintptr(h), uintptr(unsafe.Pointer(n)))
	if r != 0 {
		return fmt.Errorf("RegDeleteValue 失败：%v", e)
	}
	return nil
}
