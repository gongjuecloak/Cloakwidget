#!/usr/bin/env python3
"""
MES 转换工具 · 更新镜像服务（轻量版）

做什么：
  把 GitHub Releases 的「最新发布」资产，在服务端拉取后缓存，对外提供同名下载。
  工厂工位机只需能访问这台服务器（经 Cloudflare），不必直连 github.com——
  解决「工位机无外网，但更新源在 GitHub」的部署矛盾。

  客户端请求：
    GET /version.json            -> latest release 里的 version.json（优先新鲜，失败回退缓存）
    GET /MES-Converter-vX.Y.Z.exe -> 对应资产（首次拉取后落盘缓存，之后直接回源）
    GET /healthz                 -> 健康检查 JSON {"ok":true}
    GET /status  /              -> 运维状态仪表盘（HTML）
    GET /api/status              -> 状态 JSON（含审计与已注册令牌数）

  控制 / 安全：
    POST /admin/refresh?admin_token=XXX -> 立即强制拉取最新并预热缓存（需 MIRROR_ADMIN_TOKEN）
    基础鉴权：拉取须带令牌（?token= 或 X-Access-Token）。未知令牌 + enroll 密钥可自注册，
              成功一次即旋转为新令牌（X-Next-Token），相当于一次性令牌，避免被滥用。
    无令牌 -> 401。

  陈旧兜底（stale-while-down）：GitHub 不可达时，回退到磁盘上最后一次成功的
  version.json 与 release 元数据 / 资产缓存，并打 X-Cache: stale，绝不无谓 502。

依赖：仅 Python 标准库。
"""
import os
import sys
import json
import time
import shutil
import uuid
import threading
from urllib.request import Request, urlopen
from urllib.error import URLError, HTTPError
from urllib.parse import urlparse, parse_qs
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REPO = os.environ.get("MIRROR_REPO", "gongjuecloak/Cloakwidget")
PORT = int(os.environ.get("MIRROR_PORT", "8080"))
CACHE_DIR = os.environ.get("MIRROR_CACHE", "/data/cache")
GITHUB_TOKEN = os.environ.get("MIRROR_GITHUB_TOKEN")      # 提 GitHub API 速率上限，避免 403
ADMIN_TOKEN = os.environ.get("MIRROR_ADMIN_TOKEN")        # /admin/refresh 保护
ENROLL_SECRET = os.environ.get("MIRROR_ACCESS_TOKEN")     # 客户端首次 enroll 的密钥（不设则开放注册）
LATEST_API = f"https://api.github.com/repos/{REPO}/releaseS/latest".replace("releaseS", "releases")
UA = "MES-Mirror/1.0 (+https://github.com/gongjuecloak/Cloakwidget)"
CACHE_TTL = 60  # 最新发布元数据的内存缓存秒数

_latest_cache = {"ts": 0.0, "data": None, "stale": False}
START_TIME = time.time()

# ---- 令牌池（每客户端一个，旋转式一次性） ----
TOKEN_FILE = os.path.join(CACHE_DIR, "tokens.json")
_token_lock = threading.Lock()
_valid_tokens = set()

# ---- 审计 / 计数 ----
_stats_lock = threading.Lock()
_stats = {"total": 0, "by_asset": {}, "last_ts": 0, "by_status": {}}
AUDIT_FILE = os.path.join(CACHE_DIR, "audit.log")


def _load_tokens():
    global _valid_tokens
    try:
        with open(TOKEN_FILE, encoding="utf-8") as f:
            _valid_tokens = set(json.load(f).get("tokens", []))
    except Exception:
        _valid_tokens = set()
    if not _valid_tokens and ENROLL_SECRET:
        # 用 enroll 密钥作为初始令牌，方便运维预置
        _valid_tokens.add(ENROLL_SECRET)
        _save_tokens()


def _save_tokens():
    try:
        with open(TOKEN_FILE, "w", encoding="utf-8") as f:
            json.dump({"tokens": list(_valid_tokens)}, f)
    except Exception:
        pass


