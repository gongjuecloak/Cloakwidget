#!/usr/bin/env python3
"""
Cloak Update Mirror · 私有应用发布与更新中枢（轻量版，纯标准库）

定位：
  GitHub Releases 是源头；Cloak Update Mirror 负责同步(sync)、缓存(cache)、
  校验(verify)、分发(distribute)、审计(audit)。客户端(Cloakwidget 等)只需连接
  Mirror，不必直连 github.com —— 解决「工位机无外网，但更新源在 GitHub」的矛盾。

数据模型（SQLite，存于缓存目录 mirror.db）：
  releases   每次同步写入各应用的发布元数据（版本/渠道/时间/状态机）
  assets     每个发布的资产清单（文件名/大小/sha256/是否已缓存）
  downloads  下载记录（审计）
  activities 活动日志（下载/同步/客户端签到/服务事件）
  clients    客户端登记表（v1.2 Client Management 预览，已预留）

客户端请求协议（保持不变，向后兼容）：
  GET /                                        -> 控制台 Overview（HTML）
  GET /healthz                                 -> 健康检查 {"ok":true}
  GET /{app_id}/version.json                    -> 该应用 latest release 的 manifest
  GET /{app_id}/{asset}                         -> 对应资产（首次拉取后落盘缓存）
  GET /status  /api/status                      -> 状态入口（/status 现为控制台兼容入口）
  GET /releases /assets /activity /system       -> 控制台各页面
  GET /api/v1/overview|releases|assets|activity -> JSON API（供前端/外部消费）
  POST /api/v1/client/checkin                   -> 客户端主动上报（新增，不改动既有更新协议）
  POST /admin/refresh  (+ ?app=)                -> 强制刷新（需 MIRROR_ADMIN_TOKEN）

安全（沿用既有设计）：
  - 拉取资产需令牌；令牌一次性旋转（X-Next-Token），泄露面低
  - 管理员刷新用独立令牌（X-Admin-Token / ?admin_token=），与拉取令牌隔离
  - enroll 默认关闭（需密钥）；日志对令牌脱敏并轮转
  - GitHub 不可达时回退磁盘缓存并打 X-Cache: stale，绝不无谓 502

配置（环境变量，建议 .env + docker compose env_file 注入）：
  MIRROR_APPS_JSON      应用注册表路径（默认 ./apps.json）
  MIRROR_DEFAULT_APP    默认应用 id（根路径 /version.json 落到它）
  MIRROR_PORT           监听端口（默认 8080）
  MIRROR_CACHE          缓存根目录（默认 /data/cache，SQLite 库也在此）
  MIRROR_GITHUB_TOKEN   GitHub Token（可选，提升 API 速率上限）
  MIRROR_ADMIN_TOKEN    管理员令牌（POST /admin/refresh 必须，留空则永远 401）
  MIRROR_ACCESS_TOKEN   客户端 enroll 密钥（可选）
  MIRROR_ENROLL_OPEN    是否开放注册（默认 false）
  MIRROR_SYNC_INTERVAL  后台自动同步周期秒（默认 300）

依赖：仅 Python 标准库（含 sqlite3）。
"""
import os
import re
import sys
import json
import time
import shutil
import hashlib
import sqlite3
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
GITHUB_TOKEN = os.environ.get("MIRROR_GITHUB_TOKEN")
ADMIN_TOKEN = os.environ.get("MIRROR_ADMIN_TOKEN")
ENROLL_SECRET = os.environ.get("MIRROR_ACCESS_TOKEN")
ENROLL_OPEN = os.environ.get("MIRROR_ENROLL_OPEN", "false").lower() in ("1", "true", "yes")
APPS_JSON_PATH = os.environ.get("MIRROR_APPS_JSON",
                                os.path.join(os.path.dirname(os.path.abspath(__file__)), "apps.json"))
DEFAULT_APP_ID = os.environ.get("MIRROR_DEFAULT_APP", "")
CACHE_TTL = 60  # 最新发布元数据的内存缓存秒数
SYNC_INTERVAL = int(os.environ.get("MIRROR_SYNC_INTERVAL", "300"))
START_TIME = time.time()
SERVER_VERSION = "Cloak-Mirror/1.0.0"

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


def _redact(s):
    if not s:
        return s
    return _TOKEN_RE.sub(r"\1=***", s)


def _safe(name):
    return re.sub(r"[^A-Za-z0-9._-]", "_", name)[:64]


# ---------------------------------------------------------------------------
# SQLite 存储
# ---------------------------------------------------------------------------
DB_PATH = os.path.join(CACHE_DIR, "mirror.db")
_db_lock = threading.Lock()
_db_conn = None

SCHEMA = """
CREATE TABLE IF NOT EXISTS releases(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app TEXT NOT NULL,
  version TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT 'stable',
  tag TEXT,
  name TEXT,
  published_at TEXT,
  synced_at INTEGER,
  status TEXT DEFAULT 'discovered',
  prerelease INTEGER DEFAULT 0,
  UNIQUE(app, version)
);
CREATE TABLE IF NOT EXISTS assets(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  release_id INTEGER,
  app TEXT NOT NULL,
  version TEXT NOT NULL,
  name TEXT NOT NULL,
  size INTEGER,
  sha256 TEXT,
  cached INTEGER DEFAULT 0,
  cached_at INTEGER,
  UNIQUE(app, version, name)
);
CREATE TABLE IF NOT EXISTS downloads(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app TEXT, asset TEXT, client_id TEXT, ip TEXT,
  status INTEGER, bytes INTEGER, created_at INTEGER
);
CREATE TABLE IF NOT EXISTS activities(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT, client_id TEXT, app TEXT, version TEXT,
  message TEXT, created_at INTEGER
);
CREATE TABLE IF NOT EXISTS clients(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id TEXT UNIQUE,
  app TEXT, version TEXT, channel TEXT,
  hostname TEXT, os TEXT, last_seen INTEGER, status TEXT
);
"""


