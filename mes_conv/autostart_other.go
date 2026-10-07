//go:build !windows

package main

import "fmt"

// 非 Windows 平台没有 HKCU Run 这一套，直接报错让界面提示不支持。
func autostartEnabled() (bool, string) { return false, "" }

func setAutostart(on bool) error {
	return fmt.Errorf("当前系统不支持开机自启")
}
