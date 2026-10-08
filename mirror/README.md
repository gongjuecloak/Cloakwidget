# Cloak Update Mirror

> **Cloak 软件生态的私有发布与更新基础设施** —— 负责 Release 同步、版本管理、资产缓存、完整性验证、客户端分发与更新审计。

GitHub Releases 是源头；Cloak Update Mirror 负责 **Sync（同步）→ Cache（缓存）→ Verify（校验）→ Distribute（分发）→ Audit（审计）**。客户端（Cloakwidget 等）只需连接 Mirror，不必直连 github.com —— 解决「工位机无外网，但更新源在 GitHub」的部署矛盾。

轻量、私有、自托管。纯 Python 标准库实现（含 SQLite），无外部依赖。

---

## 定位

```
                       GitHub Releases
                            │
                            │ Release
                            ▼
                ┌───────────────────────┐
                │   Cloak Update Mirror  │
                │                       │
                │  Sync   Cache  Verify  │
                │  Publish  Audit        │
                └───────────┬───────────┘
                            │
            ┌───────────────┼───────────────┐
            ▼               ▼               ▼
       Cloakwidget      MES Client      Other Apps
```

Mirror 不再只是「给某工具下载 EXE 的中转站」，而是所有自研软件的**统一发布中枢**：

- **Release Metadata** / **version.json** / **EXE** / **SHA256** 集中缓存
- **发布状态机**：discovered → cached → verified → published（控制台可视化）
- **客户端分发 + 审计**：谁、何时、下载了什么，一清二楚

---

## 控制台（v1.0 Console）

访问根路径 `/` 即控制台首页（Overview）。左侧导航六个分区：

| 分区 | 路径 | 内容 |
|------|------|------|
| **Overview** | `/` | 运行状态、最新发布、同步状态、活动统计 |
| **Releases** | `/releases` · `/releases/{app}/{version}` | 全量发布历史 + 单个发布明细（资产、SHA256、缓存/校验状态） |
| **Assets** | `/assets` | 缓存资产视角（文件名 / 大小 / 版本 / 已缓存 / 下载次数），可手动 Refresh |
| **Activity** | `/activity` | 下载、同步、客户端签到等活动日志 |
| **System** | `/system` | 服务、存储、上游、安全配置 |

`/status` 作为兼容入口，同样进入控制台 Overview。

> **Clients** 分区（客户端登记表 + 版本分布 + 更新状态）计划在 v1.2 接入；当前已预留
> `clients` 数据表与 `POST /api/v1/client/checkin` 上报端点，客户端主动上报即可点亮。

### JSON API（`/api/v1/...`）

供前端或外部系统消费，返回结构化 JSON：

- `GET /api/v1/overview` —— 最新发布 / 应用数 / 同步时间 / 缓存大小 / 活动统计
- `GET /api/v1/releases` —— 发布列表（含每版本资产数、已缓存数）
- `GET /api/v1/releases/{app}/{version}` —— 单发布明细（含资产 SHA256）
- `GET /api/v1/assets` —— 缓存资产列表
- `GET /api/v1/activity` —— 活动日志
- 兼容旧接口：`GET /api/status` 仍返回运维状态 JSON

---

## 客户端更新协议（保持不变）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/{app_id}/version.json` | 该应用 latest release 的 manifest（优先新鲜，失败回退缓存） |
| GET | `/{app_id}/{asset}` | 任意发布资产（首次拉取后落盘缓存） |
| POST | `/api/v1/client/checkin` | 客户端主动上报（app / version / os / hostname） |
| GET | `/healthz` | 健康检查 `{"ok":true}`（供探针 / 反代） |
| POST | `/admin/refresh` · `?app={id}` | 强制刷新全部 / 指定应用（需 `MIRROR_ADMIN_TOKEN`） |
| POST | `/{app_id}/admin/refresh` | 按命名空间刷新的等价写法 |

**拉取需令牌**：`?token=` 或请求头 `X-Access-Token`。未知令牌 + enroll 密钥可自注册，成功一次即旋转为新令牌（`X-Next-Token`，一次性），无令牌 → 401。**兼容老客户端**：无 app 前缀的根路径请求（如 `/version.json`）自动落到默认应用。

---

## 快速开始（服务器部署）

```bash
cd /opt/mes-mirror
./deploy.sh            # 首次生成 .env 与 apps.json（样例），提示你填 MIRROR_ADMIN_TOKEN
# 编辑 .env，至少设置：
#   MIRROR_ADMIN_TOKEN=$(openssl rand -hex 24)
./deploy.sh            # 真正构建并拉起
```