def _db_init():
    global _db_conn
    os.makedirs(CACHE_DIR, exist_ok=True)
    _db_conn = sqlite3.connect(DB_PATH, check_same_thread=False)
    try:
        _db_conn.execute("PRAGMA journal_mode=WAL")
    except Exception:
        pass
    _db_conn.executescript(SCHEMA)
    _db_conn.commit()


def db_exec(sql, params=()):
    with _db_lock:
        _db_conn.execute(sql, params)
        _db_conn.commit()


def db_query(sql, params=()):
    with _db_lock:
        cur = _db_conn.execute(sql, params)
        rows = cur.fetchall()
        cols = [d[0] for d in cur.description] if cur.description else []
    return cols, rows


def upsert_release(app, version, channel, tag, name, published_at, prerelease, status="published"):
    with _db_lock:
        _db_conn.execute(
            "INSERT INTO releases(app,version,channel,tag,name,published_at,prerelease,synced_at,status) "
            "VALUES(?,?,?,?,?,?,?,?,?) "
            "ON CONFLICT(app,version) DO UPDATE SET "
            "channel=excluded.channel,tag=excluded.tag,name=excluded.name,"
            "published_at=excluded.published_at,prerelease=excluded.prerelease,"
            "synced_at=excluded.synced_at,status=excluded.status",
            (app, version, channel, tag, name, published_at, 1 if prerelease else 0,
             int(time.time()), status))
        _db_conn.commit()


def upsert_asset(app, version, name, size, cached):
    with _db_lock:
        _db_conn.execute(
            "INSERT INTO assets(app,version,name,size,cached,cached_at) VALUES(?,?,?,?,?,?) "
            "ON CONFLICT(app,version,name) DO UPDATE SET size=excluded.size,"
            "cached=excluded.cached,cached_at=excluded.cached_at",
            (app, version, name, size, 1 if cached else 0,
             int(time.time()) if cached else None))
        _db_conn.commit()


def add_activity(type_, message, app=None, version=None, client_id=None):
    with _db_lock:
        _db_conn.execute(
            "INSERT INTO activities(type,client_id,app,version,message,created_at) VALUES(?,?,?,?,?,?)",
            (type_, client_id, app, version, message, int(time.time())))
        _db_conn.commit()


def upsert_client(cid, app, ver, chan, host, osname):
    with _db_lock:
        _db_conn.execute(
            "INSERT INTO clients(client_id,app,version,channel,hostname,os,last_seen,status) "
            "VALUES(?,?,?,?,?,?,?,?) "
            "ON CONFLICT(client_id) DO UPDATE SET app=excluded.app,version=excluded.version,"
            "channel=excluded.channel,hostname=excluded.hostname,os=excluded.os,last_seen=excluded.last_seen",
            (cid, app, ver, chan, host, osname, int(time.time()), "active"))
        _db_conn.commit()


# ---------------------------------------------------------------------------
# 令牌池（每客户端一个，旋转式一次性）
# ---------------------------------------------------------------------------
TOKEN_FILE = os.path.join(CACHE_DIR, "tokens.json")
_token_lock = threading.Lock()
_valid_tokens = set()

# 审计 / 计数（全局，附 app 维度）
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
    """一个被托管的应用：对应一个 GitHub 仓库，独立缓存目录与发布元数据缓存。"""

    def __init__(self, app_id, repo, pubkey=None, github_token=None):
        self.app_id = app_id
        self.repo = repo
        self.pubkey = pubkey
        self.github_token = github_token or GITHUB_TOKEN
        self.cache_dir = os.path.join(CACHE_DIR, _safe(app_id))
        self.latest_api = "https://api.github.com/repos/{}/releases/latest".format(repo)
        self.list_api = "https://api.github.com/repos/{}/releases?per_page=20".format(repo)
        self._cache = {"ts": 0.0, "data": None, "stale": False}
        os.makedirs(self.cache_dir, exist_ok=True)

    @property
    def release_cache_file(self):
        return os.path.join(self.cache_dir, "release.cache.json")

    @property
    def version_cache_file(self):
        return os.path.join(self.cache_dir, "version.json.cache")

    @property
    def releases_list_cache_file(self):
        return os.path.join(self.cache_dir, "releases.list.json")

    def asset_cache_path(self, name):
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
        """返回 (release_dict, stale_bool)。优先内存缓存；未命中打 GitHub；失败回退磁盘。"""
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

    def get_releases_list(self, per_page=20, allow_cache=True):
        """返回发布列表（含历史）。优先 GitHub；失败回退磁盘列表缓存。"""
        now = time.time()
        cp = self.releases_list_cache_file
        if allow_cache and os.path.exists(cp):
            try:
                if now - os.path.getmtime(cp) < CACHE_TTL:
                    with open(cp, encoding="utf-8") as f:
                        return json.load(f)
            except Exception:
                pass
        try:
            data = _http_get_json(self.list_api, self._gh_headers())
            try:
                with open(cp, "w", encoding="utf-8") as f:
                    json.dump(data, f)
            except Exception:
                pass
            return data
        except Exception:
            try:
                with open(cp, encoding="utf-8") as f:
                    return json.load(f)
            except Exception:
                return []

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


UA = "Cloak-Mirror/1.0 (+https://github.com/gongjuecloak/Cloakwidget)"
APPS = {}
APP_ORDER = []


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
        apps["mes-converter"] = App("mes-converter", default_repo, default_pubkey)
        order.append("mes-converter")
    return apps, order


APPS, APP_ORDER = _load_apps()
if not DEFAULT_APP_ID or DEFAULT_APP_ID not in APPS:
    DEFAULT_APP_ID = APP_ORDER[0] if APP_ORDER else "mes-converter"


def _app_by_id(aid):
    return APPS.get(aid)


# ---- HTTP 辅助 ----
def _http_get_bytes(url, headers=None, timeout=60):
    req = Request(url, headers=headers or {"User-Agent": UA, "Accept": "*/*"})
    with urlopen(req, timeout=timeout) as resp:
        return resp.read()


