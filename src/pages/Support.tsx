import { useState } from "react";
import {
  Link,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import {
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  Copy,
  LifeBuoy,
  MessageSquare,
  Plus,
  RotateCcw,
  Send,
  X,
} from "lucide-react";
import { Select } from "../components/Select";
import {
  useApi,
  useAction,
  useRefresh,
  useToast,
  post,
  copy,
  date,
  money,
  PageHeader,
  Field,
  Form,
  SearchBox,
  Pagination,
  Loading,
  ErrorState,
  Empty,
  Badge,
  type RecordData,
  type PageData,
  type User,
} from "../lib";

const categories = [
  { value: "generation", label: "生成问题" },
  { value: "billing", label: "充值与余额" },
  { value: "api", label: "API 接入" },
  { value: "account", label: "账号问题" },
  { value: "other", label: "其他问题" },
];
const statuses = [
  { value: "open", label: "待处理" },
  { value: "replied", label: "待您回复" },
  { value: "resolved", label: "已解决" },
  { value: "closed", label: "已关闭" },
];
type Message = {
  id: string;
  authorId: string;
  authorName: string;
  authorRole: string;
  content: string;
  createdAt: string;
};
type Ticket = {
  id: string;
  number: string;
  userId: string;
  username: string;
  displayName: string;
  subject: string;
  category: string;
  status: string;
  relatedJobId?: string;
  createdAt: string;
  updatedAt: string;
  messages?: Message[];
  relatedJob?: {
    id: string;
    modelName: string;
    status: string;
    cost: number;
    refunded: boolean;
  };
};
const categoryLabel = (value: string) =>
  categories.find((category) => category.value === value)?.label || value;
function TicketState({
  status,
  admin = false,
}: {
  status: string;
  admin?: boolean;
}) {
  return (
    <span className={`ticket-state ${status}`}>
      {status === "replied" && admin
        ? "已回复"
        : statuses.find((item) => item.value === status)?.label || status}
    </span>
  );
}
function TicketHeading({
  admin,
  title,
  description,
  children,
}: {
  admin: boolean;
  title: string;
  description: string;
  children?: React.ReactNode;
}) {
  return admin ? (
    <header className="section-header support-admin-heading">
      <div>
        <h2>{title}</h2>
        <p className="muted">{description}</p>
      </div>
      <div className="header-actions">{children}</div>
    </header>
  ) : (
    <PageHeader title={title} description={description}>
      {children}
    </PageHeader>
  );
}

function TicketList({ admin }: { admin: boolean }) {
  const [status, setStatus] = useState("all");
  const [category, setCategory] = useState("all");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const base = admin ? "/admin/tickets" : "/tickets";
  const query = new URLSearchParams({
    status,
    category,
    search,
    page: String(page),
    limit: "20",
  });
  const { data, isLoading, error, refetch } = useApi<PageData<Ticket>>(
    base + "?" + query,
    { refetchInterval: 10000 },
  );
  return (
    <>
      <TicketHeading
        admin={admin}
        title={admin ? "工单管理" : "提交工单"}
        description={
          admin
            ? "查看用户问题、回复工单并更新处理状态。"
            : "提交问题并查看处理进度，与客服继续沟通。"
        }
      >
        {!admin && (
          <Link className="button primary" to="/tickets/new">
            <Plus size={16} />
            新建工单
          </Link>
        )}
      </TicketHeading>
      <div className="ticket-filters">
        <SearchBox
          value={search}
          onChange={(value) => {
            setSearch(value);
            setPage(1);
          }}
          placeholder="搜索标题或工单编号"
        />
        <Select
          label="工单状态"
          value={status}
          onChange={(value) => {
            setStatus(value);
            setPage(1);
          }}
          options={[
            { value: "all", label: "全部状态" },
            ...statuses.map((item) => ({
              ...item,
              label: admin && item.value === "replied" ? "已回复" : item.label,
            })),
          ]}
        />
        <Select
          label="问题分类"
          value={category}
          onChange={(value) => {
            setCategory(value);
            setPage(1);
          }}
          options={[{ value: "all", label: "全部分类" }, ...categories]}
        />
      </div>
      <section className="panel ticket-list-panel">
        {isLoading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} retry={refetch} />
        ) : data?.items.length ? (
          <div className="ticket-list">
            {data.items.map((ticket) => (
              <Link
                className="ticket-list-item"
                key={ticket.id}
                to={`${base}/${ticket.id}`}
              >
                <div className="ticket-list-copy">
                  <div className="ticket-list-title">
                    <strong>{ticket.subject}</strong>
                    <TicketState status={ticket.status} admin={admin} />
                  </div>
                  <div className="ticket-list-meta">
                    <code>{ticket.number}</code>
                    <span>{categoryLabel(ticket.category)}</span>
                    {admin && (
                      <span>
                        {ticket.displayName} · {ticket.username}
                      </span>
                    )}
                    <span>更新于 {date(ticket.updatedAt)}</span>
                  </div>
                </div>
                <ArrowRight size={16} />
              </Link>
            ))}
          </div>
        ) : (
          <Empty
            title={
              search || status !== "all" || category !== "all"
                ? "没有匹配的工单"
                : admin
                  ? "暂无待处理的工单"
                  : "还没有提交过工单"
            }
            text={
              admin
                ? "用户提交的问题会出现在这里。"
                : "遇到生成、充值或账户问题，可以新建工单说明情况。"
            }
            icon={<LifeBuoy size={28} />}
          >
            {search || status !== "all" || category !== "all" ? (
              <button
                className="button"
                onClick={() => {
                  setSearch("");
                  setStatus("all");
                  setCategory("all");
                  setPage(1);
                }}
              >
                清空筛选
              </button>
            ) : (
              !admin && (
                <Link className="button" to="/tickets/new">
                  提交第一个工单
                </Link>
              )
            )}
          </Empty>
        )}
        <Pagination
          page={page}
          total={data?.total || 0}
          limit={20}
          onChange={setPage}
        />
      </section>
    </>
  );
}
function CreateTicket() {
  const [query] = useSearchParams();
  const navigate = useNavigate(),
    refresh = useRefresh();
  const [subject, setSubject] = useState("");
  const [category, setCategory] = useState(
    categories.some((item) => item.value === query.get("category"))
      ? query.get("category")!
      : "generation",
  );
  const [content, setContent] = useState("");
  const [jobId, setJobId] = useState(query.get("jobId") || "");
  const [error, setError] = useState("");
  const { busy, act } = useAction();
  return (
    <>
      <PageHeader
        title="提交工单"
        description="填写问题详情，提交后可在工单中继续补充信息。"
      >
        <Link className="button" to="/tickets">
          <ArrowLeft size={15} />
          返回工单列表
        </Link>
      </PageHeader>
      <div className="ticket-create-layout">
        <section className="panel ticket-create-panel">
          <div className="ticket-form-intro">
            <h2>问题详情</h2>
            <p>选择问题类型，并提供便于核对的信息。</p>
          </div>
          <Form
            onSubmit={() =>
              act(async () => {
                setError("");
                try {
                  const ticket = await post<Ticket>("/tickets", {
                    subject: subject.trim(),
                    category,
                    content: content.trim(),
                    jobId: jobId.trim(),
                  });
                  refresh("/tickets", "/admin/tickets");
                  navigate(`/tickets/${ticket.id}`);
                } catch (error) {
                  setError((error as Error).message);
                  throw error;
                }
              }, "工单已提交")
            }
          >
            <div className="ticket-form-top">
              <Field label="问题标题">
                <input
                  value={subject}
                  onChange={(event) => setSubject(event.target.value)}
                  required
                  minLength={3}
                  maxLength={120}
                  placeholder="简要说明遇到的问题"
                />
              </Field>
              <div className="ticket-form-category">
                <span>问题分类</span>
                <Select
                  label="问题分类"
                  value={category}
                  onChange={setCategory}
                  options={categories}
                />
              </div>
            </div>
            <Field
              label="问题描述"
              hint="建议包含发生时间、操作步骤和报错信息。"
            >
              <textarea
                value={content}
                onChange={(event) => setContent(event.target.value)}
                minLength={5}
                maxLength={6000}
                required
                rows={6}
                placeholder="请详细描述问题，便于客服核对…"
              />
            </Field>
            <Field
              label="关联任务 ID（选填）"
              hint="如果与某次生成有关，可从生成记录中复制任务 ID。"
            >
              <input
                value={jobId}
                onChange={(event) => setJobId(event.target.value)}
                maxLength={100}
                placeholder="粘贴当前账户的任务 ID"
              />
            </Field>
            {error && (
              <p className="inline-error" role="alert">
                {error}
              </p>
            )}
            <div className="ticket-form-footer">
              <span>提交后可在工单列表查看回复</span>
              <button
                className="button primary"
                disabled={
                  busy || subject.trim().length < 3 || content.trim().length < 5
                }
              >
                <Send size={15} />
                {busy ? "提交中…" : "提交工单"}
              </button>
            </div>
          </Form>
        </section>
        <aside className="panel ticket-guide">
          <h2>
            <LifeBuoy size={18} />
            提交前，可以先看看
          </h2>
          <ol>
            <li>生成问题：附上任务 ID、模型名称和发生时间。</li>
            <li>账单问题：附上流水号，以及你看到的扣费或到账情况。</li>
            <li>
              接口问题：提供错误提示与调用时间，请勿填写完整 API Key 或密码。
            </li>
          </ol>
          <div className="ticket-guide-links">
            <Link to="/history">
              查看生成记录 <ArrowRight size={14} />
            </Link>
            <Link to="/transactions">
              核对资金流水 <ArrowRight size={14} />
            </Link>
            <Link to="/developer">
              查看接口接入说明 <ArrowRight size={14} />
            </Link>
          </div>
        </aside>
      </div>
    </>
  );
}
function TicketDetail({ admin, id }: { admin: boolean; id: string }) {
  const base = admin ? "/admin/tickets" : "/tickets";
  const path = `${base}/${encodeURIComponent(id)}`;
  const {
    data: ticket,
    isLoading,
    error,
    refetch,
  } = useApi<Ticket>(path, { refetchInterval: 5000 });
  const { data: user } = useApi<User>("/me");
  const [reply, setReply] = useState("");
  const [replyError, setReplyError] = useState("");
  const { busy, act } = useAction();
  const refresh = useRefresh(),
    toast = useToast();
  const changeStatus = (status: string) =>
    act(
      async () => {
        await post(path, { status }, "PATCH");
        refresh("/tickets", "/admin/tickets");
      },
      status === "open"
        ? "工单已重新打开"
        : status === "resolved"
          ? "工单已标记为解决"
          : "工单已关闭",
    );
  if (isLoading) return <Loading />;
  if (error)
    return (
      <>
        <Link className="text-link" to={base}>
          <ArrowLeft size={14} />
          返回工单列表
        </Link>
        <ErrorState error={error} retry={refetch} />
      </>
    );
  if (!ticket) return null;
  const finished = ["resolved", "closed"].includes(ticket.status);
  return (
    <>
      <TicketHeading
        admin={admin}
        title="工单详情"
        description={`${ticket.number} · ${categoryLabel(ticket.category)}`}
      >
        <Link className="button" to={base}>
          <ArrowLeft size={15} />
          返回列表
        </Link>
        {finished ? (
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={() => changeStatus("open")}
          >
            <RotateCcw size={15} />
            重新打开
          </button>
        ) : (
          <>
            {admin && (
              <button
                type="button"
                className="button primary"
                disabled={busy}
                onClick={() => changeStatus("resolved")}
              >
                <CheckCircle2 size={15} />
                标记已解决
              </button>
            )}
            <button
              type="button"
              className="button"
              disabled={busy}
              onClick={() => changeStatus("closed")}
            >
              <X size={15} />
              关闭工单
            </button>
          </>
        )}
      </TicketHeading>
      <section className="panel ticket-detail-panel">
        <header className="ticket-detail-heading">
          <div>
            <h2>{ticket.subject}</h2>
            <p>
              {admin ? `${ticket.displayName}（${ticket.username}） · ` : ""}
              提交于 {date(ticket.createdAt)}
            </p>
          </div>
          <TicketState status={ticket.status} admin={admin} />
        </header>
        {ticket.relatedJobId && (
          <div className="ticket-related">
            <span>关联任务</span>
            <code>{ticket.relatedJobId}</code>
            <button
              className="icon-button"
              type="button"
              aria-label="复制关联任务 ID"
              onClick={() =>
                copy(ticket.relatedJobId!)
                  .then(() => toast("任务 ID 已复制"))
                  .catch(() => toast("复制失败", true))
              }
            >
              <Copy size={14} />
            </button>
            {ticket.relatedJob ? (
              <>
                <span>{ticket.relatedJob.modelName}</span>
                <Badge status={ticket.relatedJob.status} />
                <span>
                  {money(ticket.relatedJob.cost)}
                  {ticket.relatedJob.refunded ? " · 已退款" : ""}
                </span>
                {!admin && (
                  <Link
                    className="text-link"
                    to={`/history?search=${encodeURIComponent(ticket.relatedJobId)}`}
                  >
                    查看生成记录
                  </Link>
                )}
              </>
            ) : (
              <span>原任务记录已删除</span>
            )}
          </div>
        )}
        <div className="ticket-thread" aria-label="工单对话">
          {ticket.messages?.map((message) => (
            <article
              className={`ticket-message ${message.authorRole === "staff" ? "is-staff" : ""}`}
              key={message.id}
            >
              <header>
                <span className="ticket-avatar">
                  {message.authorRole === "staff" ? (
                    <LifeBuoy size={15} />
                  ) : (
                    message.authorName[0]
                  )}
                </span>
                <strong>
                  {message.authorRole === "staff"
                    ? `客服 · ${message.authorName}`
                    : message.authorId === user?.id
                      ? "我"
                      : message.authorName}
                </strong>
                <time>{date(message.createdAt)}</time>
              </header>
              <p>{message.content}</p>
            </article>
          ))}
        </div>
        {finished ? (
          <div className="ticket-finished">
            <CheckCircle2 size={18} />
            <span>
              工单已{ticket.status === "resolved" ? "解决" : "关闭"}
              。如需继续沟通，可以重新打开。
            </span>
          </div>
        ) : (
          <Form
            onSubmit={() =>
              act(async () => {
                setReplyError("");
                try {
                  await post(path + "/messages", { content: reply.trim() });
                  setReply("");
                  refresh("/tickets", "/admin/tickets");
                } catch (error) {
                  setReplyError((error as Error).message);
                  refresh(path);
                  throw error;
                }
              }, "回复已提交")
            }
          >
            <Field label={admin ? "回复用户" : "补充说明"}>
              <textarea
                value={reply}
                onChange={(event) => setReply(event.target.value)}
                rows={4}
                maxLength={6000}
                required
                placeholder={
                  admin
                    ? "填写处理说明或需要用户补充的信息…"
                    : "补充问题细节，或回复客服…"
                }
              />
            </Field>
            {replyError && (
              <p className="inline-error" role="alert">
                {replyError}
              </p>
            )}
            <div className="ticket-form-footer">
              <span>
                {admin ? "回复后状态更新为已回复" : "提交后将等待客服继续处理"}
              </span>
              <button
                className="button primary"
                disabled={busy || !reply.trim()}
              >
                <Send size={15} />
                {busy ? "发送中…" : "发送回复"}
              </button>
            </div>
          </Form>
        )}
      </section>
    </>
  );
}
export default function Support({ admin = false }: { admin?: boolean }) {
  const location = useLocation();
  const base = admin ? "/admin/tickets" : "/tickets";
  const segment = location.pathname.slice(base.length).replace(/^\//, "");
  return (
    <div className={admin ? "support-admin" : "page support-page"}>
      {!segment ? (
        <TicketList admin={admin} />
      ) : segment === "new" && !admin ? (
        <CreateTicket />
      ) : (
        <TicketDetail key={segment} admin={admin} id={segment} />
      )}
    </div>
  );
}
