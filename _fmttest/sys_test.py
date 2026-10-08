# -*- coding: utf-8 -*-
"""#38/#39/#40/#43/#44 验证：系统设置、口令锁、局域网、开机自启、排障包、统计、检查更新。

注意：会短暂改写 mes_conv/sys_settings.json 与 HKCU 的自启项，
脚本结束（含异常）时都会还原。
"""
import sys, io, os, json, time, subprocess, urllib.request, urllib.error, urllib.parse, uuid, zipfile, shutil
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MES = os.path.join(HERE, "mes_conv")
EXE = os.path.join(MES, os.environ.get("MES_EXE", "_newbuild.exe"))
PORT = int(os.environ.get("APP_PORT", "8767"))
BASE = "http://127.0.0.1:%d" % PORT
SYSJSON = os.path.join(MES, "sys_settings.json")
OUT = os.path.join(MES, "out")

# 版本号从唯一来源 mes_conv/version.go 读，避免每升一次版就要改测试
import re
_m = re.search(r'const appVersion = "([^"]+)"', open(os.path.join(MES, "version.go"), encoding="utf-8").read())
VER = _m.group(1) if _m else ""

PASS = FAIL = 0
def check(name, ok, extra=""):
    global PASS, FAIL
    if ok: PASS += 1; print("  OK  %s" % name)
    else:  FAIL += 1; print("  NG  %s   %s" % (name, extra))

def multipart(fields, files=None):
    b = "----wb" + uuid.uuid4().hex
    body = io.BytesIO()
    def w(s): body.write(s.encode("utf-8") if isinstance(s, str) else s)
    for k, v in (fields or {}).items():
        w("--%s\r\n" % b); w('Content-Disposition: form-data; name="%s"\r\n\r\n' % k); w(v); w("\r\n")
    for k, arr in (files or {}).items():
        for fn, data in arr:
            w("--%s\r\n" % b)
            w('Content-Disposition: form-data; name="%s"; filename="%s"\r\n' % (k, fn))
            w("Content-Type: application/octet-stream\r\n\r\n"); w(data); w("\r\n")
    w("--%s--\r\n" % b)
    return body.getvalue(), "multipart/form-data; boundary=%s" % b

def post(path, fields, files=None, token=None, timeout=120):
    data, ct = multipart(fields, files)
    h = {"Content-Type": ct}
    if token: h["X-Token"] = token
    req = urllib.request.Request(BASE + path, data=data, headers=h)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        return {"_http": e.code, "body": e.read().decode("utf-8", "replace")[:200]}

def get(path, token=None, timeout=20):
    h = {}
    if token: h["X-Token"] = token
    req = urllib.request.Request(BASE + path, headers=h)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        return {"_http": e.code, "body": e.read().decode("utf-8", "replace")[:200]}

