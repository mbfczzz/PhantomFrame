import { useRef, useState } from "react";
import { Upload } from "lucide-react";
import { api, Modal, useAction, useRefresh, type RecordData } from "../lib";

export function ProviderInventoryImport({
  onImported,
}: {
  onImported: (result: RecordData) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [selection, setSelection] = useState<{
    file: File;
    preview: RecordData;
  } | null>(null);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  const inspect = (file: File) =>
    act(async () => {
      if (file.size > 8 * 1024 * 1024) {
        throw new Error("请选择不超过 8MB 的 UTF-8 CSV 文件");
      }
      const form = new FormData();
      form.append("file", file);
      const preview = await api<RecordData>(
        "/admin/provider-inventory/import",
        {
          method: "POST",
          body: form,
        },
      );
      setSelection({ file, preview });
    });
  return (
    <>
      <input
        type="file"
        accept=".csv,text/csv"
        aria-label="供应商 CSV 文件"
        ref={input}
        hidden
        disabled={busy || !!selection}
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) void inspect(file);
          event.target.value = "";
        }}
      />
      <button
        type="button"
        className="button"
        disabled={busy || !!selection}
        onClick={() => input.current?.click()}
      >
        <Upload size={16} />
        {busy ? "正在处理 CSV…" : "导入 CSV"}
      </button>
      {selection && (
        <Modal
          title="导入供应商路由"
          description="批量导入供应商地址、独立密钥和模型映射。"
          onClose={() => {
            if (!busy) setSelection(null);
          }}
        >
          <p>
            将处理 {selection.preview.providers} 家供应商、
            {selection.preview.routes} 条路由；排除{" "}
            {selection.preview.excluded3D} 条 3D 路由。
          </p>
          <p>
            重复导入会按 CSV
            替换同一路由的地址、密钥（包括空值）、模型映射和源启用状态。已有售价和参数保留。
          </p>
          <p>
            新增模型默认停用。同名且规格兼容的模型沿用已有价格，其余待定；请核对后启用。密钥加密保存，管理员可在路由配置中点击眼睛查看。
          </p>
          <p>
            {selection.preview.warnings?.length || 0}{" "}
            条路由需要补充地址或有效密钥。
          </p>
          <p className="muted">确认前不保存配置；导入不会调用模型或扣费。</p>
          <div className="modal-footer">
            <button
              type="button"
              className="button"
              disabled={busy}
              onClick={() => setSelection(null)}
            >
              取消
            </button>
            <button
              type="button"
              className="button primary"
              disabled={busy || !selection.preview.routes}
              onClick={() =>
                act(async () => {
                  const form = new FormData();
                  form.append("file", selection.file);
                  form.append("apply", "true");
                  const result = await api<RecordData>(
                    "/admin/provider-inventory/import",
                    { method: "POST", body: form },
                  );
                  onImported(result);
                  setSelection(null);
                  refresh(
                    "/admin/provider-inventory",
                    "/admin/models",
                    "/admin/providers",
                    "/models",
                    "/pricing/quote",
                  );
                }, "路由已导入，请检查待配置项")
              }
            >
              {busy ? "正在导入…" : "确认导入"}
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}

export function ProviderInventoryImportResult({
  result,
}: {
  result: RecordData | null;
}) {
  if (!result) return null;
  return (
    <p role="status" className="muted">
      导入完成：{result.providers} 家供应商、{result.routes} 条路由；
      {result.pricesInherited || 0} 个模型沿用同名价格，
      {result.pricesPreserved || 0} 个保留已有价格规则，
      {result.pricesPending?.length || 0}{" "}
      个待定价。请在供应商路由页检查配置，并核对模型售价后启用。
    </p>
  );
}
