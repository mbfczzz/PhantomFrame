import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Plus,
  Upload,
  Grid2X2,
  MoreHorizontal,
  Download,
  Trash2,
  Pencil,
  Workflow,
  ArrowUpRight,
} from "lucide-react";
import {
  useApi,
  useRefresh,
  useAction,
  api,
  post,
  date,
  downloadJSON,
  PageHeader,
  SearchBox,
  Modal,
  Field,
  Form,
  Empty,
  Loading,
  ErrorState,
  type RecordData,
} from "../lib";
export function Projects() {
  const { data, isLoading, error, refetch } = useApi<RecordData[]>("/projects");
  const [search, setSearch] = useState("");
  const [rename, setRename] = useState<RecordData | null>(null);
  const [title, setTitle] = useState("");
  const file = useRef<HTMLInputElement>(null);
  const { busy, act } = useAction();
  const refresh = useRefresh();
  const navigate = useNavigate();
  const create = () =>
    act(async () => {
      const p = await post("/projects", { title: "未命名画布" });
      refresh("/projects");
      navigate("/canvas/" + p.id);
    });
  const filtered = data?.filter((p) => p.title.includes(search)) || [];
  return (
    <div className="page">
      <PageHeader
        title="画布"
        description="连接素材与生成节点，保存你的工作流程。"
      >
        <button
          className="button"
          disabled={busy}
          onClick={() => file.current?.click()}
        >
          <Upload size={16} />
          导入
        </button>
        <button className="button primary" disabled={busy} onClick={create}>
          <Plus size={17} />
          新建画布
        </button>
        <input
          hidden
          type="file"
          ref={file}
          accept=".json"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (!f) return;
            act(async () => {
              if (f.size > 3 * 1024 * 1024)
                throw new Error("画布文件不能超过 3MB");
              const data = JSON.parse(await f.text());
              const p = await post("/projects", {
                title: data.title || f.name.replace(".json", ""),
                data: data.data || data,
              });
              refresh("/projects");
              navigate("/canvas/" + p.id);
            }, "画布导入成功");
            e.target.value = "";
          }}
        />
      </PageHeader>
      <div className="toolbar divider">
        <SearchBox value={search} onChange={setSearch} placeholder="搜索画布" />
        <span className="muted">{filtered.length} 个画布</span>
      </div>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} retry={refetch} />
      ) : filtered.length ? (
        <div className="project-grid">
          {filtered.map((p, i) => (
            <article className="project-card" key={p.id}>
              <button
                className={"project-preview color-" + (i % 3)}
                onClick={() => navigate("/canvas/" + p.id)}
              >
                <div className="mini-flow">
                  <span />
                  <i />
                  <span />
                  <i />
                  <span />
                </div>
                <ArrowUpRight size={18} className="project-open" />
              </button>
              <div className="project-info">
                <button
                  className="title-button"
                  onClick={() => navigate("/canvas/" + p.id)}
                >
                  {p.title}
                </button>
                <p>
                  {p.nodeCount} 个节点 <span>·</span> {p.edgeCount} 条连线
                </p>
                <footer>
                  <small>{date(p.updatedAt)}</small>
                  <div>
                    <button
                      className="icon-button"
                      aria-label={"重命名 " + p.title}
                      onClick={() => {
                        setRename(p);
                        setTitle(p.title);
                      }}
                    >
                      <Pencil size={14} />
                    </button>
                    <button
                      className="icon-button"
                      aria-label={"导出 " + p.title}
                      onClick={() =>
                        act(async () => {
                          const full = await api("/projects/" + p.id);
                          downloadJSON(
                            {
                              format: "phantomframe-canvas-v1",
                              title: full.title,
                              data: full.data,
                            },
                            p.title + ".json",
                          );
                        })
                      }
                    >
                      <Download size={14} />
                    </button>
                    <button
                      className="icon-button danger"
                      aria-label={"删除 " + p.title}
                      onClick={() => {
                        if (confirm("确定删除画布「" + p.title + "」？"))
                          act(async () => {
                            await api("/projects/" + p.id, {
                              method: "DELETE",
                            });
                            refresh("/projects");
                          }, "画布已删除");
                      }}
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                </footer>
              </div>
            </article>
          ))}
        </div>
      ) : (
        <Empty
          title={search ? "没有匹配的画布" : "还没有画布项目"}
          text="把灵感、素材与生成任务，连接成你的创作流程。"
          icon={<Workflow size={34} />}
        >
          <button className="button primary" onClick={create} disabled={busy}>
            <Plus size={16} />
            创建第一个画布
          </button>
        </Empty>
      )}
      {rename && (
        <Modal title="重命名画布" onClose={() => setRename(null)}>
          <Form
            onSubmit={() =>
              act(async () => {
                await post(
                  "/projects/" + rename.id,
                  { title, version: rename.version },
                  "PATCH",
                );
                setRename(null);
                refresh("/projects");
              }, "画布已重命名")
            }
          >
            <Field label="画布名称">
              <input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                required
                maxLength={100}
              />
            </Field>
            <div className="modal-footer">
              <button
                type="button"
                className="button"
                onClick={() => setRename(null)}
              >
                取消
              </button>
              <button className="button primary" disabled={busy}>
                保存
              </button>
            </div>
          </Form>
        </Modal>
      )}
    </div>
  );
}
