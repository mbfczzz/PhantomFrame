import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  BookOpen,
  Copy,
  KeyRound,
  Plus,
  Trash2,
  Wallet,
  Zap,
  ArrowUpRight,
  Code2,
} from "lucide-react";
import {
  api,
  post,
  useApi,
  useAction,
  useConfirm,
  useRefresh,
  useToast,
  number,
  money,
  date,
  copy,
  PageHeader,
  Tabs,
  Field,
  Form,
  Modal,
  Loading,
  ErrorState,
  Empty,
  Pagination,
  type RecordData,
  type PageData,
  type Job,
} from "../lib";
import { Stat, JobsTable, UsageChart } from "./Account";

function Quickstart({ base }: { base: string }) {
  const [mode, setMode] = useState("image");
  const [language, setLanguage] = useState("curl");
  const [requestId, setRequestId] = useState(() => crypto.randomUUID());
  const toast = useToast();
  const querying = mode === "query";
  const path = querying ? "/jobs/YOUR_JOB_ID" : "/jobs";
  const method = querying ? "GET" : "POST";
  const payload =
    mode === "video"
      ? {
          modelId: "sd2.0-933-op",
          prompt: "山间云海缓缓流动，镜头向前推进",
          params: { aspectRatio: "16:9", resolution: "720p", duration: 15 },
        }
      : {
          modelId: "nanobanana",
          prompt: "窗边的陶瓷花瓶，自然光，产品摄影",
          params: { aspectRatio: "16:9" },
        };
  const idempotency = `${mode}-${requestId}`;
  const curl = `curl -X ${method} "${base}${path}" \\\n  -H "Authorization: Bearer $PHANTOMFRAME_API_KEY"${querying ? "" : ` \\\n  -H "Content-Type: application/json" \\\n  -H "Idempotency-Key: ${idempotency}" \\\n  -d '${JSON.stringify(payload, null, 2)}'`}`;
  const javascript = `const apiKey = process.env.PHANTOMFRAME_API_KEY;
if (!apiKey) throw new Error("请设置 PHANTOMFRAME_API_KEY");

const response = await fetch("${base}${path}", {
  method: "${method}",
  headers: {
    Authorization: \`Bearer \${apiKey}\`${
      querying
        ? ""
        : `,
    "Content-Type": "application/json",
    "Idempotency-Key": "${idempotency}"`
    }
  }${
    querying
      ? ""
      : `,
  body: JSON.stringify(${JSON.stringify(payload, null, 2).replaceAll("\n", "\n  ")})`
  }
});
const result = await response.json();
if (!response.ok) throw new Error(result.message);
console.log(result);`;
  const python = `import os
import json
import urllib.request

api_key = os.environ["PHANTOMFRAME_API_KEY"]
headers = {"Authorization": f"Bearer {api_key}"}
${
  querying
    ? ""
    : `headers["Content-Type"] = "application/json"
headers["Idempotency-Key"] = "${idempotency}"
payload = ${JSON.stringify(payload, null, 2)}
`
}
request = urllib.request.Request(
    "${base}${path}",
    headers=headers,
    method="${method}"${
      querying
        ? ""
        : `,
    data=json.dumps(payload).encode("utf-8")`
    }
)
with urllib.request.urlopen(request, timeout=30) as response:
    result = json.load(response)
print(result)`;
  const sample =
    language === "curl"
      ? curl
      : language === "javascript"
        ? javascript
        : python;
  return (
    <section className="panel platform-quickstart">
      <div className="section-header">
        <div>
          <h2>开始第一次调用</h2>
          <p>
            将创建的密钥设为环境变量 <code>PHANTOMFRAME_API_KEY</code>。
          </p>
        </div>
        <Link className="text-link" to="/docs">
          完整文档 <ArrowUpRight size={14} />
        </Link>
      </div>
      <div className="platform-example-toolbar">
        <Tabs
          value={mode}
          onChange={setMode}
          items={[
            { value: "image", label: "图片生成" },
            { value: "video", label: "视频生成" },
            { value: "query", label: "查询结果" },
          ]}
        />
        <Tabs
          value={language}
          onChange={setLanguage}
          items={[
            { value: "curl", label: "cURL" },
            { value: "javascript", label: "Node.js" },
            { value: "python", label: "Python" },
          ]}
        />
      </div>
      <div className="platform-code-header">
        <span>
          <b>{method}</b> {path}
        </span>
        <button
          className="text-link"
          type="button"
          onClick={() =>
            copy(sample)
              .then(() => toast("调用示例已复制"))
              .catch(() => toast("复制失败，请手动复制", true))
          }
        >
          <Copy size={14} />
          复制示例
        </button>
      </div>
      <pre className="code-block platform-code">
        <code>{sample}</code>
      </pre>
      {!querying && (
        <p className="platform-idempotency">
          每个新任务使用不同的 Idempotency-Key，重试同一任务时保持不变。
          <button
            className="text-link"
            type="button"
            onClick={() => setRequestId(crypto.randomUUID())}
          >
            更新示例请求 ID
          </button>
        </p>
      )}
      <ol className="platform-steps">
        <li>
          <strong>提交任务</strong>
          <span>
            成功返回 HTTP 202，保存响应中的 <code>id</code>。模型 ID
            和参数可在「模型与价格」或 <code>GET /models</code> 查询。
          </span>
        </li>
        <li>
          <strong>查询进度</strong>
          <span>
            使用同一 API Key 请求 <code>GET /jobs/:id</code>，建议间隔 2–3
            秒查询。状态为 <code>succeeded</code> 时读取 <code>outputs</code>。
          </span>
        </li>
        <li>
          <strong>下载结果</strong>
          <span>
            <code>outputs[].url</code> 为本站 OSS / CDN
            链接或站内文件路径。HTTPS 链接可直接读取，无需发送平台 API Key；
            <code>downloadUrl</code> 为站内下载入口，需携带 API
            Key。签名链接过期后重新查询任务。失败时查看 <code>error</code> 和{" "}
            <code>refunded</code>。
          </span>
        </li>
      </ol>
      <div className="platform-endpoints">
        <h3>常用接口</h3>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>方法</th>
                <th>路径</th>
                <th>用途</th>
              </tr>
            </thead>
            <tbody>
              {[
                ["GET", "/models", "查看可用模型和参数"],
                ["POST", "/pricing/quote", "获取参数组合的准确报价"],
                ["POST", "/jobs", "提交图片或视频生成任务"],
                ["GET", "/jobs/:id", "查询任务状态和结果"],
                ["POST", "/assets", "上传参考素材，使用 multipart/form-data"],
                ["GET", "/assets/:id/file", "下载生成文件或素材"],
                ["GET", "/usage?source=api", "查询 API 生成用量"],
              ].map(([method, path, label]) => (
                <tr key={path}>
                  <td>
                    <code>{method}</code>
                  </td>
                  <td>
                    <code>{path}</code>
                  </td>
                  <td>{label}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

export default function Developer() {
  const confirm = useConfirm();
  const {
    data: keys,
    isLoading,
    error: keyError,
    refetch: refetchKeys,
  } = useApi<RecordData[]>("/keys", { refetchInterval: 10000 });
  const { data: usage, error: usageError } = useApi<RecordData>(
    "/usage?source=api",
    { refetchInterval: 5000 },
  );
  const { data: config } = useApi<RecordData>("/config");
  const [tab, setTab] = useState("keys");
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const {
    data: jobs,
    isLoading: jobsLoading,
    error: jobsError,
    refetch: refetchJobs,
  } = useApi<PageData<Job>>(
    `/jobs?source=api&status=${status}&page=${page}&limit=20`,
    { enabled: tab === "records", refetchInterval: 5000 },
  );
  const [create, setCreate] = useState(false);
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const { act, busy } = useAction();
  const toast = useToast(),
    refresh = useRefresh();
  const base = `${window.location.origin}/api`;
  const finished = Number(usage?.succeeded || 0) + Number(usage?.failed || 0);
  useEffect(() => setPage(1), [status]);
  useEffect(() => {
    if (usage?.balance !== undefined) refresh("/me");
  }, [usage?.balance, refresh]);
  useEffect(() => {
    if (tab === "keys") refresh("/keys");
  }, [tab, refresh]);
  return (
    <div className="page developer-page">
      <PageHeader
        title="开放平台"
        description="通过 API Key 调用图片、视频、文本、音频与超分模型，与网页创作共用账户余额。"
      >
        <Link className="button" to="/docs">
          <BookOpen size={15} />
          接入文档
        </Link>
        <button
          className="button primary"
          type="button"
          onClick={() => setCreate(true)}
        >
          <Plus size={16} />
          创建 Key
        </button>
      </PageHeader>
      <div className="platform-endpoint">
        <Code2 size={18} />
        <span>API 地址</span>
        <code>{base}</code>
        <button
          className="icon-button"
          type="button"
          aria-label="复制 API 地址"
          onClick={() =>
            copy(base)
              .then(() => toast("API 地址已复制"))
              .catch(() => toast("复制失败", true))
          }
        >
          <Copy size={15} />
        </button>
        <small>Bearer Token</small>
      </div>
      {usageError && (
        <div className="inline-error" role="alert">
          用量暂时无法加载：{usageError.message}
        </div>
      )}
      <div className="stats-grid platform-stats">
        <Stat
          label="账户可用余额"
          value={usage ? money(usage.balance) : "—"}
          icon={<Wallet size={17} />}
          sub="所有 Key 与网页创作共用"
        />
        <Stat
          label="API 生成次数"
          value={usage ? number(usage.total) : "—"}
          sub="仅统计 API 提交的生成任务"
        />
        <Stat
          label="API 已消费金额"
          value={usage ? money(usage.spent) : "—"}
          icon={<Zap size={17} />}
          sub="已扣除失败及取消任务的退款"
        />
        <Stat
          label="生成成功率"
          value={
            finished
              ? `${Math.round((Number(usage?.succeeded || 0) / finished) * 100)}%`
              : "—"
          }
          sub="仅统计成功和失败的任务"
        />
      </div>
      <div className="platform-tabs">
        <Tabs
          panelId="developer-panel"
          value={tab}
          onChange={setTab}
          items={[
            { value: "keys", label: "API 密钥" },
            { value: "quickstart", label: "快速接入" },
            { value: "records", label: "调用记录" },
          ]}
        />
        {config?.demoMode && (
          <span className="platform-demo-note">演示通道输出会标注 demo</span>
        )}
      </div>
      <div
        role="tabpanel"
        id="developer-panel"
        aria-labelledby={`developer-panel-tab-${tab}`}
        tabIndex={0}
      >
        {tab === "keys" && (
          <section className="panel">
            <div className="section-header">
              <div>
                <h2>我的 API Keys</h2>
                <p>完整密钥仅在创建时展示一次，最多可创建 20 个。</p>
              </div>
              <span className="count-badge">{keys?.length || 0} / 20</span>
            </div>
            {isLoading ? (
              <Loading />
            ) : keyError ? (
              <ErrorState error={keyError} retry={refetchKeys} />
            ) : keys?.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>名称</th>
                      <th>API Key</th>
                      <th>创建时间</th>
                      <th>最近使用</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {keys.map((key) => (
                      <tr key={key.id}>
                        <td>{key.name}</td>
                        <td>
                          <code>{key.prefix}••••••••••••</code>
                        </td>
                        <td>{date(key.created_at)}</td>
                        <td>
                          {key.last_used_at
                            ? date(key.last_used_at)
                            : "尚未使用"}
                        </td>
                        <td>
                          <button
                            type="button"
                            className="icon-button danger"
                            aria-label={`撤销 ${key.name}`}
                            disabled={busy}
                            onClick={async () => {
                              if (
                                await confirm(
                                  "撤销后此 API Key 将立即失效，使用它的应用将无法继续调用。",
                                  {
                                    title: "撤销 API Key",
                                    confirmLabel: "确认撤销",
                                  },
                                )
                              )
                                void act(async () => {
                                  await api(`/keys/${key.id}`, {
                                    method: "DELETE",
                                  });
                                  refresh("/keys");
                                }, "API Key 已撤销");
                            }}
                          >
                            <Trash2 size={16} />
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty
                title="创建你的第一个 API Key"
                text="为应用或脚本创建密钥，即可调用模型、提交任务和获取结果。"
                icon={<KeyRound size={28} />}
              >
                <button
                  type="button"
                  className="text-link"
                  onClick={() => setTab("quickstart")}
                >
                  先查看调用示例 <ArrowUpRight size={14} />
                </button>
              </Empty>
            )}
            <p className="platform-key-note">
              API Key
              只能访问所属账户的数据，不能管理用户、修改密码或创建其他密钥。
            </p>
          </section>
        )}
        {tab === "quickstart" && <Quickstart base={base} />}
        {tab === "records" && (
          <>
            <section className="panel">
              <div className="section-header">
                <div>
                  <h2>API 调用记录</h2>
                  <p>仅显示通过 API Key 提交的生成任务。</p>
                </div>
                <Tabs
                  value={status}
                  onChange={setStatus}
                  items={[
                    { value: "all", label: "全部" },
                    { value: "running", label: "生成中" },
                    { value: "succeeded", label: "已完成" },
                    { value: "failed", label: "失败" },
                  ]}
                />
              </div>
              {jobsLoading ? (
                <Loading />
              ) : jobsError ? (
                <ErrorState error={jobsError} retry={refetchJobs} />
              ) : (
                <JobsTable jobs={jobs?.items || []} />
              )}
              <Pagination
                page={page}
                total={jobs?.total}
                limit={20}
                onChange={setPage}
              />
            </section>
            <section className="panel">
              <div className="section-header">
                <div>
                  <h2>API 消费金额</h2>
                  <p>最近 7 天，按 UTC 日期统计</p>
                </div>
                <strong className="gold">
                  {money(
                    usage?.daily?.reduce(
                      (sum: number, day: RecordData) => sum + day.credits,
                      0,
                    ),
                  )}
                </strong>
              </div>
              <UsageChart data={usage?.daily || []} />
            </section>
          </>
        )}
      </div>
      {create && (
        <Modal
          title="创建 API Key"
          description="为应用或脚本创建独立的访问凭证。"
          icon={<KeyRound size={20} />}
          onClose={() => setCreate(false)}
        >
          <Form
            onSubmit={() =>
              act(async () => {
                const result = await post("/keys", { name });
                setToken(result.token);
                setCreate(false);
                setName("");
                refresh("/keys");
              }, "API Key 已创建")
            }
          >
            <Field label="Key 名称">
              <input
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="例如：我的应用"
                required
                maxLength={80}
              />
            </Field>
            <div className="dialog-info">
              <KeyRound size={16} />
              <p>
                使用当前账户余额，仅可访问当前账户的数据。完整密钥只会在创建后显示一次。
              </p>
            </div>
            <div className="modal-footer dialog-actions">
              <button
                type="button"
                className="button"
                onClick={() => setCreate(false)}
              >
                取消
              </button>
              <button
                className="button primary"
                disabled={busy || !name.trim()}
              >
                {busy ? "创建中…" : "创建"}
              </button>
            </div>
          </Form>
        </Modal>
      )}
      {token && (
        <Modal
          title="API Key 已创建"
          description="请复制并保存，关闭后无法再次查看完整密钥。"
          icon={<KeyRound size={20} />}
          onClose={() => setToken("")}
        >
          <p>请保存你的 API Key，完整密钥仅展示一次。</p>
          <div className="secret-value">{token}</div>
          <div className="modal-footer dialog-actions">
            <button
              type="button"
              className="button"
              onClick={() => setToken("")}
            >
              我已保存
            </button>
            <button
              type="button"
              className="button primary"
              onClick={() =>
                copy(token)
                  .then(() => toast("API Key 已复制"))
                  .catch(() => toast("复制失败，请手动复制", true))
              }
            >
              <Copy size={16} />
              复制 API Key
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}
