import { generationLabels, generationOptions } from "../lib";
import { useState, useEffect, useRef } from "react";
import { Link } from "react-router-dom";
import {
  Boxes,
  Plus,
  RefreshCw,
  Pencil,
  Trash2,
  ArrowUpRight,
  Upload,
  KeyRound,
} from "lucide-react";
import {
  useApi,
  useAction,
  useConfirm,
  useRefresh,
  useToast,
  api,
  post,
  Modal,
  Form,
  Field,
  Tabs,
  SearchBox,
  Pagination,
  Badge,
  Loading,
  ErrorState,
  Empty,
  JsonEditor,
  type Model,
  type Parameter,
  type RecordData,
  type PageData,
} from "../lib";
import { ReferenceSettings } from "../components/ReferenceSettings";

type ChannelModel = {
  id: string;
  providerId: string;
  upstreamModel: string;
  name: string;
  type: string;
  description: string;
  parameters: Parameter[];
  config?: RecordData;
  origin: string;
  hasKey: boolean;
  publishedModels: Pick<Model, "id" | "name" | "enabled" | "type">[];
};
const managementURL = (id: string) =>
  "/admin/models?model=" + encodeURIComponent(id) + "&tab=pricing";
const defaultAlias = (providerName: string, modelName: string) => {
  const prefix = Array.from(providerName.trim())[0]?.toLowerCase() || "p";
  return Array.from(`${prefix}-${modelName.trim()}`).slice(0, 120).join("");
};

