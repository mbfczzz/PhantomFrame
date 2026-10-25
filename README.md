# PhantomFrame · 幻帧创作工作室

完整前后端 AI 图片 / 视频创作工具。当前 UI 使用中性深色、清晰的操作层级和简洁文案；登录页采用真实的黑白建筑摄影。默认深色，同时提供浅色主题。

项目最初依据用户授权参考 [aggc.site](https://aggc.site/generate) 的功能，初始模型配置来自参考账号可见的 **22 个模型**，包含显示名称、分组、动态参数与价格规则。这些业务配置继续保留；当前视觉、标识、导航和主要页面文案已重新设计，原站标识已移除。设计约定见 [docs/DESIGN.md](docs/DESIGN.md)。

前端使用 **React 19 + TypeScript + Vite**，后端使用 **Go + net/http**，数据库使用 **MySQL 8.4 / InnoDB**，**Redis** 承担登录会话、限流、任务唤醒与调度选主。业务数据、余额、生成任务与素材记录保存在 MySQL，文件保存在 `data/files/`。

工作台使用顶部横向主导航：**创作、生成记录、模型与价格、资产库、开放平台**；下方服务栏提供 **提交工单、资金流水、兑换充值**。手机主导航同样位于顶部，其他服务通过展开菜单进入，顶部余额直达充值。后台管理从右上角账户入口进入。创作页采用左侧内容编辑、右侧独立参数设置、下方结果画廊的布局；手机的生成按钮固定在底部。画布已移除，旧地址跳转至创作页，历史项目数据及接口保留。

模型卡片展示真实调用状态：近 24 小时成功率、成功任务的完成耗时范围，以及最近 10 次成功／失败状态条（左旧右新）。统计基于全站保留的任务记录，网页和 API 调用均计入，取消、进行中和演示任务不计入；删除任务记录后相应统计也会更新。耗时从提交到完成，包含排队和结果下载。无记录显示“暂无调用”，只失败时成功耗时为空。`GET /api/models/status` 仅向登录用户或有效 API Key 返回可见模型的匿名统计，服务端缓存 15 秒，模型页每 30 秒刷新。

## 本地启动

需要 Node.js 22.13+（前端构建工具）、Go 1.24+、MySQL 8.4+ 和 Redis 7+。当前 Windows 本地实例使用 MySQL 8.4.13 和 Redis 8.10.2 的社区 Windows 构建；容器部署使用官方 MySQL 和 Redis 镜像。

```powershell
npm install
npm run services
npm run dev
```

- 前端：<http://127.0.0.1:5173>
- 后端：<http://127.0.0.1:3001/api/health>
- 管理后台：<http://127.0.0.1:5173/admin>
- 本机 MySQL：`127.0.0.1:3307`；Redis：`127.0.0.1:6380`。仅绑定本机地址。

`npm run services` 是 Windows 本地辅助脚本：下载到 `.tools/`、校验下载、初始化项目专用数据库并生成 `.env` 密码，不修改系统服务或其他项目实例。已配置外部数据库时跳过此命令，按 `.env.example` 填写 MySQL 和 Redis 连接信息。可以分别运行 `npm run dev:web` 和 `npm run dev:server`；Go 代码修改后重启后端。前端由 Vite 热更新。

后端健康检查同时检查 MySQL 和 Redis，成功返回 `{"status":"ok","backend":"go","database":"mysql","cache":"redis"}`。依赖不可用时返回 HTTP 503。

首次启动创建 `admin` 管理员，随机密码保存在根目录 `.local-credentials.json`，该文件已被 Git 忽略。也可以在首次启动前通过 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 配置管理员。修改这两个变量不会重置已有数据库中的账号；登录后可在账户中心修改密码。

开发环境默认开启演示模式，管理员初始有 ¥1,000 演示额度，普通注册用户默认获赠 ¥100。登录页的「体验创作工作台」创建独立体验账户。生成演示图片与视频会执行实际任务队列、扣费、记录和文件保存，但输出明确标注 **演示、非真实 AI 生成**。

## 已实现功能

| 模块       | 内容                                                                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| 创作       | 图片 / 视频切换、按次 / 按秒筛选、模型搜索与价格提示、统一下拉控件、参考素材上传 / 拖放 / 粘贴、提示词素材引用、动态报价、独立结果预览        |
| 模型与价格 | 分组、搜索、价格分档、参数说明、API 示例、跳转创作                                                                                            |
| 生成记录   | 类型 / 状态 / 日期筛选、搜索、任务详情、复制提示词、再次创作、下载、删除、排队取消                                                            |
| 资产库     | 上传、分类、搜索、媒体预览、下载、删除，生成结果自动入库                                                                                      |
| 开放平台   | API Key 创建与撤销、仅首次显示完整密钥、使用统计、调用记录、接入文档                                                                          |
| 提交工单   | 分类提交、关联本账户生成任务、查询与追问、客服回复、关闭和重新打开；后台工单管理支持标记解决、分类和状态筛选                                  |
| 资金流水   | 按类型、收支、UTC 日期、说明或流水号筛选，分页查询金额变动与余额，查看关联任务和筛选范围内收支汇总                                            |
| 兑换充值   | 兑换码入账、到账回执与流水号、最近兑换记录、无效 / 重复 / 过期提示，与用户账户和 API 共用余额                                                 |
| 账户中心   | 登录注册、昵称与密码、账户余额、兑换码、流水、子账户与余额划转                                                                                |
| 管理后台   | 统计、模型 CRUD 与上下架、参数配置、价格规则 CRUD、供应商 CRUD / 连通测试、用户角色 / 状态 / 调账、批量兑换码、任务、流水、站点设置、操作日志 |

## 管理员后台

管理员登录后，可从桌面右上角或手机导航菜单进入 `/admin`。普通用户看不到后台入口，直接打开后台地址会返回创作页；Go 接口逐次验证登录身份与管理员角色，API Key（包括管理员创建的 Key）不能调用管理接口。

- 渠道管理：维护各渠道支持的上游模型，可手动添加或从兼容 `/models` 接口拉取；待发布条目保存在渠道列表，发布后才进入模型管理。
- 模型管理：维护已发布模型的参数、价格与启用状态。新发布模型默认停用，配置完成后启用；列表提供「配置价格」快捷入口。
- 计费方式：视频可按次或按秒，图片统一按次，一次生成一张；Midjourney 单次任务可返回最多四张，仍计费一次。图片生成数量在服务端固定为 1，创作页不提供批量数量选择。图片分辨率、质量及参考素材附加费仍按所选规格结算；参考图片数量上限不受输出张数限制影响。
- 按规格定价：在模型「计费规则 → 按规格定价」中，为模型实际支持的分辨率、质量组合分别输入金额，例如 1K / 2K / 4K。只有填入的价格会保存，空白不会自动成为免费价格，也不删除既有规则；其他已有默认规则仍参与匹配。新增价格规则还可按具体视频时长设置条件。
- 兑换码管理：使用人民币金额，批量生成兑换码、设置有效期、复制或导出本批；按可兑换 / 已使用 / 已过期和使用者搜索，查询实际兑换时间。
- 全站调用：查询所有用户的网页/API 生成任务，按用户、模型、来源、状态、类型与 UTC 日期筛选，查看提示词、费用、退款状态和生成结果。
- 全站流水、用户、工单、站点设置和管理日志：沿用角色校验，兑换码删除等管理动作写入审计日志。

`GET /api/admin/jobs`、`/codes`、`/transactions`、`/audit` 返回 `{items,total,page,limit}`，通过 `page`、`limit` 分页，不再限制为最新 500/300 条；调用记录另有筛选范围内的 `summary`。详情与输出使用 `/api/admin/jobs/:id`、`/api/admin/jobs/:id/outputs/:assetID`，用户原有素材接口仍只允许访问自己的文件。

## 工单、流水与充值

- `/tickets`：本账户工单列表；`/tickets/new`：提交问题，可选关联本账户的任务 ID；`/tickets/:id`：对话详情。用户可补充说明、关闭和重新打开工单。管理员在 `/admin/tickets` 回复、标记已解决或关闭；普通用户不能访问其他账户工单，API Key 不能代替网页登录提交工单。
- `/transactions`：账户资金流水。`GET /api/transactions` 支持 `kind`（generation / refund / redeem / welcome / adjustment / transfer）、`direction`（in / out）、`from` / `to`（YYYY-MM-DD，UTC）、`search`、`page` 与 `limit`。返回的 `summary` 包含当前余额及筛选范围内的入账、支出汇总。
- `/redeem`：输入兑换码充值。仍使用 MySQL 事务和行锁保证兑换码只能入账一次；成功响应包含 `credits`、`balance` 和 `transactionId`。充值单位为人民币元（CNY），不含未接入的银行卡、微信或支付宝支付流程。配置 `purchaseUrl` 后显示获取兑换码链接，否则可通过工单咨询。

新增 `support_tickets`、`support_messages` 两张 MySQL 表，启动时自动创建，现有账户、余额、流水、兑换码均保留。已结束工单需先重新打开才能回复，客服操作写入审计日志。

## 开放平台与 API 调用

开放平台位于 <http://127.0.0.1:5173/developer>。创建 API Key 后，在请求头使用 `Authorization: Bearer YOUR_API_KEY`（也支持 `X-API-Key`），即可在没有网页登录 Cookie 的情况下调用本账户的模型、报价、生成任务、素材上传和文件下载接口。完整密钥只展示一次，后端仅存 SHA-256 摘要，撤销后立即失效。

开放平台的「快速接入」提供图片、视频和任务查询的 cURL、Node.js、Python 示例；详细协议见 <http://127.0.0.1:5173/docs>。API 与网页创作共用账户余额，提交生成任务返回 HTTP 202，随后轮询 `GET /api/jobs/:id`；结果文件下载也需要同一个账户的有效密钥。

`GET /api/usage?source=api` 只统计 API 生成任务；`GET /api/jobs?source=api&page=1&limit=20` 分页查询 API 生成记录。`source` 支持 `api`、`web`、`all`，默认全部。API 用量不包含模型查询、轮询和下载次数，消费金额扣除已退款金额。每个 Key 共用所属账户余额，不单独储值。

## 配置真实模型供应商

1. 使用管理员账号进入「管理后台 → 渠道管理 → 新增渠道」。
2. 配置供应商基础地址、接入协议和 API Key。
3. 在「渠道管理 → 管理模型」手动添加上游模型 ID，或通过「拉取模型列表」读取渠道返回的 ID。可配置发布时的初始参数和参考素材；拉取不会覆盖已有条目，也不会自动发布。
4. 点击该条目的「发布」，设置全站唯一的站内模型 ID、发布名称和生成类型。发布后进入「模型管理 → 对应模型 → 计费规则」配置价格，再在基本信息中启用模型。参考素材及高级参数仍可在模型管理中调整。

参考素材数量由模型 `parameters` 中的 `assets` 字段配置：`max` 留空/省略/null 表示不限数量，0 表示禁止添加，正整数为上限。不启用的素材类型不会出现在创作上传选项中。新增参考字段默认不设置上限，已有明确数字保留；模型卡片、详情、创作页及 API 报价/提交读取同一份配置。服务端同时核对素材类型、所有权和重复 ID，超限在扣费前拒绝。普通数字参数的 `min` / `max` 也只在配置后生效。
5. 从创作页进行小额验证。模型清单中的现有渠道名来自参考站显示信息，**不包含其私有供应商地址、密钥或服务器实现**。

支持三种真实协议适配器：

- **OpenAI 兼容图片**：`/images/generations`；有参考图片时使用 multipart `/images/edits`，解析 `data[].url` / `data[].b64_json`。默认将比例映射到常见的 `1024x1024`、`1536x1024`、`1024x1536`；如供应商有特殊尺寸要求，可给模型定义 `size` 参数。
- **OpenAI 兼容视频**：multipart `/videos`，轮询 `/videos/{id}`，获取 `/videos/{id}/content`，最多支持一张首帧参考图。
- **通用 JSON 异步接口**：可配置提交路径、查询路径、任务 ID 路径、状态路径和结果路径。模型参数原名传递，引用本账户素材时转换为 data URL。不同供应商的专有鉴权、字段映射、上传协议可能需要扩展适配器。

通用配置示例：

```json
{
  "submitPath": "/generate/video",
  "statusPath": "/tasks/{id}",
  "idPath": "data.task_id",
  "statusField": "data.status",
  "outputPath": "data.result_urls",
  "pollInterval": 3000
}
```

这些适配器已提供可运行的请求、轮询和结果落盘代码；由于本项目未获得真实供应商凭据，实际商业模型调用仍需使用你的供应商进行联调。不会拿参考站账号的 Cookie 或 Token 作为本地模型通道。

## 计费与任务约定

全站金额按人民币元（CNY）计，**1 积分 = ¥1**。账户余额、兑换和流水仍精确到分，历史数值不换算。模型单价支持六位小数，如 `¥0.108`；生成费与参考素材附加费相加后，整笔费用四舍五入到分，不在单价阶段提前取整。API 保留 `credits` 等既有字段名，报价、余额等响应带 `currency: "CNY"`。

- 后端以 **人民币分（¥0.01）为最小整数单位**存储金额，防止浮点累积误差。
- 单价使用独立的 `unit_price_micros` 整数保存（人民币百万分之一元），旧单价列保留兼容；新增 `billing_extras` 保存免费参考图片数量、超额图片单价、参考视频秒价与向上取整方式。参考视频计费从本地素材读取真实时长，不信任客户端提交的秒数；Windows 运行 `scripts/install-media-tools.ps1` 安装并校验 ffprobe，也可设置 `FFPROBE_PATH`；Docker 包含 ffmpeg。
- 报价由后端根据模型参数与价格规则计算，不接受客户端传入费用。
- 同一模型的启用价格规则采用同一种计费方式；若同一参数组合匹配多个同优先级且金额不同的规则，会拒绝报价，避免任意扣费。
- `request` 每次提交计费一次；`second` 单价乘 `duration` 秒数；`image` 单价乘 `n` 张数（未定义生成数量时为一张）。图片分辨率和质量按价格条件匹配，不会统一折算为同一档价格。
- `PUT /api/admin/models/:id/prices` 仅允许管理员网页登录调用，接收 `expectedVersion`（模型响应的 `pricingVersion`）及 `prices` 数组，在一个事务中保存整批规格价格。价格已变更返回 409，任一规则不合法则整批回滚；不覆盖表格之外的其他价格规则。
- 单次提交的价格、供应商配置和模型参数形成快照，后续调价不影响已提交任务。
- 创作表单提交时校验展示给用户的报价；若管理员刚刚调价，会要求确认最新报价后重试，不会按旧页面未显示的新价格直接扣费。API 可使用可选的 `expectedCost` 字段启用相同保护。
- 创建任务、余额扣减与资金流水处于同一个 MySQL 事务；账户行锁和唯一索引保护并发余额与 `Idempotency-Key`，相同用户同一个 key 不会重复扣费。
- 失败 / 排队取消只退款一次。请求已经发往供应商后不提供取消，以免产生不可核对的供应商费用。
- MySQL 保存持久任务队列，Redis 提供唤醒通知和调度租约；单个活跃调度器同时运行 2 个任务，每个账户最多有 10 个未结束任务。通知丢失后仍会扫描 MySQL，任务不会因 Redis 唤醒列表丢失而丢失。
- 多个 API 实例共享 Redis 会话和限流；Redis 租约选出一个任务调度器，MySQL `FOR UPDATE SKIP LOCKED` 保护任务领取。租约续期失败会中止当前调度；中断的上游请求不会自动重发。
- 服务重启时，排队任务继续处理；已派发任务不会自动重复提交，而是标为失败并退还费用，需管理员到上游核对可能已完成的任务。
- 文件当前存放在本地磁盘。运行多个 API 实例时必须共享文件目录和加密密钥；对象存储、支付网关、实时多人协同及供应商特有协议仍需另行接入。

## 构建与部署

```powershell
npm run build
npm start
```

构建产物为 `dist/` 的 React 静态文件和 `bin/phantomframe.exe`（Linux 为 `bin/phantomframe`）的 Go 程序。Go 同时提供静态资源和 API，默认地址为 <http://127.0.0.1:3001>；运行 API 本身不需要 Node.js。`npm start` 只是启动该二进制的跨平台便捷命令。如果使用这个地址进行本机生产构建预览，将 `APP_URL` 配置为 `http://127.0.0.1:3001`。

线上部署时复制 `.env.example` 为 `.env`，至少设置：

```dotenv
APP_ENV=production
HOST=0.0.0.0
PORT=3001
APP_URL=https://your-domain.example
DEMO_MODE=false
COOKIE_SECURE=true
ADMIN_USERNAME=admin
ADMIN_PASSWORD=your-own-strong-initial-password
ENCRYPTION_KEY=64-character-random-hex-key
DATA_DIR=/app/data
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_DATABASE=phantomframe
MYSQL_USER=phantomframe
MYSQL_PASSWORD=your-database-password
MYSQL_ROOT_PASSWORD=your-separate-database-root-password
REDIS_ADDR=redis:6379
REDIS_PASSWORD=your-redis-password
REDIS_PREFIX=phantomframe:
```

生成加密密钥：

```powershell
node -e "console.log(require('node:crypto').randomBytes(32).toString('hex'))"
```

`ENCRYPTION_KEY` 必须保持稳定，换密钥后不能解密已有供应商密钥。开发环境自动生成的密钥位于 `data/.encryption-key`；如迁移已有数据，应继续使用该文件里的密钥值。HTTPS 通过反向代理终止，设置 `APP_URL` 为实际外部地址。

提供多阶段 `Dockerfile` 和包含 Go 应用、MySQL、Redis 三个服务的 `compose.yaml`。配置 `.env` 后可运行 `docker compose up --build -d`。MySQL 和 Redis 通过容器内部网络访问，不映射到公网；三个服务均有健康检查和持久数据卷。没有执行对外部署。

## 数据与文件

```text
cmd/api/                Go HTTP 服务入口
cmd/migrate/            SQLite 快照导入 MySQL 的一次性工具
internal/api/           Go 路由、鉴权、MySQL 存储、计费、适配器、Redis 调度
internal/api/schema.sql MySQL 表结构
server/assets.go        Go 内嵌模型快照和演示视频
server/reference-models.json   参考站可见模型配置快照，不含账号信息
src/pages/              前端业务页面与管理后台
data/mysql/             本地辅助脚本的 MySQL 数据目录
data/redis/             本地 Redis AOF 数据目录
data/migration/         旧 SQLite 备份、导出快照、迁移报告
data/files/             本账户私有上传与生成文件
data/.encryption-key     开发环境供应商密钥加密密钥
.local-credentials.json 首次启动创建的管理员凭据（仅本机）
internal/api/integration_test.go 真实 MySQL / Redis 的后端集成检查
scripts/test-ui.mjs     隔离数据库的浏览器端到端检查
test-results/           本地验证截图和报告（Git 忽略）
```

备份时保存 MySQL 逻辑备份、`data/files/`、稳定的加密密钥和 Redis 持久数据。MySQL 运行期间使用 `mysqldump --single-transaction`，不要直接复制正在写入的数据目录。Redis 会话默认有效 7 天，密码修改或账号状态变更通过 MySQL 的会话版本使旧登录失效。API 密钥不受密码变更影响，可单独撤销。

## 从旧 SQLite 版本迁移

先等待任务完成并停止旧 Node API；保留原来的 `data/files/` 和 `data/.encryption-key`。使用一个空的目标 MySQL 数据库：

```powershell
npm run migrate:export
npm run migrate:import
npm run dev
```

导出工具先生成一致的 SQLite 备份，再将所有业务表导出到 `data/migration/sqlite-export.json`。导入工具在单个 MySQL 事务中恢复账户、密码哈希、余额、模型、价格、任务、流水、素材、项目、API Key、兑换码和设置，再将未过期会话导入 Redis。账号 ID、文件引用及供应商密文保持兼容，原始 SQLite 文件保留不删除。

目标库已有业务数据时导入会拒绝执行；不要先启动新 API 自动播种再导入。迁移快照包含密码哈希和加密后的通道密钥，已经放在 Git 忽略目录中。

正式迁移后可立即运行 `node scripts/verify-migration.mjs`，通过 Go API 对照导出快照检查原管理员登录、账户余额、模型参数、价格和站点设置；报告保存在 `data/migration/verification.json`。应在产生新的业务记录前运行。当前机器上的旧 Node 源码备份位于 `data/migration/node-backend-before-go.zip`。

所有素材下载都要经过身份认证；API Key 仅以摘要存储，供应商密钥以 AES-256-GCM 加密存储。普通用户不可访问管理 API。图片等素材由账户隔离。

## 验证

```powershell
npm test
npm run build
npm run test:ui
go vet ./...
```

先启动 MySQL 和 Redis。`npm test` 在真实服务上创建随机命名的 `phantomframe_test_*` 数据库和独立 Redis 前缀，验证鉴权、并发扣费、幂等、兑换、退款、协议适配和服务恢复；结束后只清理自己创建的数据。直接 `go test ./...` 默认跳过需要外部服务的集成检查，设置 `PF_INTEGRATION=1` 可启用。

浏览器测试运行自己的 Go 后端进程、随机 MySQL 库与 Redis 前缀，不修改开发数据。数据库用户需要对 `phantomframe_test_*` 测试库具有创建和删除权限。需要 Chromium；缺少时运行 `npx playwright install chromium`，也可用 `PLAYWRIGHT_EXECUTABLE_PATH` 指定浏览器。测试覆盖登录、生成、调价、API Key、开放平台调用、兑换回执与重复兑换、流水筛选、工单完整往返处理、服务导航、旧画布地址跳转、服务重启和手机端布局。

## 与参考站的边界

这是依据用户授权访问的前端页面、资源及账号可见模型信息重新实现的工程，不是原站后端源码或数据库的导出。后台界面为本项目实现。原站画布使用 ZIP 的导入导出，本项目采用带格式标识的 JSON；没有接入原站的私有 API、外部充值支付、验证码服务或客服系统。所有这些边界在本地产品中以实际可用的行为体现，没有伪装成已连接原站服务。
