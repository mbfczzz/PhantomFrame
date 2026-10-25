import { Image, Video, Music } from "lucide-react";
import { referenceTypes } from "../model-config";
import type { Parameter } from "../lib";

export function ReferenceSettings({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  let parameters: Parameter[];
  try {
    const parsed = JSON.parse(value);
    if (
      !Array.isArray(parsed) ||
      parsed.some((item) => !item || typeof item !== "object")
    )
      throw new Error("Invalid parameters");
    parameters = parsed;
  } catch {
    return (
      <div className="inline-error">
        请先修正高级参数配置中的 JSON，再编辑参考素材限制。
      </div>
    );
  }
  const save = (next: Parameter[]) => onChange(JSON.stringify(next, null, 2));
  return (
    <section className="reference-settings" aria-label="参考素材配置">
      <div className="reference-settings-heading">
        <h3>参考素材</h3>
        <p>启用支持的素材类型。数量上限留空表示不限，填 0 表示不允许添加。</p>
      </div>
      <div className="reference-settings-grid">
        {referenceTypes.map(({ type, label, unit, field }) => {
          const fields = parameters
            .map((item, index) => ({ item, index }))
            .filter(
              ({ item }) =>
                item.type === "assets" && (item.itemType || "image") === type,
            );
          const Icon =
            type === "image" ? Image : type === "video" ? Video : Music;
          return (
            <div className="reference-setting" key={type}>
              <label className="reference-support">
                <Icon size={17} />
                <span>{label}</span>
                <input
                  type="checkbox"
                  aria-label={`支持${label}参考`}
                  checked={!!fields.length}
                  onChange={(event) => {
                    if (!event.target.checked)
                      save(
                        parameters.filter(
                          (item) =>
                            item.type !== "assets" ||
                            (item.itemType || "image") !== type,
                        ),
                      );
                    else {
                      let name: string = field;
                      for (
                        let suffix = 2;
                        parameters.some((item) => item.name === name);
                        suffix++
                      )
                        name = field + suffix;
                      save([
                        ...parameters,
                        {
                          name,
                          label: `参考${label}`,
                          type: "assets",
                          itemType: type,
                        },
                      ]);
                    }
                  }}
                />
              </label>
              {fields.length ? (
                fields.map(({ item, index }) => (
                  <label className="reference-limit-field" key={item.name}>
                    <span>
                      {fields.length > 1 ? item.label : "数量上限"}
                      <small>（{unit}）</small>
                    </span>
                    <input
                      type="number"
                      aria-label={
                        fields.length > 1
                          ? `${item.label}数量上限`
                          : `${label}数量上限`
                      }
                      min={0}
                      step={1}
                      value={item.max ?? ""}
                      placeholder="不限"
                      onChange={(event) => {
                        const next = { ...item };
                        delete next.limitSource;
                        if (event.target.value === "") delete next.max;
                        else next.max = Number(event.target.value);
                        save(
                          parameters.map((current, i) =>
                            i === index ? next : current,
                          ),
                        );
                      }}
                    />
                    {fields.length > 1 && <code>{item.name}</code>}
                    {item.maxByResolution && (
                      <small className="muted">
                        分规格限制：
                        {Object.entries(item.maxByResolution)
                          .map(
                            ([resolution, max]) =>
                              `${resolution} 最多 ${max} ${unit}`,
                          )
                          .join("；")}
                        （高级参数中可调整）
                      </small>
                    )}
                  </label>
                ))
              ) : (
                <p className="reference-unsupported">此模型未启用{label}参考</p>
              )}
            </div>
          );
        })}
      </div>
      {parameters.some((item) => item.type === "assets") && (
        <label className="reference-total-setting">
          <span>全部参考素材合计上限</span>
          <input
            type="number"
            min={0}
            step={1}
            aria-label="参考素材合计上限"
            placeholder="不限"
            value={
              parameters.find(
                (item) => item.type === "assets" && item.maxTotal != null,
              )?.maxTotal ?? ""
            }
            onChange={(event) =>
              save(
                parameters.map((item) => {
                  if (item.type !== "assets") return item;
                  const next = { ...item };
                  if (event.target.value === "") delete next.maxTotal;
                  else next.maxTotal = Number(event.target.value);
                  return next;
                }),
              )
            }
          />
          <small className="muted">
            图片、视频、音频一起计算，留空表示不额外限制总数。
          </small>
        </label>
      )}
    </section>
  );
}
