import { Copy } from "lucide-react";
import {
  useApi,
  useToast,
  copy,
  date,
  money,
  Badge,
  Loading,
  ErrorState,
  type RecordData,
} from "../lib";

export type UpstreamCall = {
  id: string;
  userId?: string;
  username?: string;
  displayName?: string;
  providerId?: string;
  providerName: string;
  protocol?: string;
  modelId?: string;
  modelName: string;
  type: string;
  upstreamModel: string;
  upstreamId?: string | null;
  status: string;
  phase: string;
  failurePhase?: string;
  source: string;
  demo: boolean;
  cost: number;
  refunded: boolean;
  createdAt: string;
  startedAt?: string | null;
  finishedAt?: string | null;
  totalMs?: number | null;
  elapsedMs?: number | null;
  requestCount: number;
  lastHttpStatus?: number | null;
  lastMethod?: string;
  lastUrl?: string;
  lastRequestId?: string;
  lastRequestMs?: number | null;
  lastRequestError?: string;
  params?: RecordData;
  error?: string;
  legacy: boolean;
  jobAvailable?: boolean;
};

export const upstreamPhaseLabel: Record<string, string> = {
  queued: "等待调用",
  submitting: "正在提交上游",
  polling: "上游处理中",
  receiving: "正在接收结果",
  storing: "正在保存结果",
  completed: "已完成",
  failed: "调用失败",
  refund_pending: "调用失败，等待退款",
  cancelled: "已取消",
  demo: "本地演示",
  legacy: "历史任务，未记录阶段",
};

export const upstreamFailureLabel: Record<string, string> = {
  queued: "排队调度",
  submitting: "提交上游",
  polling: "上游处理",
  receiving: "接收结果",
  storing: "保存结果",
  demo: "本地演示",
  legacy: "历史阶段未知",
};

export function elapsedTime(ms?: number | null) {
  if (ms == null) return "—";
  const seconds = Math.max(0, Math.floor(ms / 1000));
  if (seconds < 60) return `${seconds}秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}分 ${seconds % 60}秒`;
  return `${Math.floor(seconds / 3600)}小时 ${Math.floor((seconds % 3600) / 60)}分`;
}

export function UpstreamTaskID({ value }: { value?: string | null }) {
  const toast = useToast();
  if (!value)
    return <span className="muted upstream-no-id">未获取上游任务 ID</span>;
  return (
    <button
      type="button"
      className="upstream-task-id text-link"
      title={value}
      aria-label={`复制上游任务 ID ${value}`}
      onClick={() =>
        copy(value)
          .then(() => toast("上游任务 ID 已复制"))
          .catch(() => toast("复制失败", true))
      }
    >
      <span>{value}</span>
      <Copy size={13} aria-hidden="true" />
    </button>
  );
}

export function UpstreamTrace({
  id,
  admin = false,
}: {
  id: string;
  admin?: boolean;
}) {
  const {
    data: call,
    isLoading,
    error,
    refetch,
  } = useApi<UpstreamCall>(
    admin
      ? `/admin/upstream-calls/${encodeURIComponent(id)}`
      : `/jobs/${encodeURIComponent(id)}/upstream`,
    {
      refetchInterval: (query: { state: { data?: UpstreamCall } }) =>
        ["queued", "running", "refund_pending"].includes(
          query.state.data?.status || "",
        )
          ? 2000
          : false,
    },
  );
  if (isLoading) return <Loading />;
  if (error) return <ErrorState error={error} retry={refetch} />;
  if (!call) return null;
  return (
    <section className="upstream-trace" aria-label="上游调用情况">
      <div className="upstream-trace-heading">
        <h3>上游调用情况</h3>
        <Badge status={call.status}>
          {call.phase === "refund_pending" ? "退款处理中" : undefined}
        </Badge>
      </div>
      <dl className="upstream-trace-grid">
        <div>
          <dt>渠道</dt>
          <dd>
            {call.providerName || "—"}
            {call.demo && <small>本地演示，未调用付费模型</small>}
          </dd>
        </div>
        <div>
          <dt>上游模型</dt>
          <dd>{call.upstreamModel || "—"}</dd>
        </div>
        <div>
          <dt>当前阶段</dt>
          <dd>{upstreamPhaseLabel[call.phase] || call.phase}</dd>
        </div>
        {call.failurePhase && (
          <div>
            <dt>失败环节</dt>
            <dd>
              {upstreamFailureLabel[call.failurePhase] || call.failurePhase}
            </dd>
          </div>
        )}
        <div>
          <dt>总耗时（含排队）</dt>
          <dd>{elapsedTime(call.totalMs)}</dd>
        </div>
        <div>
          <dt>开始调用</dt>
          <dd>
            {call.startedAt
              ? date(call.startedAt)
              : call.legacy
                ? "升级前未记录"
                : "尚未开始"}
          </dd>
        </div>
        <div>
          <dt>执行耗时</dt>
          <dd>{elapsedTime(call.elapsedMs)}</dd>
        </div>
        <div>
          <dt>上游请求次数</dt>
          <dd>{call.legacy ? "升级前未记录" : call.requestCount}</dd>
        </div>
        <div>
          <dt>最近 HTTP 状态</dt>
          <dd
            className={
              call.lastHttpStatus && call.lastHttpStatus >= 400
                ? "upstream-http-error"
                : undefined
            }
          >
            {call.lastHttpStatus || "—"}
          </dd>
        </div>
        <div className="upstream-trace-wide">
          <dt>上游任务 ID</dt>
          <dd>
            <UpstreamTaskID value={call.upstreamId} />
          </dd>
        </div>
        {admin && (
          <>
            <div>
              <dt>用户</dt>
              <dd>
                {call.displayName} · {call.username}
              </dd>
            </div>
            <div>
              <dt>计费情况</dt>
              <dd>
                {money(call.cost)}
                {call.refunded ? " · 费用已退回" : ""}
              </dd>
            </div>
            <div>
              <dt>最近首包耗时</dt>
              <dd>
                {call.lastRequestMs == null ? "—" : `${call.lastRequestMs}毫秒`}
              </dd>
            </div>
            <div>
              <dt>最近请求 ID</dt>
              <dd>{call.lastRequestId || "—"}</dd>
            </div>
            <div className="upstream-trace-wide">
              <dt>最近请求地址</dt>
              <dd>
                {call.lastUrl ? `${call.lastMethod} ${call.lastUrl}` : "—"}
              </dd>
            </div>
          </>
        )}
      </dl>
      {call.lastRequestError && admin && (
        <p className="inline-error" role="status">
          {call.lastRequestError}
        </p>
      )}
      {call.error && (
        <p className="inline-error" role="status">
          {call.error}
        </p>
      )}
      {call.legacy && (
        <p className="muted small">
          历史任务已补入日志，上线前的请求次数和执行起点未采集。
        </p>
      )}
      {admin && (
        <details className="upstream-params">
          <summary>提交参数与本站任务 ID</summary>
          <pre>
            {JSON.stringify({ id: call.id, params: call.params }, null, 2)}
          </pre>
        </details>
      )}
    </section>
  );
}