- 容器监听 `18080`（宿主）→ `8080`（容器），由反向代理（宝塔 / Cloudflare）暴露公网。
- SQLite 库与缓存同在 `./data`（宿主机挂载），重启不丢。
- 应用注册表 `apps.json` 在宿主机，编辑后重新 `./deploy.sh` 即生效。

### 新增一个应用 / 让另一个工具接入此镜像

1. 编辑 `apps.json`，加一项：
   ```json
   {
     "my-tool": { "repo": "gongjuecloak/MyTool", "pubkey": "<该工具更新清单的 Ed25519 公钥 base64>" }
   }
   ```
2. `./deploy.sh` 重新部署。
3. 该工具客户端把更新基址指向 `https://x.lzplus.top/my-tool` 即可（复用同一套令牌 / 缓存 / 校验机制）。

---

## 配置（`.env`）

| 变量 | 默认 | 说明 |
|------|------|------|
| `MIRROR_APPS_JSON` | `./apps.json` | 应用注册表路径（容器内 `/app/apps.json`，由 compose 挂载） |
| `MIRROR_DEFAULT_APP` | apps.json 第一个 | 根路径 `/version.json` 落到哪个应用 |
| `MIRROR_PORT` | `8080` | 容器内监听端口 |
| `MIRROR_CACHE` | `/data/cache` | 缓存根目录（各应用细分子目录；SQLite `mirror.db` 也在此） |
| `MIRROR_GITHUB_TOKEN` | 空 | 全局 GitHub Token，提 API 速率上限，避免匿名 403/限流 |
| `MIRROR_ADMIN_TOKEN` | 空 | 管理员令牌，`POST /admin/refresh` 必须携带；**留空则永远 401** |
| `MIRROR_ACCESS_TOKEN` | 空 | 客户端 enroll 密钥；不设则需 `MIRROR_ENROLL_OPEN=true` |
| `MIRROR_ENROLL_OPEN` | `false` | 是否开放注册（true 风险高） |
| `MIRROR_SYNC_INTERVAL` | `300` | 后台自动同步周期（秒），把发布元数据写入 SQLite 供控制台展示 |

---

## 信任与安全

- **拉取需令牌**，令牌一次性旋转，降低泄露滥用面。
- **管理员刷新用独立令牌**，与拉取令牌隔离。
- **注册默认关闭**（需密钥），避免任意客户端囤积令牌。
- **日志对 token 脱敏**并轮转（10MB × 5）。
- **签名在客户端验**：Mirror 只搬运、不改写；`version.json` 由 CI 用 Ed25519 私钥签名，客户端内置公钥验签，验签通过才信任清单里的 SHA256。Mirror 不持有私钥。
- **陈旧兜底**：GitHub 不可达时回退磁盘缓存并打 `X-Cache: stale`，绝不无谓 502。

---

## 数据模型（SQLite，`mirror.db`）

| 表 | 作用 |
|----|------|
| `releases` | 各应用发布元数据（版本 / 渠道 / 时间 / 状态机） |
| `assets` | 每个发布的资产清单（文件名 / 大小 / sha256 / 是否已缓存） |
| `downloads` | 下载记录（审计） |
| `activities` | 活动日志（下载 / 同步 / 客户端签到 / 服务事件） |
| `clients` | 客户端登记表（v1.2 Client Management 预留） |

---

## 排障

| 现象 | 排查 |
|------|------|
| `/admin/refresh` 永远 401 | 服务器 `.env` 未设 `MIRROR_ADMIN_TOKEN`，或令牌不一致 |
| 控制台 Releases 空白 | 后台同步尚未成功（GitHub 不可达 / 限流）；手动 `POST /admin/refresh` 触发，或检查 `MIRROR_GITHUB_TOKEN` |
| 客户端拉取 401 | 客户端令牌失效，会以 enroll 密钥自动重新自注册；确认 `MIRROR_ENROLL_OPEN` / `MIRROR_ACCESS_TOKEN` 一致 |
| 缓存异常 | 看 `/assets` 面板；或直接清 `./data/{app_id}/` 重启 |
| 面板显示上游异常 | GitHub 不可达且无磁盘缓存，检查 `MIRROR_GITHUB_TOKEN` 与网络 |

依赖：仅 Python 标准库（`http.server` + `sqlite3`）。
