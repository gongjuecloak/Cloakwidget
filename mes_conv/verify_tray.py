# -*- coding: utf-8 -*-
"""端到端验证托盘：启动 exe → 找到隐藏窗口 → 模拟「退出」→ 确认进程已退出"""
import ctypes
import os
import subprocess
import sys
import time
from ctypes import wintypes

sys.stdout = __import__('io').TextIOWrapper(sys.stdout.buffer, encoding='utf-8')

HERE = os.path.dirname(os.path.abspath(__file__))
EXE = os.path.join(HERE, "物料档案转换工具.exe")
CLASS_NAME = "MesMaterialConvTrayWnd"
WM_DESTROY = 0x0002

user32 = ctypes.WinDLL("user32", use_last_error=True)
user32.FindWindowW.restype = wintypes.HWND
user32.FindWindowW.argtypes = [wintypes.LPCWSTR, wintypes.LPCWSTR]
user32.PostMessageW.argtypes = [wintypes.HWND, wintypes.UINT, wintypes.WPARAM, wintypes.LPARAM]
user32.IsWindow.argtypes = [wintypes.HWND]
user32.IsWindow.restype = wintypes.BOOL

# 0) 先清掉可能残留的实例（否则新进程会因「已有实例」直接退出，找不到属于它的窗口）
for name in ("物料档案转换工具.exe", "物料档案转换工具_new.exe", "mesconv.exe"):
    subprocess.run(["taskkill", "/F", "/IM", name], capture_output=True)
time.sleep(1)

# 1) 启动（-nobrowser 免得抢焦点）
p = subprocess.Popen([EXE, "-nobrowser"], cwd=HERE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
print("已启动 pid =", p.pid)
time.sleep(7)

if p.poll() is not None:
    print("FAIL: 进程提前退出，exit =", p.poll())
    sys.exit(1)

# 2) 隐藏窗口应已创建
hwnd = user32.FindWindowW(CLASS_NAME, None)
print("隐藏窗口 hwnd =", hwnd)
if not hwnd:
    print("FAIL: 找不到托盘窗口（说明托盘没初始化成功）")
    subprocess.run(["taskkill", "/F", "/PID", str(p.pid)], capture_output=True)
    sys.exit(1)

# 3) 日志里应有「托盘图标已挂上」的留痕，并列出增强后的菜单项
import glob
logf = sorted(glob.glob(os.path.join(HERE, "logs", "server_*.log")))
if not logf:
    print("FAIL: 没有找到日志文件")
    subprocess.run(["taskkill", "/F", "/PID", str(p.pid)], capture_output=True)
    sys.exit(1)
tail = open(logf[-1], encoding="utf-8", errors="replace").read()[-4000:]
tray_line = [ln for ln in tail.splitlines() if "托盘图标" in ln]
print("托盘日志:", tray_line[-1] if tray_line else "(无)")
if not tray_line:
    print("FAIL: 日志里没有托盘已就绪的记录")
    subprocess.run(["taskkill", "/F", "/PID", str(p.pid)], capture_output=True)
    sys.exit(1)
missing = [w for w in ("打开界面", "打开输出文件夹", "重启", "检查更新", "退出") if w not in tray_line[-1]]
if missing:
    print("FAIL: 托盘日志未提到这些菜单项:", missing)
    subprocess.run(["taskkill", "/F", "/PID", str(p.pid)], capture_output=True)
    sys.exit(1)
print("PASS: 托盘日志已列出 5 个菜单入口")

# 4) 模拟右键菜单里的「退出」（最终就是销毁窗口 → WM_DESTROY）
user32.PostMessageW(hwnd, WM_DESTROY, 0, 0)
print("已发送 WM_DESTROY（等价于点「退出」）")

# 5) 等它自己收尾退出
for _ in range(20):
    if p.poll() is not None:
        break
    time.sleep(0.5)

code = p.poll()
if code is None:
    print("FAIL: 5 秒后进程仍在运行（退出路径没生效）")
    subprocess.run(["taskkill", "/F", "/PID", str(p.pid)], capture_output=True)
    sys.exit(1)

print("PASS: 进程已退出，exit =", code)
print("窗口是否仍存在:", bool(user32.IsWindow(hwnd)))
print("剩余窗口:", user32.FindWindowW(CLASS_NAME, None))
