import {
  createContext,
  useContext,
  useState,
  useEffect,
  useRef,
  useCallback,
} from "react";
import { Link, useParams } from "react-router-dom";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  Handle,
  Position,
  addEdge,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Node,
  type NodeProps,
  type Connection,
  type Edge,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  ArrowLeft,
  Save,
  Download,
  Plus,
  Type,
  Image,
  StickyNote,
  Wand2,
  Play,
  Loader2,
  X,
  Check,
  ZoomIn,
  FolderOpen,
  Trash2,
  Info,
} from "lucide-react";
import {
  useApi,
  useToast,
  useTheme,
  useRefresh,
  api,
  post,
  defaults,
  downloadJSON,
  Modal,
  Field,
  Loading,
  ErrorState,
  type RecordData,
  type Model,
  type Job,
  type Asset,
} from "../lib";
import { AssetPicker, Media, ParameterFields } from "./Creation";

type FlowNode = Node<RecordData>;
const RunContext = createContext<{
  run: (ids?: string[]) => Promise<void>;
  running: Set<string>;
  models: Model[];
}>({ run: async () => {}, running: new Set(), models: [] });
function NodeHeader({
  id,
  icon,
  title,
  color,
}: {
  id: string;
  icon: React.ReactNode;
  title: string;
  color?: string;
}) {
  const { deleteElements } = useReactFlow();
  return (
    <header className={"node-header " + (color || "")}>
      <span>
        {icon}
        {title}
      </span>
      <button
        className="icon-button nodrag"
        aria-label="删除节点"
        onClick={() => deleteElements({ nodes: [{ id }] })}
      >
        <X size={14} />
      </button>
    </header>
  );
}
function TextNode({ id, data, selected }: NodeProps<FlowNode>) {
  const { updateNodeData } = useReactFlow();
  const note = data.kind === "note";
  return (
    <div
      className={
        "flow-node text-node " +
        (selected ? "selected" : "") +
        (note ? " note-node" : "")
      }
    >
      <NodeHeader
        id={id}
        icon={note ? <StickyNote size={16} /> : <Type size={16} />}
        title={note ? "便签" : "提示词"}
      />
      <textarea
        className="nodrag nowheel"
        aria-label={note ? "便签内容" : "节点提示词"}
        value={data.text || ""}
        placeholder={note ? "记下你的灵感…" : "描述你想要创作的画面…"}
        onChange={(e) => updateNodeData(id, { text: e.target.value })}
      />
      {!note && <Handle type="source" position={Position.Right} />}
    </div>
  );
}
function MediaNode({ id, data, selected }: NodeProps<FlowNode>) {
  return (
    <div className={"flow-node media-node " + (selected ? "selected" : "")}>
      <NodeHeader
        id={id}
        icon={<Image size={16} />}
        title={data.name || "参考素材"}
      />
      {data.url ? (
        <div className="node-media">
          <Media asset={{ url: data.url, type: data.mediaType || "image" }} />
        </div>
      ) : (
        <div className="node-empty">
          <Image size={28} />
          <span>添加参考素材</span>
        </div>
      )}
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
function GenerateNode({ id, data, selected }: NodeProps<FlowNode>) {
  const { updateNodeData } = useReactFlow();
  const { run, running, models } = useContext(RunContext);
  const model = models.find((m) => m.id === data.modelId);
  return (
    <div className={"flow-node generate-node " + (selected ? "selected" : "")}>
      <NodeHeader
        id={id}
        icon={<Wand2 size={16} />}
        title="AI 生成"
        color="green"
      />
      <Handle type="target" position={Position.Left} />
      <div className="node-content">
        <select
          aria-label="节点模型"
          className="nodrag"
          value={data.modelId || ""}
          onChange={(e) => {
            const model = models.find((m) => m.id === e.target.value)!;
            updateNodeData(id, { modelId: model.id, params: defaults(model) });
          }}
        >
          <option value="">选择模型</option>
          {models.map((m) => (
            <option value={m.id} key={m.id}>
              {m.name}
            </option>
          ))}
        </select>
        <textarea
          className="nodrag nowheel"
          aria-label="生成节点提示词"
          placeholder="填写提示词，或连接一个提示词节点"
          value={data.prompt || ""}
          onChange={(e) => updateNodeData(id, { prompt: e.target.value })}
        />
        {model && (
          <details className="nodrag nowheel">
            <summary>生成参数</summary>
            <div className="node-parameters">
              <ParameterFields
                model={model}
                params={data.params || defaults(model)}
                onChange={(params) => updateNodeData(id, { params })}
              />
            </div>
          </details>
        )}
        {data.outputs?.length > 0 && (
          <div className="node-media">
            <Media asset={data.outputs[0]} />
          </div>
        )}
        {data.error && <div className="inline-error">{data.error}</div>}
        <button
          className="button primary full nodrag"
          disabled={running.size > 0 || !model}
          onClick={() => run([id])}
        >
          {running.has(id) ? (
            <Loader2 size={15} className="spin" />
          ) : (
            <Play size={15} />
          )}{" "}
          {running.has(id) ? "生成中…" : "运行节点"}
        </button>
        {data.demo && (
          <small className="muted">演示通道 · 非真实模型输出</small>
        )}
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
const nodeTypes = { text: TextNode, media: MediaNode, generate: GenerateNode };
function FlowEditor() {
  const theme = useTheme();
  const { id: projectId } = useParams();
  const {
    data: project,
    isLoading,
    error,
    refetch,
  } = useApi<RecordData>("/projects/" + projectId);
  const { data: models = [] } = useApi<Model[]>("/models");
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [title, setTitle] = useState("");
  const [saveStatus, setSaveStatus] = useState("saved");
  const [picker, setPicker] = useState(false);
  const [running, setRunning] = useState<Set<string>>(new Set());
  const [help, setHelp] = useState(false);
  const flow = useReactFlow<FlowNode>();
  const toast = useToast();
  const refresh = useRefresh();
  const loaded = useRef(false);
  const version = useRef(1);
  const saved = useRef("");
  const nodesRef = useRef(nodes);
  const edgesRef = useRef(edges);
  const alive = useRef(true);
  const saving = useRef(Promise.resolve());
  const conflict = useRef(false);
  nodesRef.current = nodes;
  edgesRef.current = edges;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (project && !loaded.current) {
      setNodes(project.data.nodes || []);
      setEdges(project.data.edges || []);
      setTitle(project.title);
      version.current = project.version;
      loaded.current = true;
      saved.current = JSON.stringify({
        title: project.title,
        data: project.data,
      });
      if (project.data.viewport)
        setTimeout(() => flow.setViewport(project.data.viewport), 50);
    }
  }, [project]);
  const persist = useCallback(
    (manual = false) => {
      if (!loaded.current || conflict.current) return Promise.resolve();
      const data = flow.toObject();
      const body = { title: title || "未命名画布", data };
      const fingerprint = JSON.stringify(body);
      if (fingerprint === saved.current && !manual) return Promise.resolve();
      setSaveStatus("saving");
      saving.current = saving.current
        .catch(() => {})
        .then(async () => {
          try {
            const result = await post(
              "/projects/" + projectId,
              { ...body, version: version.current },
              "PATCH",
            );
            version.current = result.version;
            saved.current = fingerprint;
            setSaveStatus("saved");
          } catch (e) {
            setSaveStatus("error");
            if ((e as any).status === 409) conflict.current = true;
            toast((e as Error).message, true);
          }
        });
      return saving.current;
    },
    [title, projectId, flow],
  );
  useEffect(() => {
    if (!loaded.current) return;
    setSaveStatus("unsaved");
    const timer = setTimeout(() => persist(), 1200);
    return () => clearTimeout(timer);
  }, [nodes, edges, title]);
  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => {
      if (saveStatus !== "saved") {
        e.preventDefault();
      }
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [saveStatus]);
  const add = (type: string, data: RecordData = {}) => {
    const pos = flow.screenToFlowPosition({
      x: window.innerWidth / 2 + Math.random() * 80,
      y: window.innerHeight / 2 - 70 + Math.random() * 50,
    });
    setNodes((n) => [
      ...n,
      { id: crypto.randomUUID(), type, position: pos, data },
    ]);
  };
  const update = (id: string, data: RecordData) => {
    nodesRef.current = nodesRef.current.map((n) =>
      n.id === id ? { ...n, data: { ...n.data, ...data } } : n,
    );
    setNodes(nodesRef.current);
  };
  const connect = (c: Connection) => {
    if (c.source === c.target) return;
    const visit = (id: string, seen = new Set<string>()): boolean => {
      if (id === c.source) return true;
      if (seen.has(id)) return false;
      seen.add(id);
      return edgesRef.current
        .filter((e) => e.source === id)
        .some((e) => visit(e.target, seen));
    };
    if (visit(c.target)) {
      toast("画布不支持循环连接", true);
      return;
    }
    setEdges((e) => addEdge({ ...c, type: "smoothstep", animated: true }, e));
  };
  const run = async (ids?: string[]) => {
    if (running.size) return;
    const targets =
      ids ||
      nodesRef.current.filter((n) => n.type === "generate").map((n) => n.id);
    if (!targets.length) {
      toast("请先添加 AI 生成节点");
      return;
    }
    const ordered: string[] = [];
    const visited = new Set<string>(),
      visiting = new Set<string>();
    const visit = (nid: string) => {
      if (visiting.has(nid)) throw new Error("画布存在循环连接");
      if (visited.has(nid)) return;
      visiting.add(nid);
      for (const edge of edgesRef.current.filter((e) => e.target === nid))
        visit(edge.source);
      visiting.delete(nid);
      visited.add(nid);
      if (nodesRef.current.find((n) => n.id === nid)?.type === "generate")
        ordered.push(nid);
    };
    try {
      for (const nid of targets) visit(nid);
      setRunning(new Set(ordered));
      for (const nid of ordered) {
        if (!alive.current) break;
        const node = nodesRef.current.find((n) => n.id === nid)!;
        const model = models.find((m) => m.id === node.data.modelId);
        if (!model) throw new Error("请为所有生成节点选择模型");
        const inputs = edgesRef.current
          .filter((e) => e.target === nid)
          .map((e) => nodesRef.current.find((n) => n.id === e.source))
          .filter(Boolean) as FlowNode[];
        const prompt = [
          ...inputs.filter((n) => n.type === "text").map((n) => n.data.text),
          node.data.prompt,
        ]
          .filter(Boolean)
          .join("\n");
        if (!prompt.trim()) throw new Error("请填写提示词或连接一个提示词节点");
        const params = { ...defaults(model), ...node.data.params };
        for (const input of inputs) {
          const refs =
            input.type === "media"
              ? [{ assetId: input.data.assetId, type: input.data.mediaType }]
              : input.data.outputs || [];
          for (const ref of refs) {
            const field = model.parameters.find(
              (f) =>
                f.type === "assets" && f.itemType === (ref.type || "image"),
            );
            if (!field)
              throw new Error(model.name + " 不支持连接的参考素材类型");
            params[field.name] = [
              ...new Set([...(params[field.name] || []), ref.assetId]),
            ];
          }
        }
        update(nid, { error: "", outputs: [] });
        const job = await post<Job>(
          "/jobs",
          { modelId: model.id, prompt, params },
          "POST",
          { "Idempotency-Key": crypto.randomUUID() },
        );
        update(nid, { jobId: job.id });
        refresh("/me", "/jobs", "/usage");
        let complete = false;
        for (let i = 0; i < 400; i++) {
          if (!alive.current) return;
          await new Promise((r) => setTimeout(r, 1800));
          const result = await api<Job>("/jobs/" + job.id);
          if (result.status === "failed" || result.status === "cancelled") {
            update(nid, { error: result.error || "生成失败" });
            throw new Error(result.error || "生成失败");
          }
          if (result.status === "succeeded") {
            update(nid, { outputs: result.outputs, demo: result.demo });
            complete = true;
            break;
          }
        }
        if (!complete)
          throw new Error("任务仍在执行，请到生成历史查看最终结果");
        setRunning((s) => new Set([...s].filter((id) => id !== nid)));
      }
      toast("画布运行完成");
      setTimeout(
        () => flow.fitView({ padding: 0.2, maxZoom: 1, duration: 200 }),
        100,
      );
      await persist();
    } catch (e) {
      toast((e as Error).message, true);
    } finally {
      setRunning(new Set());
      refresh("/me", "/jobs", "/assets", "/usage");
    }
  };
  if (isLoading) return <Loading />;
  if (error) return <ErrorState error={error} retry={refetch} />;
  return (
    <RunContext.Provider value={{ run, running, models }}>
      <div className="canvas-workspace">
        <header className="canvas-header">
          <div>
            <Link
              className="icon-button"
              to="/canvas"
              onClick={() => persist(true)}
              aria-label="返回画布列表"
            >
              <ArrowLeft size={18} />
            </Link>
            <input
              aria-label="画布名称"
              value={title}
              maxLength={100}
              onChange={(e) => setTitle(e.target.value)}
            />
            <span className={"save-state " + saveStatus}>
              {saveStatus === "saving" ? (
                <Loader2 size={13} className="spin" />
              ) : saveStatus === "saved" ? (
                <Check size={13} />
              ) : null}
              {
                {
                  saved: "已保存",
                  saving: "保存中",
                  unsaved: "未保存",
                  error: "保存失败",
                }[saveStatus]
              }
            </span>
          </div>
          <div>
            <button
              className="icon-button"
              title="画布帮助"
              onClick={() => setHelp(true)}
            >
              <Info size={17} />
            </button>
            <button
              className="button"
              onClick={() =>
                downloadJSON(
                  {
                    format: "phantomframe-canvas-v1",
                    title,
                    data: flow.toObject(),
                  },
                  title + ".json",
                )
              }
            >
              <Download size={15} />
              导出
            </button>
            <button className="button" onClick={() => persist(true)}>
              <Save size={15} />
              保存
            </button>
            <button
              className="button primary"
              disabled={running.size > 0}
              onClick={() => run()}
            >
              {running.size ? (
                <Loader2 className="spin" size={15} />
              ) : (
                <Play size={15} />
              )}
              运行画布
            </button>
          </div>
        </header>
        <div className="canvas-area">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={connect}
            onMoveEnd={() => persist()}
            nodeTypes={nodeTypes}
            fitView
            colorMode={theme}
            defaultEdgeOptions={{
              type: "smoothstep",
              style: { stroke: "#8873bc", strokeWidth: 2 },
            }}
            minZoom={0.15}
            maxZoom={2}
            deleteKeyCode={["Backspace", "Delete"]}
          >
            <Background
              variant={BackgroundVariant.Dots}
              gap={24}
              size={1}
              color={theme === "dark" ? "#303547" : "#d5dbea"}
            />
            <Controls />
            <MiniMap
              pannable
              zoomable
              nodeColor={(n) =>
                n.type === "generate"
                  ? "#7355b3"
                  : n.type === "media"
                    ? "#447f9a"
                    : "#6b7186"
              }
            />
          </ReactFlow>
          <div className="canvas-toolbox">
            <button
              onClick={() => add("text", { text: "" })}
              title="添加提示词"
            >
              <Type size={21} />
              <span>提示词</span>
            </button>
            <button
              onClick={() =>
                add("generate", {
                  modelId:
                    models.find((m) => m.type === "image")?.id || models[0]?.id,
                  params: defaults(
                    models.find((m) => m.type === "image") || models[0],
                  ),
                  prompt: "",
                })
              }
              title="添加 AI 生成"
            >
              <Wand2 size={21} />
              <span>AI 生成</span>
            </button>
            <button onClick={() => setPicker(true)} title="添加素材">
              <Image size={21} />
              <span>素材</span>
            </button>
            <button
              onClick={() => add("text", { kind: "note", text: "" })}
              title="添加便签"
            >
              <StickyNote size={21} />
              <span>便签</span>
            </button>
          </div>
          {!nodes.length && (
            <div className="canvas-empty">
              <div className="empty-icon">
                <Wand2 size={30} />
              </div>
              <h2>从一个灵感开始</h2>
              <p>
                添加提示词、生成节点与素材
                <br />
                拖动节点边缘的圆点，连接你的创作流程
              </p>
              <button
                className="button primary"
                onClick={() => {
                  const promptId = "prompt-" + crypto.randomUUID(),
                    generateId = "generate-" + crypto.randomUUID();
                  setEdges([
                    {
                      id: crypto.randomUUID(),
                      source: promptId,
                      target: generateId,
                      type: "smoothstep",
                      animated: true,
                    },
                  ]);
                  setNodes([
                    {
                      id: promptId,
                      type: "text",
                      position: { x: 160, y: 170 },
                      data: {
                        text: "一只发光的透明水母漂浮在星云中，触手流淌着液态极光",
                      },
                    },
                    {
                      id: generateId,
                      type: "generate",
                      position: { x: 560, y: 170 },
                      data: {
                        modelId: models.find((m) => m.type === "image")?.id,
                        params: defaults(
                          models.find((m) => m.type === "image"),
                        ),
                        prompt: "",
                      },
                    },
                  ]);
                  setTimeout(() => flow.fitView({ padding: 0.2 }), 80);
                }}
              >
                创建第一个工作流
              </button>
            </div>
          )}
        </div>
      </div>
      {picker && (
        <AssetPicker
          onClose={() => setPicker(false)}
          onPick={(assets) =>
            assets.forEach((a) =>
              add("media", {
                assetId: a.id,
                url: a.url,
                mediaType: a.type,
                name: a.name,
              }),
            )
          }
        />
      )}{" "}
      {help && (
        <Modal title="画布使用指南" onClose={() => setHelp(false)}>
          <p>1. 从左侧工具栏添加提示词、AI 生成节点、参考素材和便签。</p>
          <p>2. 从节点右侧的圆点拖到生成节点左侧，连接提示词与素材。</p>
          <p>
            3.
            为生成节点选择模型和参数，点击「运行节点」或「运行画布」。前一个生成节点的输出可以作为后一个节点的参考素材。
          </p>
          <p>
            4. 编辑后自动保存。选中节点按 Delete
            删除；滚轮缩放，拖动空白区域平移。
          </p>
          <p>
            导出文件保存节点和连接关系，素材继续保存在当前账户的资产库中。导入其他账户时需重新选择素材。
          </p>
        </Modal>
      )}
    </RunContext.Provider>
  );
}
export default function Canvas() {
  return (
    <ReactFlowProvider>
      <FlowEditor />
    </ReactFlowProvider>
  );
}
