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

控制台（v1.0）：中文界面，需账号密码登录（`/` 即 Overview）。
  客户端请求协议（保持不变，向后兼容）：
    GET /                                        -> 控制台 Overview（HTML，需登录）
    GET /login  /logout                          -> 登录 / 退出（不受控制台鉴权保护）
    GET /healthz                                 -> 健康检查 {"ok":true}（开放）
    GET /{app_id}/version.json                    -> 该应用 latest release 的 manifest（令牌鉴权）
    GET /{app_id}/{asset}                         -> 对应资产（首次拉取后落盘缓存，令牌鉴权）
    GET /releases /assets /activity /system       -> 控制台各页面（需登录）
    GET /api/v1/overview|releases|assets|activity -> JSON API（需登录）
    POST /api/v1/client/checkin                   -> 客户端主动上报（新增，不改动既有更新协议）
    POST /admin/refresh  (+ ?app=)                -> 强制刷新（需 MIRROR_ADMIN_TOKEN）

安全（沿用并增强）：
  - 控制台全部页面与 JSON API 需账号密码会话登录（Cookie + 服务端会话表）
  - 拉取资产需令牌；令牌一次性旋转（X-Next-Token），泄露面低
  - 管理员刷新用独立令牌（X-Admin-Token / ?admin_token=），与拉取令牌隔离
  - enroll 默认关闭（需密钥）；日志对令牌脱敏并轮转
  - GitHub 不可达时回退磁盘缓存并打 X-Cache: stale，绝不无谓 502

配置（环境变量，建议 .env + docker compose env_file 注入）：
  MIRROR_APPS_JSON       应用注册表路径（默认 ./apps.json）
  MIRROR_DEFAULT_APP     默认应用 id（根路径 /version.json 落到它）
  MIRROR_PORT            监听端口（默认 8080）
  MIRROR_CACHE           缓存根目录（默认 /data/cache，SQLite 库也在此）
  MIRROR_GITHUB_TOKEN    GitHub Token（可选，提升 API 速率上限）
  MIRROR_ADMIN_TOKEN     管理员令牌（POST /admin/refresh 必须，留空则永远 401）
  MIRROR_ACCESS_TOKEN    客户端 enroll 密钥（可选）
  MIRROR_ENROLL_OPEN     是否开放注册（默认 false）
  MIRROR_SYNC_INTERVAL   后台自动同步周期秒（默认 300）
  MIRROR_CONSOLE_USER    控制台登录用户名（默认 admin）
  MIRROR_CONSOLE_PASSWORD 控制台登录密码（强烈建议设置；留空则启动时随机生成并写日志）
  MIRROR_SESSION_TTL     会话有效期秒（默认 28800 = 8 小时）
  MIRROR_SECURE_COOKIE    是否给会话 Cookie 加 Secure 标记（HTTPS 下设 1）