def _http_get_json(url, headers, timeout=30):
    req = Request(url, headers=headers)
    with urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def find_asset(rel, name):
    if not rel:
        return None
    for a in rel.get("assets", []):
        if a.get("name") == name:
            return a
    return None


def guess_ct(name):
    if name.endswith(".json"):
        return "application/json; charset=utf-8"
    return "application/octet-stream"


def _client_ip(handler):
    xff = handler.headers.get("X-Forwarded-For", "")
    if xff:
        return xff.split(",")[0].strip()
    return handler.client_address[0]


def _sha256_file(path):
    h = hashlib.sha256()
    try:
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(65536), b""):
                h.update(chunk)
        return h.hexdigest()
    except Exception:
        return None


def _record_download(app_id, name, handler, status, bytes_sent):
    ip = _client_ip(handler)
    with _stats_lock:
        _stats["total"] += 1
        _stats["last_ts"] = int(time.time())
        _stats["by_app"][app_id] = _stats["by_app"].get(app_id, 0) + 1
        _stats["by_asset"][name] = _stats["by_asset"].get(name, 0) + 1
        _stats["by_status"][str(status)] = _stats["by_status"].get(str(status), 0) + 1
    log.info("[download] ip=%s app=%s asset=%s status=%d bytes=%d", ip, app_id, name, status, bytes_sent)
    try:
        db_exec("INSERT INTO downloads(app,asset,client_id,ip,status,bytes,created_at) "
                "VALUES(?,?,?,?,?,?,?)", (app_id, name, None, ip, status, bytes_sent, int(time.time())))
        add_activity("download", "Downloaded {}".format(name), app=app_id, client_id=None)
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
        enroll_allowed = ENROLL_OPEN or (ENROLL_SECRET is not None
                                         and q.get("secret", [""])[0] == ENROLL_SECRET)
        if enroll and enroll_allowed:
            new_tok = uuid.uuid4().hex
            _valid_tokens.add(new_tok)
            _save_tokens()
            log.info("[enroll] 新客户端注册，令牌数=%d（开放=%s）", len(_valid_tokens), ENROLL_OPEN)
            return True, new_tok
    return False, None


def _enroll_mode():
    if ENROLL_OPEN:
        return "开放（任何客户端可自注册）"
    if ENROLL_SECRET is not None:
        return "需密钥"
    return "关闭"


# ---------------------------------------------------------------------------
# 同步 Worker（后台自动同步 + 写入 SQLite）
# ---------------------------------------------------------------------------
def _sync_app(app):
    rel, _stale = app.get_release_meta()
    if rel:
        ver = rel.get("tag_name") or "unknown"
        pre = bool(rel.get("prerelease"))
        chan = "prerelease" if pre else "stable"
        upsert_release(app.app_id, ver, chan, rel.get("tag_name"), rel.get("name"),
                      rel.get("published_at"), pre)
        for a in rel.get("assets", []):
            nm = a.get("name")
            if not nm:
                continue
            upsert_asset(app.app_id, ver, nm, a.get("size"), os.path.exists(app.asset_cache_path(nm)))
    try:
        lst = app.get_releases_list(per_page=20)
        for r in lst or []:
            v = r.get("tag_name") or "unknown"
            pre = bool(r.get("prerelease"))
            chan = "prerelease" if pre else "stable"
            upsert_release(app.app_id, v, chan, r.get("tag_name"), r.get("name"),
                          r.get("published_at"), pre)
            for a in r.get("assets", []):
                nm = a.get("name")
                if not nm:
                    continue
                upsert_asset(app.app_id, v, nm, a.get("size"),
                             os.path.exists(app.asset_cache_path(nm)))
    except Exception as e:
        log.warning("[sync] 列表获取失败 %s: %s", app.app_id, e)
    add_activity("sync", "Synced {}".format(app.app_id), app=app.app_id)


def _sync_all():
    for aid in APP_ORDER:
        try:
            _sync_app(APPS[aid])
        except Exception as e:
            log.warning("[sync] 应用 %s 失败: %s", aid, e)


def _sync_worker():
    time.sleep(2)
    while True:
        _sync_all()
        time.sleep(SYNC_INTERVAL)


# ---------------------------------------------------------------------------
# 状态收集（运维 / 系统页）
# ---------------------------------------------------------------------------
def _fmt_bytes(n):
    if n is None:
        return "—"
    n = float(n)
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if n < 1024 or unit == "TB":
            return ("{:.1f} {}".format(n, unit)) if unit != "B" else ("{} B".format(int(n)))
        n /= 1024.0
    return str(n)


def _fmt_uptime(sec):
    d, rem = divmod(int(sec), 86400)
    h, rem = divmod(rem, 3600)
    m, s = divmod(rem, 60)
    parts = []
    if d:
        parts.append("{}天".format(d))
    if h or d:
        parts.append("{}时".format(h))
    if m or h or d:
        parts.append("{}分".format(m))
    parts.append("{}秒".format(s))
    return "".join(parts)


def _fmt_time(ts):
    if not ts:
        return "—"
    return time.strftime("%Y-%m-%d %H:%M:%S", time.localtime(ts))


def _fmt_dt(s):
    if not s:
        return "—"
    s = str(s).replace("T", " ").replace("Z", "")
    return s[:16] if len(s) >= 16 else s


def _cache_total_bytes():
    total = 0
    for aid in APP_ORDER:
        d = APPS[aid].cache_dir
        if os.path.isdir(d):
            try:
                for fn in os.listdir(d):
                    fp = os.path.join(d, fn)
                    if os.path.isfile(fp):
                        total += os.path.getsize(fp)
            except OSError:
                pass
    return total


def gather_status():
    now = time.time()
    ok = True
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
    }


def _esc(s):
    return (str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
            .replace('"', "&quot;"))


