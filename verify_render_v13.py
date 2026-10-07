# -*- coding: utf-8 -*-
"""无头浏览器渲染验证（v1.3）：首页模块选择 / 物料档案 / 订单·工单 三个视图。

确认：视图真的切换、动态生成的配置编辑器渲染出来、四语没有裸露 key 与占位符。
"""
import io
import os
import re
import subprocess
import sys
import time
import urllib.request

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

HERE = r"C:\Users\Cloak_Zeng\WorkBuddy\2026-10-06-14-57-26"
MES = os.path.join(HERE, "mes_conv")
EXE = os.path.join(MES, "物料档案转换工具_new.exe")
PORT = 8749
BASE = "http://127.0.0.1:%d" % PORT

CANDIDATES = [
    r"C:\Program Files\Google\Chrome\Application\chrome.exe",
    r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
    os.path.expandvars(r"%LOCALAPPDATA%\Google\Chrome\Application\chrome.exe"),
    r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
]
chrome = next((p for p in CANDIDATES if os.path.exists(p)), None)
print("浏览器:", chrome)
if not chrome:
    print("未找到 Chrome/Edge，跳过渲染验证")
    sys.exit(0)


def kill_all():
    for n in ("物料档案转换工具_new.exe", "物料档案转换工具.exe"):
        subprocess.run(["taskkill", "/F", "/IM", n], capture_output=True)


kill_all()
time.sleep(0.6)
p = subprocess.Popen([EXE, "-nobrowser", "-port", str(PORT)], cwd=MES,
                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
up = False
for _ in range(40):
    try:
        urllib.request.urlopen(BASE + "/api/ping", timeout=3).read()
        up = True
        break
    except Exception:
        time.sleep(0.4)
print("服务已启动:", up)
if not up:
    sys.exit(1)

prof = os.path.join(HERE, "_chrome_prof")
os.makedirs(prof, exist_ok=True)
results = []


def check(n, c, extra=""):
    results.append((n, bool(c)))
    print(("  OK  " if c else " FAIL ") + n + (("  " + str(extra)) if extra else ""))


def dump(url, lang=None):
    args = [chrome, "--headless=new", "--disable-gpu", "--no-sandbox",
            "--user-data-dir=" + prof, "--virtual-time-budget=8000", "--dump-dom", url]
    if lang:
        args.insert(1, "--lang=" + lang)
    dom = subprocess.run(args, capture_output=True, timeout=180)
    html = dom.stdout.decode("utf-8", "replace")
    return re.sub(r"<script>[\s\S]*?</script>", "", html)


def leaks(html, keys):
    """只把这些 key 当成「可见文本」出现才算泄漏（元素 id 出现在属性里不算）。"""
    out = []
    for k in keys:
        if re.search(r">\s*" + re.escape(k) + r"\s*<", html):
            out.append(k)
    return out


print("\n【首页 · 模块选择】")
home = dump(BASE + "/")
check("首页标题渲染", "请选择要使用的模块" in home)
check("物料档案卡片渲染", "物料档案" in home and "字段映射" in home)
check("订单/工单卡片渲染", "订单 / 工单" in home and "主表 + 明细 + 工单" in home)
check("首页无裸露 key", not leaks(home, ["mod_mat_name", "mod_ord_desc", "home_title"]),
      leaks(home, ["mod_mat_name", "mod_ord_desc", "home_title"]))
check("首页不含指标卡/预检（未进入模块）", 'id="stats"' not in home or 'class="stat"' not in home)

print("\n【模块一 · 物料档案】")
mat = dump(BASE + "/?view=materials")
check("物料档案视图显示", 'id="viewMaterials"' in mat)
check("物料步骤卡渲染", "第 1 步 · 选择源文件" in mat and "第 2 步 · 选择模板" in mat)
check("开始转换按钮", ">开始转换</button>" in mat or "开始转换" in mat)
check("返回按钮已翻译", "← 返回" in mat)

print("\n【模块二 · 订单 / 工单】")
ordr = dump(BASE + "/?view=orders")
check("订单视图显示", 'id="viewOrders"' in ordr)
check("订单步骤卡渲染", "订单文件（可选）" in ordr and "工单文件（可选）" in ordr)
check("订单按钮已翻译", "开始转换" in ordr and "打开输出文件夹" in ordr)
check("订单区块标题已翻译", "最近转换" in ordr and "转换日志" in ordr)
check("配置编辑器：三张映射表都渲染", ordr.count('class="ord-map"') == 3,
      ordr.count('class="ord-map"'))
check("配置编辑器：订单主表分类标题", "销售订单主表" in ordr)
check("配置编辑器：打标前缀已填入", "61CS" in ordr)
check("配置编辑器：运行开关渲染", 'id="ordSwLine"' in ordr)
check("配置编辑器：别名表渲染", 'class="oa-k"' in ordr or "oa-k" in ordr)
check("订单视图无裸露 key",
      not leaks(ordr, ["ord_block_map", "ord_th_source", "ord_tag_column", "ord_cfg_save"]),
      leaks(ordr, ["ord_block_map", "ord_th_source", "ord_tag_column", "ord_cfg_save"]))
check("订单视图无裸露占位符", "%d" not in ordr and "%s" not in ordr)

print("\n【订单模块 · 英文渲染】")
en = dump(BASE + "/?view=orders&lang=en", lang="en-US")
check("英文标题", "MES Order / Work-Order Converter" in en)
check("英文步骤卡", "Order file (optional)" in en and "Work-order file (optional)" in en)
check("英文按钮", "Start conversion" in en)
check("英文配置编辑器", "Field mapping" in en and "Mark decision" in en)
check("英文无裸露 key", not leaks(en, ["ord_th_target", "ord_sw_header", "ord_add_row"]),
      leaks(en, ["ord_th_target", "ord_sw_header", "ord_add_row"]))

print("\n【母版 · 物料档案四语无泄漏】")
for ql, probe in (("zh", "开始转换"), ("zht", "開始轉換"), ("vi", "Bắt đầu chuyển đổi"), ("en", "Start conversion")):
    h = dump(BASE + "/?view=materials&lang=" + ql)
    check("物料档案 %s 渲染正常" % ql, probe in h)
    check("物料档案 %s 无裸 key" % ql,
          not leaks(h, ["step_source", "block_fields", "hdr_loaded"]),
          leaks(h, ["step_source", "block_fields", "hdr_loaded"]))

p.terminate()
try:
    p.wait(timeout=4)
except Exception:
    p.kill()
kill_all()

bad = [n for n, o in results if not o]
print()
print("共 %d 项，失败 %d 项" % (len(results), len(bad)))
if bad:
    print("失败项:", bad)
print("=== 全部通过 ===" if not bad else "=== 存在失败 ===")
sys.exit(0 if not bad else 1)