def _gh_headers(extra=None):
    h = {"User-Agent": UA, "Accept": "application/vnd.github+json"}
    if GITHUB_TOKEN:
        h["Authorization"] = "Bearer " + GITHUB_TOKEN
    if extra:
        h.update(extra)
    return h


def _http_get_bytes(url: str, timeout: int = 60) -> bytes:
    req = Request(url, headers=_gh_headers({"Accept": "*/*"}))
    with urlopen(req, timeout=timeout) as resp:
        return resp.read()


def _http_get_json(url: str, timeout: int = 30):
    req = Request(url, headers=_gh_headers())
    with urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def _persist_release(rel):
    try:
        with open(os.path.join(CACHE_DIR, "release.cache.json"), "w", encoding="utf-8") as f:
            json.dump(rel, f)
    except Exception:
        pass
    # version.json 单独缓存（客户端最关心的清单）
    for a in rel.get("assets", []):
        if a.get("name") == "version.json":
            try:
                body = _http_get_bytes(a["browser_download_url"])
                with open(os.path.join(CACHE_DIR, "version.json.cache"), "wb") as f:
                    f.write(body)
            except Exception:
                pass
            break


def get_release_meta():
    """返回 (release_dict, stale_bool)。优先内存缓存；内存未命中则打 GitHub；
    GitHub 失败则回退磁盘缓存（stale）。"""
    now = time.time()
    if _latest_cache["data"] and now - _latest_cache["ts"] < CACHE_TTL:
        return _latest_cache["data"], _latest_cache["stale"]
    try:
        data = _http_get_json(LATEST_API)
        _latest_cache.update(ts=now, data=data, stale=False)
        _persist_release(data)
        return data, False
    except Exception:
        # 上游不可达：回退磁盘缓存
        try:
            with open(os.path.join(CACHE_DIR, "release.cache.json"), encoding="utf-8") as f:
                data = json.load(f)
            _latest_cache.update(ts=now, data=data, stale=True)
            return data, True
        except Exception:
            return None, True


def find_asset(rel, name: str):
    if not rel:
        return None
    for a in rel.get("assets", []):
        if a.get("name") == name:
            return a
    return None


def cache_path(name: str) -> str:
    safe = name.replace("/", "_").replace("\\", "_")
    os.makedirs(CACHE_DIR, exist_ok=True)
    return os.path.join(CACHE_DIR, safe)


def guess_ct(name: str) -> str:
    if name.endswith(".json"):
        return "application/json; charset=utf-8"
    return "application/octet-stream"


def _client_ip(handler) -> str:
    xff = handler.headers.get("X-Forwarded-For", "")
    if xff:
        return xff.split(",")[0].strip()
    return handler.client_address[0]


def _record_download(name: str, handler, token_present: bool, status: int, bytes_sent: int):
    ip = _client_ip(handler)
    with _stats_lock:
        _stats["total"] += 1
        _stats["last_ts"] = int(time.time())
        _stats["by_asset"][name] = _stats["by_asset"].get(name, 0) + 1
        _stats["by_status"][str(status)] = _stats["by_status"].get(str(status), 0) + 1
    try:
        with open(AUDIT_FILE, "a", encoding="utf-8") as f:
            f.write("%s\t%s\t%s\ttoken=%s\t%d\t%d\n" % (
                time.strftime("%Y-%m-%d %H:%M:%S"), ip, name,
                "Y" if token_present else "N", status, bytes_sent))
    except Exception:
        pass


def _auth_token(handler):
    q = parse_qs(urlparse(handler.path).query)
    t = q.get("token", [None])[0]
    if not t:
        t = handler.headers.get("X-Access-Token")
    return t


def _check_auth(handler):
    """返回 (ok, next_token_or_None)。ok 时顺便旋转令牌。"""
    q = parse_qs(urlparse(handler.path).query)
    token = _auth_token(handler)
    enroll = q.get("enroll", ["0"])[0] in ("1", "true", "yes")
    if not token:
        return False, None
    with _token_lock:
        if token in _valid_tokens:
            new_tok = uuid.uuid4().hex
            _valid_tokens.discard(token)
            _valid_tokens.add(new_tok)
            _save_tokens()
            return True, new_tok
        # 未知令牌：仅在 enroll 且密钥匹配（或开放注册）时登记为新客户端
        if enroll and (ENROLL_SECRET is None or q.get("secret", [""])[0] == ENROLL_SECRET):
            new_tok = uuid.uuid4().hex
            _valid_tokens.add(new_tok)
            _save_tokens()
            return True, new_tok
    return False, None


