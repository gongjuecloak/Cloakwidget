#!/usr/bin/env python3
"""生成 version.json 更新清单（并在有私钥时签名 version.sig）。

用法：
  python gen_version.py --exe "物料档案转换工具.exe" \
      --name "MES-Converter-v1.10.4.exe" --ver 1.10.4 --out version.json

说明：
  - exe 的 sha256 / size 直接取自 --exe 指向的真实构建产物（务必先 build）。
  - version.json 的 exe.name 用「发布资产名」（ASCII，如 MES-Converter-vX.Y.Z.exe），
    与 GitHub Release / 镜像上的资产名保持一致；exe.url 用 GitHub 下载地址兜底。
  - 若提供 --key（Ed25519 私钥 PEM/原始字节路径）或环境变量 MES_SIGN_PRIVATE_KEY，
    则对 version.json 签名为 version.sig；否则只生成未签名清单
    （客户端默认 MES_UPDATE_REQUIRE_SIG 未开启时放行，仅记警告）。
"""
import argparse
import hashlib
import json
import os
import re
import sys

REPO = "gongjuecloak/Cloakwidget"


def parse_version_from_go(path):
    txt = open(path, encoding="utf-8").read()
    m = re.search(r'const appVersion\s*=\s*"([^"]+)"', txt)
    return m.group(1) if m else None


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def sign_ed25519(data: bytes, key_path: str) -> bytes:
    """用 Ed25519 私钥（PEM 或原始 64 字节）对 data 签名。返回 64 字节签名。"""
    from cryptography.hazmat.primitives.serialization import load_pem_private_key
    raw = open(key_path, "rb").read().strip()
    try:
        key = load_pem_private_key(raw, password=None)
    except Exception:
        # 当作原始 64 字节种子
        key = None
    if key is None:
        if len(raw) == 64:
            import ed25519
            return bytes(ed25519.SigningKey(raw).sign(data))
        raise RuntimeError("无法解析私钥（需 PEM 或 64 字节种子）")
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    if not isinstance(key, Ed25519PrivateKey):
        raise RuntimeError("私钥不是 Ed25519 类型")
    return key.sign(data)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--exe", required=True, help="真实构建产物 exe 路径（用于算 sha256/size）")
    ap.add_argument("--name", required=True, help="发布资产名（ASCII，如 MES-Converter-v1.10.4.exe）")
    ap.add_argument("--ver", default=None, help="版本号，缺省从 mes_conv/version.go 读取")
    ap.add_argument("--repo", default=REPO)
    ap.add_argument("--out", default="version.json")
    ap.add_argument("--key", default=None, help="Ed25519 私钥路径（PEM 或 64 字节种子）；也可用环境变量 MES_SIGN_PRIVATE_KEY")
    args = ap.parse_args()

    if not os.path.exists(args.exe):
        sys.exit("找不到 exe：{}".format(args.exe))
    ver = args.ver or parse_version_from_go(os.path.join("mes_conv", "version.go"))
    if not ver:
        sys.exit("无法解析版本号（用 --ver 指定）")

    digest = sha256_file(args.exe)
    size = os.path.getsize(args.exe)
    url = "https://github.com/{}/releases/download/v{}/{}".format(args.repo, ver, args.name)
    manifest = {
        "version": ver,
        "exe": {
            "name": args.name,
            "url": url,
            "sha256": digest,
            "size": size,
        },
    }
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print("已生成 {}: version={} sha256={} size={}".format(args.out, ver, digest, size))

    key_path = args.key or os.environ.get("MES_SIGN_PRIVATE_KEY")
    if key_path and os.path.exists(key_path):
        try:
            sig = sign_ed25519(open(args.out, "rb").read(), key_path)
            with open("version.sig", "wb") as f:
                f.write(sig)
            print("已签名 version.sig ({} 字节)".format(len(sig)))
        except Exception as e:
            print("签名失败（将保留未签名清单）：{}".format(e))
    else:
        # 无私钥：删除可能存在的旧签名，避免客户端验签失败
        if os.path.exists("version.sig"):
            os.remove("version.sig")
            print("未提供私钥：已删除旧 version.sig（清单将以未签名方式被客户端接受）")


if __name__ == "__main__":
    main()
