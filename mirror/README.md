# MES 更新镜像服务（通用更新平台）

一个**轻量、纯标准库**的更新镜像服务：把多个 GitHub 仓库的「最新发布」资产在服务端拉取后按应用缓存，对外提供同名下载。工厂工位机 / 内网机器只需能访问这台服务器（经 Cloudflare 反代），不必直连 github.com。

它不只是给 MES 转换工具用——**这是一个通用的更新平台**：通过 `apps.json` 注册任意多个应用，每个应用指向一个 GitHub 仓库，按 `/{app_id}/...` 命名空间隔离。任何工具只要在自己的更新配置里写上 `app_id` 与镜像基址，就能复用同一套「Ed25519 验签 + 一次性旋转令牌 + 陈旧兜底缓存」机制自助更新。

---

## 架构

```
客户端（各工具）
   │  GET /{app_id}/version.json  （需令牌，401 时以 enroll 密钥自注册并旋转）
   │  GET /{app_id}/{asset}
   ▼
本镜像服务（x.lzplus.top，经 Cloudflare → 127.0.0.1:18080）
   │  按 app_id 找 GitHub 仓库，拉取 latest release 资产
   │  首次落盘缓存；之后命中缓存直接回源（stale-while-down 兜底）
   ▼
GitHub Releases（每个 app 各自的仓库）
```

每个应用拥有**独立的缓存目录**与**独立的发布元数据缓存**，互不干扰。

---

## 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 全局运维面板（HTML，列出所有应用） |
| GET | `/healthz` | 健康检查 JSON `{"ok":true}`（供探针 / 反代探活） |
| GET | `/status` · `/api/status` | 全局运维面板 / 状态 JSON（含各应用审计与缓存） |
| GET | `/{app_id}/status` | 单个应用的运维面板 |
| GET | `/{app_id}/version.json` | 该应用 latest release 里的 `version.json`（优先新鲜，失败回退缓存） |
| GET | `/{app_id}/{asset}` | 该应用任意发布资产（首次拉取后落盘缓存） |
| POST | `/admin/refresh` | 强制刷新**全部**应用（需 `MIRROR_ADMIN_TOKEN`） |
| POST | `/admin/refresh?app={id}` | 只刷新指定应用 |
| POST | `/{app_id}/admin/refresh` | 等价写法（按命名空间） |

**兼容老客户端**：未带 app 前缀的根路径请求（如 `/version.json`）会自动落到**默认应用**（`apps.json` 第一个，或 `MIRROR_DEFAULT_APP` 指定），老客户端无需升级即可继续更新。

拉取类请求（`/{app_id}/...`）需要基础鉴权：`?token=` 或请求头 `X-Access-Token`。未知令牌 + enroll 密钥可自注册，成功一次即旋转为新令牌（`X-Next-Token`），相当于一次性令牌。无令牌 → 401。

---

## 快速开始（服务器部署）

```bash
cd /opt/mes-mirror
./deploy.sh            # 首次会生成 .env 与 apps.json（样例），提示你填 MIRROR_ADMIN_TOKEN
# 编辑 .env，至少设置：
#   MIRROR_ADMIN_TOKEN=$(openssl rand -hex 24)
./deploy.sh            # 真正构建并拉起
```

- 容器监听 `18080`（宿主）→ `8080`（容器），由反向代理（宝塔 / Cloudflare）暴露到公网。
- 缓存落在 `./data`（宿主机），重启不丢。
- 应用注册表 `apps.json` 同样在宿主机，编辑后重新 `./deploy.sh` 即生效。

### 新增一个应用

1. 编辑 `apps.json`，加一项：
   ```json
   {
     "my-tool": { "repo": "gongjuecloak/MyTool", "pubkey": "<该工具更新清单的 Ed25519 公钥 base64>" }
   }
   ```
2. 重新部署：`./deploy.sh`。
3. 该工具的客户端把更新基址指向 `https://x.lzplus.top/my-tool` 即可。

---

## 配置（.env，均来自环境变量；敏感项务必 gitignore）

| 变量 | 默认 | 说明 |
|------|------|------|
| `MIRROR_APPS_JSON` | `./apps.json` | 应用注册表路径（容器内 `/app/apps.json`，由 compose 挂载） |
| `MIRROR_DEFAULT_APP` | apps.json 第一个 | 根路径 `/version.json` 落到哪个应用 |
| `MIRROR_PORT` | `8080` | 容器内监听端口 |
| `MIRROR_CACHE` | `/data/cache` | 缓存根目录（各应用再细分子目录） |
| `MIRROR_GITHUB_TOKEN` | 空 | 全局 GitHub Token，提 API 速率上限，避免匿名 403/限流 |
| `MIRROR_ADMIN_TOKEN` | 空 | 管理员令牌，`POST /admin/refresh` 必须携带；**留空则永远 401** |
| `MIRROR_ACCESS_TOKEN` | 空 | 客户端 enroll 密钥；不设则需 `MIRROR_ENROLL_OPEN=true` 才能注册 |
| `MIRROR_ENROLL_OPEN` | `false` | 是否开放注册（true=任何客户端可自注册，风险高） |

---

## 信任与安全

- **拉取需令牌**，令牌一次性旋转，降低泄露后的滥用面。
- **管理员刷新需独立令牌**，与拉取令牌隔离。
- **注册默认关闭**（需密钥），避免任意客户端囤积令牌。
- **日志对 token 值脱敏**，且按大小轮转（10MB×5），不会无限增长。
- **签名在客户端验**：镜像只搬运、不改写；`version.json` 由 CI 用 Ed25519 私钥签名，客户端内置公钥验签，验签通过才信任清单里的 SHA256。镜像本身不持有私钥。
- **陈旧兜底**：GitHub 不可达时回退磁盘缓存并打 `X-Cache: stale`，绝不无谓 502。

---

## 排障

| 现象 | 排查 |
|------|------|
| `/admin/refresh` 永远 401 | 服务器 `.env` 没设 `MIRROR_ADMIN_TOKEN`，或 GitHub secret 与之一致性不对 |
| 镜像拿不到新包 | 发版后 CI 会 `POST /admin/refresh` 主动同步；也可手动 `curl -X POST ".../admin/refresh?admin_token=..."` |
| 客户端拉取 401 | 客户端令牌失效，会以 enroll 密钥自动重新自注册；确认 `MIRROR_ENROLL_OPEN` 或 `MIRROR_ACCESS_TOKEN` 与服务端一致 |
| 缓存异常 | 看 `/status` 面板各应用的「缓存资产」；或直接清 `./data/{app_id}/` 重启 |
| 面板显示「上游异常」 | GitHub 不可达且无磁盘缓存，检查 `MIRROR_GITHUB_TOKEN` 与网络 |

依赖：仅 Python 标准库。
