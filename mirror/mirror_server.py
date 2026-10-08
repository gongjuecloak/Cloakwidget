#!/usr/bin/env python3
"""
MES 转换工具 · 通用更新镜像服务（轻量版，纯标准库）

做什么：
  把多个 GitHub 仓库「最新发布」的资产，在服务端拉取后按应用缓存，对外提供同名下载。
  工厂工位机只需能访问这台服务器（经 Cloudflare），不必直连 github.com——
  解决「工位机无外网，但更新源在 GitHub」的部署矛盾。

  这是一个「通用更新平台」：通过 apps.json 注册任意多个应用（每个应用指向一个
  GitHub 仓库），按 /{app_id}/... 命名空间隔离。客户端（各工具）只需在更新配置里
  写上自己的 app_id 与镜像基址，即可复用同一套签名 / 令牌 / 缓存机制自助更新。

  客户端请求示例（以 app_id = mes-converter 为例）：
    GET /                                  -> 全局运维面板（HTML，列出所有应用）
    GET /healthz                           -> 健康检查 JSON {"ok":true}（供探针/反代）
    GET /mes-converter/version.json        -> 该应用 latest release 里的 version.json
    GET /mes-converter/MES-Converter-vX.Y.Z.exe -> 对应资产（首次拉取后落盘缓存）
    GET /status  /api/status               -> 全局运维面板 / 状态 JSON（含各应用审计）
    GET /mes-converter/status              -> 单个应用的运维面板
  兼容：老客户端仍走根路径 /version.json（路由到默认应用），无需升级即继续可用。

  控制 / 安全：
    POST /admin/refresh                    -> 立即强制拉取「全部」应用并预热（需 MIRROR_ADMIN_TOKEN）
    POST /admin/refresh?app=mes-converter  -> 只刷新指定应用
    POST /mes-converter/admin/refresh      -> 等价写法（按命名空间）
    令牌可放在 ?admin_token= 或请求头 X-Admin-Token
    GET /<应用>/<资产> 等拉取              -> 需基础鉴权（?token= 或 X-Access-Token）
                                            未知令牌 + enroll 密钥可自注册，成功一次即旋转为新令牌
                                            （X-Next-Token），相当于一次性令牌，避免被滥用
                                            无令牌 -> 401

  陈旧兜底（stale-while-down）：GitHub 不可达时，回退到磁盘上最后一次成功的
  version.json 与 release 元数据 / 资产缓存，并打 X-Cache: stale，绝不无谓 502。

配置（均来自环境变量，建议用 .env + docker compose env_file 注入）：
    MIRROR_APPS_JSON    apps.json 路径（默认 ./apps.json；不存在则用内置默认应用）
    MIRROR_DEFAULT_APP  默认应用 id（根路径 /version.json 落到它；默认取 apps.json 第一个，
                         或内置的 mes-converter）
    MIRROR_PORT         容器内监听端口（默认 8080）
    MIRROR_CACHE        缓存根目录（默认 /data/cache），各应用再细分到子目录
    MIRROR_GITHUB_TOKEN GitHub Token（可选，全局，提 API 速率上限，避免匿名 403/限流）
    MIRROR_ADMIN_TOKEN  管理员令牌（POST /admin/refresh 必须携带；强烈建议设置，留空则永远 401）
    MIRROR_ACCESS_TOKEN 客户端 enroll 密钥（可选；不设则需 MIRROR_ENROLL_OPEN=true 才能注册）
    MIRROR_ENROLL_OPEN  是否开放注册（默认 false；true=任何客户端可自注册，局域网内方便但风险高）

安全要点：
    - 拉取需要令牌，令牌一次性旋转，降低泄露后的滥用面
    - 管理员刷新需要独立令牌，且与拉取令牌隔离
    - 日志对 token 值脱敏，且按大小轮转，不会无限增长
    - 注册默认关闭（需密钥），避免任意客户端囤积令牌
    - 应用命名空间隔离：一个应用的上游 / 缓存互不干扰

依赖：仅 Python 标准库。
"""
import os
import re
import sys
import json
import time
import shutil
import uuid
import logging
import threading
from urllib.request import Request, urlopen
from urllib.error import URLError, HTTPError
from urllib.parse import urlparse, parse_qs
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from logging.handlers import RotatingFileHandler