# ---------------------------------------------------------------------------
# 数据层（供控制台 / JSON API 共用）
# ---------------------------------------------------------------------------
def _overview_data():
    _, rels = db_query(
        "SELECT app,version,channel,published_at,status FROM releases ORDER BY published_at DESC LIMIT 1")
    latest = None
    if rels:
        r = rels[0]
        latest = {"app": r[0], "version": r[1], "channel": r[2], "published_at": r[3], "status": r[4]}
    _, s = db_query("SELECT MAX(synced_at) FROM releases")
    last_sync = s[0][0] if s and s[0][0] else 0
    day_start = int(time.time()) - (int(time.time()) % 86400)
    _, td = db_query("SELECT COUNT(*) FROM downloads WHERE created_at >= ?", (day_start,))
    _, wd = db_query("SELECT COUNT(*) FROM downloads WHERE created_at >= ?", (int(time.time()) - 7 * 86400,))
    _, tt = db_query("SELECT COUNT(*) FROM downloads")
    return {
        "latest_release": latest,
        "applications": len(APPS),
        "last_sync": last_sync,
        "cache_bytes": _cache_total_bytes(),
        "activity": {
            "today": td[0][0] if td else 0,
            "week": wd[0][0] if wd else 0,
            "total": tt[0][0] if tt else 0,
        },
        "upstream_online": bool(last_sync and (int(time.time()) - last_sync) < SYNC_INTERVAL * 2 + 120),
    }


def _releases_data():
    _, rows = db_query(
        "SELECT app,version,channel,published_at,status,"
        "(SELECT COUNT(*) FROM assets a WHERE a.app=releases.app AND a.version=releases.version),"
        "(SELECT COUNT(*) FROM assets a WHERE a.app=releases.app AND a.version=releases.version AND a.cached=1) "
        "FROM releases ORDER BY published_at DESC LIMIT 100")
    out = []
    for r in rows:
        out.append({"app": r[0], "version": r[1], "channel": r[2], "published_at": r[3],
                    "status": r[4], "assets": r[5], "cached_assets": r[6]})
    return out


def _release_detail_data(app, ver):
    _, rows = db_query(
        "SELECT app,version,channel,tag,published_at,status FROM releases WHERE app=? AND version=?",
        (app, ver))
    if not rows:
        return None
    r = rows[0]
    _, ar = db_query("SELECT name,size,cached,sha256 FROM assets WHERE app=? AND version=?", (app, ver))
    assets = []
    appo = APPS.get(app)
    for a in ar:
        sha = a[3]
        if a[2] and appo and os.path.exists(appo.asset_cache_path(a[0])):
            sha = _sha256_file(appo.asset_cache_path(a[0]))
        assets.append({"name": a[0], "size": a[1], "cached": bool(a[2]), "sha256": sha})
    return {"app": r[0], "version": r[1], "channel": r[2], "tag": r[3],
            "published_at": r[4], "status": r[5], "assets": assets}


def _assets_data():
    _, rows = db_query(
        "SELECT app,version,name,size,cached,cached_at FROM assets ORDER BY cached_at DESC, name LIMIT 300")
    out = []
    for r in rows:
        out.append({"app": r[0], "version": r[1], "name": r[2], "size": r[3],
                    "cached": bool(r[4]), "cached_at": r[5]})
    return out


def _activity_data(limit=250):
    _, rows = db_query(
        "SELECT type,client_id,app,version,message,created_at FROM activities "
        "ORDER BY created_at DESC LIMIT ?", (limit,))
    out = []
    for r in rows:
        out.append({"type": r[0], "client_id": r[1], "app": r[2],
                    "version": r[3], "message": r[4], "created_at": r[5]})
    return out


