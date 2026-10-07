//go:build !windows

package main

// 非 Windows 平台没有系统托盘，退化为「服务在后台跑、进程阻塞不退出」。
func runTray(port int, lang string) {
	appLog("当前系统不支持系统托盘，程序将在后台持续运行（结束请直接关闭本进程）。")
	select {}
}
