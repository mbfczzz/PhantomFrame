import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Upload, Settings2 } from "lucide-react";
import {
  api,
  post,
  useApi,
  useAction,
  useRefresh,
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

function routeState(row: RecordData) {
  if (!row.providerEnabled) return "供应商已停用";
  if (!row.sourceEnabled) return "源路由已停用";
  if (!row.baseURL) return "待补地址";
  if (!row.hasKey) return "待补密钥";
  if (!row.hasPrices) return "待定价";
  return row.enabled ? "已启用" : "模型未启用";
}

export default function ProviderInventory() {
  const { data, isLoading, error, refetch } = useApi<RecordData[]>(
    "/admin/provider-inventory",
  );
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<RecordData | null>(null);
  const [importResult, setImportResult] = useState<RecordData | null>(null);
  const [edit, setEdit] = useState<RecordData | null>(null);
  const [key, setKey] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const rows = (data || []).filter((row) =>
    `${row.vendor} ${row.model} ${row.upstreamModel} ${row.id} ${routeState(row)}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  const inspect = (selected: File) =>
    act(async () => {
      const form = new FormData();
      form.append("file", selected);
      const result = await api("/admin/provider-inventory/import", {
        method: "POST",
        body: form,
      });
      setFile(selected);
      setPreview(result);
    });
  return (
    <section className="panel">
      <PageHeader
        title="供应商路由"
        description="每条路由独立保存地址、密钥和模型映射。缺少配置或价格的模型无法发起生成。"
      >
        <input
          type="file"
          accept=".csv,text/csv"
          ref={fileInput}
          hidden
          onChange={(event) => {
            const selected = event.target.files?.[0];
            if (selected) void inspect(selected);
            event.target.value = "";
          }}
        />
        <button
          className="button primary"
          disabled={busy}
          onClick={() => fileInput.current?.click()}
        >
          <Upload size={16} />
          导入 CSV
        </button>
      </PageHeader>
      {importResult && (
        <p role="status" className="muted">
          导入完成：{importResult.pricesInherited || 0} 个模型沿用同名价格，
          {importResult.pricesPreserved || 0} 个保留已有价格规则，
          {importResult.pricesPending?.length || 0}{" "}
          个待定价。新增模型需检查后启用。
        </p>
      )}
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
          {new Set((data || []).map((r) => r.vendor)).size} 家供应商 ·{" "}
          {data?.length || 0} 条路由
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
                          setKey("");
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
      {preview && file && (
        <Modal
          title="导入供应商路由"
          onClose={() => {
            if (!busy) {
              setPreview(null);
              setFile(null);
            }
          }}
        >
          <p>
            将处理 {preview.providers} 家供应商、{preview.routes} 条路由；排除{" "}
            {preview.excluded3D} 条 3D 路由。
          </p>
          <p>
            新增模型默认停用。同名且规格兼容的模型沿用已有价格；未匹配或价格有冲突的模型保留待定。重复导入保留已有售价与参数，确认配置后再启用。
          </p>
          <p>{preview.warnings?.length || 0} 条路由需要补充地址或有效密钥。</p>
          <div className="modal-footer">
            <button
              className="button"
              disabled={busy}
              onClick={() => {
                setPreview(null);
                setFile(null);
              }}
            >
              取消
            </button>
            <button
              className="button primary"
              disabled={busy}
              onClick={() =>
                act(async () => {
                  const form = new FormData();
                  form.append("file", file);
                  form.append("apply", "true");
                  const result = await api("/admin/provider-inventory/import", {
                    method: "POST",
                    body: form,
                  });
                  setImportResult(result);
                  setPreview(null);
                  setFile(null);
                  refresh(
                    "/admin/provider-inventory",
                    "/admin/models",
                    "/admin/providers",
                  );
                }, "路由已导入，请检查待配置项")
              }
            >
              确认导入
            </button>
          </div>
        </Modal>
      )}
      {edit && (
        <Modal
          title={`路由配置 · ${edit.vendor}`}
          onClose={() => {
            setEdit(null);
            setKey("");
          }}
        >
          <Form
            onSubmit={() =>
              act(async () => {
                await post(
                  `/admin/provider-inventory/${edit.id}`,
                  {
                    baseURL: edit.baseURL,
                    apiKey: key,
                    sourceEnabled: edit.sourceEnabled,
                  },
                  "PATCH",
                );
                setEdit(null);
                setKey("");
                refresh("/admin/provider-inventory", "/admin/models");
              }, "路由已保存")
            }
          >
            <p>{edit.model}</p>
            <Field label="API 地址">
              <input
                value={edit.baseURL}
                onChange={(e) => setEdit({ ...edit, baseURL: e.target.value })}
                maxLength={500}
              />
            </Field>
            <Field label="API Key">
              <input
                type="password"
                autoComplete="new-password"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder={
                  edit.hasKey ? "已配置，留空保留" : "填写此路由的独立密钥"
                }
              />
            </Field>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={edit.sourceEnabled}
                onChange={(e) =>
                  setEdit({ ...edit, sourceEnabled: e.target.checked })
                }
              />
              允许此路由参与调用
            </label>
            <p className="muted">
              模型还需配置价格并启用。密钥保存后不会回显。
            </p>
            <button className="button primary full" disabled={busy}>
              保存路由
            </button>
          </Form>
        </Modal>
      )}
    </section>
  );
}
