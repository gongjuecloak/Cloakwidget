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

## 控制台（Console）

访问根路径 `/` 即控制台首页（Overview）。左侧导航七个分区：

| 分区 | 路径 | 内容 |
|------|------|------|
| **概览** | `/` · `/overview` | 运行状态、最新发布、同步状态、活动统计、磁盘告警条 |
| **发布** | `/releases` · `/releases/{app}/{version}` | 全量发布历史（可按渠道筛选）+ 单发布明细（发布状态机进度、资产、SHA256、校验状态） |
| **资产** | `/assets` | 缓存资产视角 + 最近校验结果 + 「校验完整性」/「清理旧版本」操作 |
| **客户端** | `/clients` | 已登记客户端、活跃数、版本落后数、版本分布、每台机器的更新状态 |
| **下载排行** | `/downloads` | 按应用 / 按资产下载排行（带进度条）+ 响应状态分布 |
| **活动** | `/activity` | 活动日志，可按类型筛选（下载 / 同步 / 签到 / 校验 / 安全 / 登录 / 清理） |
| **系统** | `/system` | 服务、存储、上游、安全（含 24h 登录审计与锁定 IP）、渠道分布 |

`/status` 作为兼容入口，同样进入控制台 Overview。

> **控制台需登录**：概览 / 发布 / 资产 / 客户端 / 下载排行 / 活动 / 系统 等页面及对应 `/api/v1/*` 接口默认**不公开**，
> 未登录访问会跳转到 `/login`。请用 `.env` 中的 `MIRROR_CONSOLE_USER` / `MIRROR_CONSOLE_PASSWORD`
> 登录；未设置密码时服务启动会随机生成（重启失效）。客户端更新协议（`/{app}/version.json`、资产下载、
> enroll、admin 刷新、healthz）的鉴权方式不变，不受控制台登录影响。

### JSON API（`/api/v1/...`）

供前端或外部系统消费，返回结构化 JSON：

- `GET /api/v1/overview` —— 最新发布 / 应用数 / 同步时间 / 缓存大小 / 活动统计 / 磁盘告警
- `GET /api/v1/releases[?channel=beta]` —— 发布列表（含每版本资产数、已缓存数、已校验数）
- `GET /api/v1/releases/{app}/{version}` —— 单发布明细（含 `state_machine` 状态机）
- `GET /api/v1/assets` —— 缓存资产列表
- `GET /api/v1/clients` —— 客户端列表 + 版本分布 + 活跃/落后统计
- `GET /api/v1/downloads[?days=7]` —— 按应用 / 按资产下载排行
- `GET /api/v1/activity[?type=download]` —— 活动日志（可按类型筛选）
- 兼容旧接口：`GET /api/status` 仍返回运维状态 JSON

### 运维自检

```
python mirror/selftest.py     # 逐个渲染所有页面 + 校验渠道推断，退出码 0 = 全通过
```

用于在部署前发现 `.format()` 占位符与实参不匹配这类**只在运行期暴露**的错误
（曾导致页面 500/502）。新增页面后请一并加进 `selftest.py` 的 `PAGES` 列表。

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
| `MIRROR_CONSOLE_USER` | `admin` | 控制台登录用户名 |
| `MIRROR_CONSOLE_PASSWORD` | 空（随机生成） | 控制台登录密码；**建议设置强密码**，留空则启动随机生成（重启失效） |
| `MIRROR_SESSION_TTL` | `28800` | 登录会话有效期（秒）= 8 小时 |
| `MIRROR_SECURE_COOKIE` | `0` | 设为 `1` 时给会话 Cookie 加 `Secure` 标记（HTTPS 前置部署建议开启） |
| `MIRROR_LOGIN_MAX_FAILS` | `5` | 同 IP 在窗口内允许的登录失败次数，超出即锁定 |
| `MIRROR_LOGIN_WINDOW` | `600` | 登录失败统计窗口（秒） |
| `MIRROR_LOGIN_LOCKOUT` | `900` | 触发后锁定时长（秒），期间正确密码也拒绝 |
| `MIRROR_DISK_WARN_BYTES` | `3GB` | 缓存占用超此值时控制台显示黄色告警 |
| `MIRROR_DISK_CRIT_BYTES` | `8GB` | 缓存占用超此值显示红色严重告警 |
| `MIRROR_DEFAULT_CHANNEL` | `stable` | 客户端未指定渠道时镜像返回的默认渠道 |

---

## 信任与安全

