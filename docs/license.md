# 统一授权平台（ULP）接入说明

本项目已接入 ULP License Format v1，只验签、不签发（无私钥、无签发 CLI、无在线生成接口）。

## 产品信息

| 项 | 值 |
| --- | --- |
| product | `api` |
| 授权平台 | `http://111.170.146.212:7630`（项目管理 → `api`） |
| 公钥 | `pkg/license/license.go` 的 `publicKeyPEM`（可用 `LICENSE_PUBLIC_KEY` 覆盖） |
| 签名算法 | RSA-2048 / PKCS#1 v1.5 / SHA-256 |
| 授权文件 | `LICENSE_PATH`，默认 `./license.lic` |

### 公钥来源与授权导入

权威来源是 `http://111.170.146.212:7630` 控制台「项目管理 → api → 当前生效密钥」的公钥，
本项目内置的是同一把（指纹 `28df5d409e7c5385`），离线副本在 `授权信息/`。两处必须逐字一致，
否则平台签发的 `.lic` 会被判为 `license_bad_signature`（业务路径 403）。

正常使用流程：

1. 在 `http://111.170.146.212:7630` 的 `api` 项目下**签发授权**（产品必须是 `api`）；
   机器码通过 `GET /api/license/machine-code`（或后台 `/license` 页面）获取，
   也可直接选通用授权 `*`（不绑定机器）；
2. 下载 `.lic` 文件；
3. 导入项目（二选一）：
   - 打开管理后台 `/license` 页面直接上传（推荐，会校验通过后才原子替换）；
   - 或把文件放到 `data/license.lic` 后 `docker compose restart webb-api`；
4. `GET /api/license/status` 返回 `code=ok` 即生效，页面与转发接口立即可用。

> `授权信息/` 是 ULP 公钥的**离线副本**，供接入方嵌入代码与人工核对；
> 程序运行时**不会读取**该目录，它也不参与验签。平台轮换密钥后需同步更新
> `publicKeyPEM`（并刷新 `授权信息/`），重新构建镜像。


## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `LICENSE_ENFORCE` | `true` | 强制校验；仅本地开发可设 `false` |
| `LICENSE_PATH` | `./license.lic` | 授权文件路径，Docker 中为 `/data/license.lic` |
| `LICENSE_PUBLIC_KEY` | 内置公钥 | 覆盖公钥 PEM（可选） |
| `LICENSE_MACHINE_CODE` | 自动计算 | 容器场景显式固定机器码 |

时间回拨检查点固定为授权文件同目录的 `license-clock.dat`（带 HMAC），必须与授权文件一起持久化。

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/license/status` | 授权状态（HTTP 200 + 统一 `code`） |
| GET | `/api/license/machine-code` | 本机机器码 |
| POST | `/api/license/upload` | 上传 `.lic`（字段名 `file`，≤1 MiB） |
| POST | `/api/license/reload` | 强制重新读取授权文件 |

授权无效时业务路径（`/api`、`/v1`、`/v1beta`、`/mj`、`/pg`、`/dashboard`）返回 403 并携带
`code`；初始化失败返回 503。恢复通路（授权接口、登录/会话、`/api/status`、`/api/setup`、
`/api/health` 等）始终放行。转发类路径的 403 同时兼容 OpenAI SDK 的 `error` 结构。

## 运维

1. 在 ULP 选择/创建 `api` 项目并签发授权（通用授权机器码为 `*`，机器绑定授权先取机器码）；
2. 将 `.lic` 放到 `LICENSE_PATH`（或调用上传接口）；
3. 前端 `/license` 页面可查看状态、机器码并重新检测；`POST /api/license/reload` 可热重载。
