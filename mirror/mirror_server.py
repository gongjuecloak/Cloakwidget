#!/usr/bin/env python3
"""
MES 转换工具 · 更新镜像服务（轻量版）

做什么：
  把 GitHub Releases 的「最新发布」资产，在服务端拉取后缓存，对外提供同名下载。
  工厂工位机只需能访问这台服务器（经 Cloudflare），不必直连 github.com——
  解决「工位机无外网，但更新源在 GitHub」的部署矛盾。

  客户端请求：
    GET /version.json            -> latest release 里的 version.json（始终新鲜，不缓存）
    GET /MES-Converter-vX.Y.Z.exe -> 对应资产（首次拉取后落盘缓存，之后直接回源）
    GET /healthz                 -> 健康检查

  安全：本服务只是把 GitHub 上的公开资产搬过来，不做任何改写。客户端自己会校验
  version.json 里的 SHA256，镜像被投毒也骗不过校验。

依赖：仅 Python 标准库。
"""
import os
import sys
import json
import time
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REPO = os.environ.get("MIRROR_REPO", "gongjuecloak/Cloakwidget")
PORT = int(os.environ.get("MIRROR_PORT", "8080"))
CACHE_DIR = os.environ.get("MIRROR_CACHE", "/data/cache")
LATEST_API = f"https://api.github.com/repos/{REPO}/releaseS/latest".replace("releaseS", "releases")
UA = "MES-Mirror/1.0 (+https://github.com/gongjuecloak/Cloakwidget)"
CACHE_TTL = 60  # 最新发布元数据的内存缓存秒数（避免每次请求都打 GitHub API）

_latest_cache = {"ts": 0.0, "data": None}


def _http_get_bytes(url: str, timeout: int = 60) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "*/*"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read()


def _http_get_json(url: str, timeout: int = 30):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def get_latest_release():
    now = time.time()
    if _latest_cache["data"] and now - _latest_cache["ts"] < CACHE_TTL:
        return _latest_cache["data"]
    data = _http_get_json(LATEST_API)
    _latest_cache["ts"] = now
    _latest_cache["data"] = data
    return data


def find_asset(rel, name: str):
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
    if name.endswith(".exe"):
        return "application/octet-stream"
    return "application/octet-stream"


class Handler(BaseHTTPRequestHandler):
    server_version = "MES-Mirror/1.0"
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):  # 静默默认访问日志，避免刷屏
        pass

    def _send(self, code: int, body: bytes, ctype: str):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def do_HEAD(self):
        self.do_GET()

    def do_GET(self):
        path = self.path.split("?", 1)[0].lstrip("/")
        if path == "" or path == "healthz":
            self._send(200, b'{"ok":true}\n', "application/json")
            return
        try:
            rel = get_latest_release()
        except Exception as e:
            self._send(502, ("上游 GitHub 不可达: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return

        asset = find_asset(rel, path)
        if not asset:
            self._send(404, ("找不到资产: " + path).encode("utf-8"), "text/plain; charset=utf-8")
            return

        # version.json 始终新鲜（它是「最新版」的指针，不能缓存）
        if path == "version.json":
            try:
                body = _http_get_bytes(asset["browser_download_url"])
            except Exception as e:
                self._send(502, ("拉取 version.json 失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
                return
            self._send(200, body, guess_ct(path))
            return

        # 其它资产：落盘缓存，命中直接回源
        cp = cache_path(path)
        if os.path.exists(cp):
            try:
                with open(cp, "rb") as f:
                    body = f.read()
                self._send(200, body, guess_ct(path))
                return
            except Exception:
                pass  # 缓存读坏就重新拉
        try:
            body = _http_get_bytes(asset["browser_download_url"])
        except Exception as e:
            self._send(502, ("拉取资产失败: " + str(e)).encode("utf-8"), "text/plain; charset=utf-8")
            return
        tmp = cp + ".tmp"
        try:
            with open(tmp, "wb") as f:
                f.write(body)
            os.replace(tmp, cp)
        except Exception:
            pass
        self._send(200, body, guess_ct(path))


def main():
    os.makedirs(CACHE_DIR, exist_ok=True)
    srv = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print(f"[mirror] serving repo={REPO} on :{PORT}, cache={CACHE_DIR}", flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        srv.shutdown()


if __name__ == "__main__":
    main()