PORT = int(os.environ.get("MIRROR_PORT", "8080"))
CACHE_DIR = os.environ.get("MIRROR_CACHE", "/data/cache")
GITHUB_TOKEN = os.environ.get("MIRROR_GITHUB_TOKEN")      # 全局提 GitHub API 速率上限，避免 403
ADMIN_TOKEN = os.environ.get("MIRROR_ADMIN_TOKEN")        # /admin/refresh 保护
ENROLL_SECRET = os.environ.get("MIRROR_ACCESS_TOKEN")     # 客户端首次 enroll 的密钥（不设则靠开放开关）
ENROLL_OPEN = os.environ.get("MIRROR_ENROLL_OPEN", "false").lower() in ("1", "true", "yes")
APPS_JSON_PATH = os.environ.get("MIRROR_APPS_JSON", os.path.join(os.path.dirname(os.path.abspath(__file__)), "apps.json"))
DEFAULT_APP_ID = os.environ.get("MIRROR_DEFAULT_APP", "")
CACHE_TTL = 60  # 最新发布元数据的内存缓存秒数
START_TIME = time.time()

_log_file = os.path.join(CACHE_DIR, "mirror.log")


def _setup_logging():
    os.makedirs(CACHE_DIR, exist_ok=True)
    fmt = logging.Formatter("%(asctime)s %(levelname)s %(message)s", "%Y-%m-%d %H:%M:%S")
    h = RotatingFileHandler(_log_file, maxBytes=10 * 1024 * 1024, backupCount=5, encoding="utf-8")
    h.setFormatter(fmt)
    s = logging.StreamHandler(sys.stderr)
    s.setFormatter(fmt)
    logging.basicConfig(handlers=[h, s], level=logging.INFO)
    return logging.getLogger("mirror")


log = _setup_logging()

# 脱敏：把 URL / 请求头里的令牌值替换为 ***
_TOKEN_RE = re.compile(r"(?i)(token|admin_token|x-access-token|x-admin-token|x-next-token)=([^\s&]+)")


def _redact(s: str) -> str:
    if not s:
        return s
    return _TOKEN_RE.sub(r"\1=***", s)


def _safe(name: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]", "_", name)[:64]


# ---- 令牌池（每客户端一个，旋转式一次性） ----
TOKEN_FILE = os.path.join(CACHE_DIR, "tokens.json")
_token_lock = threading.Lock()
_valid_tokens = set()

# ---- 审计 / 计数（全局，附 app 维度） ----
_stats_lock = threading.Lock()
_stats = {"total": 0, "by_app": {}, "by_asset": {}, "last_ts": 0, "by_status": {}}


def _load_tokens():
    global _valid_tokens
    try:
        with open(TOKEN_FILE, encoding="utf-8") as f:
            _valid_tokens = set(json.load(f).get("tokens", []))
    except Exception:
        _valid_tokens = set()
    if not _valid_tokens and ENROLL_SECRET:
        _valid_tokens.add(ENROLL_SECRET)
        _save_tokens()


def _save_tokens():
    try:
        with open(TOKEN_FILE, "w", encoding="utf-8") as f:
            json.dump({"tokens": list(_valid_tokens)}, f)
    except Exception:
        pass


# ---------------------------------------------------------------------------
# 多应用模型
# ---------------------------------------------------------------------------

