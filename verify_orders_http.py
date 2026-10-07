# -*- coding: utf-8 -*-
"""订单 / 工单模块的 HTTP 端到端验证：起服务 → 打接口 → 校验输出 → 收尾。

覆盖：配置读写、真实文件转换、输出落盘、历史记录、报告持久化、
空文件校验、目录穿越防护。
"""
import io
import json
import mimetypes
import os
import subprocess
import sys
import time
import urllib.parse
import urllib.request

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

HERE = os.path.dirname(os.path.abspath(__file__))
EXE = os.path.join(HERE, "mes_conv", "物料档案转换工具_new.exe")
ORDER = r"D:/User/Cloak_Zeng/Code/python/project-001/code-file/015/文件/訂單資料.xlsx"
WORK = r"D:/User/Cloak_Zeng/Code/python/project-001/code-file/015/文件/工單資料.xlsx"
GOLD = os.path.join(HERE, "_golden", "go_订单工单.xlsx")
PORT = 8747
BASE = f"http://127.0.0.1:{PORT}"

passed, failed = 0, 0


def check(name, cond, extra=""):
    global passed, failed
    if cond:
        passed += 1
        print(f"  ✓ {name}")
    else:
        failed += 1
        print(f"  ✗ {name} {extra}")


def get(path, timeout=30):
    with urllib.request.urlopen(BASE + path, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8"))


def post_json(path, obj, timeout=30):
    req = urllib.request.Request(
        BASE + path, data=json.dumps(obj).encode("utf-8"),
        headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8"))


def post_multipart(path, files, fields=None, timeout=180):
    boundary = "----verifyorders" + str(int(time.time() * 1000))
    body = b""
    for k, v in (fields or {}).items():
        body += (f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n').encode("utf-8")
    for k, path_ in files.items():
        if not path_:
            continue
        fn = os.path.basename(path_)
        ctype = mimetypes.guess_type(fn)[0] or "application/octet-stream"
        body += (f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"; filename="{fn}"\r\n'
                 f"Content-Type: {ctype}\r\n\r\n").encode("utf-8")
        with open(path_, "rb") as f:
            body += f.read() + b"\r\n"
    body += f"--{boundary}--\r\n".encode("utf-8")
    req = urllib.request.Request(
        BASE + path, data=body,
        headers={"Content-Type": f"multipart/form-data; boundary={boundary}"}, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8"))


# 清掉旧的订单输出，便于确认本次真的生成了新文件
out_dir = os.path.join(HERE, "mes_conv", "out")
for n in os.listdir(out_dir) if os.path.isdir(out_dir) else []:
    if n.startswith("订单工单_"):
        os.remove(os.path.join(out_dir, n))
hist = os.path.join(out_dir, "orders_history.json")
if os.path.exists(hist):
    os.remove(hist)
print("已清理旧的订单输出与历史\n")

proc = subprocess.Popen([EXE, "-nobrowser", "-port", str(PORT)],
                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
try:
    # 等端口起来
    ok = False
    for _ in range(40):
        time.sleep(0.4)
        try:
            p = get("/api/ping", timeout=2)
            if p.get("ok"):
                ok = True
                break
        except Exception:
            pass
    check("服务已启动并能响应 /api/ping", ok)
    if not ok:
        raise SystemExit(1)

    print("\n【配置接口】")
    cfg = get("/api/orders/config")
    check("/api/orders/config 返回配置", cfg.get("ok"), cfg)
    c = cfg.get("config", {})
    check("配置含 3 类字段映射", len(c.get("field_mapping", {})) == 3, list(c.get("field_mapping", {})))
    check("配置含列顺序（工单 15 列）", len(c.get("column_order", {}).get("work_order", [])) == 15)
    check("配置含别名表", len(c.get("column_aliases", {})) >= 17, len(c.get("column_aliases", {})))
    check("打标前缀已配置", len(c.get("tag_materials", {}).get("prefixes", [])) == 9)

    print("\n【改造后的配置能存回去】")
    save = post_json("/api/orders/config", c)
    check("保存配置成功（字段零丢失）", save.get("ok"), save)
    c2 = get("/api/orders/config").get("config", {})
    check("回读字段映射一致", c2.get("field_mapping") == c.get("field_mapping"))
    check("回读列顺序一致", c2.get("column_order") == c.get("column_order"))
    check("回读别名表一致", c2.get("column_aliases") == c.get("column_aliases"))

    print("\n【转换：订单 + 工单】")
    r = post_multipart("/api/orders/convert", {"order": ORDER, "work": WORK}, {"lang": "zh"})
    check("转换返回 ok", r.get("ok"), r.get("error"))
    check("行数 = 1+3+14", r.get("rows") == 18, r.get("rows"))
    check("输出文件名以 订单工单_ 开头", str(r.get("file", "")).startswith("订单工单_"))
    check("生成了报告 id", str(r.get("report_id", "")).endswith(".json"))
    rowline = r.get("line_match", {})
    check("销售订单行号统计齐全（匹配/默认/无）",
          all(k in rowline for k in ("matched", "unmatched", "no_order")), rowline)
    out_path = os.path.join(out_dir, r.get("file", ""))
    check("输出文件真的落盘", os.path.exists(out_path), out_path)
    log_path = os.path.join(out_dir, str(r.get("file", "")).replace(".xlsx", ".log"))
    check("日志文件真的落盘", os.path.exists(log_path), log_path)

    print("\n【与 Python 版基准比对】")
    if os.path.exists(GOLD):
        import openpyxl

        def load(p, sheet):
            wb = openpyxl.load_workbook(p, data_only=True)
            sh = wb[sheet]
            rows = [[("" if v is None else str(v).strip()) for v in row]
                    for row in sh.iter_rows(values_only=True)]
            wb.close()
            return rows

        for sheet in ("销售订单主表", "销售订单明细", "工单"):
            a = load(GOLD, sheet)
            b = load(out_path, sheet)
            same_shape = len(a) == len(b) and (a[0] if a else []) == (b[0] if b else [])
            check(f"{sheet}：行列表头一致（{len(a)-1} 行 × {len(a[0])} 列）", same_shape,
                  f"golden={len(a)}x{len(a[0]) if a else 0} go={len(b)}x{len(b[0]) if b else 0}")
    else:
        check("存在 Python 基准文件", False, GOLD)

    print("\n【历史与报告】")
    h = get("/api/orders/history")
    check("/api/orders/history 返回 1 条", h.get("ok") and len(h.get("items", [])) == 1, h)
    if h.get("items"):
        it = h["items"][0]
        check("历史条目标记 module=orders", it.get("module") == "orders", it)
        rep = get("/api/report?id=" + urllib.parse.quote(it.get("report", "")))
        check("能按 id 取回报告", rep.get("ok"), rep)
        check("报告 module=orders", rep.get("report", {}).get("module") == "orders")

    print("\n【异常输入】")
    r = post_multipart("/api/orders/convert", {"order": None, "work": None}, {"lang": "zh"})
    check("两边都不给文件时明确报错", (not r.get("ok")) and r.get("error"), r)
    r = post_multipart("/api/orders/convert", {"order": None, "work": WORK}, {"lang": "en"})
    check("只给工单文件也能跑通", r.get("ok"), r.get("error"))
    if r.get("ok"):
        check("英文文案（日志含 Work/order 字样）",
              any("order" in ln.lower() or "work" in ln.lower() for ln in r.get("log", [])), r.get("log"))
    # 报告 id 目录穿越
    bad = get("/api/report?id=../orders.json")
    check("报告 id 目录穿越被拒", not bad.get("ok"), bad)

    print("\n【转换后重启服务，历史仍可查】")
    # 杀掉进程重启，确认历史/报告是落盘的
    proc.terminate()
    try:
        proc.wait(timeout=8)
    except Exception:
        proc.kill()
    proc = subprocess.Popen([EXE, "-nobrowser", "-port", str(PORT)],
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    ok2 = False
    for _ in range(40):
        time.sleep(0.4)
        try:
            if get("/api/ping", timeout=2).get("ok"):
                ok2 = True
                break
        except Exception:
            pass
    check("服务重启成功", ok2)
    if ok2:
        h2 = get("/api/orders/history")
        check("★ 重启后仍能查回订单转换历史", len(h2.get("items", [])) >= 2, h2)

finally:
    try:
        proc.terminate()
        proc.wait(timeout=8)
    except Exception:
        try:
            proc.kill()
        except Exception:
            pass

print()
print(f"结果：{passed} 项通过，{failed} 项失败")
sys.exit(0 if failed == 0 else 1)
