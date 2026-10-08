#!/usr/bin/env bash
# MES 更新镜像服务 · 一键部署
# 用法： ./deploy.sh
set -euo pipefail
cd "$(dirname "$0")"

# 1) 缺失 .env 时从样例生成，并提示必填项后退出，避免带着空令牌启动
if [ ! -f .env ]; then
  cp .env.example .env
  echo "已生成 .env（样例）。请编辑它，至少设置 MIRROR_ADMIN_TOKEN："
  echo "  openssl rand -hex 24"
  echo "然后重新运行 ./deploy.sh"
  exit 0
fi

# 1.5) 应用注册表 apps.json：不存在则从样例生成（多应用通用平台的核心配置）。
# 注意：compose 把 ./apps.json 挂进容器；若宿主不存在该文件，Docker 会误建目录覆盖，
# 导致服务端读不到注册表而回退内置默认（仅 mes-converter）。显式从这里生成更稳妥。
if [ ! -f apps.json ]; then
  cp apps.json.example apps.json
  echo "已生成 apps.json（默认含 mes-converter -> gongjuecloak/Cloakwidget）。"
  echo "要新增应用，编辑 apps.json 加一个 app_id -> {repo} 即可，然后重新部署。"
fi

# 2) 简单校验：admin 令牌不能为空，否则刷新功能完全不可用
if ! grep -qE '^MIRROR_ADMIN_TOKEN=.+' .env; then
  echo "警告：.env 中 MIRROR_ADMIN_TOKEN 为空，POST /admin/refresh 将永远 401。"
  echo "建议设置： openssl rand -hex 24  ->  写入 MIRROR_ADMIN_TOKEN="
fi

# 2.5) 控制台登录密码：若未设置则随机生成并写入 .env，避免控制台无密码裸奔
if ! grep -qE '^MIRROR_CONSOLE_PASSWORD=.+' .env; then
  GEN=$(openssl rand -base64 15 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c 20)
  if [ -n "$GEN" ]; then
    printf 'MIRROR_CONSOLE_PASSWORD=%s\n' "$GEN" >> .env
    echo "已为控制台自动生成登录密码（请妥善保存，可改 .env 固定）： $GEN"
  else
    echo "警告：未能生成控制台密码，控制台将以随机密码启动（重启后失效）。"
  fi
fi

# 2.6) 默认开启 Secure Cookie（HTTPS 前置场景）
grep -qE '^MIRROR_SECURE_COOKIE=' .env || printf 'MIRROR_SECURE_COOKIE=1\n' >> .env

# 3) 构建并拉起（--build 确保代码更新生效）
docker compose up -d --build

# 4) 等待健康检查就绪后展示状态
sleep 3
docker compose ps
echo
echo "探活："
curl -s -m 5 http://127.0.0.1:18080/healthz || echo "(本地探活失败，请检查容器日志： docker compose logs --tail 50)"
