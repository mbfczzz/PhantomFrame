import { generationLabels, generationOptions } from "../lib";
import { useState } from "react";
import { Cable, RefreshCw } from "lucide-react";
import { Select } from "../components/Select";
import {
  UpstreamTrace,
  UpstreamTaskID,
  elapsedTime,
  upstreamPhaseLabel,
  upstreamFailureLabel,
  type UpstreamCall,
} from "../components/UpstreamTrace";
import {
  useApi,
  date,
  number,
  Badge,
  Modal,
  SearchBox,
  Pagination,
  Loading,
  ErrorState,
  Empty,
  type PageData,
  type RecordData,
} from "../lib";
import {
  UserFilter,
  DateFilters,
  useRecordFilters,
  AdminJobDetail,
} from "./AdminRecords";

function parameterSummary(params: RecordData = {}) {
  const items = [];
  if (params.duration != null) items.push(`${params.duration}s`);
  if (params.resolution != null) items.push(String(params.resolution));
  if (params.size != null) items.push(String(params.size));
  if (params.quality != null) items.push(String(params.quality));
  if (params.aspectRatio != null) items.push(String(params.aspectRatio));
  for (const [key, value] of Object.entries(params))
    if (Array.isArray(value) && value.length)
      items.push(`${key}: ${value.length}个素材`);
  return items.length
    ? items.join(" · ")
    : Object.keys(params).length
      ? "查看完整参数"
      : "默认参数";
}

