import { Image, Video, Music } from "lucide-react";
import type { Model, Parameter } from "../lib";
import { referenceTotalLimit, imageOutputLabel } from "../model-config";
import { billingLabel, specLabel } from "../pricing";

const readable: Record<string, string> = {
  omni_reference: "多素材参考",
  first_last_frames: "首尾帧",
  multi_frame: "多帧参考",
  references: "素材参考",
  frames: "首尾帧",
  auto: "自动",
  adaptive: "自适应",
  low: "低",
  medium: "中",
  high: "高",
  xhigh: "超高",
  max: "最高",
  standard: "标准",
};
function valuesFor(parameter: Parameter): string[] {
  if (parameter.type === "boolean")
    return [parameter.default ? "默认开启" : "默认关闭"];
  const values = (parameter.options || []).map((option) =>
    String(option.l ?? option),
  );
  if (parameter.name === "duration" || parameter.name === "n") {
    const suffix = parameter.name === "duration" ? "秒" : "张";
    const numeric = values.map(Number).sort((a, b) => a - b);
    if (numeric.length && numeric.every(Number.isFinite)) {
      if (
        numeric.length > 2 &&
        numeric.every(
          (value, index) => index === 0 || value === numeric[index - 1] + 1,
        )
      )
        return [`${numeric[0]}–${numeric.at(-1)} ${suffix}`];
      return numeric.map((value) => `${value} ${suffix}`);
    }
    if (parameter.min != null && parameter.max != null)
      return [
        parameter.min === parameter.max
          ? `${parameter.min} ${suffix}`
          : `${parameter.min}–${parameter.max} ${suffix}`,
      ];
    if (parameter.default != null)
      return [`默认 ${parameter.default} ${suffix}`];
  }
  if (values.length)
    return values.map((value) => readable[value] || specLabel(value));
  if (parameter.default != null)
    return [
      readable[String(parameter.default)] || specLabel(parameter.default),
    ];
  return [];
}
export function ModelCapabilities({ model }: { model: Model }) {
  const references = [
    { kind: "image", label: "图片", unit: "张", Icon: Image },
    { kind: "video", label: "视频", unit: "段", Icon: Video },
    { kind: "audio", label: "音频", unit: "段", Icon: Music },
  ].filter(
    (item) =>
      item.kind === "image" ||
      model.type === "video" ||
      model.parameters.some(
        (p) => p.type === "assets" && p.itemType === item.kind,
      ),
  );
  const total = referenceTotalLimit(model);
  const fieldOrder = [
    "resolution",
    "size",
    "duration",
    "ratio",
    "aspectRatio",
    "quality",
    "detail_level",
    "function_mode",
    "functionMode",
    "mode",
    "faceEnabled",
    "generate_audio",
  ];
  const specs = fieldOrder.flatMap((name) => {
    const parameter = model.parameters.find(
      (p) => p.name === name && p.type !== "assets",
    );
    if (!parameter) return [];
    const values = valuesFor(parameter);
    if (!values.length) return [];
    const label =
      (
        {
          duration: "输出时长",
          ratio: "画面比例",
          aspectRatio: "画面比例",
          n: "生成数量",
          function_mode: "生成方式",
          functionMode: "生成方式",
          mode: "生成方式",
          faceEnabled: "人脸处理",
          generate_audio: "生成音频",
        } as Record<string, string>
      )[name] || parameter.label;
    return [{ name, label, values }];
  });
  return (
    <div className="model-capabilities" aria-label="模型能力">
      <div className="model-reference-heading">
        <span>参考素材</span>
        {Number.isFinite(total) && <small>合计最多 {total} 个</small>}
      </div>
      <div
        className={`model-reference-limits ${references.length === 1 ? "single" : ""}`}
      >
        {references.map(({ kind, label, unit, Icon }) => {
          const fields = model.parameters.filter(
            (p) => p.type === "assets" && (p.itemType || "image") === kind,
          );
          const maximum = fields.reduce(
            (sum, p) => sum + (p.max ?? Infinity),
            0,
          );
          const unknown =
            maximum === Infinity &&
            fields.some((p) => p.limitSource === "undocumented");
          const text = unknown
            ? "待确认"
            : maximum === Infinity
              ? "不限"
              : maximum === 0
                ? "不支持"
                : `${maximum} ${unit}`;
          return (
            <div
              key={kind}
              className={maximum === 0 || unknown ? "is-unavailable" : ""}
              aria-label={`参考${label}：${text}`}
            >
              <span>
                <Icon size={13} />
                {label}
              </span>
              <strong>{text}</strong>
            </div>
          );
        })}
      </div>
      <dl className="model-specification-list">
        <div>
          <dt>计费方式</dt>
          <dd>{billingLabel(model)}</dd>
        </div>
        {model.type === "image" && (
          <div>
            <dt>生成数量</dt>
            <dd>{imageOutputLabel(model)}</dd>
          </div>
        )}
        {specs.map((spec) => (
          <div key={spec.name}>
            <dt>{spec.label}</dt>
            <dd className="model-capability-values">
              {spec.values.map((value, index) => (
                <span key={index}>{value}</span>
              ))}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