依赖：仅 Python 标准库（含 sqlite3）。
"""
import os
import re
import sys
import json
import time
import hmac
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

# ---- 控制台登录配置 ----
CONSOLE_USER = os.environ.get("MIRROR_CONSOLE_USER", "admin")
CONSOLE_PASSWORD = os.environ.get("MIRROR_CONSOLE_PASSWORD", "")
SESSION_TTL = int(os.environ.get("MIRROR_SESSION_TTL", "28800"))
SECURE_COOKIE = os.environ.get("MIRROR_SECURE_COOKIE", "0") == "1"
SESSIONS = {}  # sid -> {"user":..., "exp":...}
_session_lock = threading.Lock()

START_TIME = time.time()
SERVER_VERSION = "Cloak-Mirror/1.1.0"

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
# 控制台会话（账号密码登录）
# ---------------------------------------------------------------------------
def _make_session(user):
    sid = uuid.uuid4().hex
    with _session_lock:
        SESSIONS[sid] = {"user": user, "exp": time.time() + SESSION_TTL}
    return sid


def _get_session(handler):
    cookie = handler.headers.get("Cookie", "")
    sid = None
    for part in cookie.split(";"):
        part = part.strip()
        if part.startswith("mirror_sid="):
            sid = part[len("mirror_sid="):]
    if not sid:
        return None
    with _session_lock:
        s = SESSIONS.get(sid)
        if not s:
            return None
        if s["exp"] < time.time():
            SESSIONS.pop(sid, None)
            return None
    return sid


def _session_cookie(sid):
    c = "mirror_sid={}; HttpOnly; Path=/; Max-Age={}".format(sid, SESSION_TTL)
    if SECURE_COOKIE:
        c += "; Secure"
    return c


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


UA = "Cloak-Mirror/1.1 (+https://github.com/gongjuecloak/Cloakwidget)"
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
        add_activity("download", "下载了 {}".format(name), app=app_id, client_id=None)
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
    add_activity("sync", "已同步 {}".format(app.app_id), app=app.app_id)


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
# 控制台 UI（中文 · 极简 / 工程感 / 大留白 / 数字化排版）
# ---------------------------------------------------------------------------
PAGE_CSS = """
:root{
  --bg:#F6F7F9; --panel:#FFFFFF; --ink:#0E1117; --ink-2:#5B6472; --muted:#8A93A2;
  --line:#ECEEF1; --line-2:#F3F4F6;
  --side:#0E1117; --side-2:#15191F; --side-ink:#C7CDD6; --side-mut:#6B7280;
  --accent:#2F6BFF; --accent-soft:rgba(47,107,255,.10);
  --ok:#15A34A; --ok-soft:rgba(21,163,74,.12);
  --warn:#D9870B; --warn-soft:rgba(217,135,11,.14);
  --err:#E03A3A; --err-soft:rgba(224,58,58,.12);
  --mono:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace;
  --sans:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif;
  --radius:14px; --shadow:0 1px 2px rgba(16,19,26,.04),0 6px 20px rgba(16,19,26,.05);
}
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{background:var(--bg);color:var(--ink);font:14px/1.6 var(--sans);-webkit-font-smoothing:antialiased;font-feature-settings:"tnum"}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}
.app{display:grid;grid-template-columns:248px 1fr;min-height:100vh}
.side{background:linear-gradient(180deg,var(--side),var(--side-2));color:var(--side-ink);padding:24px 16px;display:flex;flex-direction:column;position:sticky;top:0;height:100vh}
.brand{display:flex;gap:12px;align-items:center;padding:6px 8px 22px}
.logo{width:36px;height:36px;border-radius:10px;background:var(--accent);color:#fff;display:flex;align-items:center;justify-content:center;font-size:18px;flex:none;box-shadow:0 4px 14px rgba(47,107,255,.4)}
.b1{font-weight:700;font-size:13.5px;color:#fff;letter-spacing:.02em;line-height:1.3}
.b2{font-size:10px;color:var(--side-mut);letter-spacing:.14em;text-transform:uppercase;margin-top:3px}
.nav{display:flex;flex-direction:column;gap:4px;margin-top:4px}
.navitem{display:flex;align-items:center;gap:12px;padding:10px 12px;border-radius:10px;color:var(--side-ink);font-size:13.5px;font-weight:500;position:relative}
.navitem:hover{background:rgba(255,255,255,.06);text-decoration:none;color:#fff}
.navitem.active{background:rgba(47,107,255,.16);color:#fff}
.navitem.active::before{content:"";position:absolute;left:0;top:9px;bottom:9px;width:3px;border-radius:0 3px 3px 0;background:var(--accent)}
.nav-ico{width:18px;height:18px;flex:none;opacity:.85}
.side-foot{margin-top:auto;font-size:11px;color:var(--side-mut);letter-spacing:.05em;display:flex;flex-direction:column;gap:7px}
.side-foot .ver{font-family:var(--mono);font-size:10.5px}
.runbadge{display:inline-flex;align-items:center;gap:7px}
main{display:flex;flex-direction:column;min-width:0}
.topbar{display:flex;align-items:center;justify-content:space-between;padding:14px 32px;border-bottom:1px solid var(--line);background:rgba(255,255,255,.82);backdrop-filter:blur(8px);position:sticky;top:0;z-index:5}
.crumb{font-size:11px;font-weight:700;letter-spacing:.18em;text-transform:uppercase;color:var(--muted)}
.topright{display:flex;align-items:center;gap:18px}
.logout{font-size:12.5px;color:var(--ink-2);font-weight:600}
.logout:hover{color:var(--ink)}
.clock{font-family:var(--mono);font-size:12px;color:var(--muted)}
.avatar{width:30px;height:30px;border-radius:50%;background:var(--accent-soft);color:var(--accent);display:flex;align-items:center;justify-content:center;font-weight:700;font-size:12px}
.content{padding:32px;max-width:1200px;width:100%}
.hero{margin-bottom:26px}
.hstat{display:inline-flex;align-items:center;gap:8px;font-size:11px;font-weight:700;letter-spacing:.08em;color:var(--ok);background:var(--ok-soft);padding:5px 12px;border-radius:999px;text-transform:uppercase}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--ok);box-shadow:0 0 0 3px var(--ok-soft)}
.dot.ok{background:var(--ok)} .dot.err{background:var(--err)}
h1.title{font-size:27px;font-weight:800;letter-spacing:-.02em;margin:14px 0 6px}
.lede{color:var(--ink-2);font-size:13.5px;margin:0;max-width:680px;line-height:1.7}
.grid{display:grid;gap:18px}
.stats{grid-template-columns:repeat(auto-fit,minmax(190px,1fr))}
.card{background:var(--panel);border:1px solid var(--line);border-radius:var(--radius);padding:20px;box-shadow:var(--shadow)}
.card h2{font-size:11px;font-weight:700;letter-spacing:.14em;text-transform:uppercase;color:var(--muted);margin:0 0 16px}
.stat .sv{font-family:var(--mono);font-size:27px;font-weight:700;letter-spacing:-.02em;line-height:1}
.stat .sl{font-size:12px;color:var(--muted);margin-top:8px}
.row{display:flex;justify-content:space-between;gap:12px;padding:11px 0;border-bottom:1px solid var(--line-2)}
.row:last-child{border-bottom:none;padding-bottom:0}
.row .k{color:var(--ink-2);font-size:13px}
.row .v{font-family:var(--mono);font-size:13px;font-weight:600;text-align:right;word-break:break-all}
.badge{display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:999px;font-size:11px;font-weight:700;font-family:var(--mono)}
.badge.ok{background:var(--ok-soft);color:var(--ok)}
.badge.warn{background:var(--warn-soft);color:var(--warn)}
.badge.err{background:var(--err-soft);color:var(--err)}
.badge.mut{background:var(--line-2);color:var(--muted)}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;color:var(--muted);font-size:11px;letter-spacing:.05em;text-transform:uppercase;font-weight:700;padding:13px 14px;border-bottom:1px solid var(--line);background:var(--line-2)}
td{padding:13px 14px;border-bottom:1px solid var(--line-2);vertical-align:middle}
td.mono,th.mono{font-family:var(--mono)}
tr:hover td{background:var(--line-2)}
.tag{font-family:var(--mono);font-size:11px;background:var(--line-2);padding:3px 8px;border-radius:6px;color:var(--ink-2)}
.section-title{font-size:12px;font-weight:700;letter-spacing:.14em;text-transform:uppercase;color:var(--muted);margin:30px 0 14px;display:flex;align-items:center;gap:10px}
.section-title::after{content:"";flex:1;height:1px;background:var(--line)}
.two{grid-template-columns:1.5fr 1fr}
@media(max-width:880px){.app{grid-template-columns:1fr}.side{position:static;height:auto;flex-direction:row;flex-wrap:wrap;align-items:center;gap:10px}.nav{flex-direction:row;flex-wrap:wrap;margin:0}.side-foot{margin:0 0 0 auto}.two{grid-template-columns:1fr}.topbar{padding:12px 18px}.content{padding:20px}}
.rf{display:inline-flex;gap:8px;align-items:center}
.rf input{border:1px solid var(--line);border-radius:8px;padding:7px 11px;font-size:12.5px;font-family:var(--mono);outline:none}
.rf input:focus{border-color:var(--accent)}
.rf button{border:none;background:var(--accent);color:#fff;border-radius:8px;padding:8px 16px;font-size:12.5px;font-weight:600;cursor:pointer}
.empty{color:var(--muted);font-size:13px;padding:40px;text-align:center;background:var(--line-2);border-radius:var(--radius)}
.act{display:grid;grid-template-columns:120px 1fr;gap:16px;padding:13px 4px;border-bottom:1px solid var(--line-2);font-size:13px}
.act:last-child{border-bottom:none}
.act .t{font-family:var(--mono);color:var(--muted);font-size:11.5px;white-space:nowrap}
.act .m b{font-family:var(--mono);font-weight:700}
.sha{font-family:var(--mono);font-size:11px;color:var(--muted);word-break:break-all;display:block;margin-top:4px;background:var(--line-2);padding:5px 9px;border-radius:7px}
"""

PAGE_JS = """
function tick(){var d=new Date();var p=function(x){return String(x).padStart(2,'0')};var e=document.getElementById('clock');if(e)e.textContent=p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds());}
tick();setInterval(tick,1000);
(function(){
  function formatBytes(n){if(n==null)return '—';var u=['B','KB','MB','GB','TB'],i=0;while(n>=1024&&i<u.length-1){n/=1024;i++;}return (i===0?Math.round(n):n.toFixed(1))+' '+u[i];}
  function fmtTime(ts){if(!ts)return '—';var d=new Date(ts*1000);var p=function(x){return String(x).padStart(2,'0')};return d.getFullYear()+'-'+p(d.getMonth()+1)+'-'+p(d.getDate())+' '+p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds());}
  function set(id,v){var e=document.getElementById(id);if(e)e.textContent=v;}
  function poll(){
    fetch('/api/v1/overview',{credentials:'same-origin'}).then(function(r){return r.ok?r.json():null;}).then(function(d){
      if(!d)return;
      if(d.latest_release)set('ov-latest-version',d.latest_release.version);
      set('ov-latest-version2',d.latest_release?d.latest_release.version:'—');
      set('ov-apps',d.applications);
      set('ov-total',d.activity.total);
      set('ov-total-act',d.activity.total);
      set('ov-cache',formatBytes(d.cache_bytes));
      set('ov-cache2',formatBytes(d.cache_bytes));
      set('ov-lastsync',fmtTime(d.last_sync));
      set('ov-today',d.activity.today);
      set('ov-week',d.activity.week);
      var up=document.getElementById('ov-upstream');if(up)up.className='dot'+(d.upstream_online?' ok':' err');
    }).catch(function(){});
  }
  setInterval(poll,15000);
})();
"""

PAGE_SHELL = """<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>__TITLE__ · Cloak 更新镜像</title>
<style>__CSS__</style>
</head>
<body>
<div class="app">
  <aside class="side">
    <div class="brand">
      <div class="logo">&#9672;</div>
      <div><div class="b1">Cloak 更新镜像</div><div class="b2">Update Mirror</div></div>
    </div>
    <nav class="nav">__NAV__</nav>
    <div class="side-foot">
      <span class="runbadge"><span class="dot"></span> 运行中</span>
      <span class="ver">__SERVER_VERSION__</span>
    </div>
  </aside>
  <main>
    <div class="topbar">
      <div class="crumb">__CRUMB__</div>
      <div class="topright">
        <div class="clock" id="clock">--:--:--</div>
        <div class="avatar" id="avatar">A</div>
        <a href="/logout" class="logout">退出</a>
      </div>
    </div>
    <div class="content">__BODY__</div>
  </main>
