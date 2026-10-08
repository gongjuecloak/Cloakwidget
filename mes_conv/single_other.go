//go:build !windows

package main

// 非 Windows 平台不做单实例限制（本工具主要面向 Windows 使用）。
func acquireSingleInstance() bool { return true }

// releaseSingleInstance 在 Windows 上用于「更新后重启」；非 Windows 无锁可释放，留空。
func releaseSingleInstance() {}
