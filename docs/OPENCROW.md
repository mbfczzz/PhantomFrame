# OpenCrow 渠道接入

## 配置

1. 部署包含本次改动的版本，打开「管理后台 → 渠道管理 → 新增渠道」。
2. 接入协议选择 **OpenCrow 专用适配**。新建表单会自动填入名称 OpenCrow 和文档中的基础地址 `https://api.router.ai/v1`。
3. 填入自己的 OpenCrow API Key，保存渠道。密钥加密保存，接口不会回显。
4. 打开该渠道的「管理模型」，点击「拉取模型列表」。这一步只调用 `GET /v1/models`，以该 Key 实际可访问的模型为准，不发起生成。
5. 按需发布到模型管理。新模型默认停用；同名且参数兼容的已有售价会继承，未匹配、冲突或不兼容的价格保留待定。核对参数、价格后启用。

可以使用含 `/v1` 的基础地址，也可以使用根地址；模型同步、连接检查和生成接口会避免重复 `/v1`。实际发布模型必须使用同步得到的上游 ID，不将显示名称当成上游模型 ID。

截至 2026-10-27，公开目录有 160 项。本次内置适配 155 个创作模型的 ID 与能力定义：文本 88、图片 25、视频 37、语音 5。排除 5 个 Embedding 模型，不新增 3D 功能。同步时只接收已知且完成适配的模型；以后新增但未适配的上游 ID 不自动开放。

当前没有接收或配置真实 OpenCrow Key，也没有更新生产数据库。测试中的 OpenCrow 渠道、密钥与价格都是隔离数据库里的模拟数据。

## 接口与行为

| 类型／系列 | 接入方式 |
|---|---|
| 文本 | `POST /v1/chat/completions`，非流式；GPT 推理系列使用 `max_completion_tokens`，其他文本模型使用 `max_tokens` |
| GPT Image | JSON 文生图；参考图通过 multipart `/v1/images/edits` 上传 |
| Gemini 图片 | 原生 `input`、`response_format`；参考图内嵌 Base64；提取 `steps.model_output.content` 图片，不保存思考内容 |
| Qwen 图片 | 按模型区分 `input.prompt` 和多模态 `input.messages` |
| Seedream / Grok 图片 | 各自的分辨率、参考图字段，单次只生成一张 |
| 视频 | `POST /v1/videos/generations`，按模型系列映射请求；`GET /v1/videos/generations/{id}` 查询 |
| Wan 3.0 | `input.media`、`parameters` 嵌套格式；支持图／视频／音频参考 |
| Wan 2.x / HappyHorse | 区分 t2v、i2v、r2v 和视频编辑；需要素材的模型必须提供本人素材 |
| Seedance / MiniMax | 文本、首尾帧内容数组；MiniMax 图片用途字段位于内容块上层 |
| Kling / Motion Control | `contents`、`settings`；Turbo 不传不支持的音频字段；动作控制需要形象图和动作视频 |
| Qwen TTS | `POST /v1/audio/speech`，保存音频文件；部分型号须按供应商文档填写音色 |

基础创作表单只开放本次适配并校验过的参数，不承诺开放供应商全部高级功能。没有开放工具调用、文本流式输出、多轮图片编辑、局部蒙版、批量出图或所有参考素材组合。

需要公网素材的接口使用现有 OSS 签名地址或公共素材上传服务；GPT 图片编辑和 Gemini 内嵌参考图不需要额外素材托管。沿用现有每次参考素材合计 30 MB 限制。动作控制在扣费前检查：形象图为 PNG/JPEG，宽高至少 300px、画幅比在 1:2.5 至 2.5:1 之间；参考视频为 MP4，宽高 340–3850px，人物朝向 image 时 3–10 秒，video 时 3–30 秒。视频元数据通过本地 ffprobe 核实并缓存，无法核实时拒绝提交。

