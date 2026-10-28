import ProviderInventory from "./ProviderInventory";
import { generationLabels, generationOptions } from "../lib";
import { useMoneyMutation } from "../components/MoneyRecovery";
import { useState, useEffect, useRef, useId } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  NavLink,
  Navigate,
  useLocation,
  Link,
  useSearchParams,
} from "react-router-dom";
import {
  LayoutDashboard,
  Boxes,
  Coins,
  Cable,
  Users,
  Ticket,
  ListChecks,
  Settings,
  ScrollText,
  Plus,
  Pencil,
  Trash2,
  Search,
  Power,
  Copy,
  Check,
  ExternalLink,
  RefreshCw,
  Download,
  SlidersHorizontal,
  ShieldCheck,
  ArrowUpRight,
  KeyRound,
  ChevronDown,
  LifeBuoy,
} from "lucide-react";
import {
  useApi,
  useRefresh,
  useAction,
  useConfirm,
  useToast,
  api,
  post,
  number,
  money,
  date,
  copy,
  downloadJSON,
  PageHeader,
  SearchBox,
  Field,
  Modal,
  Form,
  Badge,
  Tabs,
  Loading,
  ErrorState,
  Empty,
  JsonEditor,
  Pagination,
  unitLabel,
  type RecordData,
  type Model,
  type Price,
  type User,
  type Job,
  type PageData,
} from "../lib";
import { Stat, JobsTable, UsageChart } from "./Account";
import { JobAdmin, TransactionsAdmin, AuditAdmin } from "./AdminRecords";
import Support from "./Support";
import { ReferenceSettings } from "../components/ReferenceSettings";
import ProviderModels from "./ProviderModels";
import StorageSettings from "./StorageSettings";
import UpstreamLogs from "./UpstreamLogs";
import { SpecificationPricing } from "../components/SpecificationPricing";
import {
  billingLabel,
  billingOptions,
  billingUnit,
  priceExtraLabels,
} from "../pricing";

