<div align="center">

<img src="./web/public/logo.png" alt="Webb API" width="120" />

# Webb API

### 🍥 新一代大模型网关与 AI 资产管理系统

**统一接入全球 40+ 主流大模型 · 多格式协议互转 · 智能路由负载均衡 · 额度成本精细化运营**

<p align="center">
  <img src="https://img.shields.io/badge/version-v1.0.0--rc.24-blueviolet" alt="version">
  <img src="https://img.shields.io/badge/license-AGPL--3.0-brightgreen" alt="license">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="go">
  <img src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=white" alt="react">
  <img src="https://img.shields.io/badge/Docker-一键部署-2496ED?logo=docker&logoColor=white" alt="docker">
  <img src="https://img.shields.io/badge/数据库-PostgreSQL-4169E1?logo=postgresql&logoColor=white" alt="postgres">
</p>

<p align="center">
  <a href="#-核心特性">核心特性</a> •
  <a href="#-支持的模型厂商">模型厂商</a> •
  <a href="#-一键部署">一键部署</a> •
  <a href="#-docker-compose-部署">Compose 部署</a> •
  <a href="#-配置说明">配置说明</a> •
  <a href="#-运维手册">运维手册</a>
</p>

</div>

---

## 📖 项目简介

**Webb API** 是一款面向企业与团队的大语言模型（LLM）网关和 AI 资产管理平台。它在统一的网关层屏蔽了不同 AI 厂商在协议、鉴权、计费上的差异，对外提供标准、稳定、可计量的 API；对内实现渠道聚合、智能调度、额度分配、成本核算和用量审计。

- 🔌 **统一出口**：一个 API 地址、一把密钥，调用 OpenAI、Claude、Gemini、DeepSeek、通义、文心、混元等全部模型
- 🔄 **协议互转**：OpenAI / Claude Messages / Gemini 等格式自动转换，存量业务零改造切换模型
- ⚖️ **高可用调度**：多渠道加权负载、失败自动重试、渠道健康探测与自动熔断
- 💰 **精细运营**：按用户 / 分组 / 模型多维度额度、倍率、缓存计费与数据看板
- 🐳 **私有化交付**：内置 PostgreSQL + Redis，提供**离线一键安装包**，无网络环境分钟级落地

> 本项目基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api)（AGPL-3.0）二次开发与定制，遵循 AGPL-3.0 开源协议。

---

## ✨ 核心特性

### 🎛 模型网关

| 能力 | 说明 |
| --- | --- |
| 🤖 多厂商聚合 | 接入 40+ 家大模型厂商，集中管理全部上游 Key 与渠道 |
| 🔄 协议格式互转 | OpenAI Compatible ⇄ Claude Messages、OpenAI → Gemini、Gemini → OpenAI |
| ⚡ 全接口类型 | Chat、Responses、Realtime、Image、Audio、Video、Embedding、Rerank |
| 🧠 思考模式 | 支持 OpenAI o 系列 / GPT-5、Claude Thinking、Gemini 2.5 Thinking 的思考力度（low/medium/high）控制 |
| 🔁 智能路由 | 渠道加权随机、失败自动重试、优先级调度、用户级模型限流 |
| 🩺 渠道自愈 | 定时测试、自动禁用异常渠道、响应时间监控、上游模型自动同步 |

### 💰 用量与成本管理

- **额度体系**：用户额度、令牌额度、分组倍率、模型补全倍率 / 模型倍率灵活配置
- **缓存计费**：支持 OpenAI、Azure、DeepSeek、Claude、Qwen 等的缓存命中（cache read/write）成本统计
- **充值支付**：易支付、Stripe 等支付通道，兑换码 / 卡密发放
- **订阅方案**：Subscription Plan 套餐与周期重置
- **成本看板**：Dashboard 数据看板、用量日志、消费排行、性能指标（Perf Metrics）

### 🔐 安全与鉴权

- 用户 / 令牌 / 分组三级权限，模型可用范围与来源 IP 限制
- Discord、LinuxDo、Telegram、OIDC、自定义 OAuth 等多种登录方式
- Passkey 通行密钥、会话多端管理、签发窗口与审计留存
- 可信反向代理、Refresh Cookie Secure 模式与严格 OriginGuard
- 敏感词过滤、支付合规校验

### 🎨 产品体验

- 全新现代化 UI，中 / 英 / 法 / 日多语言
- 内置 Playground 在线调试游乐场与 Chat 对话
- 模型定价公示页、公开排行榜、钱包中心
- 首次启动**初始化向导**，引导式完成管理员配置

---

## 🤖 支持的模型厂商

网关以「渠道（Channel）」方式纳管上游，已内置适配的厂商包括：

<p align="center">