def _refresh_now():
    """强制拉取最新并预热缓存。返回摘要 dict。"""
    _latest_cache["data"] = None
    _latest_cache["ts"] = 0
    rel, _ = get_release_meta()
    summary = {"tag": None, "fetched": [], "error": None}
    if not rel:
        summary["error"] = "无法从 GitHub 获取最新发布"
        return summary
    summary["tag"] = rel.get("tag_name")
    for a in rel.get("assets", []):
        name = a.get("name")
        if not name:
            continue
        try:
            body = _http_get_bytes(a["browser_download_url"])
            cp = cache_path(name)
            with open(cp, "wb") as f:
                f.write(body)
            summary["fetched"].append({"name": name, "bytes": len(body)})
        except Exception as e:
            summary["fetched"].append({"name": name, "error": str(e)})
    return summary


# ---------------------------------------------------------------------------
# 状态收集（运维界面用）
# ---------------------------------------------------------------------------

def _fmt_bytes(n: int) -> str:
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if n < 1024 or unit == "TB":
            return f"{n:.1f} {unit}" if unit != "B" else f"{n} B"
        n /= 1024.0
    return str(n)


def _fmt_uptime(sec: int) -> str:
    d, rem = divmod(int(sec), 86400)
    h, rem = divmod(rem, 3600)
    m, s = divmod(rem, 60)
    parts = []
    if d:
        parts.append(f"{d}天")
    if h or d:
        parts.append(f"{h}时")
    if m or h or d:
        parts.append(f"{m}分")
    parts.append(f"{s}秒")
    return "".join(parts)


def _fmt_time(ts: int) -> str:
    if not ts:
        return "—"
    return time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(ts))


def gather_status() -> dict:
    now = time.time()
    info = {
        "ok": True,
        "repo": REPO,
        "port": PORT,
        "cache_dir": CACHE_DIR,
        "uptime_sec": int(now - START_TIME),
        "now_ts": int(now),
        "enroll_open": ENROLL_SECRET is None,
        "registered_tokens": len(_valid_tokens),
        "upstream": None,
        "upstream_error": None,
        "disk": None,
        "stats": None,
        "cache": {"files": [], "count": 0, "bytes": 0},
    }
    rel, stale = get_release_meta()
    if rel:
        info["upstream"] = {
            "tag": rel.get("tag_name"),
            "name": rel.get("name"),
            "published_at": rel.get("published_at"),
            "asset_count": len(rel.get("assets", [])),
            "asset_names": [a.get("name") for a in rel.get("assets", [])],
            "stale": stale,
        }
    else:
        info["ok"] = False
        info["upstream_error"] = "GitHub 不可达且无磁盘缓存"
    try:
        du = shutil.disk_usage(CACHE_DIR)
        info["disk"] = {"total": du.total, "used": du.used, "free": du.free}
    except Exception:
        pass
    with _stats_lock:
        info["stats"] = {
            "total": _stats["total"],
            "last_ts": _stats["last_ts"],
            "by_asset": dict(_stats["by_asset"]),
        }
    files = []
    total = 0
    if os.path.isdir(CACHE_DIR):
        for fn in sorted(os.listdir(CACHE_DIR)):
            fp = os.path.join(CACHE_DIR, fn)
            if not os.path.isfile(fp) or fn.endswith(".tmp") or fn in (
                    "tokens.json", "release.cache.json", "version.json.cache", "audit.log"):
                continue
            try:
                st = os.stat(fp)
                files.append({"name": fn, "bytes": st.st_size, "mtime": int(st.st_mtime)})
                total += st.st_size
            except OSError:
                continue
    info["cache"] = {"files": files, "count": len(files), "bytes": total}
    return info


def _esc(s) -> str:
    return (str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
            .replace('"', "&quot;"))