export default function UpstreamLogs() {
  const { filters, page, setPage, update, reset, query } = useRecordFilters({
    search: "",
    providerId: "all",
    modelId: "all",
    userId: "all",
    status: "all",
    source: "all",
    type: "all",
    from: "",
    to: "",
  });
  const { data, isLoading, error, refetch } = useApi<
    PageData<UpstreamCall> & { summary: RecordData }
  >("/admin/upstream-calls?" + query, { refetchInterval: 5000 });
  const { data: options } = useApi<{
    providers: { id: string; name: string }[];
    models: { id: string; name: string }[];
  }>("/admin/upstream-calls/options");
  const providers = options?.providers,
    models = options?.models;
  const [selected, setSelected] = useState<UpstreamCall | null>(null);
  const [jobDetail, setJobDetail] = useState<string | null>(null);
  return (
    <section className="panel admin-records upstream-log-panel">
      <div className="section-header">
        <div>
          <h2>上游调用日志</h2>
          <p className="muted">
            按渠道追踪用户请求、上游任务和失败原因。删除生成记录后，调用日志仍保留。
          </p>
        </div>
        <button type="button" className="button" onClick={() => refetch()}>
          <RefreshCw size={15} />
          刷新
        </button>
      </div>
      <div className="admin-filter-grid">
        <label className="admin-filter">
          渠道
          <Select
            label="筛选渠道"
            searchable
            value={filters.providerId}
            onChange={(value) => update("providerId", value)}
            options={[
              { value: "all", label: "全部渠道" },
              ...(providers || []).map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
        </label>
        <label className="admin-filter">
          模型
          <Select
            label="筛选模型"
            searchable
            value={filters.modelId}
            onChange={(value) => update("modelId", value)}
            options={[
              { value: "all", label: "全部模型" },
              ...(models || []).map((m) => ({ value: m.id, label: m.name })),
            ]}
          />
        </label>
        <UserFilter
          value={filters.userId}
          onChange={(value) => update("userId", value)}
        />
        <label className="admin-filter">
          状态
          <Select
            label="调用状态"
            value={filters.status}
            onChange={(value) => update("status", value)}
            options={[
              { value: "all", label: "全部状态" },
              { value: "queued", label: "排队中" },
              { value: "running", label: "进行中" },
              { value: "refund_pending", label: "退款处理中" },
              { value: "succeeded", label: "已完成" },
              { value: "failed", label: "失败" },
              { value: "cancelled", label: "已取消" },
            ]}
          />
        </label>
        <label className="admin-filter">
          来源
          <Select
            label="日志调用来源"
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
          类型
          <Select
            label="日志生成类型"
            value={filters.type}
            onChange={(value) => update("type", value)}
            options={[
              { value: "all", label: "全部类型" },
              { value: "image", label: "图片" },
              { value: "video", label: "视频" },
              { value: "text", label: "文本" },
              { value: "audio", label: "音频" },
              { value: "upscale", label: "视频超分" },
            ]}
          />
        </label>
        <DateFilters from={filters.from} to={filters.to} onChange={update} />
      </div>
      <div className="admin-record-search">
        <SearchBox
          placeholder="搜索任务 ID、上游 ID、渠道、模型或账号"
          value={filters.search}
          onChange={(value) => update("search", value)}
        />
        <button type="button" className="button ghost" onClick={reset}>
          重置筛选
        </button>
      </div>
      <div className="admin-record-summary" aria-live="polite">
        <span>
          共 <strong>{number(data?.total || 0)}</strong> 条
        </span>
        <span>
          进行中 <strong>{number(data?.summary.active || 0)}</strong>
        </span>
        <span>
          成功 <strong>{number(data?.summary.succeeded || 0)}</strong>
        </span>
        <span>
          失败 <strong>{number(data?.summary.failed || 0)}</strong>
        </span>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : !data?.items.length ? (
        <Empty
          icon={<Cable size={28} />}
          title="暂无上游调用日志"
          text="用户提交生成任务后会自动记录；也可以重置筛选查看其他调用。"
        />
      ) : (
        <div className="table-wrap upstream-log-table">
          <table>
            <thead>
              <tr>
                <th>模型 / 渠道</th>
                <th>用户</th>
                <th>创建时间</th>
                <th>上游任务 ID</th>
                <th>参数</th>
                <th>状态</th>
                <th>总耗时</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((call) => (
                <tr key={call.id}>
                  <td>
                    <strong>{call.modelName}</strong>
                    <small>
                      {call.providerName || "未记录渠道"}
                      {call.demo ? " · 本地演示" : ""}
                    </small>
                    <small className="upstream-model-name">
                      {call.upstreamModel}
                    </small>
                  </td>
                  <td>
                    {call.username}
                    <small>{call.displayName}</small>
                  </td>
                  <td className="nowrap">
                    {date(call.createdAt)}
                    <small>
                      {call.source === "api" ? "API 调用" : "网页创作"}
                    </small>
                  </td>
                  <td>
                    <UpstreamTaskID value={call.upstreamId} />
                  </td>
                  <td>
                    <span
                      className="upstream-parameter-summary"
                      title={parameterSummary(call.params)}
                    >
                      {parameterSummary(call.params)}
                    </span>
                  </td>
                  <td>
                    <Badge status={call.status}>
                      {call.phase === "refund_pending"
                        ? "退款处理中"
                        : undefined}
                    </Badge>
                    <small>
                      {call.phase === "refund_pending"
                        ? upstreamPhaseLabel[call.phase]
                        : call.refunded
                          ? "费用已退回"
                          : upstreamPhaseLabel[call.phase] || call.phase}
                    </small>
                    {call.failurePhase && (
                      <small>
                        失败环节：
                        {upstreamFailureLabel[call.failurePhase] ||
                          call.failurePhase}
                      </small>
                    )}
                    {call.lastHttpStatus && call.lastHttpStatus >= 400 ? (
                      <small className="upstream-http-error">
                        HTTP {call.lastHttpStatus}
                      </small>
                    ) : null}
                  </td>
                  <td className="nowrap">{elapsedTime(call.totalMs)}</td>
                  <td>
                    <button
                      type="button"
                      className="text-link"
                      aria-label={`查看上游调用 ${call.id}`}
                      onClick={() => setSelected(call)}
                    >
                      查看详情
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="muted small upstream-timing-note">
        总耗时从提交起计算，包含排队。详情可查看执行耗时、上游请求次数和最近
        HTTP 状态。
      </p>
      <Pagination
        page={page}
        total={data?.total}
        limit={20}
        onChange={setPage}
      />
      {selected && (
        <Modal
          title="上游调用详情"
          description={`${selected.modelName} · ${selected.providerName}`}
          icon={<Cable size={20} />}
          onClose={() => setSelected(null)}
          wide
        >
          <UpstreamTrace id={selected.id} admin />
          <div className="modal-footer dialog-actions">
            <button
              type="button"
              className="button"
              onClick={() => setSelected(null)}
            >
              关闭
            </button>
            {selected.jobAvailable ? (
              <button
                type="button"
                className="button primary"
                onClick={() => setJobDetail(selected.id)}
              >
                查看生成任务
              </button>
            ) : (
              <span className="muted small">生成记录已删除，日志仍保留。</span>
            )}
          </div>
        </Modal>
      )}
      {jobDetail && (
        <AdminJobDetail id={jobDetail} onClose={() => setJobDetail(null)} />
      )}
    </section>
  );
}
