# 供应商清单接入与部署

本次清单支持 34 家供应商、249 条独立路由，排除 1 条 3D 路由。图片 89、视频 107、文本 38、音频 11、视频超分 4。原清单有 92 条路由缺少地址或有效密钥，必须补齐才能使用。

## 后台导入（推荐）

1. 部署本次代码构建的应用镜像，登录管理员，打开「供应商路由」(`/admin/inventory`)。
2. 点击「导入 CSV」，选择原始 `provider-routes-full-keys.csv`。预览只解析数量、格式与缺失配置，不写库、不请求上游。
3. 确认导入。每条路由独立加密保存密钥，界面不回显密钥。页面显示继承、保留与待定价的模型数量。
4. 在「路由配置」补全地址、密钥；在「模型与价格」检查规格及售价，确认后启用模型。
5. 「上游日志」(`/admin/upstream`) 查看渠道、实际请求模型、阶段、耗时、请求数、任务 ID 与 HTTP 状态。用户只能查看自己任务的调用信息。

导入不会发起模型调用或扣费。新增模型默认停用，原清单停用、缺少配置、没有匹配的启用价格、供应商停用时均不能生成。导入支持重复执行；已有价格、参数配置和用户余额不会被导入覆盖。

手动再次确认导入时，CSV 是路由地址、密钥、映射和源启用状态的依据：后台对这些字段的修改会被本次 CSV 内容替换。移出 CSV 的历史路由不会自动删除，需在后台停用。

## 同名价格继承

- 类型相同，且 CSV 源模型名与已有模型的 ID、名称、上游模型名或历史清单源模型名精确相同，才作为价格来源；不模糊删除供应商后缀。
- 排除演示渠道；同名来源有多套不同的价格、计费单位、条件、附加费或启用状态时，保留待定。
- 所有价格规则必须与目标参数兼容，且至少有一条启用规则。不能兼容的价格不自动套用。
- 金额以原有整数微元精度复制，保留条件、附加费和规则启用状态。目标已存在任何规则（包括停用规则）时，不覆盖。
- 一次导入开始前固定价格来源，不让本批新继承的模型继续作为价格来源。
- 文本、音频和超分按次计费；视频仍支持按次或按秒。未定价不等于免费。

实际继承数量取决于导入时数据库中的售价；CSV 不包含价格，预览不会猜测售价。最终以导入结果和后台模型价格为准。

## 容器启动导入（可选）

应用可通过 `PROVIDER_INVENTORY_FILE` 在迁移和初始化之后、工作进程启动之前导入。本仓库提供 `deploy/docker-compose.inventory.yml` 覆盖文件。

以服务器已有 `/root/work/phantomframe` 部署目录为例，将本仓库的覆盖文件放到现有 `docker-compose.yml` 旁边，并把原始 CSV 安全放到该目录的 `provider-routes.csv`。现有主服务名应为 `phantomframe`，`.env` 保留现有 MySQL、Redis、OSS 等配置。执行：

```bash
cd /root/work/phantomframe
chown 10001:10001 provider-routes.csv
chmod 0400 provider-routes.csv
export PROVIDER_INVENTORY_SOURCE=/root/work/phantomframe/provider-routes.csv
docker compose -f docker-compose.yml -f docker-compose.inventory.yml up -d
docker compose -f docker-compose.yml -f docker-compose.inventory.yml logs --tail=80 phantomframe
```

须先将主 Compose 的镜像更新为包含本次代码的版本；本次工作未推送或发布新镜像。覆盖文件仅只读挂载 CSV，不把 CSV 放进镜像。镜像进程 UID 为 10001，因此挂载文件必须可被该 UID 读取。

启动导入会记录内容指纹；内容相同的 CSV 在下次重启时跳过，保留后台补配的路由地址、密钥和停用状态。CSV 内容变化时重新应用，已有售价和参数仍保留。需要强制应用同一份 CSV 时，使用后台导入。

CSV 内容由原清单提供，不能写进 Git、Dockerfile、构建参数或公开日志。`.gitignore` 和 `.dockerignore` 已排除清单文件名；自定义文件名仍应放在构建目录之外。备份数据库时需同时保管原 `ENCRYPTION_KEY`，否则已加密的路由密钥无法解密。

## 命令行

开发机可只解析原 CSV，无需连接数据库，也不会调用上游：

```powershell
go run ./cmd/provider-inventory --file 'D:\mbfczzzz\claude\apiRouter\artifacts\provider-inventory\provider-routes-full-keys.csv'
```

加 `--apply` 才会连接当前环境配置的数据库并写入路由。应先核对目标数据库；生产库为独立 `phantomframe`。CLI 输出数量与缺失配置、价格待定原因，不输出密钥。

## 支持边界

本次实现清单模型的标准创作流程与原供应商协议映射。音乐翻唱、续写、混音、Midjourney 后续变体按钮等高级操作不包含在本次工作流中；3D 不导入。文本单次输出上限可配置为 1–8192 Token，结果按纯文本展示和复制。超分要求提供本人已有的视频素材。

请求采用任务快照，管理员修改密钥后不改变已提交任务的轮询凭据。提交结果不确定时不会自动重复生成或切换渠道，避免重复产生上游费用；已受理的异步任务只进行轮询。失败走现有退款和结果清理流程。

本次全部验证使用本地模拟上游，未验证真实供应商余额、账号权限、联网可达性或实时模型质量；配置就绪与模拟协议通过不代表真实上游可用。