def render_status_html(info: dict) -> str:
    ok = info.get("ok", False)
    badge = ("正常" if ok else "上游异常")
    badge_cls = ("badge ok" if ok else "badge err")

    upstream_html = "—"
    if info.get("upstream"):
        u = info["upstream"]
        tag = u.get("tag") or "—"
        stale_tag = ' <span class="err">（陈旧缓存）</span>' if u.get("stale") else ""
        names = u.get("asset_names") or []
        names_html = "<br>".join(_esc(n) for n in names) if names else "（无资产）"
        upstream_html = (
            f"<div class='kv'><span>最新版本</span><b>{_esc(tag)}{stale_tag}</b></div>"
            f"<div class='kv'><span>发布时间</span><b>{_esc(u.get('published_at') or '—')}</b></div>"
            f"<div class='kv'><span>资产数</span><b>{u.get('asset_count')}</b></div>"
            f"<div class='kv'><span>资产清单</span><b>{names_html}</b></div>"
        )
    elif info.get("upstream_error"):
        upstream_html = f"<div class='kv'><span>错误</span><b class='err'>{_esc(info['upstream_error'])}</b></div>"

    disk_html = "—"
    if info.get("disk"):
        d = info["disk"]
        disk_html = (
            f"<div class='kv'><span>分区总容量</span><b>{_fmt_bytes(d['total'])}</b></div>"
            f"<div class='kv'><span>已用</span><b>{_fmt_bytes(d['used'])}</b></div>"
            f"<div class='kv'><span>可用</span><b>{_fmt_bytes(d['free'])}</b></div>"
        )

    cache = info.get("cache", {})
    rows = []
    if cache.get("files"):
        for f in cache["files"]:
            rows.append(
                f"<tr><td>{_esc(f['name'])}</td>"
                f"<td class='num'>{_fmt_bytes(f['bytes'])}</td>"
                f"<td>{_fmt_time(f['mtime'])}</td></tr>"
            )
    else:
        rows.append("<tr><td colspan='3' class='muted'>（暂无缓存）</td></tr>")
    rows_html = "".join(rows)

    st = info.get("stats") or {}
    by_asset = st.get("by_asset", {})
    asset_rows = "".join(
        f"<div class='kv'><span>{_esc(k)}</span><b>{v}</b></div>" for k, v in by_asset.items()
    ) or "<div class='muted'>（暂无下载）</div>"

    return """<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>MES 更新镜像 · 状态</title>
<style>
  :root { --fg:#1f2328; --muted:#6b7280; --bg:#fafafa; --card:#fff; --line:#e5e7eb; --accent:#1E4D2B; --ok:#1a7f37; --err:#cf222e; }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--fg); font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif; }
  .wrap { max-width:880px; margin:0 auto; padding:32px 20px 64px; }
  header { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:12px; margin-bottom:8px; }
  h1 { font-size:20px; margin:0; font-weight:600; }
  .sub { color:var(--muted); font-size:13px; margin-bottom:24px; }
  .badge { display:inline-block; padding:3px 12px; border-radius:999px; font-size:13px; font-weight:600; }
  .badge.ok { background:rgba(26,127,55,.12); color:var(--ok); }
  .badge.err { background:rgba(207,34,46,.12); color:var(--err); }
  .card { background:var(--card); border:1px solid var(--line); border-radius:12px; padding:18px 20px; margin-bottom:16px; }
  .card h2 { font-size:14px; margin:0 0 12px; color:var(--accent); letter-spacing:.02em; text-transform:uppercase; }
  .kv { display:flex; gap:12px; padding:4px 0; border-bottom:1px dashed var(--line); }
  .kv:last-child { border-bottom:none; }
  .kv span { color:var(--muted); min-width:92px; flex:none; }
  .kv b { font-weight:600; word-break:break-all; }
  .err { color:var(--err); }
  .muted { color:var(--muted); }
  table { width:100%; border-collapse:collapse; font-size:14px; }
  th, td { text-align:left; padding:8px 6px; border-bottom:1px solid var(--line); }
  th { color:var(--muted); font-weight:600; font-size:12px; text-transform:uppercase; }
  td.num { text-align:right; font-variant-numeric:tabular-nums; }
  footer { color:var(--muted); font-size:12px; text-align:center; margin-top:24px; }
  code { background:#f0f0f0; padding:1px 6px; border-radius:4px; font-size:13px; }
</style>
</head>
<body>
<div class="wrap">
  <header>
    <h1>MES 更新镜像 · 状态</h1>
    <span class="__BADGE_CLS__">__BADGE__</span>
  </header>
  <div class="sub">镜像仓库 <code>__REPO__</code> · 服务端口 <code>__PORT__</code> · 已注册令牌 <b>__TOKENS__</b> · 注册模式 <b>__ENROLL__</b> · 页面每 30 秒自动刷新 · __NOW__</div>

  <div class="card">
    <h2>服务</h2>
    <div class="kv"><span>运行状态</span><b class="__OKCLS__">__BADGE__</b></div>
    <div class="kv"><span>已运行</span><b>__UPTIME__</b></div>
    <div class="kv"><span>缓存目录</span><b>__CACHEDIR__</b></div>
    <div class="kv"><span>缓存占用</span><b>__CACHEBYTES__（__CACHECOUNT__ 个文件）</b></div>
  </div>

  <div class="card">
    <h2>上游 GitHub</h2>
    __UPSTREAM__
  </div>

  <div class="card">
    <h2>磁盘</h2>
    __DISK__
  </div>

  <div class="card">
    <h2>下载统计</h2>
    <div class="kv"><span>总下载</span><b>__TOTAL__</b></div>
    <div class="kv"><span>最近一次</span><b>__LAST__</b></div>
    __ASSETROWS__
  </div>

  <div class="card">
    <h2>缓存资产</h2>
    <table>
      <thead><tr><th>文件名</th><th style="text-align:right">大小</th><th>缓存时间</th></tr></thead>
      <tbody>__ROWS__</tbody>
    </table>
  </div>

  <footer>MES-Mirror · 纯标准库实现 · 客户端自行校验 SHA256 · 拉取需令牌</footer>
</div>
</body>
</html>""".replace("__BADGE__", badge).replace("__BADGE_CLS__", badge_cls) \
        .replace("__OKCLS__", ("ok" if ok else "err")) \
        .replace("__REPO__", _esc(info.get("repo"))).replace("__PORT__", str(info.get("port"))) \
        .replace("__TOKENS__", str(info.get("registered_tokens", 0))) \
        .replace("__ENROLL__", ("开放" if info.get("enroll_open") else "需密钥")) \
        .replace("__NOW__", _fmt_time(info.get("now_ts", 0))) \
        .replace("__UPTIME__", _fmt_uptime(info.get("uptime_sec", 0))) \
        .replace("__CACHEDIR__", _esc(info.get("cache_dir"))) \
        .replace("__CACHEBYTES__", _fmt_bytes(cache.get("bytes", 0))) \
        .replace("__CACHECOUNT__", str(cache.get("count", 0))) \
        .replace("__UPSTREAM__", upstream_html).replace("__DISK__", disk_html) \
        .replace("__TOTAL__", str(st.get("total", 0))) \
        .replace("__LAST__", _fmt_time(st.get("last_ts", 0))) \
        .replace("__ASSETROWS__", asset_rows).replace("__ROWS__", rows_html)