- **拉取需令牌**，令牌一次性旋转，降低泄露滥用面。
- **令牌走标准 `Authorization: Bearer` 头**（不再塞进 URL，避免令牌进日志 / 浏览器历史 / Referer）；服务端仍兼容旧的 `?token=` 与 `X-Access-Token`，老客户端不受影响。
- **管理员操作用独立令牌**（`X-Admin-Token` / `?admin_token=`），与拉取令牌隔离。
- **登录防爆破**：同 IP 在 `MIRROR_LOGIN_WINDOW` 内失败达 `MIRROR_LOGIN_MAX_FAILS` 次即锁定 `MIRROR_LOGIN_LOCKOUT` 秒，返回 `429` + `Retry-After`；所有登录成功 / 失败写入 `login_attempts` 表并在「活动 → 安全」可查。
- **注册默认关闭**（需密钥），避免任意客户端囤积令牌。
- **日志对 token 脱敏**并轮转（10MB × 5）。
- **签名在客户端验**：Mirror 只搬运、不改写；`version.json` 由 CI 用 Ed25519 私钥签名，客户端内置公钥验签，验签通过才信任清单里的 SHA256。Mirror 不持有私钥。
- **陈旧兜底**：GitHub 不可达时回退磁盘缓存并打 `X-Cache: stale`，绝不无谓 502。

---

## 渠道（Channel）

镜像按 GitHub tag / release 名自动推断渠道，无需手动配置：

| 关键词（tag 或 release 名，含 nightly/edge） | 渠道 |
|---------------------------------------------|------|
| 无关键词、非 prerelease、非 draft | `stable` 稳定版 |
| `beta` / `alpha` / `rc` / `preview` / `prerelease=true` | `beta` 测试版 |
| `nightly` / `edge` / `-dev` / `draft=true` | `dev` 开发版 |

- 客户端请求 `GET /{app_id}/version.json?channel=beta` 即取该渠道最新版；也可用 `X-Update-Channel` 头。
- 客户端侧用环境变量 `MES_UPDATE_CHANNEL=stable|beta|dev` 切换（默认 `stable`，生产工位机保持 stable，测试机可设 beta）。
- 控制台「发布」页可按渠道筛选；「系统」页展示各渠道发布数量。

---

## 发布状态机

每个发布按四段进度展示（控制台「发布 → 详情」）：

```
已发现 → 已缓存 → 已校验 → 已发布
```

- **已发现**：同步到该发布及其资产清单。
- **已缓存**：全部资产文件已落盘本地。
- **已校验**：`POST /admin/verify` 重算缓存文件 SHA256 并与清单比对通过。
- **已发布**：`version.json` 清单也已缓存，可对外分发。

---

## 资产完整性校验

```
POST /admin/verify        # 校验全部应用
POST /admin/verify?app=mes-converter
```
（需管理员令牌）重算每个已缓存资产的 SHA256，与 Release 清单比对，结果写入
`asset_checks` 表并更新 `assets.verified_at` / `state`；控制台「资产」页可看到
最近校验结果（通过 / 不符 / 缺失）。

## 磁盘预警与清理

- 缓存占用超 `MIRROR_DISK_WARN_BYTES` → 概览页 / 系统页显示黄色告警条；超 `MIRROR_DISK_CRIT_BYTES` → 红色严重告警；磁盘剩余 ≤10% 也会告警。
- `POST /admin/prune`（表单参数 `keep=N`，默认 2）删除每个应用最近 N 个稳定版之外的已缓存资产，返回释放空间。控制台「资产」页底部有对应按钮。

---

## 客户端管理与签到

```
POST /api/v1/client/checkin
{"client_id":"MES-PC-001","app":"mes-converter","version":"1.10.3",
 "channel":"stable","os":"windows","hostname":"MES-PC-001"}
```

客户端启动 / 手动检查更新时自动上报（best-effort，失败不影响使用）。控制台
「客户端」页展示：已登记数、15 分钟内活跃数、版本落后数、版本分布，以及每台
机器的「已是最新 / 可更新至 X」。`MES_CLIENT_ID` 可显式指定客户端标识，默认用
`主机名-用户名`。

---

## 数据模型（SQLite，`mirror.db`）

| 表 | 作用 |
|----|------|
| `releases` | 各应用发布元数据（版本 / 渠道 / 时间 / 状态） |
| `assets` | 每个发布的资产清单（文件名 / 大小 / sha256 / 是否已缓存 / 校验时间 / 状态） |
| `downloads` | 下载记录（审计；支撑按应用 / 按资产排行） |
| `activities` | 活动日志（下载 / 同步 / 签到 / 校验 / 清理 / 安全 / 登录） |
| `clients` | 客户端登记表（版本 / 渠道 / 主机名 / 最近活动） |
| `login_attempts` | 登录审计（IP / 用户名 / 成功与否 / 时间） |
| `asset_checks` | 资产完整性校验结果（期望 / 实际 SHA256 / 结论 / 时间） |

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