# ---------------------------------------------------------------------------
# 控制台 UI（极简 / 工程感 / 大留白 / 数字化排版）
# ---------------------------------------------------------------------------
PAGE_CSS = """
:root{
 --bg:#F5F6F8; --panel:#FFFFFF; --ink:#10131A; --muted:#707684;
 --line:#E7E9EE; --line2:#F0F2F5;
 --accent:#10131A; --link:#2F6BFF;
 --ok:#15A34A; --warn:#D9870B; --err:#E03A3A;
 --mono:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace;
 --sans:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif;
}
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{background:var(--bg);color:var(--ink);font:14px/1.6 var(--sans);-webkit-font-smoothing:antialiased}
a{color:var(--link);text-decoration:none}
a:hover{text-decoration:underline}
.app{display:grid;grid-template-columns:252px 1fr;min-height:100vh}
.side{background:var(--panel);border-right:1px solid var(--line);padding:22px 18px;display:flex;flex-direction:column;position:sticky;top:0;height:100vh}
.brand{display:flex;gap:11px;align-items:center;margin-bottom:26px}
.logo{width:34px;height:34px;border-radius:9px;background:var(--ink);color:#fff;display:flex;align-items:center;justify-content:center;font-size:17px;flex:none}
.b1{font-weight:700;font-size:13px;letter-spacing:.04em}
.b2{font-size:11px;color:var(--muted);letter-spacing:.02em}
nav{display:flex;flex-direction:column;gap:2px}
.navitem{display:block;padding:9px 12px;border-radius:8px;color:var(--ink);font-size:13.5px;font-weight:500}
.navitem:hover{background:var(--line2);text-decoration:none}
.navitem.active{background:var(--ink);color:#fff}
.side-foot{margin-top:auto;font-size:11px;color:var(--muted);letter-spacing:.08em}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--ok);margin-right:6px;vertical-align:middle}
.dot.ok{background:var(--ok)} .dot.warn{background:var(--warn)} .dot.err{background:var(--err)}
.ver{margin-top:8px;font-family:var(--mono);font-size:10.5px;color:var(--muted)}
main{display:flex;flex-direction:column;min-width:0}
.topbar{display:flex;align-items:center;justify-content:space-between;padding:16px 30px;border-bottom:1px solid var(--line);background:rgba(255,255,255,.65);backdrop-filter:blur(6px);position:sticky;top:0;z-index:5}
.crumb{font-size:12px;font-weight:700;letter-spacing:.18em;color:var(--muted)}
.clock{font-family:var(--mono);font-size:12px;color:var(--muted)}
.content{padding:30px;max-width:1180px;width:100%}
.hero{margin-bottom:26px}
.hero .hstat{font-size:12px;font-weight:700;letter-spacing:.14em;color:var(--ok);margin-bottom:10px}
h1.title{font-size:26px;font-weight:800;letter-spacing:-.01em;margin:0 0 6px}
.lede{color:var(--muted);font-size:13.5px;margin:0;max-width:760px}
.grid{display:grid;gap:16px}
.stats{grid-template-columns:repeat(auto-fit,minmax(180px,1fr))}
.card{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:18px 20px}
.card h2{font-size:11px;font-weight:700;letter-spacing:.16em;text-transform:uppercase;color:var(--muted);margin:0 0 14px}
.stat .sv{font-family:var(--mono);font-size:24px;font-weight:700;letter-spacing:-.02em}
.stat .sl{font-size:12px;color:var(--muted);margin-top:4px}
.row{display:flex;justify-content:space-between;gap:12px;padding:7px 0;border-bottom:1px dashed var(--line)}
.row:last-child{border-bottom:none}
.row .k{color:var(--muted);font-size:13px}
.row .v{font-family:var(--mono);font-size:13px;font-weight:600;text-align:right;word-break:break-all}
.badge{display:inline-block;padding:2px 9px;border-radius:999px;font-size:11px;font-weight:700;font-family:var(--mono)}
.badge.ok{background:rgba(21,163,74,.12);color:var(--ok)}
.badge.warn{background:rgba(217,135,11,.14);color:var(--warn)}
.badge.err{background:rgba(224,58,58,.12);color:var(--err)}
.badge.mut{background:var(--line2);color:var(--muted)}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;color:var(--muted);font-size:11px;letter-spacing:.08em;text-transform:uppercase;font-weight:700;padding:10px 12px;border-bottom:1px solid var(--line)}
td{padding:11px 12px;border-bottom:1px solid var(--line2);vertical-align:middle}
td.mono,th.mono{font-family:var(--mono)}
tr:hover td{background:var(--line2)}
.tag{font-family:var(--mono);font-size:11px;background:var(--line2);padding:2px 7px;border-radius:6px;color:var(--muted)}
.section-title{font-size:13px;font-weight:700;letter-spacing:.14em;text-transform:uppercase;color:var(--muted);margin:28px 0 12px}
.two{grid-template-columns:1.4fr 1fr}
@media(max-width:860px){.app{grid-template-columns:1fr}.side{position:static;height:auto;flex-direction:row;flex-wrap:wrap;align-items:center;gap:12px}.side nav{flex-direction:row;flex-wrap:wrap}.side-foot{margin:0 0 0 auto}.two{grid-template-columns:1fr}}
.rf{display:inline-flex;gap:6px;align-items:center}
.rf input{border:1px solid var(--line);border-radius:7px;padding:5px 9px;font-size:12px;font-family:var(--mono)}
.rf button{border:1px solid var(--ink);background:var(--ink);color:#fff;border-radius:7px;padding:5px 12px;font-size:12px;cursor:pointer}
.empty{color:var(--muted);font-size:13px;padding:30px;text-align:center}
.act{display:grid;grid-template-columns:88px 1fr;gap:14px;padding:11px 4px;border-bottom:1px solid var(--line2);font-size:13px}
.act .t{font-family:var(--mono);color:var(--muted);font-size:12px}
.act .m b{font-family:var(--mono);font-weight:700}
.sha{font-family:var(--mono);font-size:11px;color:var(--muted);word-break:break-all;display:block;margin-top:2px}
"""

PAGE_JS = """
function tick(){var d=new Date();var p=function(x){return String(x).padStart(2,'0')};var e=document.getElementById('clock');if(e)e.textContent=p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds());}
tick();setInterval(tick,1000);
"""

PAGE_SHELL = """<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>__TITLE__ · Cloak Update Mirror</title>
<style>__CSS__</style>
</head>
<body>
<div class="app">
  <aside class="side">
    <div class="brand">
      <div class="logo">&#9672;</div>
      <div><div class="b1">CLOAK UPDATE MIRROR</div><div class="b2">private release infrastructure</div></div>
    </div>
    <nav>__NAV__</nav>
    <div class="side-foot">
      <span class="dot ok"></span> OPERATIONAL
      <div class="ver">__SERVER_VERSION__</div>
    </div>
  </aside>
  <main>
    <div class="topbar">
      <div class="crumb">__CRUMB__</div>
      <div class="clock" id="clock">--:--:--</div>
    </div>
    <div class="content">__BODY__</div>
  </main>
</div>
<script>__JS__</script>
</body>
</html>"""


def _nav(active):
    items = [("overview", "01 · Overview"), ("releases", "02 · Releases"),
             ("assets", "03 · Assets"), ("activity", "04 · Activity"), ("system", "05 · System")]
    out = []
    for k, label in items:
        cls = " navitem active" if k == active else " navitem"
        out.append('<a class="{}" href="/{}">{}</a>'.format(cls, k, label))
    return "\n".join(out)


def _badge(text, kind="ok"):
    return '<span class="badge {}">{}</span>'.format(kind, _esc(text))


def _channel_badge(channel):
    if channel == "prerelease":
        return _badge("PRERELEASE", "warn")
    return _badge("STABLE", "ok")


def _status_badge(status):
    s = (status or "").upper()
    if s in ("PUBLISHED", "VERIFIED", "CACHED", "SYNCED", "DISCOVERED"):
        return _badge(s, "ok")
    if s in ("FAILED",):
        return _badge(s, "err")
    return _badge(s or "UNKNOWN", "mut")