</div>
<script>__JS__</script>
</body>
</html>"""


_ICONS = {
  "overview": '<svg class="nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg>',
  "releases": '<svg class="nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3 7l9-4 9 4v10l-9 4-9-4z"/><path d="M3 7l9 4 9-4M12 11v10"/></svg>',
  "assets": '<svg class="nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M21 16V8a2 2 0 0 0-1-1.7l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.7l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/></svg>',
  "activity": '<svg class="nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3 12h4l3 8 4-16 3 8h4"/></svg>',
  "system": '<svg class="nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="3.2"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.5 5.5l2 2M16.5 16.5l2 2M18.5 5.5l-2 2M7.5 16.5l-2 2"/></svg>'
}


def _nav(active):
    items = [("overview", "概览"), ("releases", "发布"),
             ("assets", "资产"), ("activity", "活动"), ("system", "系统")]
    out = []
    for k, label in items:
        cls = " navitem active" if k == active else " navitem"
        out.append('<a class="{}" href="/{}">{}{}</a>'.format(cls, k, _ICONS.get(k, ""), label))
    return "\n".join(out)


_CRUMB_CN = {"overview": "概览", "releases": "发布", "assets": "资产", "activity": "活动", "system": "系统"}
_STATUS_CN = {"PUBLISHED": "已发布", "VERIFIED": "已校验", "CACHED": "已缓存",
              "SYNCED": "已同步", "DISCOVERED": "已发现", "FAILED": "失败"}


def _badge(text, kind="ok"):
    return '<span class="badge {}">{}</span>'.format(kind, _esc(text))


def _channel_badge(channel):
    if channel == "prerelease":
        return _badge("预发布", "warn")
    return _badge("稳定版", "ok")


def _status_badge(status):
    s = (status or "").upper()
    cn = _STATUS_CN.get(s, s or "未知")
    if s in ("PUBLISHED", "VERIFIED", "CACHED", "SYNCED", "DISCOVERED"):
        return _badge(cn, "ok")
    if s == "FAILED":
        return _badge(cn, "err")
    return _badge(cn, "mut")


# ---- 页面构造 ----
def _overview_html():
    d = _overview_data()
    latest = d["latest_release"]
    if latest:
        lat = ('<div class="card"><h2>最新发布</h2>'
               '<div class="row"><span class="k">版本</span><span class="v" id="ov-latest-version">{}</span></div>'
               '<div class="row"><span class="k">应用</span><span class="v">{}</span></div>'
               '<div class="row"><span class="k">渠道</span><span class="v">{}</span></div>'
               '<div class="row"><span class="k">发布于</span><span class="v">{}</span></div>'
               '<div style="margin-top:16px;display:flex;gap:8px;flex-wrap:wrap">'
               '{} {} {} {}</div></div>').format(
            _esc(latest["version"]), _esc(latest["app"]), _channel_badge(latest["channel"]),
            _fmt_dt(latest["published_at"]),
            _badge("GitHub · 在线", "ok"), _badge("镜像 · 就绪", "ok"),
            _badge("资产 · 就绪", "ok"), _badge("更新 · 就绪", "ok"))
    else:
        lat = ('<div class="card"><h2>最新发布</h2>'
               '<div class="empty">尚未从 GitHub 同步到发布数据，请稍候或手动刷新。</div></div>')
    sync = ('<div class="card"><h2>同步</h2>'
            '<div class="row"><span class="k">上次成功同步</span><span class="v" id="ov-lastsync">{}</span></div>'
            '<div class="row"><span class="k">上游</span><span class="v">github.com/{}</span></div>'
            '<div class="row"><span class="k">缓存</span><span class="v" id="ov-cache">{}</span></div>'
            '<div class="row"><span class="k">间隔</span><span class="v">{}s</span></div></div>').format(
        _fmt_time(d["last_sync"]), _esc(APPS[DEFAULT_APP_ID].repo) if DEFAULT_APP_ID in APPS else "—",
        _fmt_bytes(d["cache_bytes"]), SYNC_INTERVAL)
    stats = ('<div class="grid stats">'
             '<div class="card stat"><div class="sv" id="ov-latest-version2">{}</div><div class="sl">最新版本</div></div>'
             '<div class="card stat"><div class="sv" id="ov-apps">{}</div><div class="sl">应用数</div></div>'
             '<div class="card stat"><div class="sv" id="ov-total">{}</div><div class="sl">累计下载</div></div>'
             '<div class="card stat"><div class="sv" id="ov-cache2">{}</div><div class="sl">缓存大小</div></div>'
             '</div>').format(
        _esc(latest["version"]) if latest else "—", d["applications"],
        d["activity"]["total"], _fmt_bytes(d["cache_bytes"]))
    act = ('<div class="card"><h2>活动</h2>'
           '<div class="grid stats" style="grid-template-columns:repeat(3,1fr)">'
           '<div class="stat"><div class="sv" id="ov-today">{}</div><div class="sl">今日下载</div></div>'
           '<div class="stat"><div class="sv" id="ov-week">{}</div><div class="sl">近 7 天</div></div>'
           '<div class="stat"><div class="sv" id="ov-total-act">{}</div><div class="sl">累计下载</div></div>'
           '</div></div>').format(d["activity"]["today"], d["activity"]["week"], d["activity"]["total"])
    hero = ('<div class="hero">'
            '<div class="hstat"><span class="dot" id="ov-upstream"></span> 运行正常</div>'
            '<h1 class="title">Cloak 更新镜像</h1>'
            '<p class="lede">Cloak 应用的私有发布与更新基础设施。GitHub Releases 是源头 —— '
            '镜像负责同步、缓存、校验、分发与审计。</p></div>')
    return hero + stats + '<div class="grid two" style="margin-top:18px">' + lat + sync + '</div>' + act


def _releases_html():
    rows = _releases_data()
    if not rows:
        return '<div class="empty">暂无发布记录，请等待后台同步或手动刷新。</div>'
    body = []
    for r in rows:
        cached = r["cached_assets"]
        total = r["assets"]
        cache_badge = _badge("已缓存 {}/{}".format(cached, total), "ok" if cached == total else "warn")
        body.append(
            '<tr><td class="mono"><a href="/releases/{}/{}">{}</a></td>'
            '<td>{}</td><td>{}</td><td class="mono">{}</td><td>{}</td><td>{}</td></tr>'.format(
                _esc(r["app"]), _esc(r["version"]), _esc(r["version"]),
                _esc(r["app"]), _channel_badge(r["channel"]),
                _fmt_dt(r["published_at"]), cache_badge, _status_badge(r["status"])))
    return ('<table><thead><tr>'
            '<th class="mono">版本</th><th>应用</th><th>渠道</th>'
            '<th class="mono">发布于</th><th>缓存</th><th>状态</th>'
            '</tr></thead><tbody>{}</tbody></table>').format("".join(body))


def _release_detail_html(app, version):
    d = _release_detail_data(app, version)
    if not d:
        return '<div class="empty">未找到该发布：{} / {}</div>'.format(_esc(app), _esc(version))
    rows = []
    for a in d["assets"]:
        if a["cached"]:
            sha = _esc((a["sha256"] or "")[:16]) + ("…" if a["sha256"] else "")
            cache_b = _badge("已缓存", "ok")
            vrf = _badge("已校验", "ok") if a["sha256"] else _badge("无 SHA", "mut")
        else:
            sha = ""
            cache_b = _badge("缺失", "warn")
            vrf = _badge("—", "mut")
        rows.append(
            '<tr><td class="mono">{}</td><td class="mono">{}</td>'
            '<td>{} {}</td></tr>'
            '<tr><td colspan="4"><span class="sha">SHA256 {}</span></td></tr>'.format(
                _esc(a["name"]), _fmt_bytes(a["size"]), cache_b, vrf,
                sha if sha else "—"))
    return ('<div class="card"><h2>{}</h2>'
            '<div class="row"><span class="k">应用</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">渠道</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">标签</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">发布时间</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">状态</span><span class="v">{}</span></div></div>'
            '<div class="section-title">资产</div>'
            '<table><thead><tr><th class="mono">名称</th><th class="mono">大小</th>'
            '<th>缓存 / 校验</th><th class="mono">SHA256</th></tr></thead>'
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
        cb = _badge("已缓存", "ok") if r["cached"] else _badge("缺失", "warn")
        body.append(
            '<tr><td class="mono">{}</td><td>{}</td><td class="mono">{}</td>'
            '<td class="mono">{}</td><td>{}</td><td class="mono">{}</td></tr>'.format(
                _esc(r["name"]), _esc(r["app"]), _esc(r["version"]),
                _fmt_bytes(r["size"]), cb, _fmt_time(r["cached_at"])))
    refresh = ('<div class="section-title">操作</div>'
               '<form class="rf" method="post" action="/admin/refresh?app={}">'
               '<input type="text" name="admin_token" placeholder="管理员令牌" autocomplete="off">'
               '<button type="submit">刷新</button></form>'
               '<p class="lede" style="margin-top:10px">缓存列展示文件的实时 SHA256（服务端计算），'
               '可在资产清单页查看。刷新需管理员令牌。</p>').format(_esc(DEFAULT_APP_ID))
    return ('<table><thead><tr><th class="mono">名称</th><th>应用</th><th class="mono">版本</th>'
            '<th class="mono">大小</th><th>缓存</th><th class="mono">缓存时间</th></tr></thead>'
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
    gh = _badge("在线", "ok") if (last_sync and (int(time.time()) - last_sync) < SYNC_INTERVAL * 2 + 120) \
        else _badge("未知", "warn")
    admin_on = _badge("已启用", "ok") if ADMIN_TOKEN else _badge("已禁用", "err")
    enroll = _badge(_enroll_mode(), "mut")
    disk_html = ("<div class=\"row\"><span class=\"k\">总量</span><span class=\"v\">{}</span></div>"
                 "<div class=\"row\"><span class=\"k\">已用</span><span class=\"v\">{}</span></div>"
                 "<div class=\"row\"><span class=\"k\">可用</span><span class=\"v\">{}</span></div>").format(
        _fmt_bytes(disk["total"]), _fmt_bytes(disk["used"]), _fmt_bytes(disk["free"])) if disk else \
        '<div class="empty">无法读取磁盘信息</div>'
    return ('<div class="grid two">'
            '<div class="card"><h2>服务</h2>'
            '<div class="row"><span class="k">版本</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">运行时长</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">Python</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">端口</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">应用数</span><span class="v">{}</span></div></div>'
            '<div class="card"><h2>存储</h2>{}</div></div>'
            '<div class="grid two" style="margin-top:16px">'
            '<div class="card"><h2>上游</h2>'
            '<div class="row"><span class="k">GitHub</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">上次同步</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">同步间隔</span><span class="v">{}s</span></div></div>'
            '<div class="card"><h2>安全</h2>'
            '<div class="row"><span class="k">已注册令牌</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">注册策略</span><span class="v">{}</span></div>'
            '<div class="row"><span class="k">管理员刷新</span><span class="v">{}</span></div></div>'
            '</div>').format(
        SERVER_VERSION, _fmt_uptime(st["uptime_sec"]), _esc(sys.version.split()[0]),
        st["port"], st["app_count"], disk_html, gh, _fmt_time(last_sync), SYNC_INTERVAL,
        st["registered_tokens"], enroll, admin_on)


# ---------------------------------------------------------------------------
# HTTP Handler
# ---------------------------------------------------------------------------
class Handler(BaseHTTPRequestHandler):
    server_version = "Cloak-Mirror/1.1"
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

    def _send_redirect(self, loc):
        self._send(302, b"", "text/html", {"Location": loc})

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

    # ---- 登录 / 退出 ----
    def _handle_login_get(self):
        self._send(200, _login_html().encode("utf-8"), "text/html; charset=utf-8")
        self._access_log(200, "login-page")

    def _handle_login_post(self):
        form = self._post_form()
        user = (form.get("username", [None])[0] or "").strip()
        pwd = form.get("password", [None])[0] or ""
        if user == CONSOLE_USER and CONSOLE_PASSWORD and hmac.compare_digest(pwd, CONSOLE_PASSWORD):
            sid = _make_session(user)
            log.info("[auth] 登录成功 user=%s ip=%s", user, _client_ip(self))
            self._send(302, b"", "text/html",
                       {"Location": "/", "Set-Cookie": _session_cookie(sid)})
            return
        log.warning("[auth] 登录失败 user=%s ip=%s", user, _client_ip(self))
        self._send(200, _login_html(error=True).encode("utf-8"), "text/html; charset=utf-8")

    def _handle_logout(self):
        cookie = self.headers.get("Cookie", "")
        sid = None
        for part in cookie.split(";"):
            part = part.strip()
            if part.startswith("mirror_sid="):
                sid = part[len("mirror_sid="):]
        if sid:
            with _session_lock:
                SESSIONS.pop(sid, None)
        self._send_redirect("/login")

    # ---- 控制台渲染（需登录） ----
    def _console(self, active, body, crumb=None):
        crumb = crumb or _CRUMB_CN.get(active, active)
        html = (PAGE_SHELL.replace("__TITLE__", "Cloak 更新镜像")
                .replace("__CSS__", PAGE_CSS)
                .replace("__NAV__", _nav(active))
                .replace("__CRUMB__", crumb)
                .replace("__BODY__", body)
                .replace("__SERVER_VERSION__", SERVER_VERSION)
                .replace("__JS__", PAGE_JS))
        self._send(200, html.encode("utf-8"), "text/html; charset=utf-8")
        self._access_log(200, "console:" + active)

    def _console_guarded(self, active, body, crumb=None):
        if not _get_session(self):
            self._send_redirect("/login")
            return
        self._console(active, body, crumb)

    def _console_dispatch(self, segs):
        kind = segs[0]
        if kind == "overview":
            return self._console("overview", _overview_html())
        if kind == "releases":
            if len(segs) >= 3:
                return self._console("releases", _release_detail_html(segs[1], segs[2]),
                                     crumb="发布 / " + segs[2])
            return self._console("releases", _releases_html())
        if kind == "assets":
            return self._console("assets", _assets_html())
        if kind == "activity":
            return self._console("activity", _activity_html())
        if kind == "system":
            return self._console("system", _system_html())
        return self._console("overview", _overview_html())

    def _api_dispatch(self, rest):
        if not _get_session(self):
            self._send(401, b'{"error":"unauthorized"}\n', "application/json")
            self._access_log(401, "api-auth-fail")
            return
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
        # 登录 / 退出（不受控制台鉴权保护）
        if segs and segs[0] == "login":
            return self._handle_login_get()
        if segs and segs[0] == "logout":
            return self._handle_logout()
        # 控制台（v1.0）—— 需登录
        if not segs or segs[0] == "":
            return self._console_guarded("overview", _overview_html())
        if segs[0] == "status":
            return self._console_guarded("overview", _overview_html(), crumb="概览")
        if segs[0] in ("overview", "releases", "assets", "activity", "system"):
            if not _get_session(self):
                self._send_redirect("/login")
                return
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
            return self._console_guarded("overview", _overview_html())
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
        # 控制台登录提交
        if segs and segs[0] == "login":
            return self._handle_login_post()
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
            add_activity("checkin", "{} 已签到 v{}".format(cid, ver),
                         app=app, version=ver, client_id=cid)
        except Exception as e:
            log.warning("[checkin] 写入失败: %s", e)
        self._send_json({"ok": True, "client_id": cid, "server_time": int(time.time())})


def _login_html(error=False):
    err = '<p class="err">用户名或密码错误</p>' if error else ''
    return """<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>登录 · Cloak 更新镜像</title>