class App:
    """一个被托管的应用：对应一个 GitHub 仓库，拥有独立的缓存目录与发布元数据缓存。"""

    def __init__(self, app_id: str, repo: str, pubkey: str = None, github_token: str = None):
        self.app_id = app_id
        self.repo = repo
        self.pubkey = pubkey  # 可选；客户端侧验签用，服务端仅做转发（留作扩展）
        self.github_token = github_token or GITHUB_TOKEN
        self.cache_dir = os.path.join(CACHE_DIR, _safe(app_id))
        self.latest_api = f"https://api.github.com/repos/{repo}/releases/latest"
        self._cache = {"ts": 0.0, "data": None, "stale": False}
        os.makedirs(self.cache_dir, exist_ok=True)

    @property
    def release_cache_file(self):
        return os.path.join(self.cache_dir, "release.cache.json")

    @property
    def version_cache_file(self):
        return os.path.join(self.cache_dir, "version.json.cache")

    def asset_cache_path(self, name: str) -> str:
        safe = name.replace("/", "_").replace("\\", "_")
        return os.path.join(self.cache_dir, safe)

    @property
    def name(self):
        return self.app_id

    def _gh_headers(self, extra=None):
        h = {"User-Agent": UA, "Accept": "application/vnd.github+json"}
        if self.github_token:
            h["Authorization"] = "Bearer " + self.github_token
        if extra:
            h.update(extra)
        return h

    def get_release_meta(self):
        """返回 (release_dict, stale_bool)。优先内存缓存；未命中则打 GitHub；失败回退磁盘缓存。"""
        now = time.time()
        if self._cache["data"] and now - self._cache["ts"] < CACHE_TTL:
            return self._cache["data"], self._cache["stale"]
        try:
            data = _http_get_json(self.latest_api, self._gh_headers())
            self._cache.update(ts=now, data=data, stale=False)
            self._persist_release(data)
            return data, False
        except Exception:
            try:
                with open(self.release_cache_file, encoding="utf-8") as f:
                    data = json.load(f)
                self._cache.update(ts=now, data=data, stale=True)
                return data, True
            except Exception:
                return None, True

    def _persist_release(self, rel):
        try:
            with open(self.release_cache_file, "w", encoding="utf-8") as f:
                json.dump(rel, f)
        except Exception:
            pass
        for a in rel.get("assets", []):
            if a.get("name") == "version.json":
                try:
                    body = _http_get_bytes(a["browser_download_url"], self._gh_headers({"Accept": "*/*"}))
                    with open(self.version_cache_file, "wb") as f:
                        f.write(body)
                except Exception:
                    pass
                break

    def refresh_now(self):
        """强制拉取最新并预热缓存。返回摘要 dict。"""
        self._cache["data"] = None
        self._cache["ts"] = 0
        rel, _ = self.get_release_meta()
        summary = {"app": self.app_id, "repo": self.repo, "tag": None, "fetched": [], "error": None}
        if not rel:
            summary["error"] = "无法从 GitHub 获取最新发布"
            return summary
        summary["tag"] = rel.get("tag_name")
        for a in rel.get("assets", []):
            name = a.get("name")
            if not name:
                continue
            try:
                body = _http_get_bytes(a["browser_download_url"], self._gh_headers({"Accept": "*/*"}))
                with open(self.asset_cache_path(name), "wb") as f:
                    f.write(body)
                summary["fetched"].append({"name": name, "bytes": len(body)})
            except Exception as e:
                summary["fetched"].append({"name": name, "error": str(e)})
        return summary


UA = "MES-Mirror/2.0 (+https://github.com/gongjuecloak/Cloakwidget)"
APPS = {}        # app_id -> App
APP_ORDER = []   # 保持注册顺序


def _load_apps():
    default_repo = "gongjuecloak/Cloakwidget"
    default_pubkey = "89ymVon//tfWP9d+KzKZxg3oCBUT+w31nUqZN5LBML0="
    apps = {}
    order = []
    loaded = None
    try:
        with open(APPS_JSON_PATH, encoding="utf-8") as f:
            loaded = json.load(f)
    except Exception:
        loaded = None
    if isinstance(loaded, dict) and loaded:
        for aid, cfg in loaded.items():
            if not isinstance(cfg, dict) or not cfg.get("repo"):
                continue
            apps[aid] = App(aid, cfg["repo"], cfg.get("pubkey"), cfg.get("github_token"))
            order.append(aid)
    else:
        # 无 apps.json：内置一个默认应用（保持老部署行为）
        apps["mes-converter"] = App("mes-converter", default_repo, default_pubkey)
        order.append("mes-converter")
    return apps, order


APPS, APP_ORDER = _load_apps()
if not DEFAULT_APP_ID or DEFAULT_APP_ID not in APPS:
    DEFAULT_APP_ID = APP_ORDER[0] if APP_ORDER else "mes-converter"


def _app_by_id(aid: str):
    return APPS.get(aid)


