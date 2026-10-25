import type { Model, Parameter, Price } from "./lib";
import { money } from "./lib";
export function priceExtraLabels(price: Pick<Price, "extras">): string[] {
  const extras = price.extras || {};
  const lines = [];
  if (extras.imagePrice)
    lines.push(
      extras.includedImages
        ? `参考图片前 ${extras.includedImages} 张免费，超出 ${money(extras.imagePrice)}/张`
        : `参考图片另计 ${money(extras.imagePrice)}/张`,
    );
  if (extras.videoSecondPrice)
    lines.push(
      `参考视频 ${money(extras.videoSecondPrice)}/秒（${extras.videoRounding === "each" ? "逐段" : "合计"}向上取整）`,
    );
  if (extras.videoOutputSecondPrice)
    lines.push(
      `带参考视频时，按生成时长另计 ${money(extras.videoOutputSecondPrice)}/秒`,
    );
  return lines;
}

export const billingOptions = (kind: string) =>
  kind === "image"
    ? [{ value: "request", label: "按次" }]
    : [
        { value: "request", label: "按次" },
        { value: "second", label: "按秒" },
      ];
export const billingUnit = (model?: Model) =>
  model?.prices.find((price) => price.enabled)?.unit || "";
export const billingLabel = (model: Model) => {
  const units = [
    ...new Set(model.prices.filter((p) => p.enabled).map((p) => p.unit)),
  ];
  return units.length
    ? units
        .map(
          (unit) =>
            ({ request: "按次", second: "按秒", image: "按次" })[unit] || unit,
        )
        .join(" / ")
    : "待配置";
};
export const specLabel = (value: unknown) =>
  String(value).replace(/^(\d+)k$/i, "$1K");
export const pricingDimensions = (model: Model): Parameter[] =>
  model.parameters.filter(
    (p) =>
      p.type === "select" &&
      ["resolution", "size", "quality", "detail_level"].includes(p.name),
  );
export function specificationRates(model: Model) {
  const dimensions = pricingDimensions(model);
  return model.prices
    .filter(
      (price) => price.enabled && Object.keys(price.conditions).length > 0,
    )
    .map((price) => ({
      ...price,
      specification: [
        ...dimensions
          .map((p) => p.name)
          .filter((name) => name in price.conditions),
        ...Object.keys(price.conditions).filter(
          (name) => !dimensions.some((p) => p.name === name),
        ),
      ]
        .map((name) => {
          const value = price.conditions[name];
          const p = model.parameters.find(
            (parameter) => parameter.name === name,
          );
          const option = p?.options?.find(
            (o) => String(o.v ?? o) === String(value),
          );
          const label =
            typeof value === "boolean"
              ? value
                ? "开启"
                : "关闭"
              : specLabel(option?.l ?? value);
          return dimensions.some((parameter) => parameter.name === name)
            ? label
            : `${name === "faceEnabled" ? "人脸处理" : p?.label || name} ${label}`;
        })
        .join(" · "),
    }));
}
export const matchingSpecification = (
  price: Price,
  conditions: Record<string, unknown>,
) => {
  const keys = Object.keys(conditions);
  return (
    Object.keys(price.conditions).length === keys.length &&
    keys.every(
      (key) => String(price.conditions[key]) === String(conditions[key]),
    )
  );
};
