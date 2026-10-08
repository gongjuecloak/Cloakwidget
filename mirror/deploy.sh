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

# 3) 构建并拉起（--build 确保代码更新生效）
docker compose up -d --build

# 4) 等待健康检查就绪后展示状态
sleep 3
docker compose ps
echo
echo "探活："
curl -s -m 5 http://127.0.0.1:18080/healthz || echo "(本地探活失败，请检查容器日志： docker compose logs --tail 50)"
