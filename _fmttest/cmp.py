# -*- coding: utf-8 -*-
import sys, io, os, openpyxl
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

BASE = "_golden/go_订单工单.xlsx"

def norm(v):
    if v is None: return ""
    if hasattr(v, "strftime"): return v.strftime("%Y-%m-%d")
    if isinstance(v, float) and abs(v-int(v))<1e-9: return str(int(v))
    if isinstance(v, float): return "%.6g" % v
    return str(v).strip()

def load(p, sheet):
    wb = openpyxl.load_workbook(p)
    sh = wb[sheet]
    rows = [[norm(c) for c in r] for r in sh.iter_rows(values_only=True)]
    wb.close(); return rows

variants = [
    ("xlsx 改名成 .xls", "_fmttest/out_伪装.xls.xlsx"),
    ("UTF-8 CSV",        "_fmttest/out_訂單資料_utf8.csv.xlsx"),
    ("GBK CSV",          "_fmttest/out_訂單資料_gbk.csv.xlsx"),
]
sheets = ["销售订单主表", "销售订单明细", "工单"]
allok = True
for label, path in variants:
    if not os.path.exists(path):
        print("✗ 缺少产物：", path); allok = False; continue
    bad = 0; detail = []
    for s in sheets:
        a, b = load(BASE, s), load(path, s)
        if len(a) != len(b):
            bad += 1; detail.append(f"{s} 行数 {len(a)} vs {len(b)}"); continue
        for i in range(len(a)):
            for j in range(max(len(a[i]), len(b[i]))):
                va = a[i][j] if j < len(a[i]) else ""
                vb = b[i][j] if j < len(b[i]) else ""
                if va != vb:
                    bad += 1
                    if len(detail) < 3: detail.append(f"{s} 第{i+1}行 第{j+1}列 {va!r} vs {vb!r}")
    if bad == 0:
        print(f"✓ {label}：三张表与基准逐格一致（{len(load(BASE,'工单'))-1} 行工单 / {len(load(BASE,'销售订单明细'))-1} 行明细）")
    else:
        allok = False
        print(f"✗ {label}：{bad} 处差异")
        for d in detail: print("    ", d)
print("\n结果：", "三种伪装格式都能得到与 .xlsx 完全相同的结果 ✓" if allok else "存在差异 ✗")
