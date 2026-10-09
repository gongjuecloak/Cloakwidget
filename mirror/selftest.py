#!/usr/bin/env python3
"""镜像控制台自检：逐个渲染 HTML 页面函数 + 校验渠道推断。

用途：避免 `.format()` 占位符与实参个数不匹配这类只在运行期暴露的错误
（曾两次导致页面 500/502）。

用法：
    python mirror/selftest.py
退出码 0 = 全通过；非 0 = 有失败项。
"""
import os
import sys
import tempfile

# 必须在 import mirror_server 之前设置环境
_TMP = tempfile.mkdtemp(prefix="mirror_selftest_")
os.environ.update({
    "MIRROR_CACHE": _TMP,
    "MIRROR_APPS_JSON": os.path.join(_TMP, "apps.json"),
    "MIRROR_PORT": os.environ.get("MIRROR_SELFTEST_PORT", "18100"),
    "MIRROR_ADMIN_TOKEN": os.environ.get("MIRROR_SELFTEST_ADMIN", "selftest"),
    "MIRROR_CONSOLE_USER": "admin",
    "MIRROR_CONSOLE_PASSWORD": "selftest",
    "MIRROR_SYNC_INTERVAL": "999999",
})
with open(os.environ["MIRROR_APPS_JSON"], "w", encoding="utf-8") as f:
    f.write('{"mes-converter":{"repo":"gongjuecloak/Cloakwidget",'
            '"pubkey":"89ymVon//tfWP9d+KzKZxg3oCBUT+w31nUqZN5LBML0="}}')

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import mirror_server as m  # noqa: E402

m._db_init()

PAGES = [
    ("_overview_html", lambda: m._overview_html()),
    ("_releases_html", lambda: m._releases_html(None)),
    ("_releases_html(beta)", lambda: m._releases_html("beta")),
    ("_releases_html(dev)", lambda: m._releases_html("dev")),
    ("_release_detail_html", lambda: m._release_detail_html("mes-converter", "v9.9.9")),
    ("_assets_html", lambda: m._assets_html()),
    ("_clients_html", lambda: m._clients_html()),
    ("_downloads_html", lambda: m._downloads_html()),
    ("_activity_html", lambda: m._activity_html(None)),
    ("_activity_html(checkin)", lambda: m._activity_html("checkin")),
    ("_system_html", lambda: m._system_html()),
    ("_login_html", lambda: m._login_html()),
    ("_login_html(error)", lambda: m._login_html(error=True)),
    ("_login_html(note)", lambda: m._login_html(error=True, note="还可尝试 3 次")),
    ("_stages_html", lambda: m._stages_html("mes-converter", "v9.9.9")),
    ("_rank_html", lambda: m._rank_html([{"label": "x.exe", "count": 2, "bytes": 10, "pct": 50}])),
]

DATA = [
    ("_overview_data", lambda: m._overview_data()),
    ("_releases_data", lambda: m._releases_data()),
    ("_assets_data", lambda: m._assets_data()),
    ("_activity_data", lambda: m._activity_data()),
    ("_clients_data", lambda: m._clients_data()),
    ("_download_rank", lambda: m._download_rank()),
    ("_disk_alert", lambda: m._disk_alert()),
    ("_disk_alert_html", lambda: m._disk_alert_html()),
    ("_verify_assets", lambda: m._verify_assets()),
    ("_prune_assets", lambda: m._prune_assets(None, 2)),
    ("_release_stage", lambda: m._release_stage("mes-converter", "v9.9.9")),
    ("gather_status", lambda: m.gather_status()),
    ("_enroll_mode", lambda: m._enroll_mode()),
    ("_channel_label", lambda: m._channel_label("beta")),
]

CHANNEL_CASES = [
    ({"tag_name": "v1.2.3"}, "stable"),
    ({"tag_name": "v1.2.3-beta.1"}, "beta"),
    ({"tag_name": "v1.2.3-dev"}, "dev"),
    ({"tag_name": "v1.2.3-rc1"}, "beta"),
    ({"tag_name": "v1.2.3", "name": "Nightly build"}, "dev"),
    ({"tag_name": "v1.2.3", "prerelease": True}, "beta"),
    ({"tag_name": "v1.2.3", "draft": True}, "dev"),
    ({"tag_name": "v1.2.3", "name": "正式发布"}, "stable"),
]

NORMALIZE_CASES = [
    ("stable", "stable"), ("beta", "beta"), ("dev", "dev"),
    ("prerelease", "beta"), ("rc", "beta"), ("alpha", "beta"),
    ("nightly", "dev"), ("edge", "dev"), ("", "stable"), ("垃圾值", "stable"),
]


def run(title, cases, validator):
    failed = 0
    print("=== {} ===".format(title))
    for name, fn in cases:
        try:
            out = fn()
            if not validator(out):
                raise AssertionError("输出不合法: {!r}".format(str(out)[:80]))
            print("  OK   {}".format(name))
        except Exception as e:
            failed += 1
            print("  FAIL {}: {}: {}".format(name, type(e).__name__, e))
    return failed


def main():
    failed = 0
    failed += run("页面渲染", PAGES, lambda o: isinstance(o, str) and len(o) > 10)
    failed += run("数据函数", DATA, lambda o: o is not None)

    print("=== 渠道推断 ===")
    for rel, want in CHANNEL_CASES:
        got = m._channel_of(rel)
        if got == want:
            print("  OK   {} -> {}".format(rel.get("tag_name") or rel, got))
        else:
            failed += 1
            print("  FAIL {} -> {} (期望 {})".format(rel, got, want))

    print("=== 渠道归一化 ===")
    for raw, want in NORMALIZE_CASES:
        got = m._normalize_channel(raw)
        if got == want:
            print("  OK   {!r} -> {}".format(raw, got))
        else:
            failed += 1
            print("  FAIL {!r} -> {} (期望 {})".format(raw, got, want))

    print("\n结果: {} 个失败".format(failed))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())