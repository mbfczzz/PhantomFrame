# PhantomFrame 镜像部署

与 `canvas/deploy/docker-compose.yml` 一样，生产服务使用 Linux host 网络，连接宿主机已有的 MySQL 和 Redis。Nginx 安装在宿主机，负责域名与 HTTPS。应用镜像中只有 Go 后端、React 构建产物和媒体处理工具，不包含 Nginx、MySQL 或 Redis。

```text
https://phantomframe.tcmzhan.com
             |
       宿主机 Nginx :443
             |
   PhantomFrame 容器 127.0.0.1:3001
             |
   宿主机 MySQL :3306 / Redis :6379
```

## 镜像

镜像地址：`ghcr.io/mbfczzz/phantomframe:latest`（Linux amd64）。推送 `main` 或手动运行 GitHub Actions 的 `docker-images` 工作流后，先运行真实 MySQL / Redis 集成测试，再构建镜像，验证生产启动、经过独立 Nginx 的 HTTPS 访问、前端资源、登录、API Key，以及重建容器后的素材和会话持久化，然后推送到 GHCR。也提供 `sha-<完整提交 SHA>` 标签，方便固定版本和回滚。

镜像不包含数据库密码、管理员密码、加密密钥或 OSS AccessKey。这些配置在运行时读取。

## 首次部署

服务器需要已有 Docker Engine、Compose 插件、MySQL、Redis 和 Nginx。将本目录的 `docker-compose.yml`、`.env` 和 Nginx 配置复制到服务器，例如 `/opt/phantomframe/`。服务器只需要这些部署文件，无须安装 Go、Node.js 或复制项目源码。

尚无 `.env` 时，复制 `.env.example` 并填写空值：

```bash
cd /opt/phantomframe
cp .env.example .env
chmod 600 .env
```

关键配置：

```dotenv
APP_URL=https://phantomframe.tcmzhan.com
HOST=127.0.0.1
PORT=3001
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_DATABASE=canvas
MYSQL_USER=root
MYSQL_PASSWORD=填写实际密码
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=填写实际密码
REDIS_DB=0
REDIS_PREFIX=phantomframe:
ENCRYPTION_KEY=填写固定的64位十六进制密钥
ADMIN_USERNAME=admin
ADMIN_PASSWORD=填写初始管理员密码
STORAGE_DRIVER=oss
OSS_BUCKET=flowlinght
OSS_PREFIX=phantomframe/uploads/
```

新部署可以用 `openssl rand -hex 32` 生成 `ENCRYPTION_KEY`。此密钥须与数据库一起备份并长期保持不变；如使用已有 PhantomFrame 数据，继续使用原来的密钥。

`MYSQL_DATABASE` 指定的数据库须已存在，并且专属于 PhantomFrame。参考项目 Canvas 的生产配置同样使用 `canvas` 库，且两项目的 `users`、`projects` 表结构不同；如果连接同一 MySQL 实例，应为 PhantomFrame 新建独立数据库（例如 `phantomframe`），再修改 `.env` 的 `MYSQL_DATABASE`。只有这个 `canvas` 库专属于 PhantomFrame 时，才能沿用上面的库名。启动时应用先检查同名表是否兼容，不兼容时在执行迁移前停止；兼容时自动创建和迁移业务表，不会自动创建数据库。

OSS AccessKey 可以在 `.env` 填写，或启动后在「管理后台 → 站点设置 → 生成结果存储」填写。后台已保存的存储配置优先，已有配置需同步设置文件前缀为 `phantomframe/uploads/`。

运行：

```bash
cd /opt/phantomframe
docker compose pull
docker compose up -d
docker compose ps
curl -fsS http://127.0.0.1:3001/api/health
```

如果 GHCR 返回权限错误，需要先用具有 `read:packages` 权限的 GitHub Token 登录，再拉取：

```bash
docker login ghcr.io -u mbfczzz
```

`ADMIN_PASSWORD` 仅用于首次创建管理员，不会在重启时重置已有管理员密码。首次启动后，MySQL / Redis 正常时健康检查返回 `status: ok`；OSS 未配置完整时，管理界面仍可使用，生成提交会提示补齐存储配置。

## 不使用 Compose，直接运行镜像

`.env` 中保留模板里的 `APP_ENV=production`、`HOST=127.0.0.1`、`DATA_DIR=/app/data`、`FRONTEND_DIR=/app/dist`、`COOKIE_SECURE=true` 等配置，再运行：

```bash
docker pull ghcr.io/mbfczzz/phantomframe:latest
docker run -d \
  --name phantomframe \
  --restart unless-stopped \
  --network host \
  --env-file /opt/phantomframe/.env \
  --mount type=volume,source=phantomframe-data,target=/app/data \
  --stop-timeout 30 \
  ghcr.io/mbfczzz/phantomframe:latest
```

Compose 和 `docker run` 任选一种。Linux host 网络可访问宿主机的 `127.0.0.1:3306/6379`，无需 `-p` 映射。应用仅监听 `127.0.0.1:3001`，供宿主机 Nginx 反代。`phantomframe-data` 保存本地素材及缓存；余额、用户、任务等业务数据保存在已有 MySQL，登录会话保存在 Redis。

## 宿主机 Nginx

域名 A / AAAA 记录应指向此服务器，且服务器开放 80、443。申请 `phantomframe.tcmzhan.com` 的证书后，将 `nginx/phantomframe.tcmzhan.com.conf` 安装到 `/etc/nginx/conf.d/phantomframe.tcmzhan.com.conf`，修改其中的证书和私钥路径为服务器实际位置。证书与 Nginx 都不放入应用容器。

证书的 SAN 必须包含 `phantomframe.tcmzhan.com` 或有效的 `*.tcmzhan.com` 通配符。其他子域名单独签发的证书不能直接复用。CI 使用临时测试证书验证代理请求链路，不能代替生产服务器的域名证书验证。

```bash
sudo cp nginx/phantomframe.tcmzhan.com.conf /etc/nginx/conf.d/phantomframe.tcmzhan.com.conf
sudo nginx -t && sudo systemctl reload nginx
curl -fsS https://phantomframe.tcmzhan.com/api/health
```

配置将前端页面和 `/api/` 都反代到 `http://127.0.0.1:3001`，由 Go 服务处理静态文件和前端路由回退。如果修改应用端口，须同步修改 Nginx 中的两个 `proxy_pass`。

## 更新与排查

```bash
cd /opt/phantomframe
docker compose pull
docker compose up -d
docker compose logs --tail=100 phantomframe
```

在 `.env` 中设置 `PHANTOMFRAME_IMAGE=ghcr.io/mbfczzz/phantomframe:sha-<完整提交 SHA>` 可固定版本。更新时保留 `.env`、`ENCRYPTION_KEY` 和数据卷，勿执行 `docker compose down -v`。

项目根目录的 `compose.yaml` 仍是包含独立 MySQL / Redis 容器的完整示例；使用服务器现有数据库时，采用本目录的 `docker-compose.yml`。