`OpenAI` · `Azure OpenAI` · `Anthropic Claude` · `Google Gemini / Vertex AI` · `xAI Grok` · `DeepSeek` · `通义千问（阿里）` · `文心一言（百度）` · `混元（腾讯）` · `星火（讯飞）` · `智谱 GLM` · `月之暗面 Kimi` · `零一万物` · `MiniMax` · `Mistral` · `Cohere` · `Perplexity` · `360 智脑` · `字节火山引擎（豆包）` · `即梦` · `硅基流动` · `扣子 Coze` · `Cloudflare` · `OpenRouter` · `Replicate` · `Ollama` · `Xinference` · `Dify` · `Jina Rerank` · `Midjourney` · `Suno` · `AWS Bedrock` · `PALM` · `MoKaAI` · `高级自定义渠道`

</p>

<details>
<summary><strong>📦 查看全部内置渠道适配器</strong></summary>

```
openai  azure  claude  gemini  vertex  palm  codex  xai  deepseek
ali  baidu  baidu_v2  tencent  xunfei  zhipu  zhipu_4v  moonshot
lingyiwanwu  minimax  mistral  cohere  perplexity  ai360  volcengine
jimeng  siliconflow  coze  cloudflare  openrouter  replicate  ollama
xinference  dify  jina  aws  newapi  mokaai  sub2api  submodel
advancedcustom  task
```

</details>

<details>
<summary><strong>🔌 支持的 API 接口类型</strong></summary>

| 接口 | 说明 |
| --- | --- |
| Chat Completions | OpenAI 兼容聊天补全 |
| Responses | OpenAI Responses 格式 |
| Realtime | 实时语音对话（含 Azure） |
| Images | 图像生成（含 Midjourney-Proxy） |
| Audio | 语音合成 / 语音识别 |
| Video | 视频生成 |
| Embeddings | 文本向量化 |
| Rerank | 重排序（Cohere、Jina） |
| Claude Messages | Anthropic 原生格式 |
| Gemini | Google 原生格式 |
| Suno | AI 音乐生成 |

</details>

---

## 🚀 一键部署

> [!TIP]
> 面向**离线 / 内网**环境的开箱即用方案。安装包已内置 `webb-api`、`PostgreSQL 15`、`Redis` 全部镜像，目标机器**无需联网拉取镜像**。

### 📋 前置条件

- 64 位 Linux（amd64 / arm64）
- 已安装 `docker` 与 `docker compose`（v2 插件或 v1 均可）

### ⬇️ 下载安装包

从仓库的 **Releases** 页面下载，标签均为 `v1.0.0`：

- **GitHub（单文件，推荐）**：<https://github.com/webb-chen/webb-api/releases> —— 直接下载 `webb-api-deploy.tar.gz`（约 243MB）
- **Gitee（分卷）**：<https://gitee.com/chenqi_com/webb-api/releases> —— 单附件限 100MB，需下载 3 个分卷后合并

<details>
<summary><strong>📂 Gitee 分卷合并方法（点击展开）</strong></summary>

在 Gitee Release 页面下载以下 3 个文件并放到同一目录：

- `webb-api-deploy.tar.gz.part-00`（90MB）
- `webb-api-deploy.tar.gz.part-01`（90MB）
- `webb-api-deploy.tar.gz.part-02`（63MB）

合并并校验完整性（MD5 应为 `f283201c0da2a09f7e4b02ef25021a68`）：

```bash
cat webb-api-deploy.tar.gz.part-* > webb-api-deploy.tar.gz
md5sum webb-api-deploy.tar.gz
```

</details>

### ⚙️ 执行安装

```bash
# 1. 解压（Gitee 用户请先按上面的分卷合并步骤得到 tar.gz）
tar -zxf webb-api-deploy.tar.gz
cd webb-api-deploy

# 2. 一键安装（默认安装到 /opt/webb-api）
sudo bash install.sh

# 也可以指定安装目录
sudo bash install.sh /opt/your-dir
```

安装脚本会自动完成：

1. 检测 `docker` / `docker compose` 运行环境
2. 离线加载全部镜像（约 252MB）
3. 初始化数据与日志目录
4. 通过 Compose 启动 `webb-api` / `webbapi-postgres` / `webbapi-redis`
5. 等待健康检查通过并输出访问地址

### 🌐 访问与初始化

部署完成后访问：

```
http://<目标机 IP>:3000
```

**首次访问会进入初始化向导**，按提示设置管理员账号后即可进入控制台。

### 📦 安装包内容

```
webb-api-deploy/
├── webb-api-images.tar   # 离线镜像（webb-api + postgres:15 + redis:latest）
├── docker-compose.yml    # 编排配置
├── VERSION               # 版本号
└── install.sh            # 一键安装脚本
```

---

## 🐳 Docker Compose 部署

在可联网环境，也可以直接使用源码中的 `docker-compose.yml`：