def start():
    p = subprocess.Popen([EXE, "-nobrowser", "-nosingle", "-port", str(PORT)],
                         cwd=MES, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(40):
        time.sleep(0.4)
        try:
            get("/api/ping", timeout=2); return p
        except Exception:
            pass
    p.kill(); return None

def stop(p):
    if not p: return
    p.terminate()
    try: p.wait(timeout=8)
    except Exception: p.kill()

# ---------- 备份 / 还原 ----------
SYSBACK = None
if os.path.exists(SYSJSON):
    SYSBACK = open(SYSJSON, "rb").read()

app = None
try:
    app = start()
    print("服务已启动:", bool(app))
    if not app: sys.exit(1)

    print("\n【1】系统设置总览 /api/system")
    s = get("/api/system")
    check("接口 ok", s.get("ok"), json.dumps(s, ensure_ascii=False)[:250])
    check("★ 返回版本号", s.get("update_current") == VER, s.get("update_current"))
    check("★ 返回端口", s.get("port") == PORT, s.get("port"))
    check("默认未设口令", s.get("has_password") is False, s.get("has_password"))
    check("返回局域网地址清单字段", isinstance(s.get("addrs"), list), s.get("addrs"))
    print("     addrs:", s.get("addrs"))

    print("\n【2】开机自启（#39）")
    r = post("/api/system/autostart", {"lang": "zh", "on": "1"})
    check("开启接口 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:250])
    check("★ 返回开启说明", "开机自启" in (r.get("message") or ""), r.get("message"))
    check("★ 状态变为已开启", post("/api/system/autostart", {"lang": "zh", "on": "1"}).get("autostart") is True)
    s2 = get("/api/system")
    check("★ 总览里 autostart=true", s2.get("autostart") is True, s2.get("autostart"))
    check("★ 注册命令带 -nobrowser", "-nobrowser" in (s2.get("autostart_cmd") or ""), s2.get("autostart_cmd"))
    try:
        import winreg
        k = winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Software\Microsoft\Windows\CurrentVersion\Run")
        try:
            v, _ = winreg.QueryValueEx(k, "MES物料档案转换工具")
        finally:
            winreg.CloseKey(k)
        print("     Run 项:", v)
        check("★ 注册表里确实写进去了（含 -nobrowser）", "-nobrowser" in v, v)
    except FileNotFoundError:
        check("★ 注册表里确实写进去了（含 -nobrowser）", False, "未找到 Run 项")
    except Exception as e:
        check("★ 注册表里确实写进去了（含 -nobrowser）", False, str(e))
    r = post("/api/system/autostart", {"lang": "zh", "on": "0"})
    check("关闭接口 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:200])
    check("★ 状态回到未开启", get("/api/system").get("autostart") is False)

    print("\n【3】口令锁（#38）")
    r = post("/api/system/password", {"lang": "zh", "password": "test1234"})
    check("设置口令 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:250])
    tok = r.get("token")
    check("★ 设完立刻发 token（不会被自己挡在门外）", bool(tok), str(r)[:200])
    check("★ 落盘的是哈希不是明文",
          ("test1234" not in open(SYSJSON, encoding="utf-8").read()), open(SYSJSON, encoding="utf-8").read()[:200])

    ping = get("/api/ping")
    check("★ 探活接口不需要口令", ping.get("ok") is True, json.dumps(ping, ensure_ascii=False))
    blocked = get("/api/history")
    check("★ 未带 token 的接口被拦住", blocked.get("need_auth") is True, json.dumps(blocked, ensure_ascii=False)[:200])
    okh = get("/api/history", token=tok)
    check("★ 带 token 可以访问", okh.get("ok") is True, json.dumps(okh, ensure_ascii=False)[:200])
    st = get("/api/auth/state")
    check("★ 认证状态接口报告需要口令", st.get("need_auth") is True, json.dumps(st, ensure_ascii=False))
    bad = post("/api/auth", {"lang": "zh", "password": "wrong"})
    check("★ 错口令被拒", bad.get("ok") is False and "口令" in (bad.get("error") or ""), json.dumps(bad, ensure_ascii=False)[:200])
    good = post("/api/auth", {"lang": "zh", "password": "test1234"})
    check("★ 对口令换到 token", good.get("ok") and bool(good.get("token")), json.dumps(good, ensure_ascii=False)[:200])
    tok2 = good.get("token")
    check("★ 新 token 有效", get("/api/system", token=tok2).get("ok") is True)

    r = post("/api/system/password", {"lang": "zh", "clear": "1"}, token=tok2)
    check("清除口令 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:200])
    check("★ 清除后接口恢复开放", get("/api/history").get("ok") is True)

    print("\n【4】局域网开关（#38，只验证设置与落盘；监听地址需重启生效）")
    r = post("/api/system/lan", {"lang": "zh", "on": "1"})
    check("开启 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:250])
    check("★ 返回需重启的说明", "重启" in (r.get("message") or ""), r.get("message"))
    check("★ 落盘 lan=true", json.load(open(SYSJSON, encoding="utf-8")).get("lan") is True)
    r = post("/api/system/lan", {"lang": "zh", "on": "0"})
    check("关闭 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:200])
    check("★ 落盘 lan=false", json.load(open(SYSJSON, encoding="utf-8")).get("lan") is False)

    print("\n【5】重启后真的监听 0.0.0.0（LAN 生效验证）")
    post("/api/system/lan", {"lang": "zh", "on": "1"})
    stop(app); app = start()
    check("重启后服务仍可用", bool(app))
    try:
        ns = subprocess.run(["netstat", "-ano", "-p", "TCP"], capture_output=True, text=True,
                            encoding="mbcs", timeout=20).stdout or ""
        hit = [l.strip() for l in ns.splitlines() if (":%d" % PORT) in l and "LISTENING" in l.upper()]
        print("     netstat:", " | ".join(hit)[:300])
        check("★ 监听在 0.0.0.0（局域网可达）", any(l.startswith("TCP") and "0.0.0.0:%d" % PORT in l for l in hit), str(hit)[:300])
    except Exception as e:
        check("★ 监听在 0.0.0.0（局域网可达）", False, str(e))
    post("/api/system/lan", {"lang": "zh", "on": "0"})

    print("\n【6】排障包（#40）")
    r = post("/api/system/diag", {"lang": "zh"})
    check("生成 ok", r.get("ok"), json.dumps(r, ensure_ascii=False)[:250])
    zname = r.get("file") or ""
    check("★ 返回 zip 文件名", zname.endswith(".zip"), zname)
    zp = os.path.join(OUT, zname)
    check("★ zip 已落盘", os.path.exists(zp), zp)
    if os.path.exists(zp):
        with zipfile.ZipFile(zp) as z:
            names = z.namelist()
            print("     内含:", names)
        check("★ 含环境信息 env.txt", "env.txt" in names, str(names))
        check("★ 含映射配置", "config/mapping.json" in names, str(names))
        check("★ 含系统设置", "config/sys_settings.json" in names, str(names))
        if "config/sys_settings.json" in names:
            with zipfile.ZipFile(zp) as z:
                ss = json.loads(z.read("config/sys_settings.json").decode("utf-8"))
            check("★ 口令哈希已脱敏", not ss.get("password_hash"), json.dumps(ss, ensure_ascii=False)[:200])
        with zipfile.ZipFile(zp) as z:
            env = z.read("env.txt").decode("utf-8", "replace") if "env.txt" in names else ""
        check("★ env.txt 带版本与端口", VER in env and str(PORT) in env, env[:300])
        # 下载接口要能取到这个 zip
        try:
            with urllib.request.urlopen(BASE + "/download?file=" + urllib.parse.quote(zname), timeout=20) as rr:
                got = rr.read()
            check("★ /download 能取到排障包", len(got) == os.path.getsize(zp), "%d vs %d" % (len(got), os.path.getsize(zp)))
        except urllib.error.HTTPError as e:
            check("★ /download 能取到排障包", False, "HTTP %s" % e.code)

    print("\n【7】检查更新（#44）")
    u = get("/api/system/update", timeout=30)
    if u.get("ok"):
        print("     latest:", u.get("latest"), "has_update:", u.get("has_update"), "url:", u.get("url"))
        check("★ 返回当前版本", u.get("current") == VER, u.get("current"))
        check("★ 返回 latest 标签", bool(u.get("latest")), str(u)[:200])
        check("★ has_update 是布尔", isinstance(u.get("has_update"), bool), str(u)[:200])
        check("★ 带可打开的链接", str(u.get("download") or u.get("url") or "").startswith("http"), str(u)[:250])
    else:
        print("     （网络不可达，按设计降级）error:", u.get("error"))
        check("★ 网络不可达时接口不报 500，而是给出可读错误", "检查更新失败" in (u.get("error") or ""), str(u)[:250])

    print("\n【8】历史统计（#43）")
    stz = get("/api/stats")
    check("接口 ok", stz.get("ok"), json.dumps(stz, ensure_ascii=False)[:250])
    check("★ 返回累计次数", isinstance(stz.get("runs"), int), str(stz)[:200])
    check("★ 返回累计行数", isinstance(stz.get("rows"), int), str(stz)[:200])
    check("★ by_day 是数组", isinstance(stz.get("by_day"), list), str(stz.get("by_day"))[:200])
    check("★ recent 是数组", isinstance(stz.get("recent"), list), str(stz.get("recent"))[:200])
    check("★ 干净/有问题次数之和 = 总次数",
          (stz.get("clean", 0) + stz.get("with_issues", 0)) == stz.get("runs"), json.dumps(stz, ensure_ascii=False)[:250])
    check("★ 物料+订单次数 = 总次数",
          (stz.get("materials", 0) + stz.get("orders", 0)) == stz.get("runs"), json.dumps(stz, ensure_ascii=False)[:250])
    print("     stats:", json.dumps({k: stz.get(k) for k in ("runs", "rows", "clean", "with_issues", "materials", "orders")}, ensure_ascii=False))

finally:
    stop(app)
    # 还原系统设置
    if SYSBACK is not None:
        open(SYSJSON, "wb").write(SYSBACK)
    elif os.path.exists(SYSJSON):
        os.remove(SYSJSON)
    # 还原自启项
    try:
        import winreg
        k = winreg.OpenKey(winreg.HKEY_CURRENT_USER,
                           r"Software\Microsoft\Windows\CurrentVersion\Run", 0, winreg.KEY_SET_VALUE)
        try:
            winreg.DeleteValue(k, "MES物料档案转换工具")
        except FileNotFoundError:
            pass
        finally:
            winreg.CloseKey(k)
    except Exception:
        pass

print("\n结果：%d 通过 / %d 失败" % (PASS, FAIL))
sys.exit(1 if FAIL else 0)
