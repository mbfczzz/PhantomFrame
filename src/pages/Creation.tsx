import GenerationTypeIcon from "../components/GenerationTypeIcon";
import TextOutput from "../components/TextOutput";
import { generationLabels, generationOptions } from "../lib";
import { useState, useEffect, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Select } from "../components/Select";
import { referenceLimitLabel, isImageCountParameter } from "../model-config";
import { specificationRates, priceExtraLabels } from "../pricing";
import { ModelCapabilities } from "../components/ModelCapabilities";
import { UpstreamTrace } from "../components/UpstreamTrace";
import {
  ModelStatus,
  type ModelStatusResponse,
} from "../components/ModelStatus";
import {
  Image,
  Video,
  Plus,
  ArrowUp,
  ChevronsUp,
  ChevronLeft,
  ChevronRight,
  Rocket,
  X,
  FolderOpen,
  Upload,
  Download,
  Trash2,
  RotateCcw,
  Copy,
  Clock3,
  Music,
  Check,
  SlidersHorizontal,
  Info,
  Sparkles,
  Eye,
  Loader2,
  ExternalLink,
} from "lucide-react";
import {
  useApi,
  useRefresh,
  useToast,
  useAction,
  useConfirm,
  api,
  post,
  defaults,
  money,
  date,
  unitLabel,
  statusLabel,
  Modal,
  Field,
  Empty,
  Loading,
  ErrorState,
  PageHeader,
  Tabs,
  SearchBox,
  Pagination,
  Badge,
  copy,
  type Model,
  type Job,
  type Asset,
  type User,
  type RecordData,
  type PageData,
  type Parameter,
} from "../lib";