# ---- HTTP 辅助（按应用取 GitHub） ----
def _http_get_bytes(url: str, headers=None, timeout: int = 60) -> bytes:
    req = Request(url, headers=headers or {"User-Agent": UA, "Accept": "*/*"})
    with urlopen(req, timeout=timeout) as resp:
        return resp.read()


def _http_get_json(url: str, headers, timeout: int = 30):
    req = Request(url, headers=headers)
    with urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def find_asset(rel, name: str):
    if not rel:
        return None
    for a in rel.get("assets", []):
        if a.get("name") == name:
            return a
    return None


def guess_ct(name: str) -> str:
    if name.endswith(".json"):
        return "application/json; charset=utf-8"
    return "application/octet-stream"


def _client_ip(handler) -> str:
    xff = handler.headers.get("X-Forwarded-For", "")
    if xff:
        return xff.split(",")[0].strip()
    return handler.client_address[0]


def _record_download(app_id: str, name: str, handler, status: int, bytes_sent: int):
    ip = _client_ip(handler)
    with _stats_lock:
        _stats["total"] += 1
        _stats["last_ts"] = int(time.time())
        _stats["by_app"][app_id] = _stats["by_app"].get(app_id, 0) + 1
        _stats["by_asset"][name] = _stats["by_asset"].get(name, 0) + 1
        _stats["by_status"][str(status)] = _stats["by_status"].get(str(status), 0) + 1
    log.info("[download] ip=%s app=%s asset=%s status=%d bytes=%d", ip, app_id, name, status, bytes_sent)


def _auth_token(handler):
    q = parse_qs(urlparse(handler.path).query)
    t = q.get("token", [None])[0]
    if not t:
        t = handler.headers.get("X-Access-Token")
    return t


def _check_auth(handler):
    """返回 (ok, next_token_or_None)。ok 时顺便旋转令牌。令牌池全局共享（个人平台足够）。"""
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
        enroll_allowed = ENROLL_OPEN or (ENROLL_SECRET is not None
                                         and q.get("secret", [""])[0] == ENROLL_SECRET)
        if enroll and enroll_allowed:
            new_tok = uuid.uuid4().hex
            _valid_tokens.add(new_tok)
            _save_tokens()
            log.info("[enroll] 新客户端注册，令牌数=%d（开放=%s）", len(_valid_tokens), ENROLL_OPEN)
            return True, new_tok
    return False, None


def _enroll_mode() -> str:
    if ENROLL_OPEN:
        return "开放（任何客户端可自注册）"
    if ENROLL_SECRET is not None:
        return "需密钥"
    return "关闭"


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


def _app_status(app: App) -> dict:
    rel, stale = app.get_release_meta()
    info = {
        "app_id": app.app_id,
        "repo": app.repo,
        "cache_dir": app.cache_dir,
        "upstream": None,
        "upstream_error": None,
        "cache": {"files": [], "count": 0, "bytes": 0},
    }
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
        info["upstream_error"] = "GitHub 不可达且无磁盘缓存"
    files = []
    total = 0
    if os.path.isdir(app.cache_dir):
        for fn in sorted(os.listdir(app.cache_dir)):
            fp = os.path.join(app.cache_dir, fn)
            if not os.path.isfile(fp) or fn.endswith(".tmp") or fn in (
                    "release.cache.json", "version.json.cache", "mirror.log"):
                continue
            try:
                st = os.stat(fp)
                files.append({"name": fn, "bytes": st.st_size, "mtime": int(st.st_mtime)})
                total += st.st_size
            except OSError:
                continue
    info["cache"] = {"files": files, "count": len(files), "bytes": total}
    return info


