import type { Model, Parameter, RecordData } from "./lib";
import {
  referenceCapacities,
  referenceLimit,
  referenceTotalLimit,
} from "./model-config";

export type StudioMode = "text" | "image" | "reference";
const wireModes: Record<StudioMode, string[]> = {
  text: [
    "text_to_video",
    "text-to-video",
    "t2v",
    "text",
    "omni_reference",
    "references",
  ],
  image: [
    "first_last_frames",
    "frames",
    "image_to_video",
    "image-to-video",
    "i2v",
    "omni_reference",
    "references",
    "multi_frame",
  ],
  reference: ["omni_reference", "references", "multi_frame"],
};
export function studioModeParameter(model?: Model): Parameter | undefined {
  if (model?.type !== "video") return;
  return model.parameters.find(
    (p) =>
      ["function_mode", "functionMode", "mode"].includes(p.name) &&
      p.type === "select" &&
      p.options?.some((o) =>
        Object.values(wireModes)
          .flat()
          .includes(String(o.v ?? o)),
      ),
  );
}
export function studioModeParams(
  model: Model,
  params: RecordData,
  mode: StudioMode,
): RecordData {
  const field = studioModeParameter(model);
  const value = studioWireMode(model, params, mode);
  return value ? { ...params, [field!.name]: value } : { ...params };
}
function studioWireMode(model: Model, params: RecordData, mode: StudioMode) {
  const field = studioModeParameter(model);
  const imageCapacity = Math.min(
    referenceCapacities(model, params).image || 0,
    referenceTotalLimit(model),
  );
  return (
    field &&
    wireModes[mode].find((value) => {
      if (!field.options?.some((o) => String(o.v ?? o) === value)) return false;
      if (value === "frames") return imageCapacity >= 2;
      if (
        [
          "first_last_frames",
          "multi_frame",
          "image_to_video",
          "image-to-video",
          "i2v",
        ].includes(value)
      )
        return imageCapacity >= 1;
      return true;
    })
  );
}
export function studioFrameMode(
  model: Model | undefined,
  params: RecordData,
): boolean {
  const field = studioModeParameter(model);
  return (
    !!field &&
    ["first_last_frames", "frames"].includes(String(params[field.name]))
  );
}
export function studioModeOptions(model?: Model, params: RecordData = {}) {
  if (!model || !["image", "video"].includes(model.type)) return [];
  const field = studioModeParameter(model);
  const required = model.parameters.filter(
    (p) => p.type === "assets" && (p.required || (p.min ?? 0) > 0),
  );
  const supportsWire = (mode: StudioMode) =>
    !field || !!studioWireMode(model, params, mode);
  const modes: StudioMode[] =
    model.type === "image" ? ["text", "image"] : ["text", "image", "reference"];
  return modes.map((mode) => {
    const effectiveModel = studioReferenceModel(
      model,
      mode,
      studioModeParams(model, params, mode),
    )!;
    const caps = referenceCapacities(effectiveModel, params);
    return {
      value: mode,
      label:
        mode === "text"
          ? model.type === "image"
            ? "文生图"
            : "文生视频"
          : mode === "image"
            ? model.type === "image"
              ? "图生图"
              : "图生视频"
            : "全能参考",
      disabled:
        !supportsWire(mode) ||
        required.some((p) => {
          const effective = effectiveModel.parameters.find(
            (field) => field.name === p.name,
          )!;
          return (
            referenceLimit(effective, params) <
            Math.max(p.required ? 1 : 0, p.min ?? 0)
          );
        }) ||
        (mode === "text"
          ? required.length > 0
          : mode === "image"
            ? !(caps.image > 0) ||
              required.some((p) => p.itemType && p.itemType !== "image")
            : !Object.values(caps).some((n) => n > 0)),
    };
  });
}
export function initialStudioMode(
  model: Model,
  params: RecordData,
  types: string[] = [],
): StudioMode {
  const options = studioModeOptions(model, params);
  const preferred: StudioMode =
    types.some((t) => t !== "image") ||
    (model.type === "video" && types.length > 2)
      ? "reference"
      : types.length
        ? "image"
        : "text";
  const eligible = options
    .filter((o) => !o.disabled)
    .sort(
      (a, b) => Number(b.value === preferred) - Number(a.value === preferred),
    );
  if (!types.length) return eligible[0]?.value || "text";
  const counts = types.reduce<Record<string, number>>((all, type) => {
    all[type] = (all[type] || 0) + 1;
    return all;
  }, {});
  const retained = (mode: StudioMode) => {
    const caps = referenceCapacities(
      studioReferenceModel(model, mode, studioModeParams(model, params, mode)),
      params,
    );
    return Math.min(
      referenceTotalLimit(model),
      Object.entries(counts).reduce(
        (sum, [type, count]) => sum + Math.min(count, caps[type] || 0),
        0,
      ),
    );
  };
  return (
    eligible.reduce<StudioMode | undefined>(
      (best, option) =>
        !best || retained(option.value) > retained(best) ? option.value : best,
      undefined,
    ) || "text"
  );
}
export function studioReferenceModel(
  model: Model | undefined,
  mode: StudioMode,
  params: RecordData,
): Model | undefined {
  if (!model || !["image", "video"].includes(model.type)) return model;
  const frames =
    model.type === "video" &&
    mode === "image" &&
    studioFrameMode(model, params);
  let imageSlots =
    model.type === "video" && mode === "image" ? (frames ? 2 : 1) : Infinity;
  const wireField = studioModeParameter(model);
  const imageOnlyWire =
    !!wireField &&
    [
      "multi_frame",
      "first_last_frames",
      "frames",
      "image_to_video",
      "image-to-video",
      "i2v",
    ].includes(String(params[wireField.name]));
  return {
    ...model,
    parameters: model.parameters.map((p) => {
      if (p.type !== "assets") return p;
      const allowed =
        (mode === "reference" ||
          (mode === "image" && (p.itemType || "image") === "image")) &&
        (!imageOnlyWire || (p.itemType || "image") === "image");
      const max = allowed ? Math.min(referenceLimit(p, params), imageSlots) : 0;
      if ((p.itemType || "image") === "image" && Number.isFinite(imageSlots))
        imageSlots -= max;
      return { ...p, max, maxByResolution: undefined };
    }),
  };
}
export function studioModeError(
  model: Model | undefined,
  mode: StudioMode,
  params: RecordData,
  refs: { type: string }[],
): string {
  if (!model || !["image", "video"].includes(model.type)) return "";
  if (studioModeOptions(model, params).find((o) => o.value === mode)?.disabled)
    return "当前模型不支持此创作模式，请切换模式或模型";
  if (mode === "image" && !refs.some((r) => r.type === "image"))
    return model.type === "image"
      ? "请添加参考图片后生成"
      : "请添加首帧图片后生成";
  const field = studioModeParameter(model);
  if (
    mode === "image" &&
    field &&
    params[field.name] === "frames" &&
    refs.filter((r) => r.type === "image").length < 2
  )
    return "当前模型的首尾帧模式需要两张图片";
  if (mode === "reference" && !refs.length) return "请添加参考素材后生成";
  return "";
}