export default function ProviderModels({
  provider,
  onClose,
}: {
  provider: RecordData;
  onClose: () => void;
}) {
  const [search, setSearch] = useState(""),
    [status, setStatus] = useState("all"),
    [page, setPage] = useState(1);
  const [appliedSearch, setAppliedSearch] = useState("");
  const [composing, setComposing] = useState(false);
  const resultsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (composing) return;
    const timer = window.setTimeout(
      () => {
        setAppliedSearch(search.trim());
        setPage(1);
      },
      search.trim() ? 250 : 0,
    );
    return () => window.clearTimeout(timer);
  }, [search, composing]);
  const [edit, setEdit] = useState<ChannelModel | null | undefined>(undefined);
  const [keyEntry, setKeyEntry] = useState<ChannelModel | null>(null);
  const [publish, setPublish] = useState<ChannelModel | null>(null),
    [published, setPublished] = useState<Model | null>(null);
  const path =
    "/admin/providers/" + encodeURIComponent(provider.id) + "/models";
  const queryPath =
    path +
    "?" +
    new URLSearchParams({
      search: appliedSearch,
      status,
      page: String(page),
      limit: "20",
    });
  const { data, isLoading, isFetching, isPlaceholderData, error, refetch } =
    useApi<PageData<ChannelModel>>(queryPath, {
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        api(queryPath, { signal }),
      placeholderData: (
        previous: PageData<ChannelModel> | undefined,
        query: { queryKey: readonly unknown[] } | undefined,
      ) =>
        String(query?.queryKey[0]).startsWith(path + "?")
          ? previous
          : undefined,
    });
  const updating = composing || search.trim() !== appliedSearch || isFetching;
  const staleResults =
    composing || search.trim() !== appliedSearch || isPlaceholderData;
  useEffect(() => {
    if (!isPlaceholderData) resultsRef.current?.scrollTo({ top: 0 });
  }, [appliedSearch, status, page, isPlaceholderData]);
  const { act, busy } = useAction();
  const toast = useToast(),
    refresh = useRefresh(),
    confirm = useConfirm();
  const refreshModels = () =>
    refresh(path, "/admin/providers", "/admin/models", "/models");
  return (
    <Modal
      title="渠道模型"
      description={`${provider.name} · 先维护支持列表，发布后进入模型管理。`}
      icon={<Boxes size={20} />}
      wide
      className="channel-model-dialog"
      onClose={onClose}
    >
      <div className="channel-model-toolbar">
        <div className="channel-model-actions">
          <button
            className="button"
            disabled={busy || !provider.modelsPath}
            title={
              !provider.modelsPath
                ? "请手动添加，或在供应商设置中填写模型列表接口路径"
                : `从 ${provider.modelsPath} 读取模型目录，不调用生成接口`
            }
            onClick={() =>
              act(async () => {
                const result = await post(
                  "/admin/providers/" + provider.id + "/sync-models",
                );
                refreshModels();
                toast(
                  `已读取 ${result.received} 个模型，新增 ${result.added} 个待发布条目`,
                );
              })
            }
          >
            <RefreshCw size={15} />
            {busy ? "读取中…" : "拉取模型列表"}
          </button>
          <button
            className="button primary"
            disabled={busy}
            onClick={() => setEdit(null)}
          >
            <Plus size={15} />
            添加支持模型
          </button>
        </div>
        <p className="muted small">
          接口不提供模型列表时可手动添加。发布后在模型管理配置价格并启用。
        </p>
      </div>
      {published && (
        <div className="channel-publish-result">
          <span>「{published.name}」已进入模型管理</span>
          <Link
            className="text-link"
            to={managementURL(published.id)}
            onClick={onClose}
          >
            去配置参数与价格 <ArrowUpRight size={14} />
          </Link>
        </div>
      )}
      <div className="channel-model-filters">
        <Tabs
          value={status}
          onChange={(value) => {
            setStatus(value);
            setPage(1);
          }}
          items={[
            { value: "all", label: "全部" },
            { value: "unpublished", label: "待发布" },
            { value: "published", label: "已发布" },
          ]}
        />
        <SearchBox
          value={search}
          onChange={(value) => {
            setSearch(value);
          }}
          onCompositionChange={setComposing}
          placeholder="搜索上游模型 ID 或名称"
        />
      </div>
      <div
        className="channel-model-results"
        aria-busy={updating}
        ref={resultsRef}
      >
        {isLoading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} retry={refetch} />
        ) : data?.items.length ? (
          <div
            className="table-wrap channel-model-table"
            inert={staleResults || undefined}
          >
            <table>
              <colgroup>
                <col />
                <col />
                <col style={{ width: 64 }} />
                <col style={{ width: 80 }} />
                <col style={{ width: 136 }} />
                <col style={{ width: 180 }} />
              </colgroup>
              <thead>
                <tr>
                  <th>渠道支持的模型</th>
                  <th>别名</th>
                  <th>类型</th>
                  <th>发布状态</th>
                  <th>调用密钥</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((entry) => (
                  <tr key={entry.id}>
                    <td>
                      <strong>{entry.name}</strong>
                      <code className="small-code">{entry.upstreamModel}</code>
                    </td>
                    <td className="channel-model-alias" data-label="别名">
                      {entry.publishedModels.length ? (
                        entry.publishedModels.map((model) => (
                          <span key={model.id}>{model.name}</span>
                        ))
                      ) : (
                        <span>{defaultAlias(provider.name, entry.name)}</span>
                      )}
                    </td>
                    <td>
                      {entry.type ? (
                        <Badge status={entry.type}>
                          {generationLabels[entry.type] || entry.type}
                        </Badge>
                      ) : (
                        <span className="muted small">发布时选择</span>
                      )}
                    </td>
                    <td>
                      <Badge
                        status={
                          entry.publishedModels.length ? "succeeded" : "queued"
                        }
                      >
                        {entry.publishedModels.length ? "已发布" : "待发布"}
                      </Badge>
                    </td>
                    <td className="channel-model-key-cell">
                      <button
                        className={
                          "channel-model-key-button" +
                          (entry.hasKey ? " custom" : "")
                        }
                        aria-label={"配置调用密钥 " + entry.name}
                        onClick={() => setKeyEntry(entry)}
                      >
                        <KeyRound size={14} />
                        {entry.hasKey ? "独立 Key" : "渠道默认"}
                        <Pencil size={12} />
                      </button>
                    </td>
                    <td>
                      {entry.publishedModels.length ? (
                        <div className="channel-published-links">
                          {entry.publishedModels.map((model) => (
                            <Link
                              key={model.id}
                              className="text-link"
                              to={managementURL(model.id)}
                              onClick={onClose}
                            >
                              {entry.publishedModels.length > 1
                                ? model.name
                                : "前往模型管理"}
                              <ArrowUpRight size={13} />
                            </Link>
                          ))}
                        </div>
                      ) : (
                        <div className="row-actions">
                          <button
                            className="button primary small-button"
                            disabled={busy || !provider.enabled}
                            title={
                              !provider.enabled ? "请先启用渠道" : undefined
                            }
                            aria-label={"发布 " + entry.name}
                            onClick={() => setPublish(entry)}
                          >
                            <Upload size={14} />
                            发布
                          </button>
                          <button
                            className="icon-button"
                            aria-label={"编辑渠道模型 " + entry.name}
                            onClick={() => setEdit(entry)}
                          >
                            <Pencil size={15} />
                          </button>
                          <button
                            className="icon-button danger"
                            disabled={busy}
                            aria-label={"移除渠道模型 " + entry.name}
                            onClick={async () => {
                              if (
                                await confirm(
                                  `从此渠道的待发布列表移除「${entry.name}」？`,
                                  {
                                    title: "移除渠道模型",
                                    confirmLabel: "移除",
                                  },
                                )
                              )
                                act(async () => {
                                  await api(path + "/" + entry.id, {
                                    method: "DELETE",
                                  });
                                  refreshModels();
                                }, "已移除渠道模型");
                            }}
                          >
                            <Trash2 size={15} />
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty
            title={
              search || status !== "all"
                ? "没有匹配的渠道模型"
                : "还没有配置支持的模型"
            }
            text="手动添加上游模型 ID，或从渠道接口拉取列表。"
            icon={<Boxes size={26} />}
          />
        )}
      </div>
      <div className="channel-model-pagination" inert={updating || undefined}>
        <Pagination
          page={data?.page || page}
          total={data?.total}
          limit={20}
          onChange={setPage}
        />
      </div>
      <div className="modal-footer">
        <span className="muted" role="status" aria-live="polite">
          {updating
            ? "正在更新搜索结果…"
            : error
              ? "搜索失败，请重试"
              : `共 ${data?.total || 0} 个符合条件的模型`}
        </span>
        <button className="button" onClick={onClose}>
          关闭列表
        </button>
      </div>
      {keyEntry && (
        <ChannelModelKey
          provider={provider}
          entry={keyEntry}
          onClose={() => setKeyEntry(null)}
          onSaved={refreshModels}
        />
      )}
      {edit !== undefined && (
        <ChannelModelForm
          provider={provider}
          initial={edit}
          onClose={() => setEdit(undefined)}
          onSaved={() => {
            refreshModels();
            setPage(1);
          }}
        />
      )}
      {publish && (
        <PublishModel
          provider={provider}
          entry={publish}
          onClose={() => setPublish(null)}
          onPublished={(model) => {
            setPublished(model);
            refreshModels();
          }}
        />
      )}
    </Modal>
  );
}
function ChannelModelKey({
  provider,
  entry,
  onClose,
  onSaved,
}: {
  provider: RecordData;
  entry: ChannelModel;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [useDefault, setUseDefault] = useState(!entry.hasKey);
  const [apiKey, setApiKey] = useState("");
  const { act, busy } = useAction();
  const changed = useDefault ? entry.hasKey : !!apiKey.trim();
  return (
    <Modal
      title="模型调用密钥"
      description={`${provider.name} · ${entry.upstreamModel}`}
      icon={<KeyRound size={20} />}
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          act(
            async () => {
              await post(
                `/admin/providers/${encodeURIComponent(provider.id)}/models/${encodeURIComponent(entry.id)}/key`,
                useDefault ? { useDefault: true } : { apiKey: apiKey.trim() },
                "PUT",
              );
              setApiKey("");
              onSaved();
              onClose();
            },
            useDefault ? "已恢复渠道默认 Key" : "模型独立 Key 已保存",
          )
        }
      >
        <fieldset className="model-key-choices">
          <legend>此模型使用的 API Key</legend>
          <label className={useDefault ? "selected" : ""}>
            <input
              type="radio"
              name="model-key-source"
              checked={useDefault}
              onChange={() => {
                setUseDefault(true);
                setApiKey("");
              }}
            />
            <span>
              <strong>使用渠道默认 Key</strong>
              <small>
                {provider.hasKey
                  ? "跟随渠道设置，适用于大多数模型。"
                  : "渠道尚未配置默认 Key，请先配置或使用独立 Key。"}
              </small>
            </span>
          </label>
          <label className={!useDefault ? "selected" : ""}>
            <input
              type="radio"
              name="model-key-source"
              checked={!useDefault}
              onChange={() => setUseDefault(false)}
            />
            <span>
              <strong>使用模型独立 Key</strong>
              <small>仅用于此渠道下的这个模型及其发布别名。</small>
            </span>
          </label>
        </fieldset>
        {!useDefault && (
          <Field
            label="模型独立 API Key"
            hint={
              entry.hasKey
                ? "已配置独立 Key。如需替换，请输入新 Key；现有密钥不回显。"
                : "填写具有该上游模型调用权限的 Key，加密保存在服务端。"
            }
          >
            <input
              type="password"
              autoComplete="new-password"
              spellCheck={false}
              maxLength={4096}
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              placeholder={
                entry.hasKey ? "输入新 Key 以替换" : "粘贴此模型的 API Key"
              }
            />
          </Field>
        )}
        <p className="muted small model-key-note">
          保存后对新提交的任务生效，进行中的任务继续使用提交时的 Key。
        </p>
        <div className="modal-footer dialog-actions">
          <button type="button" className="button" onClick={onClose}>
            取消
          </button>
          <button className="button primary" disabled={busy || !changed}>
            {busy ? "保存中…" : useDefault ? "恢复渠道默认" : "保存密钥"}
          </button>
        </div>
      </Form>
    </Modal>
  );
}
function ChannelModelForm({
  provider,
  initial,
  onClose,
  onSaved,
}: {
  provider: RecordData;
  initial: ChannelModel | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [upstreamModel, setUpstream] = useState(initial?.upstreamModel || ""),
    [name, setName] = useState(initial?.name || "");
  const [kind, setKind] = useState(
    initial?.type ||
      (provider.protocol === "openai-video"
        ? "video"
        : provider.types?.[0] || "image"),
  );
  const native = provider.protocol.startsWith("native:");
  const [useDefaults, setUseDefaults] = useState(native && !initial);
  const [protocolConfig, setProtocolConfig] = useState(
    JSON.stringify(initial?.config || {}, null, 2),
  );
  const [description, setDescription] = useState(initial?.description || "");
  const [parameters, setParameters] = useState(
    JSON.stringify(
      initial?.parameters || [
        {
          name: "aspectRatio",
          label: "比例",
          type: "select",
          default: "16:9",
          options: [
            { l: "16:9", v: "16:9" },
            { l: "9:16", v: "9:16" },
            { l: "1:1", v: "1:1" },
          ],
        },
        {
          name: "imageUrls",
          label: "参考图片",
          type: "assets",
          itemType: "image",
        },
      ],
      null,
      2,
    ),
  );
  const { act, busy } = useAction();
  return (
    <Modal
      title={initial ? "编辑渠道模型" : "添加支持模型"}
      description={provider.name + " · 保存到待发布列表"}
      icon={<Boxes size={20} />}
      wide
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          act(async () => {
            await post(
              "/admin/providers/" +
                provider.id +
                "/models" +
                (initial ? "/" + initial.id : ""),
              {
                upstreamModel: upstreamModel.trim(),
                name: name.trim() || upstreamModel.trim().slice(0, 120),
                type: kind,
                description,
                parameters: useDefaults ? [] : JSON.parse(parameters),
                useProtocolDefaults: useDefaults,
                config: JSON.parse(protocolConfig),
              },
              initial ? "PUT" : "POST",
            );
            onSaved();
            onClose();
          }, "渠道模型已保存，发布后进入模型管理")
        }
      >
        <div className="two-columns">
          <Field label="上游模型 ID" hint="填写此渠道实际接受的 model 值。">
            <input
              value={upstreamModel}
              onChange={(event) => setUpstream(event.target.value)}
              required
              maxLength={160}
              placeholder="例如：provider-image-model"
            />
          </Field>
          <Field label="模型名称">
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={120}
              placeholder="留空使用上游模型 ID"
            />
          </Field>
        </div>
        <Field
          label="默认别名"
          hint="渠道名首字母加模型名称，发布时可调整显示别名。"
        >
          <input
            readOnly
            value={
              name.trim() || upstreamModel.trim()
                ? defaultAlias(
                    provider.name,
                    name.trim() || upstreamModel.trim(),
                  )
                : ""
            }
            placeholder="填写模型名称后自动生成"
          />
        </Field>
        <Field label="生成类型">
          <select
            value={kind}
            onChange={(event) => setKind(event.target.value)}
          >
            {generationOptions
              .filter(
                (o) => !provider.types || provider.types.includes(o.value),
              )
              .map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
          </select>
        </Field>
        <Field label="模型说明">
          <textarea
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            rows={2}
            maxLength={2000}
          />
        </Field>
        {native && (
          <label className="checkbox">
            <input
              type="checkbox"
              checked={useDefaults}
              onChange={(e) => setUseDefaults(e.target.checked)}
            />
            使用协议默认参数
          </label>
        )}
        {!useDefaults && (
          <ReferenceSettings value={parameters} onChange={setParameters} />
        )}
        <details className="advanced-model-parameters">
          <summary>高级参数配置</summary>
          <p className="muted small">
            作为发布时的初始配置，之后可在模型管理中调整。
          </p>
          {native && (
            <Field
              label="模型协议选项（JSON）"
              hint="可配置 sourceModel、chatAPI（responses 或 chat-completions）、submit_path_override、poll_path_override。留空对象使用协议默认值。"
            >
              <JsonEditor value={protocolConfig} onChange={setProtocolConfig} />
            </Field>
          )}
          {!useDefaults && (
            <JsonEditor
              label="生成参数定义（JSON）"
              value={parameters}
              onChange={setParameters}
            />
          )}
        </details>
        <div className="modal-footer">
          <span className="muted">保存只加入渠道列表，尚未发布。</span>
          <div>
            <button type="button" className="button" onClick={onClose}>
              取消
            </button>
            <button className="button primary" disabled={busy}>
              {busy ? "保存中…" : "保存渠道模型"}
            </button>
          </div>
        </div>
      </Form>
    </Modal>
  );
}
function PublishModel({
  provider,
  entry,
  onClose,
  onPublished,
}: {
  provider: RecordData;
  entry: ChannelModel;
  onClose: () => void;
  onPublished: (model: Model) => void;
}) {
  const slug = entry.upstreamModel.replace(/[^a-zA-Z0-9_.:-]/g, "-") || "model";
  const [modelId, setModelId] = useState(
    (provider.id.slice(0, 8) + ":" + slug).slice(0, 100),
  );
  const [name, setName] = useState(defaultAlias(provider.name, entry.name));
  const [kind, setKind] = useState(
    entry.type ||
      (provider.protocol === "openai-image"
        ? "image"
        : provider.protocol === "openai-video"
          ? "video"
          : ""),
  );
  const [category, setCategory] = useState("默认分组");
  const { act, busy } = useAction();
  const toast = useToast();
  return (
    <Modal
      title="发布到模型管理"
      description={provider.name + " · " + entry.upstreamModel}
      icon={<Upload size={20} />}
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          act(async () => {
            const result = await post<{
              model: Model;
              alreadyPublished: boolean;
            }>(
              "/admin/providers/" +
                provider.id +
                "/models/" +
                entry.id +
                "/publish",
              {
                modelId: modelId.trim(),
                name: name.trim(),
                type: kind,
                category,
              },
            );
            onPublished(result.model);
            onClose();
            toast(
              result.alreadyPublished
                ? "该渠道模型已经发布"
                : "已发布到模型管理，请配置价格后启用",
            );
          })
        }
      >
        <Field label="站内模型 ID" hint="用于本平台 API 调用，在全站唯一。">
          <input
            required
            maxLength={100}
            pattern="[a-zA-Z0-9_.:\-]+"
            value={modelId}
            onChange={(event) => setModelId(event.target.value)}
          />
        </Field>
        <Field label="发布名称" hint="用作显示别名，上游调用仍使用原模型 ID。">
          <input
            required
            maxLength={120}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </Field>
        <Field label="生成类型">
          <select
            required
            value={kind}
            onChange={(event) => setKind(event.target.value)}
          >
            <option value="" disabled>
              请选择生成类型
            </option>
            {generationOptions
              .filter(
                (o) => !provider.types || provider.types.includes(o.value),
              )
              .map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
          </select>
        </Field>
        <Field label="模型分组">
          <input
            maxLength={80}
            value={category}
            onChange={(event) => setCategory(event.target.value)}
          />
        </Field>
        <div className="dialog-info">
          <Boxes size={16} />
          <p>
            发布后进入模型管理，默认停用。配置参数和计费规则后，再启用供用户调用。
          </p>
        </div>
        <div className="modal-footer dialog-actions">
          <button type="button" className="button" onClick={onClose}>
            取消
          </button>
          <button className="button primary" disabled={busy || !kind}>
            {busy ? "发布中…" : "确认发布"}
          </button>
        </div>
      </Form>
    </Modal>
  );
}