# ---- 页面构造 ----
def _overview_html():
    d = _overview_data()
    st = gather_status()
    latest = d["latest_release"]
    if latest:
        lat = ('<div class="card"><h2>Latest Release</h2>'
               '<div class="row"><span class="k">Version</span><span class="v">{}</span></div>'
               '<div class="row"><span class="k">Application</span><span class="v">{}</span></div>'
               '<div class="row"><span class="k">Channel</span><span class="v">{}</span></div>'
               '<div class="row"><span class="k">Released</span><span class="v">{}</span></div>'
               '<div style="margin-top:14px">'
               '{} {} {} {}'
               '</div></div>').format(
            _esc(latest["version"]), _esc(latest["app"]), _channel_badge(latest["channel"]),
            _fmt_dt(latest["published_at"]),
            _badge("GitHub · Online", "ok"), _badge("Mirror · Ready", "ok"),
            _badge("Assets · Ready", "ok"), _badge("Update · Ready", "ok"))
    else:
        lat = ('<div class="card"><h2>Latest Release</h2>'
               '<div class="empty">尚未从 GitHub 同步到发布数据，请稍候或手动刷新。</div></div>')
    sync = ('<div class="card"><h2>Sync</h2>'
            '<div class="row"><span class="k">Last successful sync</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Upstream</span><span class="v">github.com/{}</span></div>'
            '<div class="row"><span class="k">Cache</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Interval</span><span class="v">{}s</span></div></div>').format(
        _fmt_time(d["last_sync"]), _esc(APPS[DEFAULT_APP_ID].repo) if DEFAULT_APP_ID in APPS else "—",
        _fmt_bytes(d["cache_bytes"]), SYNC_INTERVAL)
    act = ('<div class="card"><h2>Activity</h2>'
           '<div class="grid stats" style="grid-template-columns:repeat(3,1fr)">'
           '<div><div class="stat"><div class="sv">{}</div><div class="sl">Today</div></div></div>'
           '<div><div class="stat"><div class="sv">{}</div><div class="sl">7 days</div></div></div>'
           '<div><div class="stat"><div class="sv">{}</div><div class="sl">Total downloads</div></div></div>'
           '</div></div>').format(d["activity"]["today"], d["activity"]["week"], d["activity"]["total"])
    stats = ('<div class="grid stats">'
             '<div class="card stat"><div class="sv">{}</div><div class="sl">Latest Release</div></div>'
             '<div class="card stat"><div class="sv">{}</div><div class="sl">Applications</div></div>'
             '<div class="card stat"><div class="sv">{}</div><div class="sl">Total Downloads</div></div>'
             '<div class="card stat"><div class="sv">{}</div><div class="sl">Cache Size</div></div>'
             '</div>').format(
        _esc(latest["version"]) if latest else "—", d["applications"],
        d["activity"]["total"], _fmt_bytes(d["cache_bytes"]))
    hero = ('<div class="hero">'
            '<div class="hstat"><span class="dot ok"></span> OPERATIONAL</div>'
            '<h1 class="title">CLOAK UPDATE MIRROR</h1>'
            '<p class="lede">Private update infrastructure for Cloak applications. '
            'GitHub Releases is the source — Mirror handles sync, cache, verify, distribute, audit.</p>'
            '</div>')
    return hero + '<meta http-equiv="refresh" content="30">' + stats + \
        '<div class="grid two" style="margin-top:16px">' + lat + sync + '</div>' + act


def _releases_html():
    rows = _releases_data()
    if not rows:
        return '<div class="empty">暂无发布记录，请等待后台同步或手动刷新。</div>'
    body = []
    for r in rows:
        cached = r["cached_assets"]
        total = r["assets"]
        cache_badge = _badge("Cached {}/{}".format(cached, total), "ok" if cached == total else "warn")
        body.append(
            '<tr><td class="mono"><a href="/releases/{}/{}">{}</a></td>'
            '<td>{}</td><td>{}</td><td class="mono">{}</td><td>{}</td><td>{}</td></tr>'.format(
                _esc(r["app"]), _esc(r["version"]), _esc(r["version"]),
                _esc(r["app"]), _channel_badge(r["channel"]),
                _fmt_dt(r["published_at"]), cache_badge, _status_badge(r["status"])))
    return ('<table><thead><tr>'
            '<th class="mono">Version</th><th>Application</th><th>Channel</th>'
            '<th class="mono">Released</th><th>Cache</th><th>Status</th>'
            '</tr></thead><tbody>{}</tbody></table>').format("".join(body))


def _release_detail_html(app, version):
    d = _release_detail_data(app, version)
    if not d:
        return '<div class="empty">未找到该发布：{} / {}</div>'.format(_esc(app), _esc(version))
    rows = []
    for a in d["assets"]:
        if a["cached"]:
            sha = _esc((a["sha256"] or "")[:16]) + ("…" if a["sha256"] else "")
            cache_b = _badge("Cached", "ok")
            vrf = _badge("Verified", "ok") if a["sha256"] else _badge("No SHA", "mut")
        else:
            sha = ""
            cache_b = _badge("Missing", "warn")
            vrf = _badge("—", "mut")
        rows.append(
            '<tr><td class="mono">{}</td><td class="mono">{}</td>'
            '<td>{} {}</td><td class="mono">{}</td></tr>'
            '<tr><td colspan="4"><span class="sha">SHA256 {}</span></td></tr>'.format(
                _esc(a["name"]), _fmt_bytes(a["size"]), cache_b, vrf,
                sha if sha else "—"))
    return ('<div class="card"><h2>{}</h2>'
            '<div class="row"><span class="k">Application</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Channel</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Tag</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Published</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Status</span><span class="v">{}</span></div></div>'
            '<div class="section-title">Assets</div>'
            '<table><thead><tr><th class="mono">Name</th><th class="mono">Size</th>'
            '<th>Cache / Verify</th><th class="mono">SHA256</th></tr></thead>'
            '<tbody>{}</tbody></table>').format(
        _esc(d["version"]), _esc(d["app"]), _channel_badge(d["channel"]),
        _esc(d["tag"] or ""), _fmt_dt(d["published_at"]), _status_badge(d["status"]),
        "".join(rows))