生成走现有预扣、任务、结果存储、上游日志、失败退款流程。视频请求发送稳定的任务 `nonce`；提交响应不明确时不自动重复 POST，也不自动换渠道。轮询连接中断只查询原任务；`timeout` 是失败终态。模型结果返回的嵌套视频 URL 与 Gemini Base64 均转存为用户自己的素材。

临时 HTTP 错误若携带模型一致、任务 ID 合法、状态明确且无业务错误的任务回执，会保留回执继续查询原任务。仅 HTTP 错误码或不完整响应不会被当成已受理。OpenCrow 顶层公开任务 ID 优先于内部上游 ID；没有顶层 ID 时，按嵌套任务标识筛选查询结果，不接受明确属于其他任务的文件。

报价和提交同时验证模型参数定义。缺少原生必需的规格字段、空白音色、素材字段被改成错误类型时，在创建任务和扣费之前拦截。OpenCrow 的修正位置是「模型管理」，不是 CSV 独立路由页。

## 定价

OpenCrow 页面显示的是上游美元成本／Token 等价格，不会直接写为本站人民币售价。本站继续使用既有计费规则：图片、文本、语音按次；有明确时长参数的视频可按次或按秒；跟随素材时长的动作控制按次。

发布时只继承类型相同、名称／ID／上游标识精确匹配且规则兼容的已有价格，排除演示价格。多套同名价格冲突时不选择其中一套；已发布模型不重复创建或覆盖已有价格。未定价、Key 未配置、类型不匹配、必填参考素材缺失时不能提交真实任务。

## 验证

- 155 个模型逐一校验参数定义、请求计划，以及可见参数变更必须改变请求或被拒绝。
- 16 个代表模型通过完整的同步、发布、定价、建单、模拟上游、保存输出、扣费流程；覆盖文本、语音和各主要图片／视频系列。
- 测试同名价格继承、未定价阻断、Key 不回显、重复同步去重、失败退款、嵌套视频 URL、Gemini 图片和封面过滤。
- 全量后端回归：74 个顶层测试、944 个 PASS 节点通过。随后对结果解析补充边界再次定向通过。
- 浏览器验证：后台新增 OpenCrow、自动填入文档地址、模型同步、排除 Embedding、发布默认停用、创作页视频生成、单次扣费、桌面／手机布局通过，页面错误为 0。
- 原有 41 条浏览器工作流再次通过，没有页面脚本异常，记录为 `test-results/opencrow-baseline-ui.log`。
- 本地前端／Go 构建、`go vet ./...`、`git diff --check` 通过。
- 真实模型调用 0，真实支付调用 0。所有生成测试使用 localhost 模拟服务和独立 MySQL 3307／Redis 6380；不能据此确认真实 Key 权限、余额或即时上游可用性。

记录：`test-results/opencrow-backend.log`、`test-results/opencrow-final-targeted.log`、`test-results/opencrow-ui.log`、`test-results/opencrow-ui/results.json`。

## 文档依据

进一步复查与修复记录见 [OpenCrow 复查报告](OPENCROW-RECHECK-2026-10-27.md)。

读取日期：2026-10-27。参考 OpenCrow 官方公开页面和其公开文档 API：

- `https://opencrow.ai/models`
- `https://opencrow.ai/api/public/models`
- `https://opencrow.ai/api/public/api-docs`
- `https://opencrow.ai/api/public/api-docs/openai-gpt-image`
- `https://opencrow.ai/api/public/api-docs/gemini-banana-image`
- `https://opencrow.ai/api/public/api-docs/aliyun-tts`
- `https://opencrow.ai/api/public/api-docs/submit-wan3-video-task`
- `https://opencrow.ai/api/public/docs/models/minimax-video-h3-max`
- `https://opencrow.ai/api/public/docs/models/kling-video-v3-turbo`

同时对照了用户参考项目 apiRouter 的 `OpenCrowAdapter.java` 中 Wan 3.0 的公开请求结构，没有复制其中的凭据或原始请求日志做法。