def gather_status() -> dict:
    now = time.time()
    apps = []
    ok = True
    for aid in APP_ORDER:
        a = APPS[aid]
        try:
            apps.append(_app_status(a))
        except Exception as e:
            ok = False
            apps.append({"app_id": aid, "repo": a.repo, "error": str(e)})
    disk = None
    try:
        du = shutil.disk_usage(CACHE_DIR)
        disk = {"total": du.total, "used": du.used, "free": du.free}
    except Exception:
        pass
    with _stats_lock:
        stats = {
            "total": _stats["total"],
            "last_ts": _stats["last_ts"],
            "by_app": dict(_stats["by_app"]),
            "by_asset": dict(_stats["by_asset"]),
        }
    return {
        "ok": ok,
        "default_app": DEFAULT_APP_ID,
        "app_count": len(APPS),
        "port": PORT,
        "cache_dir": CACHE_DIR,
        "uptime_sec": int(now - START_TIME),
        "now_ts": int(now),
        "enroll_mode": _enroll_mode(),
        "registered_tokens": len(_valid_tokens),
        "disk": disk,
        "stats": stats,
        "apps": apps,
    }


def _esc(s) -> str:
    return (str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
            .replace('"', "&quot;"))


def _status_cards(apps: list, full=False) -> str:
    out = []
    for a in apps:
        if "error" in a:
            out.append(
                f'<div class="card"><h2>{_esc(a.get("app_id"))}</h2>'
                f'<div class="kv"><span>仓库</span><b>{_esc(a.get("repo"))}</b></div>'
                f'<div class="kv"><span>错误</span><b class="err">{_esc(a["error"])}</b></div></div>')
            continue
        u = a.get("upstream")
        if u:
            stale = ' <span class="err">（陈旧缓存）</span>' if u.get("stale") else ""
            names = u.get("asset_names") or []
            names_html = "<br>".join(_esc(n) for n in names) if names else "（无资产）"
            up = (f"<div class='kv'><span>最新版本</span><b>{_esc(u.get('tag') or '—')}{stale}</b></div>"
                  f"<div class='kv'><span>发布时间</span><b>{_esc(u.get('published_at') or '—')}</b></div>"
                  f"<div class='kv'><span>资产数</span><b>{u.get('asset_count')}</b></div>"
                  f"<div class='kv'><span>资产清单</span><b>{names_html}</b></div>")
        else:
            up = f"<div class='kv'><span>错误</span><b class='err'>{_esc(a.get('upstream_error'))}</b></div>"
        c = a.get("cache", {})
        rows = []
        for f in c.get("files", []):
            rows.append(f"<tr><td>{_esc(f['name'])}</td><td class='num'>{_fmt_bytes(f['bytes'])}</td>"
                        f"<td>{_fmt_time(f['mtime'])}</td></tr>")
        rows_html = "".join(rows) if rows else "<tr><td colspan='3' class='muted'>（暂无缓存）</td></tr>"
        card = (
            f'<div class="card"><h2>{_esc(a.get("app_id"))} <span class="muted">· {_esc(a.get("repo"))}</span></h2>'
            f'<div class="kv"><span>缓存目录</span><b>{_esc(a.get("cache_dir"))}</b></div>{up}'
            f'<div class="card" style="margin:12px 0 0"><h2>缓存资产</h2>'
            f'<table><thead><tr><th>文件名</th><th style="text-align:right">大小</th><th>缓存时间</th></tr></thead>'
            f'<tbody>{rows_html}</tbody></table>'
            f'<div class="muted" style="margin-top:8px">共 {c.get("count",0)} 个文件，{_fmt_bytes(c.get("bytes",0))}</div></div>'
            f'<p><a class="btn" href="/{_esc(a.get("app_id"))}/status">单独查看</a></p>'
            f'</div>')
        out.append(card)
    return "".join(out)