def _assets_html():
    rows = _assets_data()
    if not rows:
        return '<div class="empty">暂无缓存资产。</div>'
    body = []
    for r in rows:
        cb = _badge("Cached", "ok") if r["cached"] else _badge("Missing", "warn")
        body.append(
            '<tr><td class="mono">{}</td><td>{}</td><td class="mono">{}</td>'
            '<td class="mono">{}</td><td>{}</td><td class="mono">{}</td></tr>'.format(
                _esc(r["name"]), _esc(r["app"]), _esc(r["version"]),
                _fmt_bytes(r["size"]), cb, _fmt_time(r["cached_at"])))
    refresh = ('<div class="section-title">Actions</div>'
               '<form class="rf" method="post" action="/admin/refresh?app={}">'
               '<input type="text" name="admin_token" placeholder="admin token" autocomplete="off">'
               '<button type="submit">Refresh</button></form>'
               '<p class="lede" style="margin-top:10px">Verify 列展示文件的实时 SHA256（服务端计算），'
               '可在资产清单页查看。刷新需管理员令牌。</p>').format(_esc(DEFAULT_APP_ID))
    return ('<table><thead><tr><th class="mono">Name</th><th>App</th><th class="mono">Version</th>'
            '<th class="mono">Size</th><th>Cache</th><th class="mono">Cached At</th></tr></thead>'
            '<tbody>{}</tbody></table>').format("".join(body)) + refresh


def _activity_html():
    rows = _activity_data(250)
    if not rows:
        return '<div class="empty">暂无活动记录。</div>'
    body = []
    for r in rows:
        t = _fmt_time(r["created_at"])
        msg = _esc(r["message"] or "")
        if r["app"]:
            msg += ' <b>· {}</b>'.format(_esc(r["app"]))
        if r["version"]:
            msg += ' <b>{}</b>'.format(_esc(r["version"]))
        body.append('<div class="act"><div class="t">{}</div><div class="m">{}</div></div>'.format(t, msg))
    return "".join(body)


def _system_html():
    st = gather_status()
    disk = st.get("disk")
    _, s = db_query("SELECT MAX(synced_at) FROM releases")
    last_sync = s[0][0] if s and s[0][0] else 0
    gh = _badge("Online", "ok") if (last_sync and (int(time.time()) - last_sync) < SYNC_INTERVAL * 2 + 120) \
        else _badge("Unknown", "warn")
    admin_on = _badge("Enabled", "ok") if ADMIN_TOKEN else _badge("Disabled", "err")
    enroll = _badge(_enroll_mode(), "mut")
    disk_html = ("<div class=\"row\"><span class=\"k\">Total</span><span class=\"v\">{}</span></div>"
                 "<div class=\"row\"><span class=\"k\">Used</span><span class=\"v\">{}</span></div>"
                 "<div class=\"row\"><span class=\"k\">Free</span><span class=\"v\">{}</span></div>").format(
        _fmt_bytes(disk["total"]), _fmt_bytes(disk["used"]), _fmt_bytes(disk["free"])) if disk else \
        '<div class="empty">无法读取磁盘信息</div>'
    return ('<div class="grid two">'
            '<div class="card"><h2>Service</h2>'
            '<div class="row"><span class="k">Version</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Uptime</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Python</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Port</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Applications</span><span class="v">{}</span></div></div>'
            '<div class="card"><h2>Storage</h2>{}</div></div>'
            '<div class="grid two" style="margin-top:16px">'
            '<div class="card"><h2>Upstream</h2>'
            '<div class="row"><span class="k">GitHub</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Last sync</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Sync interval</span><span class="v">{}s</span></div></div>'
            '<div class="card"><h2>Security</h2>'
            '<div class="row"><span class="k">Registered tokens</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Enrollment</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Admin refresh</span><span class="v">{}</span></div></div>'
            '</div>').format(
        SERVER_VERSION, _fmt_uptime(st["uptime_sec"]), _esc(sys.version.split()[0]),
        st["port"], st["app_count"], disk_html, gh, _fmt_time(last_sync), SYNC_INTERVAL,
        st["registered_tokens"], enroll, admin_on)


