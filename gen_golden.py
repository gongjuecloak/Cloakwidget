# -*- coding: utf-8 -*-
"""用 Python 版工具跑一遍订单/工单，产出 Go 版要比对的基准文件。"""
import importlib.util
import os
import sys
import io
from unittest.mock import MagicMock

# 2.py 顶层 import tkinter；这里只借它的业务类，把 GUI 依赖打桩掉
for _name in ("tkinter", "tkinter.ttk", "tkinter.filedialog",
              "tkinter.messagebox", "tkinter.scrolledtext"):
    sys.modules[_name] = MagicMock()

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")
ROOT = os.path.dirname(os.path.abspath(__file__))
HERE = os.environ.get("MES_LEGACY_DIR", "")
SAMPLES = os.environ.get("MES_ORD_DIR", os.path.join(ROOT, "_fixtures"))
OUT = os.environ.get("MES_GOLD_DIR", os.path.join(ROOT, "_golden"))
if not HERE or not os.path.isfile(os.path.join(HERE, "2.py")):
    raise SystemExit("请设置 MES_LEGACY_DIR，指向含 2.py 和 config.json 的旧版目录")
os.chdir(HERE)
os.makedirs(OUT, exist_ok=True)

spec = importlib.util.spec_from_file_location("conv2", os.path.join(HERE, "2.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

cm = mod.ConfigManager(os.path.join(HERE, "config.json"))
cm.load()
imp = mod.DataImporter(cm)

order_out = os.path.join(OUT, "py_订单.xlsx")
work_out = os.path.join(OUT, "py_工单.xlsx")

print("=== import_order ===")
order_df = imp.import_order(os.path.join(SAMPLES, "訂單資料.xlsx"), order_out,
                            log_callback=lambda s: print("  LOG:", s.replace("\n", " | ")))
print("order_df rows =", 0 if order_df is None else len(order_df), "| last_error =", imp.last_error)

print("=== import_work_order ===")
ok = imp.import_work_order(os.path.join(SAMPLES, "工單資料.xlsx"), work_out,
                           order_df=order_df,
                           log_callback=lambda s: print("  LOG:", s.replace("\n", " | ")))
print("ok =", ok, "| last_error =", imp.last_error)
print("输出：", order_out, work_out)
