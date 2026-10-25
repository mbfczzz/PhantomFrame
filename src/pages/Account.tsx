import { useState } from "react";
import { Link } from "react-router-dom";
import { Brand } from "../components/Brand";
import {
  Wallet,
  Zap,
  Image,
  KeyRound,
  Plus,
  Copy,
  Trash2,
  ArrowUpRight,
  Code2,
  BookOpen,
  Check,
  UserPlus,
  Coins,
  ArrowRightLeft,
  ShieldCheck,
  Terminal,
  Download,
  ChevronRight,
  ArrowLeft,
  Settings2,
} from "lucide-react";
import {
  useApi,
  useRefresh,
  useAction,
  useConfirm,
  useToast,
  api,
  post,
  money,
  moneyChange,
  transactionDescription,
  date,
  copy,
  PageHeader,
  Field,
  Modal,
  Tabs,
  Form,
  Badge,
  Loading,
  Empty,
  ErrorState,
  Pagination,
  type User,
  type RecordData,
  type Job,
  type PageData,
} from "../lib";

export function Stat({
  label,
  value,
  icon,
  sub,
}: {
  label: string;
  value: string | number;
  icon?: React.ReactNode;
  sub?: string;
}) {
  return (
    <div className="stat-card">
      <div>
        <span>{label}</span>
        {icon}
      </div>
      <strong>{value}</strong>
      {sub && <small>{sub}</small>}
    </div>
  );
}
export function TransactionTable({
  items,
  showUser = false,
  details = false,
}: {
  items: RecordData[];
  showUser?: boolean;
  details?: boolean;
}) {
  const toast = useToast();
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>交易时间</th>
            {showUser && <th>用户</th>}
            <th>类型</th>
            <th>描述</th>
            <th>金额变动（元）</th>
            <th>变动后余额</th>
            {details && <th>流水 / 关联任务</th>}
          </tr>
        </thead>
        <tbody>
          {items.map((t) => (
            <tr key={t.id}>
              <td className="muted nowrap">{date(t.created_at)}</td>
              {showUser && <td>{t.username}</td>}
              <td>
                <Badge>
                  {(
                    {
                      generation: "生成扣费",
                      refund: "任务退款",
                      redeem: "兑换充值",
                      welcome: "注册赠送",
                      adjustment: "人工调账",
                      transfer: "余额划转",
                    } as RecordData
                  )[t.kind] || t.kind}
                </Badge>
              </td>
              <td>{transactionDescription(t.description)}</td>
              <td className={t.amount >= 0 ? "green" : "muted"}>
                {moneyChange(t.amount)}
              </td>
              <td>{money(t.balance)}</td>
              {details && (
                <td>
                  <button
                    type="button"
                    className="ledger-id"
                    aria-label={`复制流水号 ${t.id}`}
                    title={t.id}
                    onClick={() =>
                      copy(t.id)
                        .then(() => toast("流水号已复制"))
                        .catch(() => toast("复制失败", true))
                    }
                  >
                    <code>{t.id.slice(0, 8)}…</code>
                    <Copy size={12} />
                  </button>
                  {t.job_id && (
                    <Link
                      className="ledger-job-link"
                      to={`/history?search=${encodeURIComponent(t.job_id)}`}
                    >
                      查看关联任务
                    </Link>
                  )}
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {!items.length && <Empty title="暂无资金流水" />}
    </div>
  );
}
export function Profile() {
  const confirm = useConfirm();
  const { data: user } = useApi<User>("/me");
  const { data: usage } = useApi<RecordData>("/usage");
  const { data: config } = useApi<RecordData>("/config");
  const [tab, setTab] = useState("redeem");
  const [code, setCode] = useState("");
  const [page, setPage] = useState(1);
  const { data: transactions } = useApi<PageData<RecordData>>(
    "/transactions?page=" + page,
  );
  const { data: children } = useApi<User[]>("/sub-users");
  const [displayName, setDisplayName] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [addUser, setAddUser] = useState(false);
  const [childUsername, setChildUsername] = useState("");
  const [childPassword, setChildPassword] = useState("");
  const [transfer, setTransfer] = useState<User | null>(null);
  const [amount, setAmount] = useState(100);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  if (!user) return <Loading />;
  return (
    <div className="page profile-page">
      <PageHeader
        title="账户与余额"
        description="查看余额和流水，管理账户资料。"
      >
        <Link className="button" to="/developer">
          <Code2 size={16} />
          开放平台
        </Link>
        {user.role === "admin" && (
          <Link className="button" to="/admin">
            <ShieldCheck size={16} />
            管理后台
          </Link>
        )}
      </PageHeader>
      <section className="profile-summary">
        <div className="profile-person">
          <span className="avatar large">{user.displayName[0]}</span>
          <div>
            <h2>{user.displayName}</h2>
            <p>@{user.username}</p>
            <Badge>{user.role === "admin" ? "管理员" : "创作者"}</Badge>
          </div>
        </div>
        <div>
          <strong className="gold">{money(user.balance)}</strong>
          <span>可用余额</span>
        </div>
        <div>
          <strong>{money(usage?.redeemed)}</strong>
          <span>累计兑换</span>
        </div>
        <div>
          <strong>{usage?.total || 0}</strong>
          <span>生成次数</span>
        </div>
      </section>
      <div className="section-tabs">
        <Tabs
          value={tab}
          onChange={setTab}
          items={[
            { value: "redeem", label: "兑换充值" },
            { value: "transactions", label: "资金流水" },
            { value: "edit", label: "编辑资料" },
            { value: "children", label: "我的子账户" },
          ]}
        />
      </div>
      {tab === "redeem" && (
        <section className="panel redeem-panel">
          <div className="redeem-symbol">
            <Wallet size={34} />
          </div>
          <h2>兑换充值</h2>
          <p className="muted">使用兑换码为账户充值，生成按实际费用扣款。</p>
          <Form
            onSubmit={() =>
              act(async () => {
                await post("/redeem", { code });
                setCode("");
                refresh("/me", "/usage", "/transactions");
              }, "兑换成功，金额已到账")
            }
          >
            <div className="redeem-input">
              <input
                aria-label="兑换码"
                placeholder="输入兑换码…"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                required
              />
              <button className="button primary" disabled={busy}>
                兑换
              </button>
            </div>
          </Form>
          {config?.purchaseUrl && (
            <a
              className="text-link"
              href={config.purchaseUrl}
              rel="noreferrer"
              target="_blank"
            >
              购买兑换码 <ArrowUpRight size={14} />
            </a>
          )}
        </section>
      )}
      {tab === "transactions" && (
        <section className="panel">
          <TransactionTable items={transactions?.items || []} />
          <Pagination
            page={page}
            total={transactions?.total || 0}
            onChange={setPage}
          />
        </section>
      )}
      {tab === "edit" && (
        <div className="two-columns">
          <section className="panel">
            <h2>个人资料</h2>
            <Form
              onSubmit={() =>
                act(async () => {
                  await post(
                    "/me",
                    { displayName: displayName || user.displayName },
                    "PATCH",
                  );
                  refresh("/me");
                }, "资料已更新")
              }
            >
              <Field label="用户名">
                <input value={user.username} disabled />
              </Field>
              <Field label="昵称">
                <input
                  placeholder={user.displayName}
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  maxLength={50}
                />
              </Field>
              <button className="button primary" disabled={busy}>
                保存资料
              </button>
            </Form>
          </section>
          <section className="panel">
            <h2>修改密码</h2>
            <Form
              onSubmit={() =>
                act(async () => {
                  await post("/me/password", {
                    currentPassword,
                    password: newPassword,
                  });
                  setCurrentPassword("");
                  setNewPassword("");
                }, "密码已修改，其他登录会话已退出")
              }
            >
              <Field label="原密码">
                <input
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                />
              </Field>
              <Field label="新密码">
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  minLength={8}
                  autoComplete="new-password"
                  required
                  placeholder="至少 8 位"
                />
              </Field>
              <button className="button" disabled={busy}>
                更新密码
              </button>
            </Form>
          </section>
        </div>
      )}
      {tab === "children" && (
        <section className="panel">
          <div className="section-header">
            <div>
              <h2>我的子账户</h2>
              <p className="muted">为团队成员分配独立账号和余额。</p>
            </div>
            <button
              className="button primary"
              onClick={() => setAddUser(true)}
              disabled={!!user.parentId}
            >
              <UserPlus size={16} />
              添加子账户
            </button>
          </div>
          {children?.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>账号</th>
                    <th>昵称</th>
                    <th>账户余额</th>
                    <th>状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {children.map((c) => (
                    <tr key={c.id}>
                      <td>{c.username}</td>
                      <td>{c.displayName}</td>
                      <td className="gold">{money(c.balance)}</td>
                      <td>
                        <Badge status={c.status} />
                      </td>
                      <td>
                        <div className="row-actions">
                          <button
                            className="text-link"
                            onClick={() => setTransfer(c)}
                          >
                            转账
                          </button>
                          <button
                            className="text-link danger"
                            onClick={async () => {
                              if (
                                await confirm(
                                  "解除关联后，此账户将成为独立账户。确定继续？",
                                  {
                                    title: "解除子账户关联",
                                    confirmLabel: "解除关联",
                                  },
                                )
                              )
                                act(async () => {
                                  await api("/sub-users/" + c.id, {
                                    method: "DELETE",
                                  });
                                  refresh("/sub-users");
                                }, "已解除关联");
                            }}
                          >
                            解除关联
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <Empty
              title="还没有子账户"
              text="创建子账户，与团队一起协作创作。"
              icon={<UserPlus size={28} />}
            />
          )}
        </section>
      )}
      {addUser && (
        <Modal
          title="添加子账户"
          description="为团队成员创建独立的登录账号。"
          icon={<UserPlus size={20} />}
          onClose={() => setAddUser(false)}
        >
          <Form
            onSubmit={() =>
              act(async () => {
                await post("/sub-users", {
                  username: childUsername,
                  password: childPassword,
                });
                setAddUser(false);
                setChildPassword("");
                refresh("/sub-users");
              }, "子账户已创建")
            }
          >
            <Field label="用户名">
              <input
                value={childUsername}
                onChange={(e) => setChildUsername(e.target.value)}
                required
                minLength={3}
              />
            </Field>
            <Field label="初始密码">
              <input
                type="password"
                value={childPassword}
                onChange={(e) => setChildPassword(e.target.value)}
                minLength={8}
                required
                autoComplete="new-password"
              />
            </Field>
            <div className="modal-footer dialog-actions">
              <button
                type="button"
                className="button"
                onClick={() => setAddUser(false)}
              >
                取消
              </button>
              <button className="button primary" disabled={busy}>
                创建子账户
              </button>
            </div>
          </Form>
        </Modal>
      )}
      {transfer && (
        <Modal
          title={"转账给 " + transfer.username}
          icon={<Coins size={20} />}
          description="金额会从当前账户划转到该子账户。"
          onClose={() => setTransfer(null)}
        >
          <Form
            onSubmit={() =>
              act(async () => {
                await post(`/sub-users/${transfer.id}/transfer`, {
                  credits: amount,
                });
                setTransfer(null);
                refresh("/sub-users", "/me", "/transactions");
              }, "余额已划转")
            }
          >
            <p className="muted">将从你的账户余额中扣除对应金额。</p>
            <Field label="转账金额（元）">
              <input
                type="number"
                value={amount}
                onChange={(e) => setAmount(Number(e.target.value))}
                min="0.01"
                step="0.01"
                max={user.balance}
                required
              />
            </Field>
            <div className="modal-footer dialog-actions">
              <button
                type="button"
                className="button"
                onClick={() => setTransfer(null)}
              >
                取消
              </button>
              <button className="button primary" disabled={busy}>
                确认分配
              </button>
            </div>
          </Form>
        </Modal>
      )}
    </div>
  );
}
export function JobsTable({
  jobs,
  showUser = false,
  showSource = false,
  onDetail,
}: {
  jobs: Job[];
  showUser?: boolean;
  showSource?: boolean;
  onDetail?: (job: Job) => void;
}) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>类型</th>
            <th>模型</th>
            {showUser && <th>用户</th>}
            {showSource && <th>来源</th>}
            <th>状态</th>
            <th>费用（元）</th>
            <th>任务 ID</th>
            <th>提交时间</th>
            {onDetail && <th>操作</th>}
          </tr>
        </thead>
        <tbody>
          {jobs.map((j) => (
            <tr key={j.id}>
              <td>
                <Badge status={j.type}>
                  {j.type === "image" ? "图片" : "视频"}
                </Badge>
              </td>
              <td>{j.modelName}</td>
              {showUser && <td>{j.username}</td>}
              {showSource && (
                <td>
                  <span className={`record-source ${j.source}`}>
                    {j.source === "api" ? "API" : "网页"}
                  </span>
                </td>
              )}
              <td>
                <Badge status={j.status} />
              </td>
              <td>
                {money(j.cost)}
                {j.refunded && <small className="green"> 已退款</small>}
              </td>
              <td>
                <code title={j.id}>{j.id.slice(0, 8)}…</code>
              </td>
              <td className="muted nowrap">{date(j.createdAt)}</td>
              {onDetail && (
                <td>
                  <button
                    className="text-link"
                    onClick={() => onDetail(j)}
                    aria-label={`查看任务 ${j.id}`}
                  >
                    详情
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {!jobs.length && <Empty title="暂无调用记录" />}
    </div>
  );
}
export function UsageChart({ data }: { data: RecordData[] }) {
  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date();
    d.setUTCDate(d.getUTCDate() - 6 + i);
    const day = d.toISOString().slice(0, 10);
    return {
      date: day,
      credits: data.find((r) => r.date === day)?.credits || 0,
    };
  });
  const max = Math.max(1, ...days.map((d) => d.credits));
  return (
    <div className="usage-chart">
      {days.map((d) => (
        <div className="chart-column" key={d.date}>
          <span>{money(d.credits)}</span>
          <div className="bar-track">
            <div
              style={{ height: Math.max(2, (d.credits / max) * 100) + "%" }}
            />
          </div>
          <small>{d.date.slice(5)}</small>
        </div>
      ))}
    </div>
  );
}
export function Docs() {
  const { data: config } = useApi<RecordData>("/config");
  const [section, setSection] = useState("quickstart");
  const toast = useToast();
  const code = `curl -X POST "${window.location.origin}/api/jobs" \\\n  -H "Authorization: Bearer YOUR_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -H "Idempotency-Key: unique-request-id" \\\n  -d '{"modelId":"nanobanana","prompt":"星云中的透明水母","params":{"aspectRatio":"16:9","imageUrls":[]}}'`;
  return (
    <div className="docs-page">
      <header className="docs-header">
        <Brand name={config?.siteName} />
        <Link className="button" to="/developer">
          <ArrowLeft size={15} />
          返回控制台
        </Link>
      </header>
      <div className="docs-layout">
        <aside>
          <span className="eyebrow">DEVELOPER DOCS</span>
          {[
            ["quickstart", "快速开始"],
            ["auth", "身份验证"],
            ["models", "模型与计费"],
            ["generation", "创建生成任务"],
            ["assets", "参考素材"],
            ["errors", "错误处理"],
          ].map(([k, v]) => (
            <button
              className={section === k ? "active" : ""}
              key={k}
              onClick={() => setSection(k)}
            >
              {v}
              <ChevronRight size={15} />
            </button>
          ))}
        </aside>
        <article>
          {section === "quickstart" && (
            <>
              <span className="eyebrow">GET STARTED</span>
              <h1>让创作，融入你的应用</h1>
              <p className="lead">
                通过统一 API 调用图片与视频模型，管理素材与生成任务。
              </p>
              <h2>1. 创建 API Key</h2>
              <p>
                进入「开放平台」，点击「创建
                Key」。密钥使用当前账户余额（人民币）。
              </p>
              <h2>2. 获取可用模型</h2>
              <CodeBlock
                code={`GET ${window.location.origin}/api/models\nAuthorization: Bearer YOUR_API_KEY`}
              />
              <h2>3. 提交生成任务</h2>
              <CodeBlock code={code} />
              <button
                className="button"
                onClick={() => copy(code).then(() => toast("示例已复制"))}
              >
                <Copy size={15} />
                复制 cURL
              </button>
              <h2>4. 查询并下载结果</h2>
              <p>
                生成接口返回 HTTP 202 和任务 ID。使用{" "}
                <code>GET /api/jobs/:id</code> 轮询，状态为{" "}
                <code>succeeded</code> 时下载 <code>outputs[].url</code>
                。OSS / CDN 的 HTTPS 链接直接读取，不发送平台 API Key；站内{" "}
                <code>downloadUrl</code> 下载入口需要 API
                Key。签名链接过期后重新查询任务获取新链接。
              </p>
            </>
          )}
          {section === "auth" && (
            <>
              <h1>身份验证</h1>
              <p>在每个请求中传入 Authorization 请求头，或使用 x-api-key。</p>
              <CodeBlock
                code={
                  "Authorization: Bearer pf_YOUR_API_KEY\n# 或\nx-api-key: pf_YOUR_API_KEY"
                }
              />
              <p>
                完整密钥仅在创建时展示一次。可在控制台撤销。API Key
                不能用于管理后台、修改密码或创建新密钥。
              </p>
              <p>
                API Key 仅保存摘要，最多可创建 20 个。携带无效密钥的请求返回
                HTTP 401，不会回退到网页登录身份。
              </p>
            </>
          )}
          {section === "models" && (
            <>
              <h1>模型与计费</h1>
              <p>
                <code>GET /api/models</code>{" "}
                返回启用的模型、参数定义和价格规则。生成前可通过价格预览接口获取准确报价。
              </p>
              <CodeBlock
                code={
                  'POST /api/pricing/quote\n{\n  "modelId": "sd2.0-933-op",\n  "params": { "aspectRatio": "16:9", "resolution": "720p", "duration": 15 }\n}'
                }
              />
              <p>
                图片统一按次计费，普通图片一次一张；Midjourney
                单次可返回多张。视频支持按次或按秒。参数条件更具体的规则优先，后续调价不影响已有任务。
              </p>
              <p>
                金额单位为人民币元。提交时预扣费用，生成失败或排队取消时自动退款。已经发送给供应商的任务无法取消。
              </p>
              <p>
                接口保留 <code>credits</code> 等既有字段名，数值统一按元计算，1
                积分 = ¥1。<code>currency</code> 返回 <code>CNY</code>；例如单价
                0.1 表示
                ¥0.1。模型单价最多六位小数，整笔费用按分四舍五入；余额和充值金额最多两位小数。
              </p>
            </>
          )}
          {section === "generation" && (
            <>
              <h1>创建生成任务</h1>
              <CodeBlock code={code} />
              <h2>请求参数</h2>
              <table>
                <thead>
                  <tr>
                    <th>字段</th>
                    <th>说明</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>modelId</td>
                    <td>模型列表中的 id</td>
                  </tr>
                  <tr>
                    <td>prompt</td>
                    <td>必填，最多 10,000 字符</td>
                  </tr>
                  <tr>
                    <td>params</td>
                    <td>模型自定义参数；未传入的参数采用默认值</td>
                  </tr>
                  <tr>
                    <td>expectedCost</td>
                    <td>
                      可选，传入报价接口的 total。价格变化时返回
                      409，避免按未确认的新价格扣费。
                    </td>
                  </tr>
                </tbody>
              </table>
              <h2>任务状态</h2>
              <p>
                <code>queued → running → succeeded / failed</code>
              </p>
              <p>
                使用 <code>Idempotency-Key</code>{" "}
                防止网络重试造成重复扣费。同一账户、相同 key 将返回已有任务。
              </p>
              <h2>取消与删除</h2>
              <p>
                <code>POST /api/jobs/:id/cancel</code> 取消排队任务。
                <code>DELETE /api/jobs/:id</code> 删除已结束的任务记录。
              </p>
              <h2>调用记录与用量</h2>
              <p>
                <code>GET /api/jobs?source=api&amp;page=1&amp;limit=20</code>{" "}
                分页查看 API 生成记录；<code>GET /api/usage?source=api</code>{" "}
                获取 API 生成次数、成功和失败次数、消费金额与每日统计。source
                支持
                api、web、all，默认统计全部。模型查询和文件下载不计入生成次数。
              </p>
              <p>
                生成提交当前按来源 IP 限制为每分钟 30 次，每个账户最多同时有 10
                个排队或生成中的任务；超出限制返回 HTTP 429。
              </p>
            </>
          )}
          {section === "assets" && (
            <>
              <h1>参考素材</h1>
              <p>
                通过 multipart/form-data 上传文件，字段名称为
                file。单个文件不超过 30MB。
              </p>
              <CodeBlock
                code={
                  "POST /api/assets\nContent-Type: multipart/form-data\nAuthorization: Bearer YOUR_API_KEY\n\nfile: [binary]"
                }
              />
              <p>
                返回素材 id。将这个 id 放入模型对应的素材参数，例如{" "}
                <code>imageUrls: ["asset-id"]</code>
                。服务端会校验素材归属、类型和模型数量限制。
              </p>
              <p>
                支持 PNG、JPEG、WebP、GIF、MP4、WebM、MP3、WAV。素材列表：
                <code>GET /api/assets</code>。
              </p>
            </>
          )}
          {section === "errors" && (
            <>
              <h1>错误处理</h1>
              <table>
                <thead>
                  <tr>
                    <th>HTTP 状态码</th>
                    <th>含义</th>
                  </tr>
                </thead>
                <tbody>
                  {[
                    [400, "参数错误或价格未配置"],
                    [401, "未登录或 API Key 无效"],
                    [402, "余额不足"],
                    [403, "没有权限"],
                    [404, "资源不存在"],
                    [409, "资源冲突或版本冲突"],
                    [429, "请求频率超限"],
                    [500, "服务端错误"],
                  ].map(([k, v]) => (
                    <tr key={k}>
                      <td>{k}</td>
                      <td>{v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <CodeBlock code={'{ "message": "余额不足" }'} />
              <p>
                生成过程中供应商返回的错误会体现在任务的 <code>error</code>{" "}
                字段。请查询任务状态核对退款情况。
              </p>
            </>
          )}
        </article>
      </div>
    </div>
  );
}
function CodeBlock({ code }: { code: string }) {
  return (
    <pre className="code-block">
      <code>{code}</code>
    </pre>
  );
}
