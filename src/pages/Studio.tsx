import GenerationTypeIcon from "../components/GenerationTypeIcon";
import { generationLabels, generationOptions } from "../lib";
import { useState, useEffect, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
  Image,
  Video,
  Music,
  Plus,
  X,
  SlidersHorizontal,
  Loader2,
  CircleHelp,
  ArrowUp,
  ArrowRight,
  ChevronRight,
  ChevronDown,
  Images,
  FolderOpen,
  Upload,
} from "lucide-react";
import {
  useApi,
  useRefresh,
  useToast,
  useAction,
  api,
  post,
  defaults,
  money,
  unitLabel,
  Tabs,
  Loading,
  ErrorState,
  Modal,
  type Model,
  type Job,
  type Asset,
  type User,
  type RecordData,
  type PageData,
} from "../lib";
import {
  AssetPicker,
  ParameterFields,
  JobCard,
  JobDetail,
  Media,
} from "./Creation";
import { Select, type SelectOption } from "../components/Select";
import { useSurfaceLight } from "../components/useSurfaceLight";
import { billingOptions, billingUnit } from "../pricing";
import { isImageCountParameter, imageOutputLabel } from "../model-config";
import {
  initialStudioMode,
  studioModeOptions,
  studioModeParams,
  studioModeParameter,
  studioReferenceModel,
  studioModeError,
  studioFrameMode,
  type StudioMode,
} from "../studio-modes";
import {
  referenceCapacities as capacities,
  referenceLimit,
  referenceTotalLimit,
  referenceSpecs,
} from "../model-config";
import {
  PromptEditor,
  type PromptEditorHandle,
  type ReferenceAsset,
} from "../components/PromptEditor";

const referenceNames: Record<string, string> = {
  image: "图片",
  video: "视频",
  audio: "音频",
};
const acceptedMimes: Record<string, string[]> = {
  image: ["image/png", "image/jpeg", "image/gif", "image/webp"],
  video: ["video/mp4", "video/webm"],
  audio: ["audio/mpeg", "audio/wav", "audio/x-wav"],
};
function allocate(
  model: Model,
  params: RecordData,
  candidates: ReferenceAsset[],
  mode?: StudioMode,
) {
  const next = { ...params };
  const fields = (
    mode ? studioReferenceModel(model, mode, params)! : model
  ).parameters.filter((field) => field.type === "assets");
  for (const field of fields) next[field.name] = [];
  const assigned = new Set<string>();
  const totalLimit = referenceTotalLimit(model);
  // Reserve required/minimum slots before an unbounded field consumes the remainder.
  for (const field of fields) {
    const minimum = Math.max(field.required ? 1 : 0, field.min ?? 0);
    for (const asset of candidates) {
      if (
        assigned.size >= totalLimit ||
        next[field.name].length >=
          Math.min(minimum, referenceLimit(field, params))
      )
        break;
      if (assigned.has(asset.id) || asset.type !== (field.itemType || "image"))
        continue;
      next[field.name].push(asset.id);
      assigned.add(asset.id);
    }
  }
  for (const asset of candidates) {
    if (assigned.size >= totalLimit) break;
    if (assigned.has(asset.id)) continue;
    const field = fields.find(
      (field) =>
        (field.itemType || "image") === asset.type &&
        next[field.name].length < referenceLimit(field, params),
    );
    if (field) {
      next[field.name].push(asset.id);
      assigned.add(asset.id);
    }
  }
  return {
    params: next,
    refs: candidates.filter((asset) => assigned.delete(asset.id)),
  };
}
function removeReferenceTokens(prompt: string, removed: ReferenceAsset[]) {
  const names = new Set(removed.map((asset) => `@${asset.referenceLabel}`));
  return prompt.replace(/@(图片|视频|音频)\d+/g, (token) =>
    names.has(token) ? "" : token,
  );
}
function providerPrompt(
  prompt: string,
  model: Model,
  params: RecordData,
  refs: ReferenceAsset[],
) {
  const ordered = model.parameters
    .filter((field) => field.type === "assets")
    .flatMap((field) => (params[field.name] || []) as string[]);
  const counters: Record<string, number> = {};
  const replacements: Record<string, string> = {};
  for (const id of ordered) {
    const asset = refs.find((item) => item.id === id);
    if (!asset) continue;
    const index = (counters[asset.type] = (counters[asset.type] || 0) + 1);
    replacements[`@${asset.referenceLabel}`] =
      `参考${referenceNames[asset.type]}${index}`;
  }
  return prompt.replace(
    /@(图片|视频|音频)\d+/g,
    (token) => replacements[token] || token,
  );
}
function modelOption(model: Model): SelectOption {
  const rates = model.prices
    .filter((price) => price.enabled)
    .sort((a, b) => a.credits - b.credits);
  const specs = referenceSpecs(model).join(" · ");
  return {
    value: model.id,
    label: model.name,
    group: model.category || "其他模型",
    description: [specs || "文字驱动", model.description]
      .filter(Boolean)
      .join(" · "),
    meta: rates[0]
      ? `${money(rates[0].credits)}/${unitLabel[rates[0].unit]}${rates.length > 1 ? "起" : ""}`
      : "待定价",
  };
}