def render_status_html(info: dict, title="MES 更新镜像 · 状态") -> str:
    ok = info.get("ok", False)
    badge = ("正常" if ok else "存在异常")
    badge_cls = ("badge ok" if ok else "badge err")
    st = info.get("stats") or {}
    by_app = st.get("by_app", {})
    app_rows = "".join(
        f"<div class='kv'><span>{_esc(k)}</span><b>{v}</b></div>" for k, v in by_app.items()
    ) or "<div class='muted'>（暂无下载）</div>"
    disk_html = "—"
    if info.get("disk"):
        d = info["disk"]
        disk_html = (f"<div class='kv'><span>分区总容量</span><b>{_fmt_bytes(d['total'])}</b></div>"
                     f"<div class='kv'><span>已用</span><b>{_fmt_bytes(d['used'])}</b></div>"
                     f"<div class='kv'><span>可用</span><b>{_fmt_bytes(d['free'])}</b></div>")
    return """<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>__TITLE__</title>
<style>
  :root { --fg:#1f2328; --muted:#6b7280; --bg:#fafafa; --card:#fff; --line:#e5e7eb; --accent:#1E4D2B; --ok:#1a7f37; --err:#cf222e; }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--fg); font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif; }
  .wrap { max-width:960px; margin:0 auto; padding:32px 20px 64px; }
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
  .btn { display:inline-block; margin-top:8px; padding:6px 12px; border:1px solid var(--line); border-radius:8px; color:var(--accent); text-decoration:none; font-size:13px; }
  footer { color:var(--muted); font-size:12px; text-align:center; margin-top:24px; }
  code { background:#f0f0f0; padding:1px 6px; border-radius:4px; font-size:13px; }
</style>
</head>
<body>
<div class="wrap">
  <header>
    <h1>__TITLE__</h1>
    <span class="__BADGE_CLS__">__BADGE__</span>
  </header>
  <div class="sub">通用更新平台 · 已注册 <b>__APPCOUNT__</b> 个应用（默认 <code>__DEFAULT__</code>）· 服务端口 <code>__PORT__</code> · 已注册令牌 <b>__TOKENS__</b> · 注册模式 <b>__ENROLL__</b> · 页面每 30 秒自动刷新 · __NOW__</div>

  <div class="card">
    <h2>服务</h2>
    <div class="kv"><span>运行状态</span><b class="__OKCLS__">__BADGE__</b></div>
    <div class="kv"><span>已运行</span><b>__UPTIME__</b></div>
    <div class="kv"><span>缓存根目录</span><b>__CACHEDIR__</b></div>
    <div class="kv"><span>下载统计</span><b>总 __TOTAL__ · 最近 __LAST__</b></div>
    __APPROWS__
  </div>

  <div class="card">
    <h2>磁盘</h2>
    __DISK__
  </div>

  <h2 style="margin:8px 0 12px;color:var(--accent);font-size:14px;letter-spacing:.02em;text-transform:uppercase">各应用</h2>
  __CARDS__

  <footer>MES-Mirror · 纯标准库实现 · 多应用通用更新平台 · 客户端自行校验 SHA256 · 拉取需令牌</footer>
</div>
</body>
</html>""".replace("__TITLE__", _esc(title)).replace("__BADGE__", badge).replace("__BADGE_CLS__", badge_cls) \
        .replace("__OKCLS__", ("ok" if ok else "err")) \
        .replace("__APPCOUNT__", str(info.get("app_count", 0))).replace("__DEFAULT__", _esc(info.get("default_app", ""))) \
        .replace("__PORT__", str(info.get("port"))) \
        .replace("__TOKENS__", str(info.get("registered_tokens", 0))) \
        .replace("__ENROLL__", _esc(info.get("enroll_mode", ""))) \
        .replace("__NOW__", _fmt_time(info.get("now_ts", 0))) \
        .replace("__UPTIME__", _fmt_uptime(info.get("uptime_sec", 0))) \
        .replace("__CACHEDIR__", _esc(info.get("cache_dir"))) \
        .replace("__TOTAL__", str(st.get("total", 0))).replace("__LAST__", _fmt_time(st.get("last_ts", 0))) \
        .replace("__APPROWS__", app_rows).replace("__DISK__", disk_html) \
        .replace("__CARDS__", _status_cards(info.get("apps", [])))