const sections = [
  ["overview", "数据概览", LayoutDashboard],
  ["models", "模型管理", Boxes],
  ["providers", "渠道管理", Cable],
  ["inventory", "供应商路由", Cable],
  ["users", "用户管理", Users],
  ["codes", "兑换码管理", Ticket],
  ["tickets", "工单管理", LifeBuoy],
  ["jobs", "全站调用", ListChecks],
  ["upstream", "上游日志", Cable],
  ["transactions", "全站流水", Coins],
  ["settings", "站点设置", Settings],
  ["audit", "操作日志", ScrollText],
] as const;
export default function Admin() {
  const { data: user, isLoading } = useApi<User>("/me", {
    refetchOnWindowFocus: true,
  });
  const location = useLocation();
  const active = location.pathname.split("/")[2] || "overview";
  if (isLoading) return <Loading />;
  if (user?.role !== "admin") return <Navigate to="/generate" replace />;
  if (active === "prices") return <Navigate to="/admin/models" replace />;
  if (!sections.some(([key]) => key === active))
    return <Navigate to="/admin/overview" replace />;
  return (
    <div className="page admin-page">
      <PageHeader
        title="管理后台"
        description="管理模型、兑换额度与全站调用，仅管理员可访问。"
      >
        <Badge status="succeeded">
          <ShieldCheck size={13} />
          管理员
        </Badge>
      </PageHeader>
      <nav className="admin-nav" aria-label="后台管理导航">
        {sections.map(([key, title, Icon]) => (
          <NavLink
            end
            to={"/admin/" + key}
            key={key}
            className={() => (active === key ? "active" : "")}
          >
            <Icon size={15} />
            {title}
          </NavLink>
        ))}
      </nav>
      {active === "overview" ? (
        <Overview />
      ) : active === "models" ? (
        <ModelAdmin />
      ) : active === "inventory" ? (
        <ProviderInventory />
      ) : active === "providers" ? (
        <ProviderAdmin />
      ) : active === "users" ? (
        <UserAdmin />
      ) : active === "codes" ? (
        <CodeAdmin />
      ) : active === "tickets" ? (
        <Support admin />
      ) : active === "upstream" ? (
        <UpstreamLogs />
      ) : active === "settings" ? (
        <SettingsAdmin />
      ) : active === "audit" ? (
        <AuditAdmin />
      ) : active === "transactions" ? (
        <TransactionsAdmin />
      ) : (
        <JobAdmin />
      )}
    </div>
  );
}
function Overview() {
  const { data, isLoading, error, refetch } = useApi<RecordData>(
    "/admin/stats",
    { refetchInterval: 5000 },
  );
  if (isLoading) return <Loading />;
  if (error) return <ErrorState error={error} retry={refetch} />;
  return (
    <>
      <div className="stats-grid">
        <Stat
          label="累计用户"
          value={data?.users || 0}
          icon={<Users size={18} />}
        />
        <Stat
          label="可管理模型"
          value={data?.models || 0}
          icon={<Boxes size={18} />}
        />
        <Stat
          label="生成任务"
          value={data?.jobs || 0}
          icon={<ListChecks size={18} />}
          sub={`${data?.activeJobs || 0} 个任务进行中`}
        />
        <Stat
          label="净消费金额"
          value={money(data?.consumed)}
          icon={<Coins size={18} />}
        />
      </div>
      <div className="two-columns overview-panels">
        <section className="panel">
          <div className="section-header">
            <h2>近 7 天消费金额</h2>
            <Badge>实时统计</Badge>
          </div>
          <UsageChart data={data?.daily || []} />
        </section>
        <section className="panel quick-admin">
          <h2>常用操作</h2>
          <Link to="/admin/models">
            <Boxes />
            <div>
              <strong>配置模型与价格</strong>
              <span>上架模型、定义参数和计费规则</span>
            </div>
            <ArrowUpRight size={17} />
          </Link>
          <Link to="/admin/providers">
            <Cable />
            <div>
              <strong>接入渠道</strong>
              <span>配置供应商地址与 API 密钥</span>
            </div>
            <ArrowUpRight size={17} />
          </Link>
          <Link to="/admin/codes">
            <Ticket />
            <div>
              <strong>生成兑换码</strong>
              <span>设置充值额度、数量与有效期</span>
            </div>
            <ArrowUpRight size={17} />
          </Link>
          <Link to="/admin/jobs">
            <ListChecks />
            <div>
              <strong>查看全站调用</strong>
              <span>按用户、模型和来源核对任务</span>
            </div>
            <ArrowUpRight size={17} />
          </Link>
        </section>
      </div>
      <section className="panel">
        <div className="section-header">
          <h2>最近生成任务</h2>
          <Link className="text-link" to="/admin/jobs">
            查看全部 <ArrowUpRight size={14} />
          </Link>
        </div>
        <JobsTable jobs={data?.recent || []} showUser />
      </section>
    </>
  );
}
function ModelAdmin() {
  const [query] = useSearchParams();
  const requestedModel = query.get("model");
  const requestedTab = query.get("tab");
  const opened = useRef<string | null>(null);
  const queryClient = useQueryClient();
  const { data, isLoading, error, refetch } = useApi<Model[]>("/admin/models");
  const { data: providers } = useApi<RecordData[]>("/admin/providers");
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [channel, setChannel] = useState<RecordData | null>(null);
  const [edit, setEdit] = useState<Partial<Model> | null>(null);
  const [deleteModel, setDeleteModel] = useState<Model | null>(null);
  const [deletedName, setDeletedName] = useState("");
  const [editTab, setEditTab] = useState("basic");
  const { act, busy } = useAction();
  const refresh = useRefresh();
  useEffect(() => {
    if (!requestedModel) {
      opened.current = null;
      return;
    }
    if (opened.current === requestedModel) return;
    const model = data?.find((item) => item.id === requestedModel);
    if (model) {
      opened.current = requestedModel;
      setSearch(model.id);
      setEditTab(requestedTab === "pricing" ? "pricing" : "basic");
      setEdit(model);
    }
  }, [requestedModel, requestedTab, data]);
  const term = search.trim().toLowerCase();
  const grouped = new Map<
    string,
    {
      id: string;
      name: string;
      models: Model[];
      total: number;
      enabled: number;
    }
  >();
  for (const model of data || []) {
    let group = grouped.get(model.provider_id);
    if (!group) {
      group = {
        id: model.provider_id,
        name: model.providerName,
        models: [],
        total: 0,
        enabled: 0,
      };
      grouped.set(model.provider_id, group);
    }
    group.total++;
    if (model.enabled) group.enabled++;
    if (
      [
        model.name,
        model.id,
        model.category,
        model.providerName,
        model.upstream_model,
      ]
        .join(" ")
        .toLowerCase()
        .includes(term)
    )
      group.models.push(model);
  }
  const groups = Array.from(grouped.values())
    .filter((group) => group.models.length)
    .sort((a, b) => a.name.localeCompare(b.name, "zh-CN"));
  const isExpanded = (id: string) => expanded[id] ?? !!term;
  const allExpanded =
    groups.length > 0 && groups.every((group) => isExpanded(group.id));
  return (
    <section className="panel">
      <div className="section-header">
        <div>
          <h2>
            模型管理 <span className="muted">{data?.length || 0}</span>
          </h2>
          <p className="muted">
            按渠道查看已发布模型，展开后配置参数、价格与启用状态。
          </p>
        </div>
        <Link className="button primary" to="/admin/providers">
          <Plus size={16} />
          从渠道发布
        </Link>
      </div>
      <div className="toolbar">
        <SearchBox
          value={search}
          onChange={(value) => {
            setSearch(value);
            setExpanded({});
          }}
          placeholder="搜索渠道、模型名称或 ID"
        />
        <div className="model-group-tools">
          <span className="muted small">
            {groups.length} 个渠道
            {term
              ? ` · ${groups.reduce((sum, group) => sum + group.models.length, 0)} 个匹配模型`
              : ""}
          </span>
          <button
            className="text-link"
            disabled={!groups.length}
            onClick={() =>
              setExpanded(
                Object.fromEntries(
                  groups.map((group) => [group.id, !allExpanded]),
                ),
              )
            }
          >
            {allExpanded ? "全部收起" : "全部展开"}
          </button>
          <Link className="text-link" to="/models">
            查看模型广场 <ExternalLink size={13} />
          </Link>
        </div>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        <div className="model-channel-groups">
          {deletedName && (
            <div className="model-delete-result" role="status">
              <Check size={15} />
              已删除模型「{deletedName}」
            </div>
          )}
          {groups.map((group) => (
            <section
              className={
                "model-channel-group" +
                (isExpanded(group.id) ? " expanded" : "")
              }
              key={group.id}
            >
              <div className="model-channel-header">
                <button
                  className="model-channel-toggle"
                  aria-expanded={isExpanded(group.id)}
                  aria-controls={"channel-models-" + group.id}
                  onClick={() =>
                    setExpanded((current) => ({
                      ...current,
                      [group.id]: !isExpanded(group.id),
                    }))
                  }
                >
                  <span className="model-channel-icon">
                    <Cable size={19} />
                  </span>
                  <span className="model-channel-title">
                    <strong>{group.name}</strong>
                    <span>
                      {term
                        ? `${group.models.length} / ${group.total}`
                        : group.total}{" "}
                      个模型
                    </span>
                  </span>
                  <span className="model-channel-status">
                    <i className={group.enabled ? "active" : ""} />
                    {group.enabled ? `${group.enabled} 个已启用` : "全部停用"}
                  </span>
                  <ChevronDown className="model-channel-chevron" size={17} />
                </button>
                <button
                  className="text-link model-channel-key-link"
                  aria-label={"配置模型密钥 " + group.name}
                  disabled={
                    !providers?.some((provider) => provider.id === group.id)
                  }
                  onClick={() =>
                    setChannel(
                      providers?.find((provider) => provider.id === group.id) ||
                        null,
                    )
                  }
                >
                  <KeyRound size={14} />
                  <span>模型密钥</span>
                </button>
              </div>
              {isExpanded(group.id) && (
                <div
                  className="table-wrap model-channel-table"
                  id={"channel-models-" + group.id}
                >
                  <table>
                    <thead>
                      <tr>
                        <th>模型</th>
                        <th>类型</th>
                        <th>分组</th>
                        <th>计费方式</th>
                        <th>起步价格</th>
                        <th>状态</th>
                        <th>操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {group.models.map((m) => (
                        <tr key={m.id}>
                          <td>
                            <strong className="table-name">{m.name}</strong>
                            <code className="small-code">{m.id}</code>
                          </td>
                          <td>
                            <Badge status={m.type}>
                              {generationLabels[m.type] || m.type}
                            </Badge>
                          </td>
                          <td>{m.category}</td>
                          <td>{billingLabel(m)}</td>
                          <td className="gold">
                            {m.prices.filter((p) => p.enabled).length
                              ? money(
                                  Math.min(
                                    ...m.prices
                                      .filter((p) => p.enabled)
                                      .map((p) => p.credits),
                                  ),
                                ) +
                                "/" +
                                unitLabel[billingUnit(m)]
                              : "未配置"}
                          </td>
                          <td>
                            <button
                              className={
                                "status-switch " + (m.enabled ? "on" : "")
                              }
                              aria-label={
                                (m.enabled ? "停用 " : "启用 ") + m.name
                              }
                              disabled={busy}
                              onClick={() =>
                                act(
                                  async () => {
                                    await post(
                                      "/admin/models/" +
                                        encodeURIComponent(m.id),
                                      { ...m, enabled: !m.enabled },
                                      "PUT",
                                    );
                                    refresh("/admin/models", "/models");
                                  },
                                  m.enabled ? "模型已停用" : "模型已启用",
                                )
                              }
                            >
                              <span />
                              {m.enabled ? "启用" : "停用"}
                            </button>
                          </td>
                          <td>
                            <div className="row-actions">
                              <button
                                className="text-link model-price-action"
                                aria-label={"配置价格 " + m.name}
                                onClick={() => {
                                  setEditTab("pricing");
                                  setEdit(m);
                                }}
                              >
                                <Coins size={14} />
                                配置价格
                              </button>
                              <button
                                className="icon-button"
                                aria-label={"编辑 " + m.name}
                                onClick={() => {
                                  setEditTab("basic");
                                  setEdit(m);
                                }}
                              >
                                <Pencil size={15} />
                              </button>
                              <button
                                type="button"
                                className="icon-button danger"
                                aria-label={"删除 " + m.name}
                                title="删除模型"
                                disabled={busy}
                                onClick={() => {
                                  setDeletedName("");
                                  setDeleteModel(m);
                                }}
                              >
                                <Trash2 size={15} />
                              </button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          ))}
          {!groups.length && <Empty title="没有匹配的模型" />}
        </div>
      )}
      {deleteModel && (
        <ModelDeleteDialog
          model={deleteModel}
          onClose={() => setDeleteModel(null)}
          onDeleted={() => {
            const removed = deleteModel;
            queryClient.setQueryData<Model[]>(["/admin/models"], (current) =>
              current?.filter((model) => model.id !== removed.id),
            );
            setDeletedName(removed.name);
            setDeleteModel(null);
            refresh(
              "/admin/models",
              "/admin/prices",
              "/admin/providers",
              "/models",
            );
          }}
        />
      )}
      {channel && (
        <ProviderModels provider={channel} onClose={() => setChannel(null)} />
      )}
      {edit && (
        <ModelForm
          initial={edit}
          initialTab={editTab}
          providers={providers || []}
          onClose={() => setEdit(null)}
        />
      )}
    </section>
  );
}
function ModelDeleteDialog({
  model,
  onClose,
  onDeleted,
}: {
  model: Model;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const cancelButton = useRef<HTMLButtonElement>(null);
  return (
    <Modal
      title="删除模型"
      icon={<Trash2 size={20} />}
      className="confirmation-dialog"
      onClose={() => {
        if (!busy) onClose();
      }}
      initialFocusRef={cancelButton}
    >
      <p className="confirmation-message">
        确定删除模型「{model.name}」及其价格规则？
      </p>
      <p className="muted small">
        渠道支持列表会保留此模型，之后可以重新发布。有调用记录的模型需使用停用功能保留历史。
      </p>
      {error && (
        <div className="model-delete-error" role="alert">
          {error}
        </div>
      )}
      <div className="modal-footer">
        <button
          ref={cancelButton}
          type="button"
          className="button"
          disabled={busy}
          onClick={onClose}
        >
          取消
        </button>
        <button
          type="button"
          className="button confirmation-accept"
          disabled={busy}
          onClick={async () => {
            if (busy) return;
            setBusy(true);
            setError("");
            try {
              await api("/admin/models/" + encodeURIComponent(model.id), {
                method: "DELETE",
              });
            } catch (failure) {
              setError(
                failure instanceof Error ? failure.message : "删除失败，请重试",
              );
              setBusy(false);
              return;
            }
            onDeleted();
          }}
        >
          {busy ? "正在删除…" : error ? "重试删除" : "删除模型"}
        </button>
      </div>
    </Modal>
  );
}
function ModelForm({
  initial,
  initialTab = "basic",
  providers,
  onClose,
}: {
  initial: Partial<Model>;
  initialTab?: string;
  providers: RecordData[];
  onClose: () => void;
}) {
  const [savedModel, setSavedModel] = useState<Model | null>(
    initial.id ? (initial as Model) : null,
  );
  const [tab, setTab] = useState(initialTab);
  const pricingTab = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  useEffect(() => {
    if (tab === "pricing") pricingTab.current?.focus({ preventScroll: true });
  }, [tab]);
  const isEdit = !!savedModel;
  const [form, setForm] = useState<RecordData>({
    id: "",
    name: "",
    type: "image",
    provider_id: providers[0]?.id || "",
    upstream_model: "",
    description: "",
    category: "默认分组",
    enabled: true,
    sort_order: 100,
    tags: [],
    ...initial,
  });
  const [parameters, setParameters] = useState(
    JSON.stringify(
      initial.parameters || [
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
  const refresh = useRefresh();
  const set = (k: string, v: any) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <Modal
      title={isEdit ? "编辑模型" : "新增模型"}
      description={
        savedModel
          ? `${savedModel.name} · 模型信息与计费规则`
          : "先保存模型信息，再为此模型配置计费规则。"
      }
      icon={<Boxes size={20} />}
      onClose={onClose}
      wide
      className="model-editor-dialog"
    >
      <div
        className="model-editor-tabs"
        role="tablist"
        aria-label="模型配置"
        onKeyDown={(event) => {
          if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key))
            return;
          event.preventDefault();
          const buttons = Array.from(
            event.currentTarget.querySelectorAll<HTMLButtonElement>(
              "button:not(:disabled)",
            ),
          );
          const index = buttons.indexOf(
            document.activeElement as HTMLButtonElement,
          );
          const next =
            event.key === "Home"
              ? 0
              : event.key === "End"
                ? buttons.length - 1
                : (index +
                    (event.key === "ArrowLeft" ? -1 : 1) +
                    buttons.length) %
                  buttons.length;
          buttons[next]?.click();
          buttons[next]?.focus();
        }}
      >
        <button
          type="button"
          role="tab"
          aria-selected={tab === "basic"}
          id={`${panelId}-basic-tab`}
          aria-controls={`${panelId}-basic`}
          tabIndex={tab === "basic" ? 0 : -1}
          className={tab === "basic" ? "active" : ""}
          onClick={() => setTab("basic")}
        >
          基本信息
        </button>
        <button
          ref={pricingTab}
          type="button"
          role="tab"
          aria-selected={tab === "pricing"}
          id={`${panelId}-pricing-tab`}
          aria-controls={`${panelId}-pricing`}
          tabIndex={tab === "pricing" ? 0 : -1}
          className={tab === "pricing" ? "active" : ""}
          disabled={!savedModel}
          title={!savedModel ? "保存模型后可配置计费规则" : undefined}
          onClick={() => setTab("pricing")}
        >
          计费规则
        </button>
      </div>
      <div
        hidden={tab !== "basic"}
        role="tabpanel"
        id={`${panelId}-basic`}
        aria-labelledby={`${panelId}-basic-tab`}
        tabIndex={0}
      >
        <Form
          onSubmit={() =>
            act(
              async () => {
                const saved = await post<Model>(
                  "/admin/models" +
                    (savedModel ? "/" + encodeURIComponent(savedModel.id) : ""),
                  { ...form, parameters: JSON.parse(parameters) },
                  isEdit ? "PUT" : "POST",
                );
                setSavedModel(saved);
                refresh("/admin/models", "/models");
                if (isEdit) onClose();
                else setTab("pricing");
              },
              isEdit ? "模型已保存" : "模型已保存，请继续配置价格",
            )
          }
        >
          <div className="two-columns">
            <Field label="模型 ID">
              <input
                aria-label="模型 ID"
                value={form.id}
                onChange={(e) => set("id", e.target.value)}
                required
                disabled={isEdit}
                placeholder="例如：my-image-model"
              />
            </Field>
            <Field label="显示名称">
              <input
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                required
              />
            </Field>
            <Field label="模型类型">
              <select
                disabled={!!savedModel?.inventory_route_id}
                value={form.type}
                onChange={(e) => set("type", e.target.value)}
              >
                <option value="image">图片生成</option>
                <option value="video">视频生成</option>
                <option value="text">文本生成</option>
                <option value="audio">音频生成</option>
                <option value="upscale">视频超分</option>
              </select>
            </Field>
            <Field label="供应商渠道">
              <select
                disabled={isEdit}
                value={form.provider_id}
                onChange={(e) => set("provider_id", e.target.value)}
                required
              >
                {providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="上游模型名称">
              <input
                disabled={isEdit}
                value={form.upstream_model}
                onChange={(e) => set("upstream_model", e.target.value)}
                required
                placeholder="供应商 API 中的 model 值"
              />
            </Field>
            <Field label="分组">
              <input
                value={form.category}
                onChange={(e) => set("category", e.target.value)}
                required
              />
            </Field>
            <Field label="排序（越小越靠前）">
              <input
                type="number"
                min={0}
                value={form.sort_order}
                onChange={(e) => set("sort_order", Number(e.target.value))}
              />
            </Field>
            <Field label="状态">
              <select
                value={String(form.enabled)}
                onChange={(e) => set("enabled", e.target.value === "true")}
              >
                <option value="true">启用</option>
                <option value="false">停用</option>
              </select>
            </Field>
          </div>
          <Field label="模型说明">
            <textarea
              value={form.description}
              onChange={(e) => set("description", e.target.value)}
              rows={3}
            />
          </Field>
          <ReferenceSettings value={parameters} onChange={setParameters} />
          <details className="advanced-model-parameters">
            <summary>高级参数配置</summary>
            <p className="muted small">
              尺寸、分辨率、时长、默认值与选项均从以下定义生成，修改会同步到模型展示和创作页。
            </p>
            <JsonEditor
              label="生成参数定义（JSON）"
              value={parameters}
              onChange={setParameters}
            />
            <p className="muted small">
              支持 select、text、number、boolean、assets。素材字段通过 itemType
              区分 image / video / audio。选项格式为{" "}
              {`{"l":"显示名","v":"参数值"}`}。
            </p>
          </details>
          <div className="modal-footer">
            <span className="muted">
              {isEdit
                ? "价格在本窗口的「计费规则」中配置。"
                : "保存后自动进入此模型的价格配置。"}
            </span>
            <div>
              <button className="button" type="button" onClick={onClose}>
                取消
              </button>
              <button className="button primary" disabled={busy}>
                保存模型
              </button>
            </div>
          </div>
        </Form>
      </div>
      <div
        hidden={tab !== "pricing"}
        role="tabpanel"
        id={`${panelId}-pricing`}
        aria-labelledby={`${panelId}-pricing-tab`}
        tabIndex={0}
      >
        {tab === "pricing" && savedModel && (
          <ModelPricing savedModel={savedModel} onClose={onClose} />
        )}
      </div>
    </Modal>
  );
}
function ModelPricing({
  savedModel,
  onClose,
}: {
  savedModel: Model;
  onClose: () => void;
}) {
  const confirm = useConfirm();
  const {
    data: models,
    isLoading,
    error,
    refetch,
  } = useApi<Model[]>("/admin/models");
  const model = models?.find((item) => item.id === savedModel.id) || savedModel;
  const [edit, setEdit] = useState<Partial<Price> | null>(null);
  const [specificationPricing, setSpecificationPricing] = useState(false);
  const { act } = useAction();
  const refresh = useRefresh();
  const rows = model.prices || [];
  return (
    <section className="model-pricing-section" aria-label="当前模型的计费规则">
      <div className="section-header">
        <div>
          <h2>
            计费规则 <span className="muted">{rows.length}</span>
          </h2>
          <p className="muted">
            仅用于当前模型，条件更具体的规则优先匹配；每条规则独立保存并即时生效。
          </p>
        </div>
        <div className="pricing-rule-actions">
          <button
            className="button"
            onClick={() => setSpecificationPricing(true)}
          >
            <Coins size={16} />
            按规格定价
          </button>
          <button
            className="button primary"
            onClick={() =>
              setEdit({
                model_id: model.id,
                unit:
                  model.prices.find((price) => price.enabled)?.unit ||
                  "request",
              })
            }
          >
            <Plus size={16} />
            新增价格规则
          </button>
        </div>
      </div>
      <p className="pricing-formula">
        当前计费：{billingLabel(model)}
        {billingUnit(model) === "second"
          ? " · 单价 × 视频秒数"
          : billingUnit(model) === "request"
            ? " · 每次提交收取所选规格价格"
            : " · 视频可按次或按秒，图片统一按次"}
      </p>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>规格</th>
                <th>匹配条件</th>
                <th>计费方式</th>
                <th>价格（元）</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
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
                        Object.entries(p.conditions).map(([k, v]) => {
                          const parameter = model.parameters.find(
                            (item) => item.name === k,
                          );
                          const option = parameter?.options?.find(
                            (item) => String(item.v ?? item) === String(v),
                          );
                          return (
                            <code key={k}>
                              {parameter?.label || k} ={" "}
                              {typeof v === "boolean"
                                ? v
                                  ? "是"
                                  : "否"
                                : String(option?.l ?? v)}
                            </code>
                          );
                        })
                      ) : (
                        <span className="muted">默认规则</span>
                      )}
                    </div>
                  </td>
                  <td>按{unitLabel[p.unit]}</td>
                  <td className="gold">
                    <strong>{money(p.credits)}</strong>
                  </td>
                  <td>
                    <Badge status={p.enabled ? "active" : "disabled"} />
                  </td>
                  <td>
                    <div className="row-actions">
                      <button
                        className="icon-button"
                        aria-label={"编辑价格 " + model.name + " " + p.label}
                        onClick={() => setEdit(p)}
                      >
                        <Pencil size={15} />
                      </button>
                      <button
                        className="icon-button danger"
                        aria-label="删除价格规则"
                        onClick={async () => {
                          if (
                            await confirm(
                              "确定删除此价格规则？未配置价格的参数组合将无法生成。",
                              {
                                title: "删除价格规则",
                                confirmLabel: "删除规则",
                              },
                            )
                          )
                            act(async () => {
                              await api("/admin/prices/" + p.id, {
                                method: "DELETE",
                              });
                              refresh(
                                "/admin/prices",
                                "/admin/models",
                                "/models",
                              );
                            }, "价格规则已删除");
                        }}
                      >
                        <Trash2 size={15} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!rows.length && (
            <Empty
              title="此模型尚未配置价格"
              text="添加单价和适用规格后，用户才能获取报价并提交生成。"
              icon={<Coins size={25} />}
            />
          )}
        </div>
      )}
      <div className="modal-footer">
        <span className="muted">计费条件使用已保存的模型参数。</span>
        <button type="button" className="button" onClick={onClose}>
          完成
        </button>
      </div>
      {edit && (
        <PriceForm initial={edit} model={model} onClose={() => setEdit(null)} />
      )}
      {specificationPricing && (
        <SpecificationPricing
          model={model}
          onClose={() => setSpecificationPricing(false)}
        />
      )}
    </section>
  );
}
function PriceForm({
  initial,
  model,
  onClose,
}: {
  initial: Partial<Price>;
  model: Model;
  onClose: () => void;
}) {
  const [form, setForm] = useState<RecordData>({
    model_id: model.id,
    label: "统一价格",
    unit: "request",
    credits: 1,
    conditions: {},
    enabled: true,
    ...initial,
  });
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const set = (k: string, v: any) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <Modal
      title={initial.id ? "编辑价格规则" : "新增价格规则"}
      description={model.name + " · 设置单价与适用条件"}
      icon={<Coins size={20} />}
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          act(async () => {
            await post(
              "/admin/prices" + (initial.id ? "/" + initial.id : ""),
              { ...form, model_id: model.id },
              initial.id ? "PUT" : "POST",
            );
            refresh("/admin/prices", "/admin/models", "/models");
            onClose();
          }, "价格规则已保存")
        }
      >
        <Field label="模型">
          <input value={model.name} disabled readOnly />
        </Field>
        <div className="two-columns">
          <Field label="规格名称">
            <input
              value={form.label}
              onChange={(e) => set("label", e.target.value)}
              required
            />
          </Field>
          <Field label="计费方式">
            <select
              value={form.unit}
              disabled={model.type !== "video"}
              onChange={(e) => set("unit", e.target.value)}
            >
              {billingOptions(model.type).map((option) => (
                <option value={option.value} key={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field
          label="单价（元）"
          hint={`每${unitLabel[form.unit]}的价格；${form.unit === "second" ? "按视频秒数相乘" : "一次提交计费一次"}。`}
        >
          <input
            type="number"
            step="0.000001"
            min="0"
            value={form.credits}
            onChange={(e) => set("credits", Number(e.target.value))}
            required
          />
        </Field>
        <details className="advanced-model-parameters">
          <summary>参考素材附加费用</summary>
          <p className="muted small">
            只有渠道另收参考素材费用时填写。视频时长由服务端读取，单价和生成费用统一按人民币计算。
          </p>
          <div className="two-columns">
            <Field label="免费参考图片数量">
              <input
                type="number"
                min="0"
                step="1"
                value={form.extras?.includedImages ?? 0}
                onChange={(e) =>
                  set("extras", {
                    ...form.extras,
                    includedImages: Number(e.target.value),
                  })
                }
              />
            </Field>
            <Field label="额外参考图片单价（元/张）">
              <input
                type="number"
                min="0"
                step="0.000001"
                value={form.extras?.imagePrice ?? 0}
                onChange={(e) =>
                  set("extras", {
                    ...form.extras,
                    imagePrice: Number(e.target.value),
                  })
                }
              />
            </Field>
            <Field label="参考视频单价（元/秒）">
              <input
                type="number"
                min="0"
                step="0.000001"
                value={form.extras?.videoSecondPrice ?? 0}
                onChange={(e) =>
                  set("extras", {
                    ...form.extras,
                    videoSecondPrice: Number(e.target.value),
                  })
                }
              />
            </Field>
            <Field label="参考视频计费秒数">
              <select
                value={form.extras?.videoRounding || "total"}
                onChange={(e) =>
                  set("extras", {
                    ...form.extras,
                    videoRounding: e.target.value,
                  })
                }
              >
                <option value="total">总时长向上取整</option>
                <option value="each">每段时长分别向上取整</option>
              </select>
            </Field>
            <Field
              label="参考视频场景附加费（元/生成秒）"
              hint="仅在使用参考视频时收取，按生成视频时长计算，与参考文件自身时长无关。"
            >
              <input
                type="number"
                min="0"
                step="0.000001"
                value={form.extras?.videoOutputSecondPrice ?? 0}
                onChange={(e) =>
                  set("extras", {
                    ...form.extras,
                    videoOutputSecondPrice: Number(e.target.value),
                  })
                }
              />
            </Field>
          </div>
        </details>
        <h3>匹配条件</h3>
        <p className="muted small">
          不限定的参数留空。所有条件都匹配时使用此价格。
        </p>
        {model?.parameters
          .filter(
            (p) =>
              p.type === "select" ||
              p.type === "boolean" ||
              p.type === "number",
          )
          .map((p) => (
            <Field label={p.label} key={p.name}>
              {p.type === "number" ? (
                <input
                  type="number"
                  min={p.min ?? undefined}
                  max={p.max ?? undefined}
                  placeholder="不限"
                  value={form.conditions[p.name] ?? ""}
                  onChange={(event) => {
                    const next = { ...form.conditions };
                    if (event.target.value === "") delete next[p.name];
                    else next[p.name] = Number(event.target.value);
                    set("conditions", next);
                  }}
                />
              ) : (
                <select
                  value={String(form.conditions[p.name] ?? "")}
                  onChange={(e) => {
                    const next = { ...form.conditions };
                    if (e.target.value === "") delete next[p.name];
                    else {
                      const o = p.options?.find(
                        (o) => String(o.v ?? o) === e.target.value,
                      );
                      next[p.name] =
                        p.type === "boolean"
                          ? e.target.value === "true"
                          : (o?.v ?? o ?? e.target.value);
                    }
                    set("conditions", next);
                  }}
                >
                  <option value="">不限</option>
                  {(p.type === "boolean"
                    ? [
                        { l: "是", v: true },
                        { l: "否", v: false },
                      ]
                    : p.options || []
                  ).map((o, i) => (
                    <option value={String(o.v ?? o)} key={i}>
                      {String(o.l ?? o)}
                    </option>
                  ))}
                </select>
              )}
            </Field>
          ))}
        <Field label="状态">
          <select
            value={String(form.enabled)}
            onChange={(e) => set("enabled", e.target.value === "true")}
          >
            <option value="true">启用</option>
            <option value="false">停用</option>
          </select>
        </Field>
        <div className="modal-footer dialog-actions">
          <button type="button" className="button" onClick={onClose}>
            取消
          </button>
          <button className="button primary" disabled={busy}>
            保存价格
          </button>
        </div>
      </Form>
    </Modal>
  );
}
function ProviderAdmin() {
  const confirm = useConfirm();
  const { data, isLoading, error, refetch } =
    useApi<RecordData[]>("/admin/providers");
  const [edit, setEdit] = useState<RecordData | null>(null);
  const [catalog, setCatalog] = useState<RecordData | null>(null);
  const { act, busy } = useAction();
  const toast = useToast();
  const refresh = useRefresh();
  const protocols: RecordData = {
    inventory: "供应商清单 · 独立路由",
    demo: "本地演示",
    "openai-image": "OpenAI 兼容 · 图片",
    "openai-video": "OpenAI 兼容 · 视频",
    "json-async": "通用 JSON · 异步",
    openvideo: "OpenVideo · 图片/视频",
    suda: "Suda · 图片/视频",
    modelhub: "ModelHub · 图片/视频",
    gway: "Gway · 图片/视频",
    catcoder: "Catcoder · 图片/视频",
    wxart: "WXArt · 图片/视频",
    rolldek: "Rolldek · 图片/视频",
    maitoken: "Mai-Token · 图片/视频",
    opencrow: "OpenCrow · 图片/视频/文本/语音",
    "media-upload": "公共素材上传服务",
  };
  return (
    <section className="panel">
      <div className="section-header">
        <div>
          <h2>供应商渠道</h2>
          <p className="muted">配置渠道支持的模型，按需发布到模型管理。</p>
        </div>
        <button className="button primary" onClick={() => setEdit({})}>
          <Plus size={16} />
          新增渠道
        </button>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : (
        <div className="provider-grid">
          {data
            ?.slice()
            .sort(
              (a, b) =>
                Number(b.protocol === "opencrow" && !b.hasKey) -
                  Number(a.protocol === "opencrow" && !a.hasKey) ||
                Number(!!b.config?.preferred) - Number(!!a.config?.preferred),
            )
            .map((p) => (
              <article className="provider-card" key={p.id}>
                <header>
                  <div className="provider-icon">
                    <Cable size={21} />
                  </div>
                  <div>
                    <h3>
                      {p.name}
                      {p.config?.preferred && (
                        <small className="provider-default-label">默认</small>
                      )}
                    </h3>
                    <small>{protocols[p.protocol]}</small>
                  </div>
                  <Badge status={p.enabled ? "active" : "disabled"} />
                </header>
                <div className="provider-meta">
                  <span>API 地址</span>
                  <code>
                    {p.base_url ||
                      (p.protocol === "inventory"
                        ? "由各路由独立配置"
                        : "本地演示，无需外部连接")}
                  </code>
                  <span>密钥状态</span>
                  <strong className={p.hasKey ? "green" : "muted"}>
                    {p.protocol === "inventory"
                      ? "由各路由独立加密保存"
                      : p.hasKey
                        ? "已配置 · 加密保存"
                        : p.protocol === "demo"
                          ? "无需密钥"
                          : "未配置"}
                  </strong>
                  <span>
                    {p.protocol === "media-upload" ? "服务用途" : "支持模型"}
                  </span>
                  <strong>
                    {p.protocol === "media-upload" ? (
                      "提供公网 HTTPS 参考素材"
                    ) : (
                      <>
                        {p.supportedCount || 0} 个{" "}
                        <small className="muted">
                          · 已发布 {p.publishedCount || 0} 个
                        </small>
                      </>
                    )}
                  </strong>
                </div>
                <footer>
                  {p.protocol === "opencrow" && !p.hasKey && (
                    <button
                      className="button primary"
                      onClick={() => setEdit(p)}
                    >
                      <KeyRound size={15} />
                      配置 API Key
                    </button>
                  )}
                  {p.protocol === "inventory" && (
                    <Link className="button primary" to="/admin/inventory">
                      管理独立路由
                    </Link>
                  )}
                  {p.protocol !== "media-upload" &&
                    p.protocol !== "inventory" && (
                      <button
                        className="button primary"
                        aria-label={"管理模型 " + p.name}
                        onClick={() => setCatalog(p)}
                      >
                        <Boxes size={15} />
                        管理模型
                      </button>
                    )}
                  <button
                    className="button"
                    disabled={busy}
                    onClick={() =>
                      act(async () => {
                        const result = await post(
                          "/admin/providers/" +
                            p.id +
                            (p.protocol === "media-upload"
                              ? "/test-upload"
                              : "/test"),
                        );
                        toast(result.message, !result.ok);
                      })
                    }
                  >
                    <RefreshCw size={14} />
                    {p.protocol === "media-upload" ? "验证上传" : "测试连接"}
                  </button>
                  <button
                    className="icon-button"
                    title="编辑渠道"
                    onClick={() => setEdit(p)}
                  >
                    <Pencil size={16} />
                  </button>
                  <button
                    className="icon-button danger"
                    title="删除渠道"
                    onClick={async () => {
                      if (
                        await confirm(
                          "确定删除此供应商渠道及其未发布的模型列表？",
                          {
                            title: "删除供应商",
                            confirmLabel: "删除渠道",
                          },
                        )
                      )
                        act(async () => {
                          await api("/admin/providers/" + p.id, {
                            method: "DELETE",
                          });
                          refresh("/admin/providers");
                        }, "渠道已删除");
                    }}
                  >
                    <Trash2 size={16} />
                  </button>
                </footer>
              </article>
            ))}
        </div>
      )}
      {edit && <ProviderForm initial={edit} onClose={() => setEdit(null)} />}
      {catalog && (
        <ProviderModels
          provider={data?.find((item) => item.id === catalog.id) || catalog}
          onClose={() => setCatalog(null)}
        />
      )}
    </section>
  );
}
function ProviderForm({
  initial,
  onClose,
}: {
  initial: RecordData;
  onClose: () => void;
}) {
  const [form, setForm] = useState<RecordData>({
    name: "",
    protocol: "openai-image",
    base_url: "",
    apiKey: "",
    enabled: true,
    config: {},
    ...initial,
  });
  const [config, setConfig] = useState(
    JSON.stringify(initial.config || {}, null, 2),
  );
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const set = (k: string, v: any) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <Modal
      title={initial.id ? "编辑供应商渠道" : "新增供应商渠道"}
      description="配置上游服务地址、协议与访问凭证。"
      icon={<Cable size={20} />}
      onClose={onClose}
      wide
    >
      <Form
        onSubmit={() =>
          act(async () => {
            await post(
              "/admin/providers" + (initial.id ? "/" + initial.id : ""),
              { ...form, config: JSON.parse(config) },
              initial.id ? "PUT" : "POST",
            );
            refresh("/admin/providers", "/admin/models", "/models");
            onClose();
          }, "渠道已保存")
        }
      >
        <div className="two-columns">
          <Field label="渠道名称">
            <input
              value={form.name}
              onChange={(e) => set("name", e.target.value)}
              required
            />
          </Field>
          <Field label="接入协议">
            <select
              value={form.protocol}
              onChange={(e) => {
                const protocol = e.target.value;
                setForm((current) => ({
                  ...current,
                  protocol,
                  ...(protocol === "opencrow"
                    ? {
                        name: current.name || "OpenCrow",
                        base_url:
                          current.base_url || "https://api.router.ai/v1",
                      }
                    : {}),
                }));
              }}
            >
              <option value="openai-image">OpenAI 兼容图片接口</option>
              <option value="openai-video">OpenAI 兼容视频接口</option>
              <option value="json-async">通用 JSON 异步任务</option>
              <option value="openvideo">OpenVideo 专用适配</option>
              <option value="suda">Suda 速达专用适配</option>
              <option value="modelhub">ModelHub 专用适配</option>
              <option value="gway">Gway 专用适配</option>
              <option value="catcoder">Catcoder 专用适配</option>
              <option value="wxart">WXArt 专用适配</option>
              <option value="rolldek">Rolldek 专用适配</option>
              <option value="maitoken">Mai-Token 专用适配</option>
              <option value="opencrow">OpenCrow 专用适配</option>
              <option value="media-upload">公共素材上传服务</option>
              <option value="demo">本地演示</option>
            </select>
          </Field>
        </div>
        <Field
          label="API 基础地址"
          hint={
            form.protocol === "opencrow"
              ? "OpenCrow 文档地址：https://api.router.ai/v1。保存后同步模型，确认本站价格再启用。"
              : "填写到版本前缀，例如 https://your-provider.com/v1"
          }
        >
          <input
            type={form.protocol === "demo" ? "text" : "url"}
            value={form.base_url}
            onChange={(e) => set("base_url", e.target.value)}
            required={form.protocol !== "demo"}
            placeholder="https://your-provider.com/v1"
          />
        </Field>
        <Field
          label="API Key"
          hint={
            initial.hasKey
              ? "已配置密钥；留空保留原密钥。"
              : "密钥只会保存到服务端，不会返回到前端。"
          }
        >
          <input
            type="password"
            value={form.apiKey}
            autoFocus={form.protocol === "opencrow" && !initial.hasKey}
            onChange={(e) => set("apiKey", e.target.value)}
            placeholder={
              initial.hasKey ? "••••••••（留空不修改）" : "输入供应商 API Key"
            }
            autoComplete="new-password"
          />
        </Field>
        <Field label="状态">
          <select
            value={String(form.enabled)}
            onChange={(e) => set("enabled", e.target.value === "true")}
          >
            <option value="true">启用</option>
            <option value="false">停用</option>
          </select>
        </Field>
        <details>
          <summary>高级接口配置</summary>
          <p className="muted small">
            通用协议可配置 submitPath、statusPath（包含 {`{id}`}
            ）、idPath、statusField、outputPath、pollInterval。输出路径使用点分隔。
          </p>
          <JsonEditor value={config} onChange={setConfig} />
          <button
            type="button"
            className="button"
            onClick={() =>
              setConfig(
                JSON.stringify(
                  {
                    submitPath: "/generate/video",
                    statusPath: "/tasks/{id}",
                    idPath: "data.task_id",
                    statusField: "data.status",
                    outputPath: "data.result_urls",
                    pollInterval: 3000,
                  },
                  null,
                  2,
                ),
              )
            }
          >
            填入通用协议示例
          </button>
        </details>
        <div className="modal-footer">
          <span />
          <div>
            <button className="button" type="button" onClick={onClose}>
              取消
            </button>
            <button className="button primary" disabled={busy}>
              保存渠道
            </button>
          </div>
        </div>
      </Form>
    </Modal>
  );
}
function UserAdmin() {
  const moneyPost = useMoneyMutation();
  const confirm = useConfirm();
  const { data, isLoading, error, refetch } = useApi<User[]>("/admin/users");
  const [search, setSearch] = useState("");
  const [adjust, setAdjust] = useState<User | null>(null);
  const [amount, setAmount] = useState(100);
  const [reason, setReason] = useState("");
  const { act, busy } = useAction();
  const refresh = useRefresh();
  return (
    <section className="panel">
      <div className="section-header">
        <div>
          <h2>用户管理</h2>
          <p className="muted">管理账户状态、角色与余额，金额以人民币元计。</p>
        </div>
        <SearchBox
          value={search}
          onChange={setSearch}
          placeholder="搜索用户名或昵称"
        />
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
                <th>用户</th>
                <th>角色</th>
                <th>账户余额</th>
                <th>注册时间</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {data
                ?.filter((u) =>
                  (u.username + " " + u.displayName).includes(search),
                )
                .map((u) => (
                  <tr key={u.id}>
                    <td>
                      <strong>{u.displayName}</strong>
                      <small className="small-code muted">@{u.username}</small>
                    </td>
                    <td>
                      <select
                        aria-label={u.username + "角色"}
                        value={u.role}
                        onChange={async (e) => {
                          const role = e.target.value;
                          if (
                            await confirm(
                              "确认修改 " +
                                u.username +
                                " 的角色为 " +
                                role +
                                "？",
                            )
                          )
                            act(async () => {
                              await post(
                                "/admin/users/" + u.id,
                                { role },
                                "PATCH",
                              );
                              refresh("/admin/users");
                            }, "角色已更新");
                        }}
                      >
                        <option value="user">用户</option>
                        <option value="admin">管理员</option>
                      </select>
                    </td>
                    <td className="gold">{money(u.balance)}</td>
                    <td>{date(u.createdAt)}</td>
                    <td>
                      <Badge status={u.status} />
                    </td>
                    <td>
                      <div className="row-actions">
                        <button
                          className="text-link"
                          onClick={() => {
                            setAdjust(u);
                            setReason("");
                          }}
                        >
                          调整余额
                        </button>
                        <button
                          className={
                            "text-link " +
                            (u.status === "active" ? "danger" : "green")
                          }
                          disabled={busy}
                          onClick={async () => {
                            if (
                              await confirm(
                                `确认${u.status === "active" ? "停用" : "启用"}账户 ${u.username}？`,
                              )
                            )
                              act(async () => {
                                await post(
                                  "/admin/users/" + u.id,
                                  {
                                    status:
                                      u.status === "active"
                                        ? "disabled"
                                        : "active",
                                  },
                                  "PATCH",
                                );
                                refresh("/admin/users");
                              }, "账户状态已更新");
                          }}
                        >
                          {u.status === "active" ? "停用" : "启用"}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      )}
      {adjust && (
        <Modal
          title={"调整余额 · " + adjust.username}
          onClose={() => setAdjust(null)}
        >
          <Form
            onSubmit={() =>
              act(async () => {
                await moneyPost("/admin/users/" + adjust.id + "/credits", {
                  amount,
                  reason,
                });
                refresh("/admin/users", "/me", "/admin/transactions");
                setAdjust(null);
              }, "余额已调整")
            }
          >
            <p>
              当前余额：
              <strong className="gold">{money(adjust.balance)}</strong>
            </p>
            <Field label="调整金额（元，负数表示扣减）">
              <input
                type="number"
                step="0.01"
                value={amount}
                onChange={(e) => setAmount(Number(e.target.value))}
                required
              />
            </Field>
            <Field label="调整原因">
              <textarea
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                minLength={2}
                required
                placeholder="例如：活动奖励、补偿退款"
              />
            </Field>
            <button className="button primary full" disabled={busy}>
              确认调整
            </button>
          </Form>
        </Modal>
      )}
    </section>
  );
}
function CodeAdmin() {
  const confirm = useConfirm();
  const [create, setCreate] = useState(false);
  const [count, setCount] = useState(10);
  const [credits, setCredits] = useState(100);
  const [expiry, setExpiry] = useState("");
  const [filter, setFilter] = useState("all");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [batch, setBatch] = useState<string[]>([]);
  const { data, isLoading, error, refetch } = useApi<PageData<RecordData>>(
    "/admin/codes?" +
      new URLSearchParams({
        status: filter,
        search,
        page: String(page),
        limit: "20",
      }),
    { refetchInterval: 10000 },
  );
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const toast = useToast();
  const rows = data?.items || [];
  return (
    <section className="panel">
      <div className="section-header">
        <div>
          <h2>兑换码管理</h2>
          <p className="muted">
            兑换金额以人民币元计，每个兑换码仅可使用一次；可查询使用者及到账时间。
          </p>
        </div>
        <div className="header-actions">
          <button
            className="button"
            onClick={() => downloadJSON(rows, "兑换码-当前页.json")}
            disabled={!rows.length}
          >
            <Download size={15} />
            导出本页
          </button>
          <button className="button primary" onClick={() => setCreate(true)}>
            <Plus size={16} />
            生成兑换码
          </button>
        </div>
      </div>
      {!!batch.length && (
        <div className="code-batch-result">
          <span>
            已生成 <strong>{batch.length}</strong> 个兑换码
          </span>
          <button
            className="text-link"
            onClick={() =>
              copy(batch.join("\n"))
                .then(() => toast("本批兑换码已复制"))
                .catch(() => toast("复制失败", true))
            }
          >
            复制本批
          </button>
          <button
            className="text-link"
            onClick={() => downloadJSON(batch, "本批兑换码.json")}
          >
            导出本批
          </button>
          <button className="text-link" onClick={() => setBatch([])}>
            收起
          </button>
        </div>
      )}
      <div className="admin-code-filters">
        <Tabs
          value={filter}
          onChange={(value) => {
            setFilter(value);
            setPage(1);
          }}
          items={[
            { value: "all", label: "全部" },
            { value: "unused", label: "可兑换" },
            { value: "used", label: "已使用" },
            { value: "expired", label: "已过期" },
          ]}
        />
        <SearchBox
          value={search}
          onChange={(value) => {
            setSearch(value);
            setPage(1);
          }}
          placeholder="搜索兑换码或使用者"
        />
        <span className="muted small">{number(data?.total || 0)} 个</span>
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
                <th>兑换码</th>
                <th>兑换金额（元）</th>
                <th>状态</th>
                <th>使用者</th>
                <th>兑换时间</th>
                <th>到期时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.id}>
                  <td>
                    <code>{c.code}</code>
                  </td>
                  <td className="gold">{money(c.credits)}</td>
                  <td>
                    <Badge
                      status={
                        c.status === "used"
                          ? "disabled"
                          : c.status === "expired"
                            ? "failed"
                            : "active"
                      }
                    >
                      {c.status === "used"
                        ? "已使用"
                        : c.status === "expired"
                          ? "已过期"
                          : "可兑换"}
                    </Badge>
                  </td>
                  <td>{c.username || "—"}</td>
                  <td className="nowrap">{date(c.used_at)}</td>
                  <td>{c.expires_at ? date(c.expires_at) : "永久有效"}</td>
                  <td>
                    <div className="row-actions">
                      <button
                        className="icon-button"
                        aria-label="复制兑换码"
                        onClick={() =>
                          copy(c.code)
                            .then(() => toast("兑换码已复制"))
                            .catch(() => toast("复制失败", true))
                        }
                      >
                        <Copy size={15} />
                      </button>
                      <button
                        className="icon-button danger"
                        aria-label="删除兑换码"
                        disabled={!!c.used_by}
                        onClick={async () => {
                          if (
                            await confirm(
                              "删除后此兑换码将无法使用，确定继续？",
                              {
                                title: "删除兑换码",
                                confirmLabel: "删除兑换码",
                              },
                            )
                          )
                            act(async () => {
                              await api("/admin/codes/" + c.id, {
                                method: "DELETE",
                              });
                              refresh("/admin/codes");
                            }, "兑换码已删除");
                        }}
                      >
                        <Trash2 size={15} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!rows.length && <Empty title="暂无兑换码" />}
        </div>
      )}
      <Pagination
        page={page}
        total={data?.total}
        limit={20}
        onChange={setPage}
      />
      {create && (
        <Modal
          title="批量生成兑换码"
          description="用户在兑换充值页使用后，额度会计入账户余额。"
          icon={<Ticket size={20} />}
          onClose={() => setCreate(false)}
        >
          <Form
            onSubmit={() =>
              act(async () => {
                const generated = await post<string[]>("/admin/codes", {
                  count,
                  credits,
                  expiresAt: expiry
                    ? new Date(expiry + "T23:59:59").toISOString()
                    : null,
                });
                setBatch(generated);
                setPage(1);
                setFilter("all");
                setSearch("");
                refresh("/admin/codes");
                setCreate(false);
              }, "兑换码已生成")
            }
          >
            <Field label="数量">
              <input
                type="number"
                min={1}
                max={100}
                value={count}
                onChange={(e) => setCount(Number(e.target.value))}
                required
              />
            </Field>
            <Field label="每张额度（元）">
              <input
                type="number"
                min="0.01"
                step="0.01"
                value={credits}
                onChange={(e) => setCredits(Number(e.target.value))}
                required
              />
            </Field>
            <Field label="到期日期（留空永久有效）">
              <input
                type="date"
                value={expiry}
                onChange={(e) => setExpiry(e.target.value)}
              />
            </Field>
            <div className="modal-footer">
              <span className="muted">总金额 {money(count * credits)}</span>
              <button className="button primary" disabled={busy}>
                生成 {count} 个兑换码
              </button>
            </div>
          </Form>
        </Modal>
      )}
    </section>
  );
}
function SettingsAdmin() {
  const { data, error, refetch } = useApi<RecordData>("/admin/settings");
  const [form, setForm] = useState<RecordData | null>(null);
  useEffect(() => {
    if (data) setForm(data);
  }, [data]);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  if (error) return <ErrorState error={error} retry={refetch} />;
  if (!form) return <Loading />;
  const set = (k: string, v: string) => setForm((f) => ({ ...f, [k]: v }));
  return (
    <>
      <section className="panel settings-panel">
        <h2>站点设置</h2>
        <p className="muted">更改将同步到登录页面、导航和账户中心。</p>
        <Form
          onSubmit={() =>
            act(async () => {
              await post("/admin/settings", form, "PUT");
              refresh("/config", "/admin/settings");
            }, "站点设置已保存")
          }
        >
          <div className="two-columns">
            <Field label="站点名称">
              <input
                value={form.siteName}
                onChange={(e) => set("siteName", e.target.value)}
                required
              />
            </Field>
            <Field label="客服联系方式">
              <input
                value={form.support}
                onChange={(e) => set("support", e.target.value)}
              />
            </Field>
            <Field label="开放注册">
              <select
                value={form.registrationEnabled}
                onChange={(e) => set("registrationEnabled", e.target.value)}
              >
                <option value="true">开启</option>
                <option value="false">关闭</option>
              </select>
            </Field>
            <Field label="新用户赠送金额（元）">
              <input
                type="number"
                min={0}
                step="0.01"
                value={form.welcomeCredits}
                onChange={(e) => set("welcomeCredits", e.target.value)}
              />
            </Field>
          </div>
          <Field label="站点副标题">
            <input
              value={form.subtitle}
              onChange={(e) => set("subtitle", e.target.value)}
            />
          </Field>
          <Field label="充值购买链接">
            <input
              type="url"
              value={form.purchaseUrl}
              onChange={(e) => set("purchaseUrl", e.target.value)}
              placeholder="https://…"
            />
          </Field>
          <Field label="首页公告">
            <textarea
              value={form.announcement}
              onChange={(e) => set("announcement", e.target.value)}
              rows={4}
              placeholder="留空不显示公告"
            />
          </Field>
          <button className="button primary" disabled={busy}>
            保存设置
          </button>
        </Form>
      </section>
      <StorageSettings />
    </>
  );
}