class Handler(BaseHTTPRequestHandler):
    server_version = "MES-Mirror/1.0"
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _send(self, code: int, body: bytes, ctype: str, extra=None):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def do_HEAD(self):
        self.do_GET()

    def _require_auth(self):
        ok, nxt = _check_auth(self)
        if not ok:
            self._send(401, b'{"error":"unauthorized"}\n', "application/json")
            return None
        return nxt

    def do_GET(self):
        path = self.path.split("?", 1)[0].lstrip("/")
        if path in ("", "healthz"):
            self._send(200, b'{"ok":true}\n', "application/json")
            return
        if path in ("status", "api/status"):
            try:
                status = gather_status()
            except Exception as e:
                status = {"ok": False, "error": str(e)}
            if path == "api/status":
                self._send(200, json.dumps(status, ensure_ascii=False, indent=2).encode("utf-8"),
                           "application/json; charset=utf-8")
            else:
                self._send(200, render_status_html(status).encode("utf-8"), "text/html; charset=utf-8")
            return
        # 其余均为「拉取」：需鉴权
        nxt = self._require_auth()
        if nxt is None:
            return
        extra = {"X-Next-Token": nxt} if nxt else {}
        rel, stale = get_release_meta()
        if rel is None:
            self._send(502, '{"error":"GitHub 不可达且无缓存"}'.encode("utf-8"), "application/json")
            return
        asset = find_asset(rel, path)
        if not asset:
            self._send(404, ("找不到资产: " + path).encode("utf-8"), "text/plain; charset=utf-8")
            return

        if path == "version.json":
            cp = os.path.join(CACHE_DIR, "version.json.cache")
            if stale:
                try:
                    with open(cp, "rb") as f:
                        body = f.read()
                    self._send(200, body, guess_ct(path), {**extra, "X-Cache": "stale"})
                    _record_download(path, self, True, 200, len(body))
                    return
                except Exception:
                    pass
            try:
                body = _http_get_bytes(asset["browser_download_url"])
                try:
                    with open(cp, "wb") as f:
                        f.write(body)
                except Exception:
                    pass
                self._send(200, body, guess_ct(path), extra)
                _record_download(path, self, True, 200, len(body))
                return
            except Exception as e:
                # 上游失败但本地有陈旧缓存，回退
                try:
                    with open(cp, "rb") as f:
                        body = f.read()
                    self._send(200, body, guess_ct(path), {**extra, "X-Cache": "stale"})
                    _record_download(path, self, True, 200, len(body))
                    return
                except Exception:
                    self._send(502, ("拉取 version.json 失败: " + str(e)).encode("utf-8"),
                               "text/plain; charset=utf-8")
                    return

        # 其它资产：命中本地缓存直接回源；否则拉取并落盘
        cp = cache_path(path)
        if os.path.exists(cp):
            try:
                with open(cp, "rb") as f:
                    body = f.read()
                self._send(200, body, guess_ct(path),
                           {**extra, **({"X-Cache": "stale"} if stale else {})})
                _record_download(path, self, True, 200, len(body))
                return
            except Exception:
                pass
        try:
            body = _http_get_bytes(asset["browser_download_url"])
        except Exception as e:
            # 上游挂了且本地没缓存：若处于陈旧模式，说明拿不到新资产
            if stale:
                self._send(503, ("上游不可达且本地无缓存: " + path).encode("utf-8"),
                           "text/plain; charset=utf-8")
                return
            self._send(502, ("拉取资产失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return
        tmp = cp + ".tmp"
        try:
            with open(tmp, "wb") as f:
                f.write(body)
            os.replace(tmp, cp)
        except Exception:
            pass
        self._send(200, body, guess_ct(path), extra)
        _record_download(path, self, True, 200, len(body))

    def do_POST(self):
        path = self.path.split("?", 1)[0].lstrip("/")
        if path == "admin/refresh":
            q = parse_qs(urlparse(self.path).query)
            tok = q.get("admin_token", [None])[0] or self.headers.get("X-Admin-Token")
            if not ADMIN_TOKEN or tok != ADMIN_TOKEN:
                self._send(401, b'{"error":"unauthorized"}\n', "application/json")
                return
            try:
                summary = _refresh_now()
                self._send(200, json.dumps(summary, ensure_ascii=False, indent=2).encode("utf-8"),
                           "application/json; charset=utf-8")
            except Exception as e:
                self._send(500, ("刷新失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return
        self._send(404, b'{"error":"not found"}\n', "application/json")


def main():
    os.makedirs(CACHE_DIR, exist_ok=True)
    _load_tokens()
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print(f"[mirror] serving repo={REPO} on :{PORT}, cache={CACHE_DIR}", flush=True)
    print(f"[mirror] enroll_open={ENROLL_SECRET is None}, registered_tokens={len(_valid_tokens)}, "
          f"github_token={'set' if GITHUB_TOKEN else 'none'}", flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()


if __name__ == "__main__":
    main()