```bash
git clone https://github.com/webb-chen/webb-api.git
cd webb-api

# 自行构建本地镜像
docker build -t webb-api:local .

# 启动全部服务
docker compose up -d
```

编排包含三个服务：

| 服务 | 容器名 | 镜像 | 端口（宿主→容器） |
| --- | --- | --- | --- |
| 主应用 | `webb-api` | `webb-api:local` | `3000 → 3000` |
| PostgreSQL | `webbapi-postgres` | `postgres:15` | `5433 → 5432` |
| Redis | `webbapi-redis` | `redis:latest` | `6380 → 6379` |

> 默认的数据库密码、Redis 密码、`SESSION_SECRET` 写在 `docker-compose.yml` 中，**生产环境请务必修改**。

---

## ⚙️ 配置说明

配置通过环境变量注入，常用项如下：

| 变量名 | 说明 |
| --- | --- |
| `SQL_DSN` | PostgreSQL 连接串，如 `postgresql://user:pwd@webbapi-postgres:5432/webb-api` |
| `REDIS_CONN_STRING` | Redis 连接串，如 `redis://:password@webbapi-redis:6379/0` |
| `SESSION_SECRET` | 会话签名密钥，多节点部署必须保持一致 |
| `TZ` | 时区，默认 `Asia/Shanghai` |
| `ERROR_LOG_ENABLED` | 是否开启错误日志 |
| `BATCH_UPDATE_ENABLED` | 是否开启用量批量落库（高并发建议开启） |
| `NODE_TYPE` | 节点类型，`master` 为主节点 |
| `NODE_NAME` | 节点名称 |
| `RELAY_TIMEOUT` | 上游请求超时（秒），`0` 不限制 |
| `STREAMING_TIMEOUT` | 流式响应无数据超时（秒） |
| `MEMORY_CACHE_ENABLED` | 是否启用内存缓存 |
| `SYNC_FREQUENCY` | 与 Redis 同步频率（秒） |
| `SESSION_COOKIE_SECURE` | 生产 HTTPS 环境建议设为 `true` |

更多环境变量参见 [`.env.example`](./.env.example)。

---

## 🛠 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go 1.26 · Gin · GORM |
| 前端 | React 18 · TypeScript · Rsbuild · TanStack |
| 数据库 | PostgreSQL（也兼容 MySQL / SQLite） |
| 缓存 | Redis |
| 构建前端依赖 | Bun |
| 部署 | Docker · Docker Compose · 多阶段构建镜像 |

---

## 📁 目录结构

```
webb-api/
├── main.go              # 程序入口
├── router/              # 路由定义
├── middleware/          # 中间件（鉴权、限流、日志等）
├── controller/          # HTTP 控制器
├── model/               # 数据模型与数据库层
├── relay/               # 模型网关核心
│   └── channel/         # 各厂商渠道适配器
├── service/             # 业务服务层
├── setting/             # 系统配置模块
├── web/                 # React 前端源码
├── docs/                # 文档与图片资源
├── Dockerfile           # 多阶段镜像构建
├── docker-compose.yml   # 容器编排
└── .env.example         # 环境变量示例
```

---

## 🔧 运维手册

安装到 `/opt/webb-api` 后，常用运维命令：

```bash
cd /opt/webb-api

# 查看服务状态
docker compose ps

# 查看实时日志
docker logs -f webb-api

# 启动 / 停止 / 重启
docker compose up -d
docker compose down
docker compose restart

# 升级镜像（替换安装包内的镜像后）
docker load -i webb-api-images.tar
docker compose up -d

# 彻底卸载（⚠️ 含数据库数据，不可恢复）
docker compose down -v
```

| 路径 | 说明 |
| --- | --- |
| `/opt/webb-api/data` | 应用数据 |
| `/opt/webb-api/logs` | 运行日志 |

**健康检查**：容器内置 `GET /api/status` 健康探针，每 30 秒探测一次。

---

## ⚖️ 合规声明

> [!IMPORTANT]
> 本项目仅面向合法授权的 AI API 网关、组织内部鉴权、多模型管理、用量统计、成本核算和私有化部署场景。使用者必须合法取得上游 API Key、账号与接口授权，并遵守上游服务条款及适用法律法规。面向公众提供生成式人工智能服务时，请自行完成备案、内容安全、实名、日志留存、税务与上游授权等合规义务。

---

## 🙏 鸣谢

本项目基于开源项目二次开发，感谢上游贡献者：

- [QuantumNous/new-api](https://github.com/QuantumNous/new-api) — AGPL-3.0
- 感谢 [JetBrains](https://www.jetbrains.com/) 提供的开源开发许可证

## 📄 开源协议

本项目遵循 **GNU Affero General Public License v3.0（AGPL-3.0）** 协议开源，详见 [LICENSE](./LICENSE)。

<div align="center">

**⭐ 如果这个项目对你有帮助，欢迎点亮 Star**

</div>
