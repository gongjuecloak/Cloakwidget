//go:build windows

package main

// 单实例互斥：用系统命名互斥体（named mutex）判定「是否已有本程序在运行」。
//
// 为什么不用「读 CreateMutexW 的 last error」：Windows 的 CreateMutexW 在**成功创建**时
// 不会清除上一次遗留的 last error，若仅凭 last error == ERROR_ALREADY_EXISTS 判断，
// 可能把「新实例」误判成「已有实例」，症状是用户双击 exe 却打不开（进程直接退出）。
// 所以这里改用 OpenMutexW 先探测存在性，再创建——不依赖 last error，判定更稳。
//
// 互斥体句柄保存在全局变量里，进程存活期间一直不释放；进程退出（含被任务管理器结束）
// 时由系统自动回收，不会留下残留。

import (
	"runtime"
	"unsafe"
)

var (
	procCreateMutexW = kernel32Proc.NewProc("CreateMutexW")
	procOpenMutexW   = kernel32Proc.NewProc("OpenMutexW")
	procCloseHandle  = kernel32Proc.NewProc("CloseHandle")
	mutexHandle      uintptr
)

const (
	// 全局唯一名（Global\ 前缀：跨登录会话/服务上下文也唯一）
	singleInstanceName = `Global\MESMaterialConverter_SingleInstance_v1`
	synchronize        = 0x00100000 // SYNCHRONIZE：仅用于「能否打开」的存在性探测
)

// acquireSingleInstance 返回 true 表示本进程是首个实例；false 表示已有实例在运行。
// 任何异常（拿不到句柄）都返回 true——绝不因为互斥本身失败而挡住用户使用。
func acquireSingleInstance() bool {
	name := utf16z(singleInstanceName)
	p := uintptr(unsafe.Pointer(&name[0]))

	if h, _, _ := procOpenMutexW.Call(synchronize, 0, p); h != 0 {
		_, _, _ = procCloseHandle.Call(h)
		return false
	}
	h, _, err := procCreateMutexW.Call(0, 0, p)
	runtime.KeepAlive(name)
	if h == 0 {
		appLog("单实例互斥体创建失败（%v），跳过重复启动检测。", err)
		return true
	}
	mutexHandle = h
	return true
}

// releaseSingleInstance 主动关闭命名互斥体句柄。用于「就地更新后重启」场景：
// 旧进程先释放锁，新进程才能立即拿到锁，避免被误判为重复实例而退出。
func releaseSingleInstance() {
	if mutexHandle != 0 {
		procCloseHandle.Call(mutexHandle)
		mutexHandle = 0
	}
}
