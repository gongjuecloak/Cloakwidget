//go:build !windows

package main

// 非 Windows 平台不做单实例限制（本工具主要面向 Windows 使用）。
func acquireSingleInstance() bool { return true }
