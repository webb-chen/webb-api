# 宝塔面板部署教程

本文档提供使用宝塔面板 Docker 功能部署 Webb API 的图文教程。

> 📖 官方文档：[宝塔面板部署](https://docs.newapi.pro/zh/docs/installation/deployment-methods/bt-docker-installation)

***

## 前置要求

| 项目    | 要求                                 |
| ----- | ---------------------------------- |
| 宝塔面板  | ≥ 9.2.0 版本                         |
| 推荐系统  | CentOS 7+、Ubuntu 18.04+、Debian 10+ |
| 服务器配置 | 至少 1 核 2G 内存                       |

***

## 步骤一：安装宝塔面板

1. 前往 [宝塔面板官网](https://www.bt.cn/new/download.html) 下载适合您系统的安装脚本
2. 运行安装脚本安装宝塔面板
3. 安装完成后，使用提供的地址、用户名和密码登录宝塔面板

***

## 步骤二：安装 Docker

1. 登录宝塔面板后，在左侧菜单栏找到并点击 **Docker**
2. 首次进入会提示安装 Docker 服务，点击 **立即安装**
3. 按照提示完成 Docker 服务的安装

***

## 步骤三：安装 Webb API

### 方法一：使用宝塔应用商店（推荐）

1. 在宝塔面板 Docker 功能中，点击 **应用商店**
2. 搜索并找到 **New-API**
3. 点击 **安装**
4. 配置以下基本选项：
   - **容器名称**：可自定义，默认为 `webb-api`
   - **端口映射**：默认为 `3000:3000`
   - **环境变量**：
     - `SESSION_SECRET`：会话密钥（**必填**，多机部署时必须一致）
     - `CRYPTO_SECRET`：加密密钥（使用 Redis 时必填）
5. 点击 **确认** 开始安装
6. 等待安装完成后，访问 `http://您的服务器IP:3000` 即可使用

### 方法二：使用 Docker Compose（源码本地构建）

> ⚠️ 本项目**未发布公开容器镜像**，请勿使用 `image: webb-chen/webb-api:latest` 这类写法。该镜像不存在，且不在国内镜像加速服务的白名单内，拉取会直接失败（报错：`this image is not in the allowlist`）。
> 正确方式是拉取源码后在服务器上本地构建。

1. 在宝塔面板中创建网站目录，如 `/www/wwwroot/webb-api`
2. 在终端中拉取源码：

```bash
cd /www/wwwroot
git clone https://github.com/webb-chen/webb-api.git
cd webb-api
```

3. 项目根目录已内置 `docker-compose.yml`（包含应用 + PostgreSQL + Redis 三个服务）。**部署前请先修改其中的默认密码**，然后启动：

```bash
# 应用镜像由本地 Dockerfile 构建，不经过任何镜像仓库
# 仅 redis / postgres 需要从 Docker Hub 拉取，二者均为官方镜像，国内加速可用
docker compose up -d --build
```

4. 首次构建需编译前端与后端，耗时数分钟。等待构建完成后访问 `http://您的服务器IP:3000`

> 💡 `SESSION_SECRET` 在多机部署时必填且需保持一致。可在 `docker-compose.yml` 的 `environment` 段中取消该行注释并填入随机字符串（生成方式见下文「生成随机密钥」）。

***

## 配置说明

### 必要环境变量

| 变量名                 | 说明                 | 是否必填   |
| ------------------- | ------------------ | ------ |
| `SESSION_SECRET`    | 会话密钥，多机部署必须一致      | **必填** |
| `CRYPTO_SECRET`     | 加密密钥，使用 Redis 时必填  | 条件必填   |
| `SQL_DSN`           | 数据库连接字符串（使用外部数据库时） | 可选     |
| `REDIS_CONN_STRING` | Redis 连接字符串        | 可选     |

### 生成随机密钥

```bash
# 生成 SESSION_SECRET
openssl rand -hex 16

# 或使用 Linux 命令
head -c 16 /dev/urandom | xxd -p
```

***

## 常见问题

### Q1：无法访问 3000 端口？

1. 检查服务器防火墙是否开放 3000 端口
2. 在宝塔面板 **安全** 中放行 3000 端口
3. 检查云服务器安全组是否开放端口

### Q2：登录后提示会话失效？

确保设置了 `SESSION_SECRET` 环境变量，且值不为空。

### Q3：数据如何持久化？

使用 Docker 卷映射数据目录：

```yaml
volumes:
  - ./data:/data
```

### Q4：如何更新版本？

```bash
cd /www/wwwroot/webb-api

# 拉取最新源码
git pull

# 重新构建并重启（应用镜像为本地构建，无需 docker pull）
docker compose up -d --build
```

### Q5：拉取镜像报「这镜像不在白名单 / this image is not in the allowlist」？

该报错来自 Docker 镜像加速服务（如 DaoCloud public-image-mirror）。加速服务只放行白名单内的镜像，非白名单镜像会被直接拒绝，报错中会附带 `https://github.com/DaoCloud/public-image-mirror/issues/2328` 链接。

本项目**不使用远程应用镜像**，按「方法二」在本地构建即可完全避免该问题。若仍需拉取其他镜像，可任选一种：

1. **直连 Docker Hub**：从 `/etc/docker/daemon.json` 中移除 `registry-mirrors` 配置，执行 `systemctl restart docker`
2. **显式指定仓库地址绕过加速服务**：`docker pull docker.io/<namespace>/<image>:<tag>`
3. **申请单次同步**：向加速服务仓库提交同步 Issue（DaoCloud 用户：`https://github.com/DaoCloud/public-image-mirror/issues/new?template=sync-image.yml`）

***

## 相关链接

- [官方文档](https://docs.newapi.pro/zh/docs/installation)
- [环境变量配置](https://docs.newapi.pro/zh/docs/installation/config-maintenance/environment-variables)
- [常见问题](https://docs.newapi.pro/zh/docs/support/faq)
- [GitHub 仓库](https://github.com/webb-chen/webb-api)

***

## 截图示例

![宝塔面板 Docker 安装](https://github.com/user-attachments/assets/7a6fc03e-c457-45e4-b8f9-184508fc26b0)

> ⚠️ 注意：密钥为环境变量 `SESSION_SECRET`，请务必设置！