# ---------------------------------------------------------------------------
# HTTP Handler
# ---------------------------------------------------------------------------
class Handler(BaseHTTPRequestHandler):
    server_version = "Cloak-Mirror/1.0"
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _access_log(self, code, extra=""):
        log.info("[access] %s %s -> %d %s", _client_ip(self), _redact(self.path), code, extra)

    def _send(self, code, body, ctype, extra=None):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def _send_json(self, obj, code=200):
        body = json.dumps(obj, ensure_ascii=False, indent=2).encode("utf-8")
        self._send(code, body, "application/json; charset=utf-8")

    def do_HEAD(self):
        self.do_GET()

    def _require_auth(self):
        ok, nxt = _check_auth(self)
        if not ok:
            self._send(401, b'{"error":"unauthorized"}\n', "application/json")
            self._access_log(401, "auth-fail")
            return None
        return nxt

    def _post_form(self):
        try:
            n = int(self.headers.get("Content-Length", 0))
            if n > 0:
                data = parse_qs(self.rfile.read(n).decode("utf-8", "ignore"))
                return data
        except Exception:
            pass
        return {}

    def _console(self, active, body, crumb=None):
        html = (PAGE_SHELL.replace("__TITLE__", "Cloak Update Mirror")
                .replace("__CSS__", PAGE_CSS)
                .replace("__NAV__", _nav(active))
                .replace("__CRUMB__", (crumb or active).upper())
                .replace("__BODY__", body)
                .replace("__SERVER_VERSION__", SERVER_VERSION)
                .replace("__JS__", PAGE_JS))
        self._send(200, html.encode("utf-8"), "text/html; charset=utf-8")
        self._access_log(200, "console:" + active)

    def _console_dispatch(self, segs):
        kind = segs[0]
        if kind == "releases":
            if len(segs) >= 3:
                return self._console("releases", _release_detail_html(segs[1], segs[2]),
                                     crumb="RELEASES / " + segs[2])
            return self._console("releases", _releases_html())
        if kind == "assets":
            return self._console("assets", _assets_html())
        if kind == "activity":
            return self._console("activity", _activity_html())
        if kind == "system":
            return self._console("system", _system_html())
        return self._console("overview", _overview_html())

    def _api_dispatch(self, rest):
        if not rest or rest[0] == "status":
            self._send_json(gather_status())
            return
        if rest[0] == "v1":
            self._api_v1(rest[1:])
            return
        self._send_json({"error": "not found"}, 404)

    def _api_v1(self, rest):
        if not rest or rest[0] == "overview":
            self._send_json(_overview_data())
        elif rest[0] == "releases":
            if len(rest) >= 3:
                d = _release_detail_data(rest[1], rest[2])
                self._send_json(d or {"error": "not found"}, 404 if not d else 200)
            else:
                self._send_json(_releases_data())
        elif rest[0] == "assets":
            self._send_json(_assets_data())
        elif rest[0] == "activity":
            self._send_json(_activity_data())
        else:
            self._send_json({"error": "unknown endpoint"}, 404)

    def _resolve(self):
        raw = self.path.split("?", 1)[0].lstrip("/")
        segs = raw.split("/") if raw else []
        if not segs or segs[0] == "":
            return "GLOBAL", ""
        if segs[0] in ("healthz", "status", "api"):
            return "GLOBAL", raw
        if segs[0] == "admin":
            return "ADMIN", raw
        if segs[0] in APPS and len(segs) >= 1:
            return APPS[segs[0]], "/".join(segs[1:])
        return APPS.get(DEFAULT_APP_ID), raw

    def do_GET(self):
        raw = self.path.split("?", 1)[0].lstrip("/")
        segs = raw.split("/") if raw else []
        # 控制台（v1.0）
        if not segs or segs[0] == "":
            return self._console("overview", _overview_html())
        if segs[0] == "status":
            return self._console("overview", _overview_html())
        if segs[0] in ("releases", "assets", "activity", "system"):
            return self._console_dispatch(segs)
        if segs[0] == "api":
            return self._api_dispatch(segs[1:])
        # 既有客户端协议
        target, rel = self._resolve()
        if target == "GLOBAL":
            if rel == "healthz":
                self._send(200, b'{"ok":true}\n', "application/json")
                self._access_log(200, "healthz")
                return
            return self._console("overview", _overview_html())
        if target == "ADMIN":
            self._send(404, b'{"error":"use POST for admin"}\n', "application/json")
            return
        if target is None:
            self._send(404, b'{"error":"unknown app"}\n', "application/json")
            self._access_log(404, "no-app")
            return
        if rel == "status":
            self._send(302, b"", "text/html", {"Location": "/releases"})
            return
        # 其余均为「拉取」：需鉴权
        nxt = self._require_auth()
        if nxt is None:
            return
        extra = {"X-Next-Token": nxt} if nxt else {}
        app = target
        asset_name = rel
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
        # 客户端签到（新增，v1.2 预览；不改动既有更新协议）
        if segs and segs[0] == "api" and len(segs) >= 4 and segs[1] == "v1" \
                and segs[2] == "client" and segs[3] == "checkin":
            return self._handle_checkin()
        form = self._post_form()
        tok = (q.get("admin_token", [None])[0] or form.get("admin_token", [None])[0]
               or self.headers.get("X-Admin-Token"))
        # 全局刷新：可选 ?app=；无则刷新全部
        if (not segs or segs[0] == "admin") and (len(segs) <= 1 or segs[1] == "refresh"):
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
                    _sync_app(APPS[target_app])
                else:
                    summaries = [a.refresh_now() for a in (APPS[x] for x in APP_ORDER)]
                    _sync_all()
                self._send_json(summaries)
            except Exception as e:
                self._send(500, ("刷新失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return
        # 命名空间内刷新：/<app_id>/admin/refresh
        if len(segs) >= 2 and segs[0] in APPS and segs[1] == "admin":
            if not ADMIN_TOKEN or tok != ADMIN_TOKEN:
                self._send(401, b'{"error":"unauthorized"}\n', "application/json")
                return
            app = APPS[segs[0]]
            log.info("[admin] refresh 触发 by=%s app=%s", _client_ip(self), app.app_id)
            try:
                summary = app.refresh_now()
                _sync_app(app)
                self._send_json(summary)
            except Exception as e:
                self._send(500, ("刷新失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return
        self._send(404, b'{"error":"not found"}\n', "application/json")

    def _handle_checkin(self):
        try:
            n = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(n).decode("utf-8", "ignore") if n > 0 else "{}"
            data = json.loads(body) if body else {}
        except Exception:
            data = {}
        cid = (data.get("client_id") or data.get("hostname") or "").strip() or "unknown"
        app = data.get("app") or DEFAULT_APP_ID
        ver = data.get("version") or "unknown"
        chan = data.get("channel") or "stable"
        osname = data.get("os") or ""
        host = data.get("hostname") or cid
        try:
            upsert_client(cid, app, ver, chan, host, osname)
            add_activity("checkin", "{} checked in v{}".format(cid, ver),
                         app=app, version=ver, client_id=cid)
        except Exception as e:
            log.warning("[checkin] 写入失败: %s", e)
        self._send_json({"ok": True, "client_id": cid, "server_time": int(time.time())})


def main():
    os.makedirs(CACHE_DIR, exist_ok=True)
    _db_init()
    _load_tokens()
    for aid in APP_ORDER:
        log.info("app %s -> repo %s", aid, APPS[aid].repo)
    threading.Thread(target=_sync_worker, daemon=True).start()
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    log.info("serving %d apps on :%d, default=%s, cache=%s", len(APPS), PORT, DEFAULT_APP_ID, CACHE_DIR)
    log.info("enroll_mode=%s, registered_tokens=%d, github_token=%s, admin_token=%s, sync_interval=%ds",
             _enroll_mode(), len(_valid_tokens),
             "set" if GITHUB_TOKEN else "none", "set" if ADMIN_TOKEN else "NONE(refresh disabled)",
             SYNC_INTERVAL)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()


if __name__ == "__main__":
    main()
