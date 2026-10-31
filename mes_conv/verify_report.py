# -*- coding: utf-8 -*-
"""端到端验证：转换留下的问题报告，重启服务后仍可查看"""
import io
import json
import mimetypes
import os
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import uuid

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')

HERE = os.path.dirname(os.path.abspath(__file__))
EXE = os.environ.get("MES_EXE", os.path.join(HERE, "物料档案转换工具.exe"))
PORT = int(os.environ.get("MES_PORT", "8741"))
BASE = "http://127.0.0.1:%d" % PORT
SRC = os.environ.get("MES_SRC_FILE", os.path.join(os.path.dirname(HERE), "_fixtures", "materials.xlsx"))
TPL = os.environ.get("MES_TPL_FILE", os.path.join(os.path.dirname(HERE), "_fixtures", "materials_template.xlsx"))

for label, path in (("程序", EXE), ("源文件", SRC), ("模板", TPL)):
    if not os.path.isfile(path):
        raise SystemExit("%s不存在：%s（可用 MES_EXE / MES_SRC_FILE / MES_TPL_FILE 指定）" % (label, path))

fail = 0


def check(name, cond, extra=""):
    global fail
    if not cond:
        fail += 1
    print(("PASS  " if cond else "FAIL  ") + name + (("   " + str(extra)) if extra else ""))


def start():
    p = subprocess.Popen([EXE, "-nobrowser", "-nosingle", "-port", str(PORT)], cwd=HERE,
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(5)
    if p.poll() is not None:
        print("FAIL  进程启动后立即退出, exit =", p.poll())
        sys.exit(1)
    return p


def post_multipart(url, fields, files):
    boundary = uuid.uuid4().hex
    body = b""
    for k, v in fields.items():
        body += ('--%s\r\nContent-Disposition: form-data; name="%s"\r\n\r\n' % (boundary, k)).encode("utf-8")
        body += v.encode("utf-8") + b"\r\n"
    for k, path in files.items():
        # 用真实文件名（含中文），顺便验证中文文件名链路
        fn = os.path.basename(path)
        ctype = mimetypes.guess_type(fn)[0] or "application/octet-stream"
        body += ('--%s\r\nContent-Disposition: form-data; name="%s"; filename="%s"\r\nContent-Type: %s\r\n\r\n'
                 % (boundary, k, fn, ctype)).encode("utf-8")
        with open(path, "rb") as f:
            body += f.read() + b"\r\n"
    body += ("--%s--\r\n" % boundary).encode("utf-8")
    req = urllib.request.Request(url, data=body,
                                 headers={"Content-Type": "multipart/form-data; boundary=" + boundary})
    return json.loads(urllib.request.urlopen(req, timeout=600).read().decode("utf-8"))


def get_json(path):
    return json.loads(urllib.request.urlopen(BASE + path, timeout=30).read().decode("utf-8"))


rep_dir = os.path.join(HERE, "out", "reports")

print("\n--- 第一次启动服务 ---")
p1 = start()

cfg = open(os.path.join(HERE, "mapping.json"), encoding="utf-8").read()
resp = post_multipart(BASE + "/api/convert",
                      {"config": cfg, "lang": "zh"},
                      {"source": SRC, "template": TPL})
check("转换成功", resp.get("ok") is True)
check("返回行数 577", resp.get("rows") == 577, resp.get("rows"))
rid = resp.get("report_id")
check("响应带 report_id", bool(rid), rid)

# 报告文件应已落盘
f = os.path.join(rep_dir, rid) if rid else ""
check("报告文件已写入 out/reports/", bool(rid) and os.path.isfile(f), rid)
if rid and os.path.isfile(f):
    rep = json.load(open(f, encoding="utf-8"))
    check("报告含 source 文件名", "577" in (rep.get("source") or ""), rep.get("source"))
    check("报告含 issue_total 字段", "issue_total" in rep, rep.get("issue_total"))
    check("报告含 warnings/issues 数组",
          isinstance(rep.get("warnings"), list) and isinstance(rep.get("issues"), list))

# 历史里应带上 report
hist = get_json("/api/history")
items = hist.get("items") or []
check("历史有记录", len(items) > 0, len(items))
check("历史首条带 report 字段", bool(items[0].get("report")), items[0].get("report"))
check("历史首条 report == 本次", items[0].get("report") == rid)

# 直接查报告接口
rd = get_json("/api/report?id=" + urllib.parse.quote(rid or ""))
check("GET /api/report 成功", rd.get("ok") is True)
check("报告 rows 与转换一致", (rd.get("report") or {}).get("rows") == 577)

# 目录穿越防护
rd2 = get_json("/api/report?id=" + urllib.parse.quote("../mapping.json"))
check("拒绝目录穿越 id", rd2.get("ok") is False, rd2.get("error"))

print("\n--- 杀进程，模拟「关掉程序/重启服务」 ---")
p1.terminate()
p1.wait(timeout=8)
time.sleep(1)

p2 = start()
print("服务已重启（新进程 pid = %d）" % p2.pid)

hist2 = get_json("/api/history")
check("重启后历史仍在", len(hist2.get("items") or []) > 0, len(hist2.get("items") or []))
rd3 = get_json("/api/report?id=" + urllib.parse.quote(rid or ""))
check("★ 重启后仍能查看该次问题详情", rd3.get("ok") is True and (rd3.get("report") or {}).get("rows") == 577)

# 页面里也要能拿到（顺带确认首页是新版）
html = urllib.request.urlopen(BASE + "/", timeout=30).read().decode("utf-8")
check("首页含「查看问题」按钮逻辑", "jsview" in html and "viewReport" in html)
check("首页含报告接口调用", "/api/report?id=" in html)

p2.terminate()
p2.wait(timeout=8)
print("\n" + ("全部通过 ✓" if fail == 0 else "失败 %d 项" % fail))
sys.exit(0 if fail == 0 else 1)
