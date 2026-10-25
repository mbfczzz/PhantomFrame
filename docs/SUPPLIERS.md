# 供应商接入

供应商密钥通过后台加密保存，不放进前端或此文档。模型首先进入渠道待发布列表；发布、本站人民币定价和启用仍由管理员操作。日常拉取模型列表不改价；管理员明确要求后，可按下述核对流程导入供应商价格加 20%。

模型管理按渠道折叠展示，可搜索渠道名、模型名称或 ID。渠道分组右侧的「模型密钥」，或「渠道管理 → 管理模型 → 调用密钥」，都可以设置某个模型的独立 API Key。未设置时使用渠道默认 Key；同一渠道中相同上游模型的所有发布别名共享该独立 Key，其他模型和其他渠道不受影响。已发布模型也可替换 Key 或明确恢复渠道默认。Key 加密保存，不在接口响应、操作日志或前端回显。

后台接口 `PUT /api/admin/providers/{providerId}/models/{entryId}/key` 接收 `{ "apiKey": "..." }` 设置独立 Key，或 `{ "useDefault": true }` 清除覆盖；仅允许管理员登录会话。空 Key 不会清除配置。新任务在提交时保存实际使用的加密 Key，后续提交、轮询和下载沿用该任务快照；更换 Key 不影响已提交任务。独立 Key 被上游拒绝时会报错，不会自动改用其他 Key 重试。拉取渠道模型列表仍使用渠道默认 Key，公共素材上传服务仍使用自己的 Key。

| 渠道 | 协议 | 生成与查询 | 素材 |
| --- | --- | --- | --- |
| OpenVideo（首选） | openvideo | `/v1/videos/generations`、`/v1/images/generations`；查询 `/v1/tasks/:id` | multipart 素材，按模型返回的上游线路提交；默认关闭附加人脸处理 |
| Suda 速达 | suda | 图片和视频均 `/v1/video/generations`，参数使用 `metadata.payload` 字符串 | 使用其专属文件服务上传，分别放入 imageUrls/videoUrls/audioUrls |
| ModelHub | modelhub | 视频 `/v1/videos/generations`、`/v1/videos/tasks/:id`；图片同步 generations/compositions | multipart 原始文件与编号字段 |
| Gway | gway | 视频 `/v1/video/generations`，图片 `/v1/images/create`，统一任务轮询 | `/v1/media/upload` 后提交公网链接 |
| Catcoder | catcoder | 视频 generations + `/v1/videos/:id`；图片异步 images/generations 或 edits | 图片支持 multipart；视频先通过公共素材服务上传 |
| WXArt | wxart | OpenAI 风格图片接口及 `/v1/videos` 任务接口 | 图片 multipart，视频引用公共素材服务的 HTTPS 链接 |
| Rolldek | rolldek | OpenAI Images、Gemini `generateContent`、Seedream 与异步 `/v1/videos`；火山格式走 `/api/v3/contents/generations/tasks` | 图片文件 / Base64；视频参考素材复用公共 HTTPS 上传服务 |
| Mai-Token | maitoken | `image-v1` / `video-v1`，图片创建与查询 `/v1/images/generations`，视频 `/v1/videos` | references 的 type / role / source，引用公共 HTTPS 素材 |
| Flowlight | media-upload | `/api/open/v1/files` | 公共素材上传，独立密钥；不作为生成渠道发布模型 |

每个生成请求使用独立且稳定的 Idempotency-Key，任务查询保存供应商返回的 task_id。适配器区分受理与最终成功，Suda 不会把外层 code=success 当作生成完成。只向供应商自身域名发送该供应商密钥，下载跨域结果不会附加供应商凭据。Flowlight 的密钥不传给生成供应商。

模型参数依据可用模型接口与文档初始化，已有渠道模板不会被重复拉取覆盖。对于只返回 ID 的网关，初始参数为兼容配置，管理员发布前应按对应模型规格核对。不能从模型 ID 推导的价格不自动设置。

2026-10-25 已核对在用的 165 个模型：参考图片数量有明确依据的 147 个，其余 18 个保留未限定并标记“上限待确认”。描述按实际能力整理，区分图片/视频/音频上限、明确不支持的素材、合计上限、分辨率、输出时长和质量；不沿用上游的营销文案或与实时能力矛盾的说明。未公开的能力不根据同系列名称推断。

默认资料保存在 `internal/api/vendor_documentation.json`，每项记录来源地址。Suda 使用登录后模型广场的说明；Gway 使用 `/api/pricing` 的模型描述和参数定义；Catcoder 优先使用实时 `/v1/models` 的 capabilities，明确的 0 表示不支持；ModelHub 结合模型参数及素材说明；WXArt 结合其公开前端内置的对应模型定义；OpenVideo 结合模型/渠道接口和站内文档。网页素材中的未验证默认值不作为已确认能力写进描述。

素材字段新增 `maxTotal`（全部素材合计上限）与 `maxByResolution`（特定分辨率下的数量上限），创作页和后端报价共同校验。前者可在参考素材配置里修改，后者保留在高级 JSON 中。例如 Gway `seedance-2.5-g2-1` 的 720p 规格禁止参考视频，480p 仍按该模型参考视频上限校验。素材文件尺寸、时长等文档说明用于操作提示，仍由供应商最终校验；本轮没有加入媒体时长探测。

