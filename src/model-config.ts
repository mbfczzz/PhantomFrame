import type { Model, Parameter, RecordData } from "./lib";

export const isImageCountParameter = (model: Model, parameter: Parameter) =>
  model.type === "image" &&
  parameter.type !== "assets" &&
  [
    "n",
    "image_count",
    "num_images",
    "numImages",
    "batch_size",
    "imageCount",
    "num_outputs",
    "count",
  ].includes(parameter.name);

export const imageOutputLabel = (model: Model) =>
  model.maxOutputsPerRequest === 4
    ? "每次任务最多返回 4 张，按一次计费"
    : "每次生成 1 张，按次计费";

export const referenceTypes = [
  { type: "image", label: "图片", unit: "张", field: "imageUrls" },
  { type: "video", label: "视频", unit: "个", field: "videoUrls" },
  { type: "audio", label: "音频", unit: "段", field: "audioUrls" },
] as const;

export function referenceLimit(
  parameter: Parameter,
  params: RecordData = {},
): number {
  return (
    parameter.maxByResolution?.[String(params.resolution)] ??
    parameter.max ??
    Infinity
  );
}
export function referenceTotalLimit(model?: Model): number {
  return Math.min(
    Infinity,
    ...(model?.parameters || [])
      .filter((p) => p.type === "assets" && p.maxTotal != null)
      .map((p) => p.maxTotal!),
  );
}
export function referenceCapacities(
  model?: Model,
  params: RecordData = {},
): Record<string, number> {
  const resolved = {
    resolution: model?.parameters.find((p) => p.name === "resolution")?.default,
    ...params,
  };
  const result: Record<string, number> = {};
  for (const parameter of model?.parameters || []) {
    if (parameter.type !== "assets") continue;
    const type = parameter.itemType || "image";
    result[type] = (result[type] || 0) + referenceLimit(parameter, resolved);
  }
  return result;
}
export function referenceSpecs(model?: Model): string[] {
  const limits = referenceCapacities(model);
  return referenceTypes
    .filter((item) => (limits[item.type] || 0) > 0)
    .map((item) =>
      limits[item.type] === Infinity
        ? model?.parameters.some(
            (p) =>
              p.type === "assets" &&
              p.itemType === item.type &&
              p.limitSource === "undocumented",
          )
          ? `${item.label}上限待确认`
          : `${item.label}不限`
        : `${item.label} ${limits[item.type]} ${item.unit}`,
    );
}
export function referenceLimitLabel(parameter: Parameter): string {
  const limit = referenceLimit(parameter);
  const unit =
    referenceTypes.find((item) => item.type === (parameter.itemType || "image"))
      ?.unit || "个";
  return limit === Infinity
    ? parameter.limitSource === "undocumented"
      ? "渠道未公开上限"
      : "不限数量"
    : limit === 0
      ? "不允许添加"
      : `最多 ${limit} ${unit}`;
}
