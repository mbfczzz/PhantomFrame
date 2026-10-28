import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Settings2, RefreshCw, Eye, EyeOff } from "lucide-react";
import {
  post,
  useApi,
  useAction,
  useRefresh,
  useConfirm,
  PageHeader,
  Loading,
  ErrorState,
  SearchBox,
  Pagination,
  Modal,
  Field,
  Form,
  generationLabels,
  type RecordData,
} from "../lib";

import {
  ProviderInventoryImport,
  ProviderInventoryImportResult,
} from "../components/ProviderInventoryImport";

function routeState(row: RecordData) {
  if (!row.providerEnabled)
    return row.baseURL ? "供应商已停用" : "供应商已停用 · 待补地址";
  if (!row.sourceEnabled)
    return row.baseURL ? "源路由已停用" : "源路由已停用 · 待补地址";
  if (!row.baseURL) return "待补地址";
  if (!row.hasKey) return "待补密钥";
  if (!row.hasPrices) return "待定价";
  return row.enabled ? "已启用" : "模型未启用";
}

export default function ProviderInventory() {
  const [query] = useSearchParams();
  const providerId = query.get("provider");
  return (
    <ProviderInventoryScope key={providerId || "all"} providerId={providerId} />
  );
}

function ProviderInventoryScope({ providerId }: { providerId: string | null }) {
  const { data, isLoading, error, refetch } = useApi<RecordData[]>(
    "/admin/provider-inventory",
  );
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [importResult, setImportResult] = useState<RecordData | null>(null);
  const [edit, setEdit] = useState<RecordData | null>(null);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const confirm = useConfirm();
  const scoped = (data || []).filter(
    (row) => !providerId || row.providerId === providerId,
  );
  const rows = scoped.filter((row) =>
    `${row.vendor} ${row.model} ${row.upstreamModel} ${row.id} ${routeState(row)}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <section className="panel">
      <PageHeader
        title="供应商路由"
        description="每条路由独立保存地址、密钥和模型映射。缺少配置或价格的模型无法发起生成。"
      >
        {providerId && (
          <Link className="button" to="/admin/inventory">
            全部供应商
          </Link>
        )}
        <ProviderInventoryImport onImported={setImportResult} />
      </PageHeader>
      <ProviderInventoryImportResult result={importResult} />
      <div className="toolbar">
        <SearchBox
          value={search}
          onChange={(value) => {
            setSearch(value);
            setPage(1);
          }}
          placeholder="搜索供应商、模型、路由或待配置状态"
        />
        <span className="muted">
          {new Set(scoped.map((r) => r.vendor)).size} 家供应商 · {scoped.length}{" "}
          条路由
        </span>
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
                <th>供应商 / 模型</th>
                <th>类型</th>
                <th>上游模型</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.slice((page - 1) * 24, page * 24).map((row) => (
                <tr key={row.id}>
                  <td>
                    <strong>{row.vendor}</strong>
                    <div>{row.model}</div>
                    <small className="muted">{row.id}</small>
                  </td>
                  <td>{generationLabels[row.type] || row.type}</td>
                  <td className="inventory-upstream-id">{row.upstreamModel}</td>
                  <td>{routeState(row)}</td>
                  <td>
                    <div className="row-actions">
                      <button
                        className="button small"
                        onClick={() => {
                          setEdit({ ...row });
                        }}
                      >
                        <Settings2 size={14} />
                        路由配置
                      </button>
                      <Link
                        className="button small"
                        to={`/admin/models?model=${encodeURIComponent(row.modelId)}&tab=pricing`}
                      >
                        模型与价格
                      </Link>
                      <button
                        className="button small"
                        disabled={busy}
                        onClick={async () => {
                          if (
                            await confirm(
                              "将恢复此路由当前支持的参数并停用模型，保留已有价格。请核对价格与新规格后重新启用。",
                              {
                                title: "更新参数规范",
                                confirmLabel: "更新并停用模型",
                              },
                            )
                          ) {
                            act(async () => {
                              await post(
                                `/admin/provider-inventory/${encodeURIComponent(row.id)}/parameters`,
                                {},
                              );
                              refresh(
                                "/admin/provider-inventory",
                                "/admin/models",
                                "/models",
                                "/pricing/quote",
                              );
                            }, "参数已更新，请核对价格后启用模型");
                          }
                        }}
                      >
                        <RefreshCw size={14} />
                        更新参数规范
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!rows.length && (
            <p className="muted">
              没有匹配的路由。可导入供应商 CSV 或更换搜索条件。
            </p>
          )}
        </div>
      )}
      <Pagination page={page} total={rows.length} onChange={setPage} />
      {edit && (
        <InventoryRouteEditor
          key={edit.id}
          initial={edit}
          onClose={() => setEdit(null)}
        />
      )}
    </section>
  );
}

function InventoryRouteEditor({
  initial,
  onClose,
}: {
  initial: RecordData;
  onClose: () => void;
}) {
  const [form, setForm] = useState(initial);
  const [draftKey, setDraftKey] = useState("");
  const [keyEdited, setKeyEdited] = useState(false);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [visible, setVisible] = useState(false);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  return (
    <Modal title={`路由配置 · ${initial.vendor}`} onClose={onClose}>
      <Form
        onSubmit={() =>
          act(async () => {
            await post(
              `/admin/provider-inventory/${encodeURIComponent(initial.id)}`,
              {
                baseURL: form.baseURL,
                // Viewing a saved key must not write it back over a concurrent rotation.
                apiKey: keyEdited ? draftKey : "",
                sourceEnabled: form.sourceEnabled,
              },
              "PATCH",
            );
            refresh("/admin/provider-inventory", "/admin/models");
            onClose();
          }, "路由已保存")
        }
      >
        <p>{initial.model}</p>
        <Field label="API 地址">
          <input
            value={form.baseURL}
            onChange={(e) => setForm({ ...form, baseURL: e.target.value })}
            maxLength={500}
          />
        </Field>
        <Field label="API Key" hint="留空保留现有密钥；输入新值并保存后替换。">
          <div className="input-icon">
            <input
              type={visible ? "text" : "password"}
              autoComplete="new-password"
              autoFocus
              spellCheck={false}
              disabled={busy}
              value={keyEdited ? draftKey : (revealedKey ?? "")}
              onChange={(e) => {
                setKeyEdited(true);
                setDraftKey(e.target.value);
              }}
              placeholder={
                initial.hasKey ? "已配置，点击右侧查看" : "填写此路由的独立密钥"
              }
            />
            <button
              type="button"
              className="icon-button"
              aria-label={visible ? "隐藏密钥" : "显示密钥"}
              title={visible ? "隐藏密钥" : "显示密钥"}
              aria-pressed={visible}
              disabled={busy}
              onClick={() => {
                if (visible) {
                  setVisible(false);
                  setRevealedKey(null);
                } else if (keyEdited || !initial.hasKey) {
                  setVisible(true);
                } else {
                  void act(async () => {
                    const result = await post<{ apiKey: string }>(
                      `/admin/provider-inventory/${encodeURIComponent(initial.id)}/key`,
                    );
                    setRevealedKey(result.apiKey);
                    setVisible(true);
                  });
                }
              }}
            >
              {visible ? <EyeOff size={17} /> : <Eye size={17} />}
            </button>
          </div>
        </Field>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={form.sourceEnabled}
            onChange={(e) =>
              setForm({ ...form, sourceEnabled: e.target.checked })
            }
          />
          允许此路由参与调用
        </label>
        <p className="muted">
          模型还需配置价格并启用。更换地址或停用路由会停用模型，请核对后重新启用。密钥默认隐藏，点击眼睛可查看。
        </p>
        <button className="button primary full" disabled={busy}>
          保存路由
        </button>
      </Form>
    </Modal>
  );
}
