import { useEffect, useState } from "react";
import { CloudUpload, ShieldCheck } from "lucide-react";
import {
  useApi,
  useAction,
  useRefresh,
  post,
  Field,
  Form,
  Loading,
  ErrorState,
  type RecordData,
} from "../lib";

export default function StorageSettings() {
  const { data, error, refetch } = useApi<RecordData>("/admin/storage");
  const [form, setForm] = useState<RecordData | null>(null);
  const [testMessage, setTestMessage] = useState("");
  const { act, busy } = useAction();
  const refresh = useRefresh();
  useEffect(() => {
    if (data) setForm({ ...data, accessKeyId: "", accessKeySecret: "" });
  }, [data]);
  if (error) return <ErrorState error={error} retry={refetch} />;
  if (!form) return <Loading />;
  const set = (name: string, value: unknown) => {
    setForm((current) => ({ ...current, [name]: value }));
    setTestMessage("");
  };
  return (
    <section className="panel settings-panel storage-settings">
      <div className="section-header">
        <div>
          <h2>
            <CloudUpload size={18} /> 生成结果存储
          </h2>
          <p className="muted">
            转存完成后才将任务标记为成功，并返回本站 OSS 或 CDN 链接。
          </p>
        </div>
      </div>
      <Form
        onSubmit={() =>
          act(async () => {
            const saved = await post<RecordData>("/admin/storage", form, "PUT");
            setForm({ ...saved, accessKeyId: "", accessKeySecret: "" });
            refresh("/admin/storage");
          }, "存储配置已保存")
        }
      >
        <label className="storage-switch">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => set("enabled", event.target.checked)}
          />
          生成结果转存到阿里云 OSS
        </label>
        <div className="two-columns">
          <Field label="Bucket">
            <input
              required
              value={form.bucket}
              onChange={(event) => set("bucket", event.target.value)}
            />
          </Field>
          <Field label="Region">
            <input
              required
              value={form.region}
              onChange={(event) => set("region", event.target.value)}
              placeholder="cn-shanghai"
            />
          </Field>
          <Field label="OSS Endpoint">
            <input
              type="url"
              required
              value={form.endpoint}
              onChange={(event) => set("endpoint", event.target.value)}
            />
          </Field>
          <Field
            label="上传加速域名"
            hint="填写包含 Bucket 的完整 HTTPS 域名。"
          >
            <input
              type="url"
              value={form.accelerateEndpoint}
              onChange={(event) =>
                set("accelerateEndpoint", event.target.value)
              }
            />
          </Field>
          <Field label="文件目录前缀">
            <input
              value={form.prefix}
              onChange={(event) => set("prefix", event.target.value)}
              placeholder="canvas/uploads/"
            />
          </Field>
          <Field
            label="CDN 域名"
            hint="留空使用 OSS 域名，填写后生成结果优先使用 CDN 链接。"
          >
            <input
              type="url"
              value={form.cdnDomain}
              onChange={(event) => set("cdnDomain", event.target.value)}
              placeholder="https://cdn.example.com"
            />
          </Field>
        </div>
        <div className="storage-options">
          <label className="storage-switch">
            <input
              type="checkbox"
              checked={form.uploadAccelerate}
              onChange={(event) =>
                set("uploadAccelerate", event.target.checked)
              }
            />
            开启上传加速
          </label>
          <label className="storage-switch">
            <input
              type="checkbox"
              checked={form.readAccelerate}
              onChange={(event) => set("readAccelerate", event.target.checked)}
            />
            开启读取、上游取图加速
          </label>
        </div>
        <div className="two-columns">
          <Field
            label="文件链接访问方式"
            hint="跟随现有 Bucket / CDN 的读权限，不修改 Bucket ACL。"
          >
            <select
              value={form.publicRead ? "public" : "private"}
              onChange={(event) =>
                set("publicRead", event.target.value === "public")
              }
            >
              <option value="private">私有读取 · 签名链接</option>
              <option value="public">公共读取 · 固定链接</option>
            </select>
          </Field>
          <Field
            label="签名链接有效期（秒）"
            hint="每次查询记录都会刷新链接；下载入口保持可用。"
          >
            <input
              type="number"
              min={60}
              max={604800}
              step={1}
              disabled={form.publicRead}
              value={form.urlTTL}
              onChange={(event) => set("urlTTL", Number(event.target.value))}
            />
          </Field>
          <Field
            label="OSS AccessKey ID"
            hint={
              form.hasCredentials
                ? "已保存密钥。留空保留，更换时同时填写 ID 和 Secret。"
                : "配置具备目标目录上传、读取和删除权限的密钥。"
            }
          >
            <input
              type="password"
              autoComplete="new-password"
              value={form.accessKeyId}
              onChange={(event) => set("accessKeyId", event.target.value)}
              placeholder={
                form.hasCredentials ? "已配置，不回显" : "填写 AccessKey ID"
              }
            />
          </Field>
          <Field
            label="OSS AccessKey Secret"
            hint="加密保存在服务端，不返回前端或写入操作日志。"
          >
            <input
              type="password"
              autoComplete="new-password"
              value={form.accessKeySecret}
              onChange={(event) => set("accessKeySecret", event.target.value)}
              placeholder={
                form.hasCredentials ? "已配置，不回显" : "填写 AccessKey Secret"
              }
            />
          </Field>
        </div>
        {testMessage && (
          <div className="storage-test-result" role="status">
            <ShieldCheck size={16} />
            {testMessage}
          </div>
        )}
        <div className="storage-actions">
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={() =>
              act(async () => {
                const result = await post<RecordData>(
                  "/admin/storage/test",
                  form,
                );
                setTestMessage(result.message);
              })
            }
          >
            {busy ? "处理中…" : "测试上传与读取"}
          </button>
          <button className="button primary" disabled={busy}>
            保存存储配置
          </button>
        </div>
      </Form>
    </section>
  );
}