class Handler(BaseHTTPRequestHandler):
    server_version = "MES-Mirror/2.0"
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _access_log(self, code: int, extra: str = ""):
        log.info("[access] %s %s -> %d %s", _client_ip(self), _redact(self.path), code, extra)

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
            self._access_log(401, "auth-fail")
            return None
        return nxt

    # 把请求路径解析成 (app, rel_path)
    def _resolve(self):
        raw = self.path.split("?", 1)[0].lstrip("/")
        segs = raw.split("/") if raw else []
        # 全局端点（含根路径 /）
        if not segs or segs[0] == "":
            return "GLOBAL", ""
        if segs[0] in ("healthz", "status", "api"):
            return "GLOBAL", raw
        if segs[0] == "admin":
            return "ADMIN", raw
        # 命名空间：/<app_id>/...
        if segs[0] in APPS and len(segs) >= 1:
            return APPS[segs[0]], "/".join(segs[1:])
        # 无前缀：落到默认应用（兼容老客户端 /version.json）
        return APPS.get(DEFAULT_APP_ID), raw

    def do_GET(self):
        target, rel = self._resolve()

        if target == "GLOBAL":
            if rel == "healthz":
                self._send(200, b'{"ok":true}\n', "application/json")
                self._access_log(200, "healthz")
                return
            try:
                status = gather_status()
            except Exception as e:
                status = {"ok": False, "error": str(e)}
            if rel == "api/status":
                self._send(200, json.dumps(status, ensure_ascii=False, indent=2).encode("utf-8"),
                           "application/json; charset=utf-8")
            else:
                self._send(200, render_status_html(status).encode("utf-8"), "text/html; charset=utf-8")
            self._access_log(200, "status")
            return

        if target == "ADMIN":
            self._send(404, b'{"error":"use POST for admin"}\n', "application/json")
            return

        if target is None:
            self._send(404, b'{"error":"unknown app"}\n', "application/json")
            self._access_log(404, "no-app")
            return

        # 应用内状态页
        if rel == "status":
            try:
                info = _app_status(target)
            except Exception as e:
                info = {"app_id": target.app_id, "error": str(e)}
            self._send(200, render_status_html({"ok": "error" not in info,
                                                 "app_count": 1, "default_app": target.app_id,
                                                 "port": PORT, "cache_dir": CACHE_DIR,
                                                 "uptime_sec": int(time.time() - START_TIME),
                                                 "now_ts": int(time.time()),
                                                 "enroll_mode": _enroll_mode(),
                                                 "registered_tokens": len(_valid_tokens),
                                                 "disk": None, "stats": {}, "apps": [info]},
                                                title=f"应用 {target.app_id} · 状态").encode("utf-8"),
                           "text/html; charset=utf-8")
            self._access_log(200, "app-status")
            return

        # 其余均为「拉取」：需鉴权
        nxt = self._require_auth()
        if nxt is None:
            return
        extra = {"X-Next-Token": nxt} if nxt else {}
        app = target
        asset_name = rel  # 资产名（来自命名空间路径，如 version.json / MES-Converter-v1.10.2.exe）
        meta, stale = app.get_release_meta()
        if meta is None:
            self._send(502, '{"error":"GitHub 不可达且无缓存"}'.encode("utf-8"), "application/json")
            self._access_log(502, "no-upstream")
            return
        asset = find_asset(meta, asset_name)
        if not asset:
            self._send(404, ("找不到资产: " + asset_name).encode("utf-8"), "text/plain; charset=utf-8")
            self._access_log(404, "no-asset")
            return

        if asset_name == "version.json":
            cp = app.version_cache_file
            if stale:
                try:
                    with open(cp, "rb") as f:
                        body = f.read()
                    self._send(200, body, guess_ct(asset_name), {**extra, "X-Cache": "stale"})
                    _record_download(app.app_id, asset_name, self, 200, len(body))
                    self._access_log(200, "stale")
                    return
                except Exception:
                    pass
            try:
                body = _http_get_bytes(asset["browser_download_url"], app._gh_headers({"Accept": "*/*"}))
                try:
                    with open(cp, "wb") as f:
                        f.write(body)
                except Exception:
                    pass
                self._send(200, body, guess_ct(asset_name), extra)
                _record_download(app.app_id, asset_name, self, 200, len(body))
                self._access_log(200, "fresh")
                return
            except Exception as e:
                try:
                    with open(cp, "rb") as f:
                        body = f.read()
                    self._send(200, body, guess_ct(asset_name), {**extra, "X-Cache": "stale"})
                    _record_download(app.app_id, asset_name, self, 200, len(body))
                    self._access_log(200, "stale-fallback")
                    return
                except Exception:
                    self._send(502, ("拉取 version.json 失败: " + str(e)).encode("utf-8"),
                               "text/plain; charset=utf-8")
                    self._access_log(502, "fetch-fail")
                    return

        # 其它资产：命中本地缓存直接回源；否则拉取并落盘
        cp = app.asset_cache_path(asset_name)
        if os.path.exists(cp):
            try:
                with open(cp, "rb") as f:
                    body = f.read()
                self._send(200, body, guess_ct(asset_name),
                           {**extra, **({"X-Cache": "stale"} if stale else {})})
                _record_download(app.app_id, asset_name, self, 200, len(body))
                self._access_log(200, "cache")
                return
            except Exception:
                pass
        try:
            body = _http_get_bytes(asset["browser_download_url"], app._gh_headers({"Accept": "*/*"}))
        except Exception as e:
            if stale:
                self._send(503, ("上游不可达且本地无缓存: " + asset_name).encode("utf-8"),
                           "text/plain; charset=utf-8")
                self._access_log(503, "stale-no-cache")
                return
            self._send(502, ("拉取资产失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            self._access_log(502, "fetch-fail")
            return
        tmp = cp + ".tmp"
        try:
            with open(tmp, "wb") as f:
                f.write(body)
            os.replace(tmp, cp)
        except Exception:
            pass
        self._send(200, body, guess_ct(asset_name), extra)
        _record_download(app.app_id, asset_name, self, 200, len(body))
        self._access_log(200, "fresh")

    def do_POST(self):
        raw = self.path.split("?", 1)[0].lstrip("/")
        segs = raw.split("/") if raw else []
        q = parse_qs(urlparse(self.path).query)

        if (not segs or segs[0] == "admin") and (len(segs) <= 1 or segs[1] == "refresh"):
            # 全局刷新：可选 ?app= 指定单个；无则刷新全部
            tok = q.get("admin_token", [None])[0] or self.headers.get("X-Admin-Token")
            if not ADMIN_TOKEN or tok != ADMIN_TOKEN:
                self._send(401, b'{"error":"unauthorized"}\n', "application/json")
                log.warning("[admin] refresh 拒绝（令牌不符）from=%s %s",
                            _client_ip(self), _redact(self.path))
                return
            target_app = (q.get("app", [None])[0] or "").strip()
            log.info("[admin] refresh 触发 by=%s app=%s", _client_ip(self), target_app or "ALL")
            try:
                if target_app and target_app in APPS:
                    summaries = [APPS[target_app].refresh_now()]
                else:
                    summaries = [a.refresh_now() for a in (APPS[x] for x in APP_ORDER)]
                self._send(200, json.dumps(summaries, ensure_ascii=False, indent=2).encode("utf-8"),
                           "application/json; charset=utf-8")
            except Exception as e:
                self._send(500, ("刷新失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return

        # 命名空间内的刷新：/<app_id>/admin/refresh
        if len(segs) >= 2 and segs[0] in APPS and segs[1] == "admin":
            tok = q.get("admin_token", [None])[0] or self.headers.get("X-Admin-Token")
            if not ADMIN_TOKEN or tok != ADMIN_TOKEN:
                self._send(401, b'{"error":"unauthorized"}\n', "application/json")
                return
            app = APPS[segs[0]]
            log.info("[admin] refresh 触发 by=%s app=%s", _client_ip(self), app.app_id)
            try:
                summary = app.refresh_now()
                self._send(200, json.dumps(summary, ensure_ascii=False, indent=2).encode("utf-8"),
                           "application/json; charset=utf-8")
            except Exception as e:
                self._send(500, ("刷新失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return

        self._send(404, b'{"error":"not found"}\n', "application/json")


def main():
    os.makedirs(CACHE_DIR, exist_ok=True)
    _load_tokens()
    for aid in APP_ORDER:
        log.info("app %s -> repo %s", aid, APPS[aid].repo)
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    log.info("serving %d apps on :%d, default=%s, cache=%s", len(APPS), PORT, DEFAULT_APP_ID, CACHE_DIR)
    log.info("enroll_mode=%s, registered_tokens=%d, github_token=%s, admin_token=%s",
             _enroll_mode(), len(_valid_tokens),
             "set" if GITHUB_TOKEN else "none", "set" if ADMIN_TOKEN else "NONE(refresh disabled)")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()


if __name__ == "__main__":
    main()