<style>
:root{--bg:#F6F7F9;--panel:#fff;--ink:#0E1117;--muted:#8A93A2;--line:#ECEEF1;--accent:#2F6BFF}
*{box-sizing:border-box}html,body{margin:0;height:100%}
body{background:var(--bg);color:var(--ink);font:14px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif;display:flex;align-items:center;justify-content:center}
.wrap{background:var(--panel);border:1px solid var(--line);border-radius:18px;padding:40px 38px;width:380px;box-shadow:0 10px 40px rgba(16,19,26,.08)}
.logo{width:42px;height:42px;border-radius:12px;background:var(--accent);color:#fff;display:flex;align-items:center;justify-content:center;font-size:20px;margin-bottom:18px;box-shadow:0 6px 18px rgba(47,107,255,.35)}
h1{font-size:20px;margin:0 0 4px;font-weight:800}
.sub{color:var(--muted);font-size:13px;margin:0 0 24px}
.err{color:#E03A3A;font-size:12.5px;margin:0 0 14px;background:rgba(224,58,58,.08);padding:8px 12px;border-radius:8px}
label{display:block;font-size:12px;color:var(--muted);margin:16px 0 7px;font-weight:600;letter-spacing:.02em}
input{width:100%;border:1px solid var(--line);border-radius:10px;padding:11px 13px;font-size:14px;font-family:ui-monospace,Menlo,Consolas,monospace;outline:none;transition:border-color .15s}
input:focus{border-color:var(--accent)}
button{width:100%;margin-top:22px;border:none;background:var(--accent);color:#fff;border-radius:10px;padding:12px;font-size:14px;font-weight:700;cursor:pointer;transition:filter .15s}
button:hover{filter:brightness(1.06)}
</style>
</head>
<body>
<div class="wrap">
  <div class="logo">&#9672;</div>
  <h1>Cloak 更新镜像</h1>
  <p class="sub">控制台登录</p>
  __ERR__
  <form method="post" action="/login">
    <label>用户名</label>
    <input name="username" autocomplete="username" autofocus>
    <label>密码</label>
    <input name="password" type="password" autocomplete="current-password">
    <button type="submit">登录</button>
  </form>
</div>
</body>
</html>""".replace("__ERR__", err)


def main():
    global CONSOLE_PASSWORD
    os.makedirs(CACHE_DIR, exist_ok=True)
    _db_init()
    _load_tokens()
    if not CONSOLE_PASSWORD:
        CONSOLE_PASSWORD = uuid.uuid4().hex[:16]
        log.warning("⚠️ 未设置 MIRROR_CONSOLE_PASSWORD，已自动生成随机密码（重启失效，请尽快在 .env 中固定）：%s",
                    CONSOLE_PASSWORD)
    for aid in APP_ORDER:
        log.info("app %s -> repo %s", aid, APPS[aid].repo)
    threading.Thread(target=_sync_worker, daemon=True).start()
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    log.info("serving %d apps on :%d, default=%s, cache=%s", len(APPS), PORT, DEFAULT_APP_ID, CACHE_DIR)
    log.info("enroll_mode=%s, registered_tokens=%d, github_token=%s, admin_token=%s, sync_interval=%ds",
             _enroll_mode(), len(_valid_tokens),
             "set" if GITHUB_TOKEN else "none", "set" if ADMIN_TOKEN else "NONE(refresh disabled)",
             SYNC_INTERVAL)
    log.info("console_auth user=%s password=%s secure_cookie=%s",
             CONSOLE_USER, "set" if CONSOLE_PASSWORD else "NONE", SECURE_COOKIE)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()


if __name__ == "__main__":
    main()