`scripts/build-model-documentation.mjs` 从核对后的本地文档快照生成默认资料。`go run ./cmd/catalog-docs` 输出当前库的更新计划；停止 API 后加 `-apply` 应用，写入加密备份与审计记录。已有模型仅替换仍与最初导入快照一致的字段，保留人工修改、所有别名、ID、价格与启用状态。拉取新增模型会自动带上默认说明与限制。

核对来源：

- [OpenVideo 的站内 API 文档](https://openaivideo.top/)（页面中内置协议说明）
- [Suda API 文档](https://api-docs.sudashuiapi.com/)
- [ModelHub 快速接入](https://modelhub.cc/quick-start)
- [Gway](https://gway.dev/)（公开前端内置 API 说明，模型/价格以账户接口为准）
- [Catcoder 接入文档](https://video.catcode.top/docs)
- [WXArt 文档](https://wxart.space/console/docs)
- [Flowlight 上传与下载](https://flowlight.tcmzhan.com/api-docs)
- [Rolldek 模型价格](https://www.rolldek.com/pricing)、[接口文档](https://www.rolldek.com/docs/#/README)

接入验证分为：真实模型列表鉴权、真实文件上传/公网访问、模拟服务上的请求映射与轮询/结果下载。此轮不自动发起供应商付费生成任务，真实出片需要后续实际生成验证。

## 供应商价格加 20%（2026-10-25）

本次按用户授权读取最新价目，生成 `data/maintenance/supplier-price-plan.json` 与「供应商价格加价20核对表.csv」。160 个模型、341 条次/秒/张价格可表示为当前计费规则；单价及明确的参考素材附加费用均乘 1.2，保留最多六位小数，整单按分四舍五入。未配置费用的规格不会回退到其他模型或其他质量档收费。

| 来源 | 换算与匹配 |
| --- | --- |
| OpenVideo `/v1/channels` | 内部 1000 对应 1 元；flat/resolution 按次，per_second 按秒。当前报价仅配置关闭人脸处理的组合，未推测额外处理费用。 |
| ModelHub `/v1/models` | 100 积分对应 1 元；按分辨率、质量/细节匹配。自动路由的质量和细节同时明确时仅配置两者一致的组合；参考视频总时长向上取整计费，明确另收的参考图片也加价 20%。 |
| Suda 登录后的 `/api/pricing` | 视频按量列的输入价格即秒价（`model_ratio × 2`），按次读取 `model_price`；站点以 USD 显示，使用其配置汇率 7.3 折算 CNY，default 分组倍率为 1。 |
| Gway `/api/pricing` | 使用 video_billing / video_pricing 的实际规格价格，default 分组倍率为 1；自定义人民币兑换系数为 1。 |
| Catcoder `/v1/models` | 1,000,000 micro 对应 1 元；图片按 resolution/quality，视频按当前开放分辨率。免费参考图片额度、超额图片费、参考视频秒价一并保留。 |
| WXArt 登录后的 `/api/model-pricing-matrix` | `price_amount / 100` 得到 R 口径价格，按人民币处理；只使用当前账户 open/supported 的单元格。普通/编辑模式分别匹配，按站点开关计入参考视频输入费用。 |

暂不填入固定价格：Catcoder 的 `Seedance-2.0-官转`、`Seedance-2.5-官转` 按实际视频 Token 结算，秒价仅为预冻结；WXArt 的 `gpt-image-2-official` 按输入/输出 Token 收费。现有固定费率生成流程尚未实现这些模型的实际 Token 结算。WXArt 的 `sd2`、`veo3.1-pre` 未返回开放价格。Catcoder Minimax H3 的 768p 和 kling-4.0 的 720p 不在当前模型开放参数内，也不导入该档位。

`node scripts/prepare-supplier-prices.mjs` 产生可复核的计划；`go run ./cmd/supplier-prices` 校验，停止 API 后加 `-apply` 在单个事务中应用并保留加密备份。应用时验证每条规则能报价后才提交，保持模型 ID、别名、启用状态、账户和历史流水不变。`-verify` 只检查报价，临时启用在事务中回滚，不会创建任务或请求供应商生成。

## Rolldek 接入（2026-10-25）

API 基础地址配置为 `https://rolldek.com/v1`，文档站为 `www.rolldek.com/docs`。密钥经服务端加密保存。当前提供的 Key 通过 `/v1/models` 鉴权，只返回 `gpt-image-2`、`gpt-image-2-high`、`gpt-image-2.5-flare`、`gpt-image-2.5-sunburst` 四款图片模型；四款均已发布到模型管理，别名使用 `r-`，默认停用。

`/api/usage/token/` 表明此 Key 未启用模型白名单；当前可见模型与价格页分组的交集唯一为 `gpt-image 原生`，因此采用该组倍率 1。API 未直接返回分组字段，此处是根据可见模型集合推定，记录在渠道 `pricingGroupBasis` 中。人民币成本使用站内充值单价 `price=1`，即每单位站内额度成本 1 元，不将仅用于展示的 `usd_exchange_rate=7.3` 再乘一次；再加价 20%。GPT Image 2 / High 为 ¥0.096/张，2.5 Flare / Sunburst 为 ¥0.12/张，按尺寸、分辨率和质量共配置 57 条规则；整单仍按分结算。换 Key 或分组后应重新核对。

图片编辑保留 `image[]` 文件顺序；平台统一一次一张、按次计费，GPT Image 2 和 Seedream 的输出数量固定为 1。2.5 使用文档固定尺寸表（例如 4K 方图为 2880×2880），默认单张且不发送非官渠不支持的 `n` / `response_format` 字段。Gemini 原生适配会读取 `candidates[].content.parts[].inlineData` 并跳过 `thought` 图片；Seedream 的 size 是比例，resolution 为独立字段。视频字段按 SD、WAN、通用视频、火山格式分别转换，轮询保留原任务 ID，瞬时查询失败只重试查询，不重交生成；返回内容地址缺失时使用同任务 `/content` 下载。

已准备文档覆盖的 49 个模型名及变体模板，仅导入当前 Key 实际可见且具备已核对适配器的模型；文本、音乐及未核对的 SD CH6/CH8 接口不会误标为可用视频。其他分组需换用具备相应权限的 Key。官方火山模型按实际 Token 结算，当前不自动套固定秒价。`scripts/build-rolldek-catalog.mjs` 生成默认模板，`scripts/finalize-rolldek.mjs` 根据已验证 Key、最新公开价格及分组交集完成发布和定价，既有手动价格不会覆盖。

验证包含真实模型列表鉴权、57 条本地报价核对、模拟上游的图片多张输出/编辑/Gemini/Seedream/视频参考/轮询重试/失败状态/内容下载，未发起实际付费生成。接入记录在 `data/maintenance/rolldek-integration.json`。

## Mai-Token 接入（2026-10-25）

控制台 `/dashboard/overview` 需登录；实际公开文档为 [模型文档](https://api.mai-token.com/docs)，结构化资料从 `/api/docs/catalog` 与 `/api/docs/model-documentation?model=...` 读取。提供的 Key 通过 `/v1/models` 验证，仅开放 `sdg-2.5-480p`、`sdg-2.5-720p`、`sdg-2.5-1080p`。三款均为 4–30 整数秒视频，参考图片 30 张、视频 10 段、音频 10 段；图片仅使用普通参考角色。

对应的 `video-v1` 请求使用 `contract_version`、`duration`、`resolution`、`ratio`、`references[{type,role,source}]`。适配器优先保存创建响应的本站 task_id，轮询同一个任务并读取 result.url；图片契约另用图片查询路径及 data[]。查询失败仅重试查询，绝不重交生成。素材上传使用已有 Flowlight 独立密钥，下载跨域文件不携带 Mai-Token 的凭据。公开文档中 40 个明确声明统一契约的模型已生成模板；自定义 Markdown 覆盖且尚未核对的原生接口不会被猜测为兼容接口。

[客户价格导出文档](https://api.mai-token.com/api/docs/pricing-export) 指定 `/v1/pricing/export`，价格已包含账户折扣；按页串行查询，间隔至少 5.2 秒，并校验 schema_version、discount_included、pricing_version 和总数。此 Key 的三个模型当前返回 unavailable，不能按零元或旧 model_ratio 套价。

本次使用用户提供的登录账号核对 `/api/pricing` 的 resolution_tiered 完整数据。三款的适用组唯一为 `2.5官转`，倍率 1、币种 CNY：

| 分辨率 | 供应商普通秒价 | 本站普通秒价（×1.2） | 供应商参考视频场景附加秒价 | 本站附加秒价（×1.2） |
| --- | --- | --- | --- | --- |
| 480p | 0.64 | 0.768 | 0.90 | 1.08 |
| 720p | 1.12 | 1.344 | 1.62 | 1.944 |
| 1080p | 2.07 | 2.484 | 2.46 | 2.952 |

源规则为 reference_duration_mode=ignore、include_output_duration=true，且无最低时长表；结合接口提供的附加费说明与页面“忽略输入时长／包含生成视频时长”字段核对，参考视频场景按**生成时长**另收一项费用，不计算参考视频自身秒数。本站使用 `extras.videoOutputSecondPrice` 表达这项独立附加费，只有存在参考视频时才收取，图片或音频参考不会触发。例如 480p 生成 4 秒：无参考视频为 0.768×4≈¥3.07；有参考视频为 (0.768+1.08)×4≈¥7.39。总额统一相加后按分四舍五入。

`scripts/finalize-maitoken.mjs` 在写入前严格核对版本、分组、单位、时长模式与两份参考费数据的一致性；不支持的策略拒绝自动套价。三款模型使用 m- 别名发布，默认停用，已有其他渠道、模型和手工价格保持不变。登录密码和访问令牌只在读取价格时使用，不保存到项目；生成 API Key 单独加密保存。接入记录与报价核对计划存放在 `data/maintenance/maitoken-*.json`。
