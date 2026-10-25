import { useState } from "react";
import { Copy, Download, ListChecks, RefreshCw } from "lucide-react";
import { Select } from "../components/Select";
import {
  useApi,
  useToast,
  copy,
  number,
  money,
  date,
  statusLabel,
  Modal,
  Field,
  SearchBox,
  Pagination,
  Badge,
  Loading,
  ErrorState,
  Empty,
  type Job,
  type User,
  type Model,
  type RecordData,
  type PageData,
} from "../lib";
import { JobsTable, TransactionTable } from "./Account";
import { Media } from "./Creation";

type AdminJob = Job & {
  displayName?: string;
  providerName?: string;
  upstreamId?: string;
};
const limit = 20;
function DateFilters({
  from,
  to,
  onChange,
}: {
  from: string;
  to: string;
  onChange: (name: string, value: string) => void;
}) {
  return (
    <>
      <label className="admin-filter">
        开始日期（UTC）
        <input
          aria-label="开始日期"
          type="date"
          value={from}
          onChange={(event) => onChange("from", event.target.value)}
        />
      </label>
      <label className="admin-filter">
        结束日期（UTC）
        <input
          aria-label="结束日期"
          type="date"
          value={to}
          onChange={(event) => onChange("to", event.target.value)}
        />
      </label>
    </>
  );
}
function UserFilter({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const { data } = useApi<User[]>("/admin/users");
  return (
    <label className="admin-filter">
      用户
      <Select
        label="筛选用户"
        searchable
        value={value}
        onChange={onChange}
        options={[
          { value: "all", label: "全部用户" },
          ...(data || []).map((user) => ({
            value: user.id,
            label: `${user.displayName} · ${user.username}`,
          })),
        ]}
      />
    </label>
  );
}
function useRecordFilters(initial: Record<string, string>) {
  const [filters, setFilters] = useState(initial),
    [page, setPage] = useState(1);
  const update = (name: string, value: string) => {
    setFilters((current) => ({ ...current, [name]: value }));
    setPage(1);
  };
  const reset = () => {
    setFilters(initial);
    setPage(1);
  };
  return {
    filters,
    page,
    setPage,
    update,
    reset,
    query: new URLSearchParams({
      ...filters,
      page: String(page),
      limit: String(limit),
    }).toString(),
  };
}
export function JobAdmin() {
  const { filters, page, setPage, update, reset, query } = useRecordFilters({
    search: "",
    status: "all",
    source: "all",
    type: "all",
    userId: "all",
    modelId: "all",
    from: "",
    to: "",
  });
  const { data, isLoading, error, refetch } = useApi<
    PageData<AdminJob> & { summary: RecordData }
  >("/admin/jobs?" + query, { refetchInterval: 5000 });
  const { data: models } = useApi<Model[]>("/admin/models");
  const [detail, setDetail] = useState<string | null>(null);
  return (
    <section className="panel admin-records">
      <div className="section-header">
        <div>
          <h2>全站调用记录</h2>
          <p className="muted">查看全部用户通过网页和 API 提交的生成任务。</p>
        </div>
        <button className="button" onClick={() => refetch()}>
          <RefreshCw size={15} />
          刷新
        </button>
      </div>
      <div className="admin-filter-grid">
        <UserFilter
          value={filters.userId}
          onChange={(value) => update("userId", value)}
        />
        <label className="admin-filter">
          模型
          <Select
            label="筛选模型"
            searchable
            value={filters.modelId}
            onChange={(value) => update("modelId", value)}
            options={[
              { value: "all", label: "全部模型" },
              ...(models || []).map((model) => ({
                value: model.id,
                label: model.name,
              })),
            ]}
          />
        </label>
        <label className="admin-filter">
          调用来源
          <Select
            label="调用来源"
            value={filters.source}
            onChange={(value) => update("source", value)}
            options={[
              { value: "all", label: "全部来源" },
              { value: "web", label: "网页创作" },
              { value: "api", label: "API 调用" },
            ]}
          />
        </label>
        <label className="admin-filter">
          任务状态
          <Select
            label="任务状态"
            value={filters.status}
            onChange={(value) => update("status", value)}
            options={[
              { value: "all", label: "全部状态" },
              ...Object.entries(statusLabel)
                .filter(([value]) =>
                  [
                    "queued",
                    "running",
                    "succeeded",
                    "failed",
                    "cancelled",
                  ].includes(value),
                )
                .map(([value, label]) => ({ value, label })),
            ]}
          />
        </label>
        <label className="admin-filter">
          生成类型
          <Select
            label="生成类型筛选"
            value={filters.type}
            onChange={(value) => update("type", value)}
            options={[
              { value: "all", label: "全部类型" },
              { value: "image", label: "图片" },
              { value: "video", label: "视频" },
            ]}
          />
        </label>
        <DateFilters from={filters.from} to={filters.to} onChange={update} />
      </div>
      <div className="admin-record-search">
        <SearchBox
          value={filters.search}
          onChange={(value) => update("search", value)}
          placeholder="搜索用户名、模型、提示词或任务 ID"
        />
        <button className="button ghost" onClick={reset}>
          重置筛选
        </button>
      </div>
      <div className="admin-record-summary">
        <span>
          符合条件 <strong>{number(data?.total || 0)}</strong> 条
        </span>
        <span>
          进行中 <strong>{number(data?.summary.active || 0)}</strong>
        </span>
        <span>
          成功{" "}
          <strong className="green">
            {number(data?.summary.succeeded || 0)}
          </strong>
        </span>
        <span>
          失败 <strong>{number(data?.summary.failed || 0)}</strong>
        </span>
        <span>
          净消耗{" "}
          <strong className="gold">{money(data?.summary.spent || 0)}</strong>
        </span>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        <JobsTable
          jobs={data?.items || []}
          showUser
          showSource
          onDetail={(job) => setDetail(job.id)}
        />
      )}
      <Pagination
        page={page}
        total={data?.total || 0}
        limit={limit}
        onChange={setPage}
      />
      {detail && <AdminJobDetail id={detail} onClose={() => setDetail(null)} />}
    </section>
  );
}
function AdminJobDetail({ id, onClose }: { id: string; onClose: () => void }) {
  const {
    data: job,
    isLoading,
    error,
    refetch,
  } = useApi<AdminJob>("/admin/jobs/" + encodeURIComponent(id), {
    refetchInterval: 5000,
  });
  const toast = useToast();
  return (
    <Modal
      title="调用详情"
      description="管理员视图 · 查看所属用户、计费和生成结果。"
      icon={<ListChecks size={20} />}
      onClose={onClose}
      wide
    >
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        job && (
          <>
            <div className="detail-grid">
              <div>
                <small>用户</small>
                <strong>
                  {job.displayName} · {job.username}
                </strong>
              </div>
              <div>
                <small>调用来源</small>
                <strong>
                  {job.source === "api" ? "API 调用" : "网页创作"}
                </strong>
              </div>
              <div>
                <small>模型</small>
                <strong>{job.modelName}</strong>
              </div>
              <div>
                <small>状态</small>
                <Badge status={job.status} />
              </div>
              <div>
                <small>消费金额</small>
                <strong>
                  {money(job.cost)}
                  {job.refunded ? "（已退还）" : ""}
                </strong>
              </div>
              <div>
                <small>渠道</small>
                <strong>{job.providerName || "—"}</strong>
              </div>
              <div>
                <small>提交时间</small>
                <strong>{date(job.createdAt)}</strong>
              </div>
              <div>
                <small>完成时间</small>
                <strong>{job.finishedAt ? date(job.finishedAt) : "—"}</strong>
              </div>
            </div>
            <Field label="提示词">
              <div className="read-block">{job.prompt}</div>
            </Field>
            {job.error && <div className="inline-error">{job.error}</div>}
            {!!job.outputs.length && (
              <div className="detail-media">
                {job.outputs.map((output) => (
                  <div key={output.assetId}>
                    <Media asset={output} />
                    <a
                      className="button"
                      href={output.downloadUrl || output.url + "?download=1"}
                      download
                    >
                      <Download size={15} />
                      下载结果
                    </a>
                  </div>
                ))}
              </div>
            )}
            <details>
              <summary>任务 ID 与参数</summary>
              <pre>
                {JSON.stringify(
                  {
                    id: job.id,
                    modelId: job.modelId,
                    upstreamId: job.upstreamId || null,
                    params: job.params,
                  },
                  null,
                  2,
                )}
              </pre>
            </details>
            <div className="modal-footer">
              <button
                className="button"
                onClick={() =>
                  copy(job.id)
                    .then(() => toast("任务 ID 已复制"))
                    .catch(() => toast("复制失败", true))
                }
              >
                <Copy size={15} />
                复制任务 ID
              </button>
              <button className="button" onClick={onClose}>
                关闭详情
              </button>
            </div>
          </>
        )
      )}
    </Modal>
  );
}
export function TransactionsAdmin() {
  const { filters, page, setPage, update, reset, query } = useRecordFilters({
    search: "",
    kind: "all",
    userId: "all",
    from: "",
    to: "",
  });
  const { data, isLoading, error, refetch } = useApi<PageData<RecordData>>(
    "/admin/transactions?" + query,
  );
  return (
    <section className="panel admin-records">
      <div className="section-header">
        <div>
          <h2>全站资金流水</h2>
          <p className="muted">
            核对全部用户的充值、生成扣费、退款与人工调整。
          </p>
        </div>
        <span className="count-badge">{number(data?.total || 0)} 条</span>
      </div>
      <div className="admin-filter-grid">
        <UserFilter
          value={filters.userId}
          onChange={(value) => update("userId", value)}
        />
        <label className="admin-filter">
          交易类型
          <Select
            label="交易类型"
            value={filters.kind}
            onChange={(value) => update("kind", value)}
            options={[
              { value: "all", label: "全部类型" },
              ...Object.entries({
                generation: "生成扣费",
                refund: "任务退款",
                redeem: "兑换充值",
                welcome: "注册赠送",
                adjustment: "人工调账",
                transfer: "余额划转",
              }).map(([value, label]) => ({ value, label })),
            ]}
          />
        </label>
        <DateFilters from={filters.from} to={filters.to} onChange={update} />
      </div>
      <div className="admin-record-search">
        <SearchBox
          value={filters.search}
          onChange={(value) => update("search", value)}
          placeholder="搜索用户、流水号、任务 ID 或描述"
        />
        <button className="button ghost" onClick={reset}>
          重置筛选
        </button>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        <TransactionTable items={data?.items || []} showUser />
      )}
      <Pagination
        page={page}
        total={data?.total || 0}
        limit={limit}
        onChange={setPage}
      />
    </section>
  );
}
export function AuditAdmin() {
  const { filters, page, setPage, update, reset, query } = useRecordFilters({
    search: "",
    from: "",
    to: "",
  });
  const { data, isLoading, error, refetch } = useApi<PageData<RecordData>>(
    "/admin/audit?" + query,
  );
  return (
    <section className="panel admin-records">
      <div className="section-header">
        <div>
          <h2>管理操作日志</h2>
          <p className="muted">查询模型、价格、账户和兑换码等管理操作。</p>
        </div>
        <span className="count-badge">{number(data?.total || 0)} 条</span>
      </div>
      <div className="admin-filter-grid compact">
        <DateFilters from={filters.from} to={filters.to} onChange={update} />
      </div>
      <div className="admin-record-search">
        <SearchBox
          value={filters.search}
          onChange={(value) => update("search", value)}
          placeholder="搜索操作人、操作类型或详情"
        />
        <button className="button ghost" onClick={reset}>
          重置筛选
        </button>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>时间</th>
                <th>操作人</th>
                <th>操作</th>
                <th>详情</th>
              </tr>
            </thead>
            <tbody>
              {data?.items.map((item) => (
                <tr key={item.id}>
                  <td className="nowrap">{date(item.created_at)}</td>
                  <td>{item.username || "—"}</td>
                  <td>
                    <code>{item.action}</code>
                  </td>
                  <td>
                    <code className="audit-detail">
                      {JSON.stringify(item.detail)}
                    </code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!data?.items.length && <Empty title="暂无匹配的管理日志" />}
        </div>
      )}
      <Pagination
        page={page}
        total={data?.total || 0}
        limit={limit}
        onChange={setPage}
      />
    </section>
  );
}