const types = [
  { value: "all", label: "全部" },
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
  { value: "text", label: "文本" },
  { value: "audio", label: "音频" },
  { value: "upscale", label: "视频超分" },
];
export function Media({
  asset,
  controls = true,
}: {
  asset: { url: string; type: string; name?: string; downloadUrl?: string };
  controls?: boolean;
}) {
  if (asset.type === "text")
    return (
      <TextOutput url={asset.downloadUrl || asset.url} compact={!controls} />
    );
  if (asset.type === "video")
    return <video src={asset.url} controls={controls} preload="metadata" />;
  if (asset.type === "audio")
    return (
      <div className="audio-preview">
        <Music size={36} />
        {controls && <audio src={asset.url} controls />}
      </div>
    );
  return <img src={asset.url} alt={asset.name || "生成作品"} loading="lazy" />;
}
export function AssetPicker({
  onClose,
  onPick,
  multiple = true,
  allowed = ["image", "video", "audio"],
  existingIds = [],
  limits,
  remainingTotal = Infinity,
}: {
  onClose: () => void;
  onPick: (assets: Asset[]) => void;
  multiple?: boolean;
  allowed?: string[];
  existingIds?: string[];
  limits?: Record<string, number>;
  remainingTotal?: number;
}) {
  const [type, setType] = useState(allowed.length === 1 ? allowed[0] : "all");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Asset[]>([]);
  const { data, isLoading, error, refetch } = useApi<PageData<Asset>>(
    `/assets?type=${type}&page=${page}`,
  );
  const { busy, act } = useAction();
  const fileRef = useRef<HTMLInputElement>(null);
  const refresh = useRefresh();
  const items = data?.items.filter((a) => allowed.includes(a.type)) || [];
  const atLimit = (asset: Asset) =>
    !selected.some((item) => item.id === asset.id) &&
    (selected.length >= remainingTotal ||
      selected.filter((item) => item.type === asset.type).length >=
        (limits?.[asset.type] ?? Infinity));
  return (
    <Modal
      title="从资产库选择"
      description="选择需要引用的素材，已添加的内容会自动排除。"
      icon={<FolderOpen size={20} />}
      onClose={onClose}
      wide
      className="asset-library-dialog"
    >
      <div className="toolbar">
        <Tabs
          value={type}
          onChange={(v) => {
            setType(v);
            setPage(1);
          }}
          items={[
            { value: "all", label: "全部" },
            ...allowed.map((v) => ({
              value: v,
              label: { image: "图片", video: "视频", audio: "音频" }[v] || v,
            })),
          ]}
        />
        <button
          className="button"
          onClick={() => fileRef.current?.click()}
          disabled={busy}
        >
          <Upload size={16} />
          {busy ? "上传中…" : "本地上传"}
        </button>
        <input
          hidden
          type="file"
          ref={fileRef}
          multiple={multiple}
          accept={allowed.map((t) => t + "/*").join(",")}
          onChange={(e) => {
            const files = Array.from(e.target.files || []);
            e.target.value = "";
            act(async () => {
              if (selected.length + files.length > remainingTotal)
                throw new Error(
                  `参考素材合计还可添加 ${remainingTotal} 个，请减少文件后重试`,
                );
              const counts: Record<string, number> = {};
              for (const asset of selected)
                counts[asset.type] = (counts[asset.type] || 0) + 1;
              for (const file of files) {
                const type = file.type.split("/")[0];
                if (!allowed.includes(type))
                  throw new Error("所选文件包含当前模型不支持的素材类型");
                counts[type] = (counts[type] || 0) + 1;
                if (counts[type] > (limits?.[type] ?? Infinity))
                  throw new Error(
                    "所选素材超出当前模型的数量限制，请减少文件后重试",
                  );
              }
              const added: Asset[] = [];
              for (const file of files) {
                const fd = new FormData();
                fd.append("file", file);
                added.push(
                  await api<Asset>("/assets", { method: "POST", body: fd }),
                );
              }
              refresh("/assets");
              setSelected((s) =>
                multiple ? [...s, ...added] : added.slice(0, 1),
              );
            });
          }}
        />
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : items.length ? (
        <div className="asset-picker-grid">
          {items.map((a) => (
            <button
              key={a.id}
              className={
                "asset-choice " +
                (selected.some((s) => s.id === a.id) ? "selected" : "")
              }
              disabled={busy || existingIds.includes(a.id) || atLimit(a)}
              aria-label={
                existingIds.includes(a.id)
                  ? `${a.name}（已添加）`
                  : atLimit(a)
                    ? `${a.name}（超出数量上限）`
                    : `选择 ${a.name}`
              }
              onClick={() =>
                setSelected((s) =>
                  s.some((x) => x.id === a.id)
                    ? s.filter((x) => x.id !== a.id)
                    : multiple
                      ? [...s, a]
                      : [a],
                )
              }
            >
              <Media asset={a} controls={false} />
              <strong>
                {a.name}
                {existingIds.includes(a.id) ? " · 已添加" : ""}
              </strong>
              {selected.some((s) => s.id === a.id) && (
                <span className="select-mark">
                  <Check size={16} />
                </span>
              )}
            </button>
          ))}
        </div>
      ) : (
        <Empty
          title="资产库中暂无可选素材"
          text="上传图片、视频或音频，作为创作的参考内容。"
          icon={<FolderOpen size={30} />}
        >
          <button
            className="button primary"
            disabled={busy}
            onClick={() => fileRef.current?.click()}
          >
            <Upload size={16} />
            上传参考素材
          </button>
        </Empty>
      )}
      <Pagination page={page} total={data?.total} onChange={setPage} />
      <div className="modal-footer">
        <span className="muted">已选择 {selected.length} 个</span>
        <div>
          <button className="button" onClick={onClose}>
            取消
          </button>
          <button
            className="button primary"
            disabled={busy || !selected.length}
            onClick={() => {
              onPick(selected);
              onClose();
            }}
          >
            添加
          </button>
        </div>
      </div>
    </Modal>
  );
}
export function ParameterFields({
  model,
  params,
  onChange,
}: {
  model: Model;
  params: RecordData;
  onChange: (params: RecordData) => void;
}) {
  return (
    <>
      {model.parameters
        .filter((p) => p.type !== "assets" && !isImageCountParameter(model, p))
        .map((p) => (
          <label
            className={
              "parameter " + (p.type === "boolean" ? "checkbox-param" : "")
            }
            key={p.name}
          >
            <span>{p.label}</span>
            {p.type === "select" ? (
              <Select
                label={p.label}
                value={String(params[p.name] ?? "")}
                onChange={(value) => {
                  const option = p.options?.find(
                    (v) => String(v.v ?? v) === value,
                  );
                  onChange({ ...params, [p.name]: option?.v ?? option });
                }}
                options={(p.options || []).map((option) => ({
                  value: String(option.v ?? option),
                  label: String(option.l ?? option),
                }))}
              />
            ) : p.type === "boolean" ? (
              <input
                aria-label={p.label}
                type="checkbox"
                checked={!!params[p.name]}
                onChange={(e) =>
                  onChange({ ...params, [p.name]: e.target.checked })
                }
              />
            ) : (
              <input
                aria-label={p.label}
                type={p.type === "number" ? "number" : "text"}
                value={params[p.name] ?? ""}
                min={p.min ?? undefined}
                max={p.max ?? undefined}
                onChange={(e) =>
                  onChange({
                    ...params,
                    [p.name]:
                      p.type === "number"
                        ? Number(e.target.value)
                        : e.target.value,
                  })
                }
              />
            )}
          </label>
        ))}
    </>
  );
}
export function JobCard({ job, onDetail }: { job: Job; onDetail: () => void }) {
  const refresh = useRefresh();
  const { act } = useAction();
  const toast = useToast();
  return (
    <article className="job-card">
      <header>
        <strong title={job.modelName}>{job.modelName}</strong>
        <Badge status={job.status} />
      </header>
      <button className="job-preview" onClick={onDetail}>
        {job.outputs[0] ? (
          <Media asset={job.outputs[0]} controls={false} />
        ) : ["queued", "running", "refund_pending"].includes(job.status) ? (
          <div className="dreaming job-processing">
            {job.status === "queued" ? (
              <Clock3 size={25} />
            ) : (
              <Loader2 size={25} className="spin" />
            )}
            <strong>
              {job.status === "refund_pending"
                ? "退款处理中"
                : job.status === "queued"
                  ? "正在排队"
                  : "正在生成"}
            </strong>
            <span>
              {job.status === "refund_pending"
                ? "生成未完成，系统正在自动重试退款，无需重复提交。"
                : job.status === "queued"
                  ? "轮到此任务后会自动开始"
                  : "完成后会自动显示在这里"}
            </span>
          </div>
        ) : (
          <div className="failed-preview">
            <Info size={28} />
            <span>{job.error || "任务已取消"}</span>
          </div>
        )}
        {job.demo && <span className="demo-label">演示</span>}
      </button>
      <p className="job-prompt" title={job.prompt}>
        {job.prompt}
      </p>
      <footer>
        <span>{date(job.createdAt)}</span>
        <div>
          {job.outputs[0] && (
            <a
              className="icon-button"
              title="下载作品"
              href={
                job.outputs[0].downloadUrl || job.outputs[0].url + "?download=1"
              }
              download
            >
              <Download size={15} />
            </a>
          )}
          <button
            className="icon-button"
            title="复制提示词"
            onClick={() =>
              copy(job.prompt)
                .then(() => toast("提示词已复制"))
                .catch(() => toast("复制失败", true))
            }
          >
            <Copy size={14} />
          </button>
          <button className="icon-button" title="任务详情" onClick={onDetail}>
            <Eye size={15} />
          </button>
        </div>
      </footer>
      {job.status === "queued" && (
        <button
          className="button ghost full small-button"
          onClick={() =>
            act(async () => {
              await post(`/jobs/${job.id}/cancel`);
              refresh("/jobs", "/me");
            }, "任务已取消，费用已退还")
          }
        >
          取消排队
        </button>
      )}
    </article>
  );
}
export function JobDetail({ job, onClose }: { job: Job; onClose: () => void }) {
  const confirm = useConfirm();
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const { data: current } = useApi<Job>("/jobs/" + job.id, {
    refetchInterval: ["running", "queued", "refund_pending"].includes(
      job.status,
    )
      ? 2000
      : false,
  });
  const j = current || job;
  return (
    <Modal
      title="任务详情"
      description="查看生成结果、产生费用与任务参数。"
      icon={<Clock3 size={20} />}
      onClose={onClose}
      wide
    >
      <div className="detail-grid">
        <div>
          <small>模型</small>
          <strong>{j.modelName}</strong>
        </div>
        <div>
          <small>状态</small>
          <Badge status={j.status} />
        </div>
        <div>
          <small>消费金额</small>
          <strong>
            {money(j.cost)}
            {j.refunded ? "（已退还）" : ""}
          </strong>
        </div>
        <div>
          <small>提交时间</small>
          <strong>{date(j.createdAt)}</strong>
        </div>
      </div>
      <Field label="提示词">
        <div className="read-block">{j.prompt}</div>
      </Field>
      <UpstreamTrace id={job.id} />
      {j.demo && (
        <div className="notice">
          <Info size={16} />
          此任务使用演示通道，输出为演示素材。
        </div>
      )}
      <div className="detail-media">
        {j.outputs.map((o) => (
          <div key={o.assetId}>
            <Media asset={o} />
            <a
              className="button"
              href={o.downloadUrl || o.url + "?download=1"}
              download
            >
              <Download size={15} />
              下载
            </a>
          </div>
        ))}
      </div>
      {j.error && <div className="inline-error">{j.error}</div>}
      <details>
        <summary>请求参数 / 任务 ID</summary>
        <pre>
          {JSON.stringify(
            { id: j.id, model: j.modelId, params: j.params },
            null,
            2,
          )}
        </pre>
      </details>
      <div className="modal-footer">
        <button
          className="button danger"
          disabled={
            busy || ["queued", "running", "refund_pending"].includes(j.status)
          }
          onClick={async () => {
            if (
              await confirm("确定删除此生成记录？", {
                title: "删除生成记录",
                confirmLabel: "删除记录",
              })
            )
              act(async () => {
                await api("/jobs/" + j.id, { method: "DELETE" });
                refresh("/jobs");
                onClose();
              }, "记录已删除");
          }}
        >
          <Trash2 size={15} />
          删除记录
        </button>
        <Link
          className="button primary"
          to={
            "/generate?model=" +
            encodeURIComponent(j.modelId) +
            "&prompt=" +
            encodeURIComponent(j.prompt)
          }
          onClick={onClose}
        >
          <RotateCcw size={15} />
          再次创作
        </Link>
      </div>
    </Modal>
  );
}
export function Models() {
  const {
    data: status,
    isLoading: statusLoading,
    error: statusError,
  } = useApi<ModelStatusResponse>("/models/status", {
    refetchOnWindowFocus: "always",
    refetchOnMount: "always",
    refetchInterval: 30000,
  });
  const { data, isLoading, error, refetch } = useApi<Model[]>("/models", {
    refetchOnWindowFocus: "always",
    refetchOnMount: "always",
    refetchInterval: 30000,
  });
  const [search, setSearch] = useState("");
  const [type, setType] = useState("all");
  const [category, setCategory] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected = data?.find((model) => model.id === selectedId);
  const toast = useToast();
  const matching =
    data?.filter(
      (m) =>
        (type === "all" || m.type === type) &&
        (m.name + " " + m.id + " " + m.category + " " + m.providerName)
          .toLowerCase()
          .includes(search.toLowerCase()),
    ) || [];
  const groups = Array.from(new Set(matching.map((m) => m.category)));
  const filtered = matching.filter(
    (m) => category === "all" || m.category === category,
  );
  return (
    <div className="page catalog-page">
      <PageHeader
        title="模型与价格"
        description="直接比较素材上限、生成规格与价格，选择适合的模型。"
      >
        <span className="muted">{filtered.length} 个模型</span>
      </PageHeader>
      <div className="toolbar divider">
        <SearchBox
          value={search}
          onChange={setSearch}
          placeholder="搜索模型名称、ID 或渠道"
        />
        <Tabs
          value={type}
          onChange={(value) => {
            setType(value);
            setCategory("all");
          }}
          items={types}
        />
      </div>
      <div className="catalog-categories" aria-label="按模型分组筛选">
        <span>渠道 / 分组</span>
        <div>
          <button
            aria-pressed={category === "all"}
            onClick={() => setCategory("all")}
          >
            全部
          </button>
          {groups.map((group) => (
            <button
              aria-pressed={category === group}
              key={group}
              onClick={() => setCategory(group)}
            >
              {group}
            </button>
          ))}
        </div>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : !filtered.length ? (
        <Empty title="没有匹配的模型" />
      ) : (
        <div className="model-grid catalog-grid">
          {filtered.map((m) => {
            const prices = m.prices
              .filter((p) => p.enabled)
              .sort((a, b) => a.credits - b.credits);
            return (
              <article className={"catalog-card " + m.type} key={m.id}>
                <div className="catalog-card-top">
                  <span className={"model-glyph " + m.type}>
                    {<GenerationTypeIcon kind={m.type} />}
                  </span>
                  <Badge status={m.type}>
                    {`${generationLabels[m.type] || m.type}生成`}
                  </Badge>
                </div>
                <span className="catalog-channel">
                  {m.providerName}
                  {m.category && m.category !== "默认分组"
                    ? ` · ${m.category}`
                    : ""}
                </span>
                <h3>{m.name}</h3>
                <code>{m.id}</code>
                {m.description && (
                  <p className="catalog-description">{m.description}</p>
                )}
                <ModelCapabilities model={m} />
                {[
                  ...new Set(
                    prices.flatMap((price) => priceExtraLabels(price)),
                  ),
                ].map((note) => (
                  <small className="price-extra-note" key={note}>
                    {note}
                  </small>
                ))}
                {specificationRates(m).length > 0 && (
                  <div className="catalog-spec-prices" aria-label="规格价格">
                    <h4>规格单价</h4>
                    {specificationRates(m).map((price) => (
                      <span key={price.id}>
                        <span>{price.specification}</span>
                        <strong>
                          {money(price.credits)}/{unitLabel[price.unit]}
                        </strong>
                      </span>
                    ))}
                  </div>
                )}
                <ModelStatus
                  data={status?.models[m.id]}
                  loading={statusLoading}
                  error={!!statusError}
                />
                <div className="catalog-card-bottom">
                  <div className="catalog-price">
                    {prices.length ? (
                      <>
                        <strong>{money(prices[0].credits)}</strong>
                        <span>
                          / {unitLabel[prices[0].unit]}
                          {prices.length > 1 ? " 起" : ""}
                        </span>
                      </>
                    ) : (
                      <span>价格待配置</span>
                    )}
                  </div>
                  <button
                    className="text-link"
                    onClick={() => {
                      setSelectedId(m.id);
                      void refetch();
                    }}
                  >
                    完整参数 <ChevronRight size={13} />
                  </button>
                </div>
                <Link
                  className="catalog-use"
                  to={"/generate?model=" + encodeURIComponent(m.id)}
                >
                  使用这个模型 <ArrowUp size={14} />
                </Link>
              </article>
            );
          })}
        </div>
      )}
      {selected && (
        <Modal
          title={selected.name}
          description="查看规格价格与支持的创作参数。"
          icon={<GenerationTypeIcon kind={selected.type} />}
          onClose={() => setSelectedId(null)}
          wide
          className="model-detail-dialog"
        >
          <div className="toolbar">
            <Badge status={selected.type}>
              {`${generationLabels[selected.type] || selected.type}生成`}
            </Badge>
            <code className="muted small">{selected.id}</code>
          </div>
          {selected.description && (
            <p className="muted">{selected.description}</p>
          )}
          <h3>价格</h3>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>规格</th>
                  <th>计费条件</th>
                  <th>单价</th>
                </tr>
              </thead>
              <tbody>
                {!selected.prices.some((p) => p.enabled) && (
                  <tr>
                    <td colSpan={3} className="muted">
                      价格待配置
                    </td>
                  </tr>
                )}
                {selected.prices
                  .filter((p) => p.enabled)
                  .map((p) => (
                    <tr key={p.id}>
                      <td>
                        {p.label}
                        {priceExtraLabels(p).map((note) => (
                          <small className="price-extra-note" key={note}>
                            {note}
                          </small>
                        ))}
                      </td>
                      <td>
                        <div className="condition-tags">
                          {Object.entries(p.conditions).length ? (
                            Object.entries(p.conditions).map(
                              ([name, value]) => {
                                const parameter = selected.parameters.find(
                                  (item) => item.name === name,
                                );
                                const option = parameter?.options?.find(
                                  (item) =>
                                    String(item.v ?? item) === String(value),
                                );
                                return (
                                  <span key={name}>
                                    {parameter?.label || name}：
                                    {String(option?.l ?? value)}
                                  </span>
                                );
                              },
                            )
                          ) : (
                            <span className="muted">默认规格</span>
                          )}
                        </div>
                      </td>
                      <td className="gold">
                        {money(p.credits)}/{unitLabel[p.unit]}
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
          <h3>参数说明</h3>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>名称</th>
                  <th>默认值</th>
                  <th>可选值 / 范围</th>
                </tr>
              </thead>
              <tbody>
                {selected.parameters.map((p) => (
                  <tr key={p.name}>
                    <td>{p.label}</td>
                    <td>
                      {p.type === "boolean"
                        ? p.default
                          ? "开启"
                          : "关闭"
                        : p.type === "assets"
                          ? "未添加"
                          : String(
                              p.options?.find(
                                (option) =>
                                  String(option.v ?? option) ===
                                  String(p.default),
                              )?.l ??
                                p.default ??
                                "—",
                            )}
                    </td>
                    <td>
                      {p.options?.length ? (
                        <div className="parameter-option-tags">
                          {p.options.map((option, index) => (
                            <span key={index}>
                              {String(option.l ?? option)}
                            </span>
                          ))}
                        </div>
                      ) : p.type === "boolean" ? (
                        "开启 / 关闭"
                      ) : p.type === "assets" ? (
                        referenceLimitLabel(p)
                      ) : p.type === "number" &&
                        (p.min != null || p.max != null) ? (
                        `${p.min ?? "不限"} ～ ${p.max ?? "不限"}`
                      ) : (
                        "自定义输入"
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <details className="model-api-details">
            <summary>API 接入示例</summary>
            <pre>
              {JSON.stringify(
                {
                  modelId: selected.id,
                  prompt: "描述你想要的画面",
                  params: defaults(selected),
                },
                null,
                2,
              )}
            </pre>
            <button
              className="button"
              onClick={() =>
                copy(
                  JSON.stringify(
                    {
                      modelId: selected.id,
                      prompt: "描述你想要的画面",
                      params: defaults(selected),
                    },
                    null,
                    2,
                  ),
                ).then(() => toast("请求体已复制"))
              }
            >
              <Copy size={15} />
              复制请求体
            </button>
            <Link className="text-link" to="/developer">
              管理 API Key <ExternalLink size={13} />
            </Link>
          </details>
          <div className="modal-footer">
            <span className="muted">
              按所选规格计费，提交前可查看实际消耗。
            </span>
            <Link
              className="button primary"
              to={"/generate?model=" + encodeURIComponent(selected.id)}
            >
              <Rocket size={16} />
              立即使用
            </Link>
          </div>
        </Modal>
      )}
    </div>
  );
}
export function HistoryPage() {
  const [initialSearch] = useSearchParams();
  const [type, setType] = useState("all");
  const [status, setStatus] = useState("all");
  const [search, setSearch] = useState(initialSearch.get("search") || "");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [page, setPage] = useState(1);
  const [detail, setDetail] = useState<Job | null>(null);
  const filtered =
    type !== "all" || status !== "all" || !!search || !!from || !!to;
  const clearFilters = () => {
    setType("all");
    setStatus("all");
    setSearch("");
    setFrom("");
    setTo("");
  };
  const query = new URLSearchParams({
    type,
    status,
    search,
    from,
    to,
    page: String(page),
  });
  const { data, isLoading, error, refetch } = useApi<PageData<Job>>(
    "/jobs?" + query,
    { refetchInterval: 3000 },
  );
  useEffect(() => setPage(1), [type, status, search, from, to]);
  return (
    <div className="page history-page">
      <PageHeader
        title="生成记录"
        description="查看任务进度、下载结果或复用提示词。"
      >
        <span className="count-badge">{data?.total || 0} 条记录</span>
      </PageHeader>
      <div className="filter-bar">
        <label>
          类型
          <Select
            label="记录类型"
            value={type}
            onChange={setType}
            options={types.map((t) => ({
              value: t.value,
              label: t.value === "all" ? "全部类型" : t.label,
            }))}
          />
        </label>
        <label>
          状态
          <Select
            label="任务状态"
            value={status}
            onChange={setStatus}
            options={[
              { value: "all", label: "全部状态" },
              ...Object.entries(statusLabel)
                .slice(0, 5)
                .map(([value, label]) => ({ value, label })),
            ]}
          />
        </label>
        <label>
          时间
          <input
            aria-label="开始时间"
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
          <span>—</span>
          <input
            aria-label="结束时间"
            type="date"
            value={to}
            onChange={(e) => setTo(e.target.value)}
          />
        </label>
        <SearchBox
          value={search}
          onChange={setSearch}
          placeholder="搜索提示词、模型或任务 ID"
        />
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : data?.items.length ? (
        <div className="job-grid">
          {data.items.map((j) => (
            <JobCard key={j.id} job={j} onDetail={() => setDetail(j)} />
          ))}
        </div>
      ) : (
        <Empty
          title={filtered ? "没有符合条件的记录" : "暂无记录"}
          text={
            filtered
              ? "调整类型、状态或时间范围，也可以清空筛选重新查看。"
              : "从一个想法开始，创作你的第一份作品。"
          }
          icon={<HistoryIcon />}
        >
          {filtered ? (
            <button className="button" onClick={clearFilters}>
              清空筛选
            </button>
          ) : (
            <Link className="button primary" to="/generate">
              开始创作 <ArrowUp size={15} />
            </Link>
          )}
        </Empty>
      )}
      <Pagination page={page} total={data?.total} onChange={setPage} />
      {detail && <JobDetail job={detail} onClose={() => setDetail(null)} />}
    </div>
  );
}
function HistoryIcon() {
  return <Clock3 size={30} />;
}
export function Assets() {
  const confirm = useConfirm();
  const [type, setType] = useState("all");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [preview, setPreview] = useState<Asset | null>(null);
  const { data, isLoading, error, refetch } = useApi<PageData<Asset>>(
    "/assets?" + new URLSearchParams({ type, search, page: String(page) }),
  );
  const file = useRef<HTMLInputElement>(null);
  const { busy, act } = useAction();
  const refresh = useRefresh();
  useEffect(() => setPage(1), [type, search]);
  return (
    <div className="page assets-page">
      <PageHeader title="资产库" description="管理上传素材和生成文件。">
        <button
          className="button primary"
          onClick={() => file.current?.click()}
          disabled={busy}
        >
          <Upload size={16} />
          {busy ? "上传中…" : "上传素材"}
        </button>
        <input
          hidden
          ref={file}
          type="file"
          multiple
          accept="image/png,image/jpeg,image/webp,image/gif,video/mp4,video/webm,audio/mpeg,audio/wav"
          onChange={(e) => {
            const files = Array.from(e.target.files || []);
            act(async () => {
              for (const f of files) {
                const fd = new FormData();
                fd.append("file", f);
                await api("/assets", { method: "POST", body: fd });
              }
              refresh("/assets");
            }, "素材已上传");
            e.target.value = "";
          }}
        />
      </PageHeader>
      <div className="toolbar divider">
        <Tabs
          value={type}
          onChange={setType}
          items={types.filter((item) => item.value !== "upscale")}
        />
        <SearchBox
          value={search}
          onChange={setSearch}
          placeholder="搜索素材名称"
        />
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : data?.items.length ? (
        <div className="asset-grid">
          {data.items.map((a) => (
            <article className="asset-card" key={a.id}>
              <button className="asset-preview" onClick={() => setPreview(a)}>
                <Media asset={a} controls={false} />
              </button>
              <div className="asset-info">
                <strong title={a.name}>{a.name}</strong>
                <small>
                  {(a.size / 1024 / 1024).toFixed(2)} MB · {date(a.createdAt)}
                </small>
                <div>
                  <a
                    className="icon-button"
                    title="下载素材"
                    href={a.downloadUrl || a.url + "?download=1"}
                    download
                  >
                    <Download size={15} />
                  </a>
                  <button
                    className="icon-button danger"
                    title="删除素材"
                    onClick={async () => {
                      if (
                        await confirm(
                          "确定删除此素材？引用此素材的生成记录将无法继续显示该文件。",
                          { title: "删除素材", confirmLabel: "删除素材" },
                        )
                      )
                        act(async () => {
                          await api("/assets/" + a.id, { method: "DELETE" });
                          refresh("/assets");
                        }, "素材已删除");
                    }}
                  >
                    <Trash2 size={15} />
                  </button>
                </div>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <Empty
          title={
            type !== "all" || search ? "没有找到匹配的素材" : "资产库还是空的"
          }
          text={
            type !== "all" || search
              ? "试试其他素材类型或名称，也可以清空筛选。"
              : "上传参考素材，或生成作品后自动保存到这里。"
          }
          icon={<FolderOpen size={32} />}
        >
          {type !== "all" || search ? (
            <button
              className="button"
              onClick={() => {
                setType("all");
                setSearch("");
              }}
            >
              清空筛选
            </button>
          ) : (
            <button
              className="button primary"
              disabled={busy}
              onClick={() => file.current?.click()}
            >
              <Upload size={15} />
              上传第一份素材
            </button>
          )}
        </Empty>
      )}
      <Pagination page={page} total={data?.total} onChange={setPage} />
      {preview && (
        <Modal
          title={preview.name}
          description={`${(preview.size / 1024 / 1024).toFixed(2)} MB · ${date(preview.createdAt)}`}
          icon={<FolderOpen size={20} />}
          onClose={() => setPreview(null)}
          wide
        >
          <div className="detail-media">
            <Media asset={preview} />
          </div>
          <div className="modal-footer dialog-actions">
            <a
              href={preview.downloadUrl || preview.url + "?download=1"}
              download
              className="button primary"
            >
              <Download size={16} />
              下载素材
            </a>
          </div>
        </Modal>
      )}
    </div>
  );
}