export default function Studio() {
  const {
    data: models,
    isLoading,
    error,
    refetch,
  } = useApi<Model[]>("/models", { refetchOnWindowFocus: "always" });
  const { data: user } = useApi<User>("/me");
  const { data: config } = useApi<RecordData>("/config");
  const [search, setSearch] = useSearchParams();
  const [modelId, setModelId] = useState(search.get("model") || "");
  const [kind, setKind] = useState("image");
  const [billing, setBilling] = useState("request");
  const [prompt, setPrompt] = useState(search.get("prompt") || "");
  const [params, setParams] = useState<RecordData>({});
  const [creationMode, setCreationMode] = useState<StudioMode>("text");
  const [refs, setRefs] = useState<ReferenceAsset[]>([]);
  const [picker, setPicker] = useState<"source" | "library" | null>(null);
  const [pickerType, setPickerType] = useState<string | null>(null);
  const pickerTypeRef = useRef<string | null>(null);
  const [referencePreview, setReferencePreview] =
    useState<ReferenceAsset | null>(null);
  const [filter, setFilter] = useState("all");
  const [detail, setDetail] = useState<Job | null>(null);
  const [advanced, setAdvanced] = useState(false);
  const [preview, setPreview] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const editor = useRef<PromptEditorHandle>(null);
  const results = useRef<HTMLElement>(null);
  const previewToggle = useRef<HTMLButtonElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const dragDepth = useRef(0);
  const serials = useRef<Record<string, number>>({});
  const initialized = useRef(false);
  const definition = useRef({ id: "", schema: "" });
  const uploadController = useRef<AbortController | null>(null);
  const uploadLock = useRef(false);
  const submitLock = useRef(false);
  const requestIdentity = useRef<{ signature: string; key: string } | null>(
    null,
  );
  const alive = useRef(true);
  const { busy, act } = useAction();
  const refresh = useRefresh(),
    toast = useToast();
  const model = models?.find((item) => item.id === modelId);
  useEffect(() => {
    const unit = billingUnit(model);
    if (unit) setBilling(unit);
  }, [model]);
  const capacity = capacities(
    studioReferenceModel(model, creationMode, params),
    params,
  );
  const allowed = Object.keys(capacity).filter((type) => capacity[type] > 0);
  const canReference = Object.values(capacities(model, params)).some(
    (limit) => limit > 0,
  );
  const pickerAllowed = pickerType
    ? allowed.filter((type) => type === pickerType)
    : allowed;
  const openReferencePicker = (type: string | null = null) => {
    pickerTypeRef.current = type;
    setPickerType(type);
    setPicker("source");
  };
  const closeReferencePicker = () => {
    setPicker(null);
    pickerTypeRef.current = null;
    setPickerType(null);
  };
  const latest = useRef({ model, params, refs, mode: creationMode });
  latest.current = { model, params, refs, mode: creationMode };
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      uploadController.current?.abort();
    };
  }, []);

  const selectModel = (next: Model, updateURL = true) => {
    definition.current = {
      id: next.id,
      schema: JSON.stringify(next.parameters),
    };
    uploadController.current?.abort();
    closeReferencePicker();
    const mode = initialStudioMode(
      next,
      defaults(next),
      latest.current.refs.map((asset) => asset.type),
    );
    const retained = allocate(
      next,
      studioModeParams(next, defaults(next), mode),
      latest.current.refs,
      mode,
    );
    const removed = latest.current.refs.filter(
      (asset) => !retained.refs.some((item) => item.id === asset.id),
    );
    if (removed.length) {
      setPrompt((value) => removeReferenceTokens(value, removed));
      toast(`已移除 ${removed.length} 个新模型不支持的参考素材`);
    }
    setAdvanced(false);
    setModelId(next.id);
    setKind(next.type);
    setBilling(billingUnit(next) || "request");
    setCreationMode(mode);
    setParams(retained.params);
    setRefs(retained.refs);
    latest.current = { model: next, ...retained, mode };
    requestIdentity.current = null;
    if (updateURL) setSearch({ model: next.id }, { replace: true });
  };
  useEffect(() => {
    if (models?.length && !initialized.current) {
      selectModel(
        models.find((item) => item.id === modelId) ||
          models.find((item) => item.type === "image") ||
          models[0],
        false,
      );
      initialized.current = true;
    }
  }, [models]);
  useEffect(() => {
    if (!model || !initialized.current) return;
    const schema = JSON.stringify(model.parameters);
    if (
      definition.current.id === model.id &&
      definition.current.schema === schema
    )
      return;
    definition.current = { id: model.id, schema };
    uploadController.current?.abort();
    const current = latest.current;
    const values = defaults(model);
    for (const field of model.parameters) {
      if (field.type === "assets") continue;
      const value = current.params[field.name];
      const valid =
        field.type === "select"
          ? field.options?.some(
              (option) => String(option.v ?? option) === String(value),
            )
          : field.type === "number"
            ? typeof value === "number" &&
              Number.isFinite(value) &&
              (field.min == null || value >= field.min) &&
              (field.max == null || value <= field.max)
            : field.type === "boolean"
              ? typeof value === "boolean"
              : typeof value === "string";
      if (valid) values[field.name] = value;
    }
    const mode = studioModeOptions(model, values).some(
      (o) => o.value === current.mode && !o.disabled,
    )
      ? current.mode
      : initialStudioMode(
          model,
          values,
          current.refs.map((asset) => asset.type),
        );
    const next = allocate(
      model,
      studioModeParams(model, values, mode),
      current.refs,
      mode,
    );
    const removed = current.refs.filter(
      (asset) => !next.refs.some((item) => item.id === asset.id),
    );
    if (removed.length)
      setPrompt((value) => removeReferenceTokens(value, removed));
    setParams(next.params);
    setRefs(next.refs);
    setCreationMode(mode);
    latest.current = { model, ...next, mode };
    requestIdentity.current = null;
    refresh("/pricing/quote");
    toast(
      removed.length
        ? `模型配置已更新，已移除 ${removed.length} 个超出新限制的素材`
        : "模型参数已同步更新",
    );
  }, [model]);
  useEffect(() => {
    const requested = search.get("model");
    if (initialized.current && requested && requested !== modelId) {
      const next = models?.find((item) => item.id === requested);
      if (next) selectModel(next, false);
    }
    const requestedPrompt = search.get("prompt");
    if (requestedPrompt) setPrompt(requestedPrompt);
  }, [search]);
  const available = (type: string, mode: string) =>
    models?.filter(
      (item) =>
        item.type === type &&
        item.prices.some((price) => price.enabled && price.unit === mode),
    ) || [];
  const changeFilter = (type: string, mode: string) => {
    const options = available(type, mode);
    const next = options.find((item) => item.id === modelId) || options[0];
    if (next) selectModel(next);
    else {
      setKind(type);
      setBilling(mode);
      setModelId("");
      setParams({});
      setCreationMode("text");
      setRefs([]);
      setPrompt((value) => removeReferenceTokens(value, refs));
    }
  };
  const changeCreationMode = (mode: StudioMode) => {
    const current = latest.current;
    if (
      !current.model ||
      uploading ||
      !studioModeOptions(current.model, current.params).some(
        (o) => o.value === mode && !o.disabled,
      )
    )
      return;
    const next = allocate(
      current.model,
      studioModeParams(current.model, current.params, mode),
      current.refs,
      mode,
    );
    const removed = current.refs.filter(
      (asset) => !next.refs.some((item) => item.id === asset.id),
    );
    setPrompt((value) => removeReferenceTokens(value, removed));
    setCreationMode(mode);
    setParams(next.params);
    setRefs(next.refs);
    latest.current = { model: current.model, ...next, mode };
    closeReferencePicker();
    requestIdentity.current = null;
    if (removed.length)
      toast(`已移除 ${removed.length} 个与新模式不兼容的参考素材`);
  };
  const changeParams = (values: RecordData) => {
    const current = latest.current;
    if (!current.model || uploading) return;
    const next = allocate(
      current.model,
      studioModeParams(current.model, values, current.mode),
      current.refs,
      current.mode,
    );
    const removed = current.refs.filter(
      (asset) => !next.refs.some((item) => item.id === asset.id),
    );
    setPrompt((value) => removeReferenceTokens(value, removed));
    setParams(next.params);
    setRefs(next.refs);
    latest.current = { model: current.model, ...next, mode: current.mode };
    if (removed.length)
      toast(`已移除 ${removed.length} 个超出新参数限制的参考素材`);
  };
  const {
    data: quote,
    error: quoteError,
    isFetching: quoting,
  } = useApi<RecordData>(
    "/pricing/quote?" + JSON.stringify({ modelId, params }),
    {
      enabled: !!model && initialized.current,
      queryFn: () => post("/pricing/quote", { modelId, params }),
      staleTime: 0,
    },
  );
  const {
    data: jobs,
    error: jobsError,
    refetch: refetchJobs,
  } = useApi<PageData<Job>>("/jobs?limit=24", { refetchInterval: 2000 });
  const jobStates = jobs?.items
    .map((job) => `${job.id}:${job.status}`)
    .join("|");
  useEffect(() => {
    if (jobStates) refresh("/me", "/usage", "/assets");
  }, [jobStates, refresh]);
  const insufficient = !!quote && Number(user?.balance || 0) < quote.total;
  const modeError = studioModeError(model, creationMode, params, refs);
  const canGenerate =
    !!model &&
    !!prompt.trim() &&
    !!quote &&
    !modeError &&
    !quoteError &&
    !insufficient &&
    !busy &&
    !uploading &&
    !quoting;
  const revealResults = () => {
    if (window.innerWidth < 1280)
      setTimeout(() => {
        if (results.current && !results.current.hidden)
          results.current.scrollIntoView({
            behavior: window.matchMedia("(prefers-reduced-motion: reduce)")
              .matches
              ? "auto"
              : "smooth",
            block: "start",
          });
      }, 50);
  };
  const start = async () => {
    if (!canGenerate || submitLock.current || !model) return;
    submitLock.current = true;
    try {
      await act(async () => {
        const payload = {
          modelId: model.id,
          prompt: providerPrompt(prompt, model, params, refs),
          params,
          expectedCost: quote?.total,
        };
        const signature = JSON.stringify(payload);
        if (requestIdentity.current?.signature !== signature)
          requestIdentity.current = { signature, key: crypto.randomUUID() };
        try {
          await post("/jobs", payload, "POST", {
            "Idempotency-Key": requestIdentity.current.key,
          });
          requestIdentity.current = null;
        } finally {
          refresh("/pricing/quote", "/models");
        }
        refresh("/jobs", "/me", "/usage");
        setPreview(true);
        revealResults();
      }, "任务已加入生成队列");
    } finally {
      submitLock.current = false;
    }
  };
  const addReferences = (assets: Asset[]) => {
    const current = latest.current;
    if (!current.model) return [];
    if (pickerTypeRef.current)
      assets = assets.filter((asset) => asset.type === pickerTypeRef.current);
    const incoming = assets
      .filter((asset) => !current.refs.some((item) => item.id === asset.id))
      .map((asset) => ({
        ...asset,
        referenceLabel: `${referenceNames[asset.type]}${(serials.current[asset.type] = (serials.current[asset.type] || 0) + 1)}`,
      }));
    const mode =
      current.mode === "text" && assets.length
        ? initialStudioMode(
            current.model,
            current.params,
            assets.map((a) => a.type),
          )
        : current.mode;
    const next = allocate(
      current.model,
      studioModeParams(current.model, current.params, mode),
      [...current.refs, ...incoming],
      mode,
    );
    const added = next.refs.filter((asset) =>
      incoming.some((item) => item.id === asset.id),
    );
    if (added.length < incoming.length)
      toast("部分素材超出当前模型的类型或数量限制", true);
    setParams(next.params);
    setRefs(next.refs);
    setCreationMode(mode);
    latest.current = { model: current.model, ...next, mode };
    return added;
  };
  const removeReference = (asset: ReferenceAsset) => {
    const current = latest.current;
    if (!current.model) return;
    const firstFrameRemoved =
      current.mode === "image" &&
      studioFrameMode(current.model, current.params) &&
      current.refs.find((ref) => ref.type === "image")?.id === asset.id;
    const removed = firstFrameRemoved
      ? current.refs.filter((ref) => ref.type === "image")
      : [asset];
    const next = allocate(
      current.model,
      current.params,
      current.refs.filter((item) => !removed.some((ref) => ref.id === item.id)),
      current.mode,
    );
    setRefs(next.refs);
    setParams(next.params);
    latest.current = { model: current.model, ...next, mode: current.mode };
    setPrompt((value) => removeReferenceTokens(value, removed));
    if (removed.length > 1) toast("首帧已移除，同时清除尾帧，避免首尾顺序错位");
  };
  const uploadFiles = async (files: File[]): Promise<ReferenceAsset[]> => {
    if (!files.length) return [];
    if (uploadLock.current) {
      toast("请等待当前素材上传完成");
      return [];
    }
    const current = latest.current;
    const mode =
      current.model && current.mode === "text"
        ? initialStudioMode(
            current.model,
            current.params,
            files.map((f) => f.type.split("/")[0]),
          )
        : current.mode;
    const uploadParams = current.model
      ? studioModeParams(current.model, current.params, mode)
      : current.params;
    const max = capacities(
      studioReferenceModel(current.model, mode, uploadParams),
      uploadParams,
    );
    if (!Object.values(max).some((limit) => limit > 0)) {
      toast("当前模型不支持参考素材", true);
      return [];
    }
    const counts: Record<string, number> = {};
    for (const ref of current.refs)
      counts[ref.type] = (counts[ref.type] || 0) + 1;
    const accepted: File[] = [];
    for (const file of files) {
      if (
        current.refs.length + accepted.length >=
        referenceTotalLimit(current.model)
      ) {
        toast(
          `参考素材合计最多 ${referenceTotalLimit(current.model)} 个`,
          true,
        );
        break;
      }
      const type = file.type.split("/")[0];
      if (pickerTypeRef.current && pickerTypeRef.current !== type) {
        toast(`请选择${referenceNames[pickerTypeRef.current]}文件`, true);
        continue;
      }
      if (!(max[type] > 0) || !acceptedMimes[type]?.includes(file.type)) {
        toast(`${file.name} 的格式不受当前模型支持`, true);
        continue;
      }
      if (file.size > 30 * 1024 * 1024) {
        toast(`${file.name} 超过 30MB`, true);
        continue;
      }
      if ((counts[type] || 0) >= max[type]) {
        toast(`当前模型最多支持 ${max[type]} 个${referenceNames[type]}`, true);
        continue;
      }
      counts[type] = (counts[type] || 0) + 1;
      accepted.push(file);
    }
    if (!accepted.length) return [];
    uploadLock.current = true;
    setUploading(true);
    const controller = new AbortController();
    uploadController.current = controller;
    const uploaded: Asset[] = [];
    try {
      for (const file of accepted) {
        const form = new FormData();
        form.append("file", file);
        uploaded.push(
          await api<Asset>("/assets", {
            method: "POST",
            body: form,
            signal: controller.signal,
          }),
        );
      }
    } catch (error) {
      if (!controller.signal.aborted && alive.current)
        toast((error as Error).message || "上传失败，请重试", true);
    } finally {
      refresh("/assets");
      uploadLock.current = false;
      if (alive.current) setUploading(false);
    }
    if (
      controller.signal.aborted ||
      !alive.current ||
      latest.current.model?.id !== current.model?.id
    )
      return [];
    const added = addReferences(uploaded);
    if (added.length) {
      closeReferencePicker();
      toast(`已添加 ${added.length} 个参考素材`);
    }
    return added;
  };
  const filteredJobs =
    jobs?.items.filter((job) => filter === "all" || job.type === filter) || [];
  const activeJobs =
    jobs?.items.filter((job) =>
      ["queued", "running", "refund_pending"].includes(job.status),
    ).length || 0;
  const options = available(kind, billing)
    .map(modelOption)
    .sort((a, b) => (a.group || "").localeCompare(b.group || "", "zh-CN"));
  const referenceSummary = referenceSpecs(
    studioReferenceModel(model, creationMode, params),
  ).join(" · ");
  const modeOptions = studioModeOptions(model, params);
  const frameMode = creationMode === "image" && studioFrameMode(model, params);
  const visibleParameters =
    model?.parameters.filter(
      (p) =>
        p.type !== "assets" &&
        !isImageCountParameter(model, p) &&
        p.name !== studioModeParameter(model)?.name,
    ) || [];
  const commonNames = [
    "aspectRatio",
    "aspect_ratio",
    "ratio",
    "resolution",
    "duration",
    "quality",
    "channel",
  ];
  const commonParameters = visibleParameters.filter((p) =>
    commonNames.includes(p.name),
  );
  const extraParameters = visibleParameters.filter(
    (p) => !commonNames.includes(p.name),
  );
  const editorLight = useSurfaceLight();
  if (isLoading) return <Loading />;
  if (error) return <ErrorState error={error} retry={refetch} />;
  return (
    <div
      className={`studio-page creation-page ${preview ? "has-preview" : ""}`}
    >
      <header className="creation-heading">
        <div>
          <h1>创作</h1>
          <p>图片、视频、文本与音频，从一个清晰的描述开始。</p>
        </div>
        <button
          ref={previewToggle}
          type="button"
          className={`studio-preview-toggle ${preview ? "active" : ""}`}
          onClick={() => {
            setPreview(!preview);
            if (!preview) revealResults();
          }}
          aria-label={preview ? "收起生成预览" : "展开生成预览"}
          aria-expanded={preview}
          aria-controls="generation-preview"
        >
          <Images size={16} />
          <span>{preview ? "收起结果" : "查看结果"}</span>
          {!!jobs?.total && <small>{activeJobs || jobs.total}</small>}
        </button>
      </header>
      {config?.announcement && (
        <div className="notice">
          <CircleHelp size={16} />
          {config.announcement}
        </div>
      )}
      <div className="studio-workbench">
        <section
          ref={editorLight}
          className={`studio-composer surface-composer reactive-surface ${dragging ? "is-dragging" : ""}`}
          aria-label="创作内容"
          onDragEnter={(event) => {
            if (event.dataTransfer.types.includes("Files")) {
              event.preventDefault();
              dragDepth.current++;
              setDragging(true);
            }
          }}
          onDragOver={(event) => {
            if (event.dataTransfer.types.includes("Files")) {
              event.preventDefault();
              event.dataTransfer.dropEffect = canReference ? "copy" : "none";
            }
          }}
          onDragLeave={(event) => {
            if (event.dataTransfer.types.includes("Files")) {
              dragDepth.current = Math.max(0, dragDepth.current - 1);
              if (!dragDepth.current) setDragging(false);
            }
          }}
          onDrop={(event) => {
            event.preventDefault();
            dragDepth.current = 0;
            setDragging(false);
            pickerTypeRef.current = null;
            setPickerType(null);
            void uploadFiles(Array.from(event.dataTransfer.files));
          }}
        >
          <header className="composer-heading">
            <h2>
              {kind === "image"
                ? "画面描述"
                : kind === "video"
                  ? "镜头描述"
                  : "任务说明"}
            </h2>
            <span className={`studio-type-badge ${kind}`}>
              {<GenerationTypeIcon kind={kind} size={13} />}{" "}
              {`${generationLabels[kind]}创作`}
            </span>
          </header>
          <div className="surface-editor composer-input">
            {!!allowed.length && (
              <section className="studio-references" aria-label="参考素材">
                <div className="studio-references-heading">
                  <span>{frameMode ? "首尾帧参考" : "参考素材"}</span>
                  <small>支持 {referenceSummary}</small>
                </div>
                <div className="surface-reference-list">
                  {(frameMode
                    ? ["首帧", "尾帧"]
                    : creationMode === "reference" && kind === "video"
                      ? allowed.map((type) => referenceNames[type])
                      : ["添加素材"]
                  ).map((label, index) => (
                    <button
                      key={label}
                      type="button"
                      className="studio-reference-add"
                      onClick={() =>
                        openReferencePicker(
                          frameMode
                            ? "image"
                            : creationMode === "reference" && kind === "video"
                              ? allowed[index]
                              : null,
                        )
                      }
                      aria-label={
                        label === "添加素材" ? "添加参考内容" : `添加${label}`
                      }
                      title={referenceSummary}
                      disabled={
                        uploading ||
                        (frameMode &&
                          (refs.filter((r) => r.type === "image").length !==
                            index ||
                            capacity.image <= index))
                      }
                    >
                      {uploading ? (
                        <Loader2 size={19} className="spin" />
                      ) : (
                        <Plus size={19} />
                      )}
                      <span>{uploading ? "上传中" : label}</span>
                    </button>
                  ))}
                  {refs.map((asset) => (
                    <div className="reference-item" key={asset.id}>
                      <button
                        type="button"
                        className="reference-thumbnail"
                        aria-label={`预览${asset.referenceLabel}`}
                        title={asset.name}
                        onClick={() => setReferencePreview(asset)}
                      >
                        {asset.type === "image" ? (
                          <img src={asset.url} alt={asset.referenceLabel} />
                        ) : asset.type === "video" ? (
                          <Video size={22} />
                        ) : (
                          <Music size={22} />
                        )}
                      </button>
                      <button
                        type="button"
                        className="reference-token"
                        title={`插入 @${asset.referenceLabel}`}
                        onMouseDown={(event) => event.preventDefault()}
                        onClick={() => editor.current?.insertReference(asset)}
                      >
                        {frameMode && asset.type === "image"
                          ? refs.filter((r) => r.type === "image")[0]?.id ===
                            asset.id
                            ? "首帧"
                            : "尾帧"
                          : asset.referenceLabel}
                      </button>
                      <button
                        type="button"
                        className="reference-remove"
                        aria-label={`移除${asset.referenceLabel}`}
                        disabled={uploading}
                        onClick={() => removeReference(asset)}
                      >
                        <X size={12} />
                      </button>
                    </div>
                  ))}
                  {!refs.length && (
                    <p className="reference-empty-hint">
                      {frameMode
                        ? "先添加首帧，再添加尾帧（按模型要求选填）"
                        : `添加${allowed.map((type) => referenceNames[type]).join("、")}作为参考`}
                      <br />
                      <span>也可直接拖入文件，或粘贴图片</span>
                    </p>
                  )}
                </div>
              </section>
            )}
            <PromptEditor
              ref={editor}
              value={prompt}
              onChange={setPrompt}
              references={refs}
              supportsReferences={canReference}
              onFiles={(files) => {
                pickerTypeRef.current = null;
                setPickerType(null);
                return uploadFiles(files);
              }}
              onSubmit={start}
              canSubmit={canGenerate}
            />
            <div className="composer-config-toolbar" aria-label="创作参数">
              <div className="surface-toolbar composer-model-controls">
                <div className="surface-field surface-mode">
                  <span>生成类型</span>
                  <Select
                    label="生成类型"
                    value={kind}
                    disabled={uploading}
                    onChange={(value) => {
                      const modes = billingOptions(value).map(
                        (option) => option.value,
                      );
                      const mode =
                        [billing, ...modes].find(
                          (mode) =>
                            modes.includes(mode) &&
                            available(value, mode).length,
                        ) || "request";
                      changeFilter(value, mode);
                    }}
                    options={[
                      { value: "image", label: "图片生成" },
                      { value: "video", label: "视频生成" },
                      { value: "text", label: "文本生成" },
                      { value: "audio", label: "音频生成" },
                      { value: "upscale", label: "视频超分" },
                    ]}
                  />
                </div>
                <div className="surface-field surface-billing">
                  <span>计费方式</span>
                  <Select
                    label="计费模式"
                    value={billing}
                    disabled={uploading || kind !== "video"}
                    onChange={(value) => changeFilter(kind, value)}
                    options={billingOptions(kind)}
                  />
                </div>
                <div className="surface-field surface-model">
                  <span>模型</span>
                  <Select
                    label="模型"
                    value={modelId}
                    disabled={uploading || !options.length}
                    searchable
                    placeholder="暂无可用模型"
                    options={options}
                    onChange={(value) => {
                      const next = models?.find((item) => item.id === value);
                      if (next) selectModel(next);
                    }}
                  />
                </div>
              </div>
              {!!modeOptions.length && (
                <div className="composer-mode-control">
                  <span>创作模式</span>
                  <Select
                    label="创作模式"
                    value={creationMode}
                    disabled={uploading || !model}
                    options={modeOptions}
                    onChange={(value) =>
                      changeCreationMode(value as StudioMode)
                    }
                  />
                </div>
              )}
              <fieldset
                className="composer-inline-parameters surface-parameters"
                disabled={uploading || busy}
              >
                <legend className="sr-only">输出参数</legend>
                {model && (
                  <ParameterFields
                    model={{ ...model, parameters: commonParameters }}
                    params={params}
                    onChange={changeParams}
                  />
                )}
              </fieldset>
              {!!extraParameters.length && (
                <button
                  type="button"
                  className="composer-more-settings"
                  aria-expanded={advanced}
                  aria-controls="composer-extra-parameters"
                  onClick={() => setAdvanced(!advanced)}
                >
                  <SlidersHorizontal size={14} /> 更多设置{" "}
                  <ChevronDown size={13} />
                </button>
              )}
            </div>
            {advanced && model && (
              <fieldset
                id="composer-extra-parameters"
                className="composer-extra-parameters surface-parameters"
                disabled={uploading || busy}
              >
                <legend>更多设置</legend>
                <ParameterFields
                  model={{ ...model, parameters: extraParameters }}
                  params={params}
                  onChange={changeParams}
                />
              </fieldset>
            )}
            {model?.type === "image" && (
              <p className="composer-output-note">{imageOutputLabel(model)}</p>
            )}
          </div>
          {dragging && (
            <div className="studio-drop-overlay">
              <Upload size={28} />
              <strong>
                {canReference ? "松开以添加参考素材" : "此模型不支持参考素材"}
              </strong>
            </div>
          )}
        </section>
        <aside
          className="studio-settings composer-settings"
          aria-label="生成设置"
        >
          {modeError && (
            <div className="composer-mode-message" role="status">
              {modeError}
            </div>
          )}
          {quoteError && !modeError && (
            <div className="inline-error" role="alert">
              {quoteError.message}
            </div>
          )}
          {quote && !quoteError && (
            <div className="pricing-quote-details" aria-live="polite">
              {quote.breakdown?.length > 1 ? (
                <>
                  {quote.breakdown.map((line: RecordData, index: number) => (
                    <span key={index}>
                      {line.label} · {money(line.unitPrice)}/
                      {unitLabel[line.unit]} × {line.quantity}
                      {unitLabel[line.unit]}
                    </span>
                  ))}
                  <span>合计 {money(quote.total)}（按分结算）</span>
                </>
              ) : (
                <>
                  {quote.label} · {money(quote.unitPrice)}/
                  {unitLabel[quote.unit]} × {quote.quantity}
                  {unitLabel[quote.unit]} = {money(quote.total)}
                  {Math.abs(quote.unitPrice * quote.quantity - quote.total) >
                    0.000001 && "（按分结算）"}
                </>
              )}
            </div>
          )}
          <footer className="surface-footer">
            <div className="surface-actions">
              <div className="studio-price" aria-live="polite">
                <span>本次消耗</span>
                <strong>{quote ? money(quote.total) : "—"}</strong>
                {insufficient && (
                  <Link to="/redeem" className="surface-balance-warning">
                    余额不足，去充值
                  </Link>
                )}
              </div>
              <button
                type="button"
                className="studio-generate"
                aria-label="开始生成"
                title={busy ? "正在提交…" : "开始生成（Ctrl + Enter）"}
                disabled={!canGenerate}
                onClick={start}
              >
                {busy ? (
                  <Loader2 size={17} className="spin" />
                ) : (
                  <ArrowUp size={17} />
                )}
                <span>
                  {busy
                    ? "提交中"
                    : uploading
                      ? "上传中"
                      : kind === "upscale"
                        ? "开始超分"
                        : `生成${generationLabels[kind]}`}
                </span>
              </button>
            </div>
            <p className="settings-submit-note">提交后可在下方查看进度与结果</p>
          </footer>
        </aside>
      </div>
      <section
        id="generation-preview"
        className="studio-results"
        ref={results}
        aria-label="创作输出"
        hidden={!preview}
      >
        <div className="results-toolbar">
          <div className="studio-results-heading">
            <h2>生成结果</h2>
            <p>
              {activeJobs ? (
                <>
                  <i className="status-dot" />
                  {activeJobs} 个任务进行中
                </>
              ) : (
                `${jobs?.total || 0} 条生成记录`
              )}
            </p>
          </div>
          <div className="studio-results-filters">
            <Tabs
              value={filter}
              onChange={setFilter}
              items={[
                { value: "all", label: "全部" },
                { value: "image", label: "图片" },
                { value: "video", label: "视频" },
                { value: "text", label: "文本" },
                { value: "audio", label: "音频" },
                { value: "upscale", label: "超分" },
              ]}
            />
            <Link to="/history" className="text-link">
              全部记录 <ChevronRight size={13} />
            </Link>
            <button
              type="button"
              className="icon-button"
              aria-label="关闭生成预览"
              onClick={() => {
                setPreview(false);
                previewToggle.current?.focus();
              }}
            >
              <X size={17} />
            </button>
          </div>
        </div>
        <div className="studio-results-content">
          {jobsError ? (
            <ErrorState error={jobsError} retry={refetchJobs} />
          ) : filteredJobs.length ? (
            <div className="job-grid studio-output-grid">
              {filteredJobs.map((job) => (
                <JobCard
                  key={job.id}
                  job={job}
                  onDetail={() => setDetail(job)}
                />
              ))}
            </div>
          ) : (
            <div className="studio-output-empty">
              <div className="result-empty-frame">
                <Images size={27} strokeWidth={1.4} />
              </div>
              <div>
                <h3>
                  {filter === "all"
                    ? "生成的作品会排列在这里"
                    : `暂无${generationLabels[filter] || "生成"}记录`}
                </h3>
                <p>提交任务后，即可查看进度、预览和下载。</p>
              </div>
              <button
                type="button"
                className="text-link"
                onClick={() => editor.current?.focus()}
              >
                开始填写 <ArrowRight size={14} />
              </button>
            </div>
          )}
        </div>
      </section>

      <input
        hidden
        type="file"
        multiple={!frameMode}
        ref={fileInput}
        aria-label="上传参考素材"
        accept={pickerAllowed
          .flatMap((type) => acceptedMimes[type] || [])
          .join(",")}
        onChange={(event) => {
          const files = Array.from(event.target.files || []);
          event.target.value = "";
          void uploadFiles(files);
        }}
      />
      {picker === "source" && (
        <Modal
          title="添加参考内容"
          className="reference-source-dialog"
          onClose={closeReferencePicker}
        >
          <button
            type="button"
            className="reference-upload-zone"
            disabled={uploading}
            onClick={() => fileInput.current?.click()}
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              void uploadFiles(Array.from(event.dataTransfer.files));
            }}
          >
            {uploading ? (
              <Loader2 size={27} className="spin" />
            ) : (
              <Upload size={27} strokeWidth={1.4} />
            )}
            <strong>
              {uploading ? "正在上传素材…" : "选择文件，或拖放到这里"}
            </strong>
            <span>
              {pickerAllowed.map((type) => referenceNames[type]).join(" / ")} ·
              每个文件不超过 30MB
            </span>
          </button>
          <button
            type="button"
            className="reference-library-link"
            disabled={uploading}
            onClick={() => setPicker("library")}
          >
            <FolderOpen size={20} strokeWidth={1.5} />
            <span>
              <strong>从资产库选择</strong>
              <small>使用已上传的素材或生成结果</small>
            </span>
            <ChevronRight size={16} />
          </button>
          <p className="reference-upload-tip">
            {referenceSummary}
            <br />
            也可以直接将图片粘贴到提示词输入框。
          </p>
        </Modal>
      )}
      {picker === "library" && (
        <AssetPicker
          key="library"
          onClose={closeReferencePicker}
          onPick={addReferences}
          allowed={pickerAllowed}
          multiple={!frameMode}
          remainingTotal={Math.max(0, referenceTotalLimit(model) - refs.length)}
          existingIds={refs.map((asset) => asset.id)}
          limits={Object.fromEntries(
            allowed.map((type) => [
              type,
              capacity[type] -
                refs.filter((asset) => asset.type === type).length,
            ]),
          )}
        />
      )}
      {referencePreview && (
        <Modal
          title={referencePreview.name}
          className="reference-preview-dialog"
          onClose={() => setReferencePreview(null)}
        >
          <Media asset={referencePreview} />
          <div className="modal-footer">
            <span className="muted">{referencePreview.referenceLabel}</span>
            <button
              type="button"
              className="button primary"
              onClick={() => {
                const selected = referencePreview;
                setReferencePreview(null);
                requestAnimationFrame(() =>
                  editor.current?.insertReference(selected),
                );
              }}
            >
              引用到提示词
            </button>
          </div>
        </Modal>
      )}
      {detail && <JobDetail job={detail} onClose={() => setDetail(null)} />}
    </div>
  );
}
