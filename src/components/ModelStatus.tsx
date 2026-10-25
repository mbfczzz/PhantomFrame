export type ModelStatusData = {
  completed24h: number;
  succeeded24h: number;
  failed24h: number;
  successRate24h: number | null;
  minSeconds24h: number | null;
  maxSeconds24h: number | null;
  recent: ("succeeded" | "failed")[];
};
export type ModelStatusResponse = {
  models: Record<string, ModelStatusData>;
  updatedAt: string;
  windowHours: number;
};
const seconds = (value: number) =>
  value < 1 ? `${Number(value.toFixed(2))}` : `${Math.round(value)}`;

export function ModelStatus({
  data,
  loading,
  error,
}: {
  data?: ModelStatusData;
  loading: boolean;
  error: boolean;
}) {
  const recent = data?.recent || [];
  const successes = recent.filter((status) => status === "succeeded").length;
  const rate = data?.successRate24h;
  const unavailable = error || (!loading && !data);
  const message = loading ? "加载中…" : unavailable ? "暂不可用" : "暂无调用";
  const duration =
    data?.minSeconds24h != null && data.maxSeconds24h != null
      ? data.minSeconds24h === data.maxSeconds24h
        ? `${seconds(data.minSeconds24h)} 秒`
        : `${seconds(data.minSeconds24h)}–${seconds(data.maxSeconds24h)} 秒`
      : "—";
  return (
    <section
      className="model-status"
      aria-label="模型调用状态"
      aria-busy={loading}
    >
      <div className="model-status-top">
        <span
          className="model-status-rate"
          title="按全站保留记录统计，近 24 小时成功数 ÷（成功数＋失败数）；取消、进行中和演示任务不计入。"
        >
          24小时成功率
          <strong
            className={
              rate == null
                ? ""
                : rate === 100
                  ? "succeeded"
                  : rate === 0
                    ? "failed"
                    : "mixed"
            }
          >
            {rate == null ? message : `${rate}%`}
          </strong>
        </span>
        <div
          className="model-status-recent"
          title="最近已完成的真实调用，最多 10 次，左旧右新；绿色成功、红色失败、灰色无记录。"
        >
          <span>{recent.length ? `近${recent.length}次` : "近10次"}</span>
          <div
            className="model-status-bars"
            role="img"
            aria-label={
              recent.length
                ? `最近 ${recent.length} 次调用：${successes} 次成功，${recent.length - successes} 次失败，左旧右新`
                : "暂无已完成调用"
            }
          >
            {Array.from({ length: 10 - recent.length }, (_, index) => (
              <i className="empty" key={`empty-${index}`} />
            ))}
            {recent.map((status, index) => (
              <i
                className={status}
                key={index}
                title={`第 ${index + 1} 次：${status === "succeeded" ? "成功" : "失败"}`}
              />
            ))}
          </div>
          {!!recent.length && (
            <strong>{Math.round((successes / recent.length) * 100)}%</strong>
          )}
        </div>
      </div>
      <div
        className="model-status-duration"
        title="近 24 小时成功任务从提交到完成的耗时范围，包含排队与结果下载时间。"
      >
        <span>24小时完成耗时</span>
        <span>{duration}</span>
        <small>含排队</small>
      </div>
    </section>
  );
}
