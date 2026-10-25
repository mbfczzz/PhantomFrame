import { useState } from "react";
import { Coins } from "lucide-react";
import {
  Modal,
  Form,
  Field,
  post,
  useAction,
  useRefresh,
  unitLabel,
  type Model,
  type RecordData,
} from "../lib";
import {
  billingOptions,
  billingUnit,
  matchingSpecification,
  pricingDimensions,
  specLabel,
} from "../pricing";

function combinations(model: Model) {
  let rows: { label: string; conditions: RecordData }[] = [
    { label: "", conditions: {} },
  ];
  for (const parameter of pricingDimensions(model)) {
    if (rows.length * (parameter.options?.length || 0) > 200) return null;
    rows = rows.flatMap((row) =>
      (parameter.options || []).map((option) => ({
        label: [row.label, specLabel(option.l ?? option)]
          .filter(Boolean)
          .join(" · "),
        conditions: { ...row.conditions, [parameter.name]: option.v ?? option },
      })),
    );
  }
  return rows.map((row) => ({ ...row, label: row.label || "统一价格" }));
}
export function SpecificationPricing({
  model: initialModel,
  onClose,
}: {
  model: Model;
  onClose: () => void;
}) {
  const [model] = useState(initialModel);
  const activeUnit = billingUnit(model);
  const [unit, setUnit] = useState(activeUnit || "request");
  const specs = combinations(model);
  const buildRows = (unit: string) =>
    (specs || []).map((spec) => {
      const current = model.prices.find(
        (p) => p.unit === unit && matchingSpecification(p, spec.conditions),
      );
      return {
        ...spec,
        id: current?.id,
        label: current?.label || spec.label.slice(0, 80),
        value: current ? String(current.credits) : "",
        enabled: current?.enabled ?? true,
      };
    });
  const [rows, setRows] = useState(() => buildRows(unit));
  const { act, busy } = useAction();
  const refresh = useRefresh();
  return (
    <Modal
      title="按规格定价"
      description={model.name + " · 为不同规格分别设置人民币单价"}
      icon={<Coins size={20} />}
      wide
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          act(async () => {
            await post(
              "/admin/models/" + encodeURIComponent(model.id) + "/prices",
              {
                expectedVersion: model.pricingVersion,
                prices: rows
                  .filter((row) => row.value.trim() !== "")
                  .map((row) => ({
                    id: row.id,
                    label: row.label,
                    unit,
                    credits: Number(row.value),
                    conditions: row.conditions,
                    enabled: row.enabled,
                  })),
              },
              "PUT",
            );
            refresh(
              "/admin/models",
              "/admin/prices",
              "/models",
              "/pricing/quote",
            );
            onClose();
          }, "规格价格已保存")
        }
      >
        <Field
          label="计费方式"
          hint={
            model.type === "image"
              ? "图片统一按次计费，每次提交收取所选规格的价格。"
              : activeUnit
                ? "当前模型已有启用价格，修改计费方式前需先停用原价格规则。"
                : "保存价格后，将按此方式报价和扣费。"
          }
        >
          <select
            value={unit}
            disabled={model.type === "image" || !!activeUnit || busy}
            onChange={(event) => {
              setUnit(event.target.value);
              setRows(buildRows(event.target.value));
            }}
          >
            {billingOptions(model.type).map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </Field>
        <p className="pricing-formula">
          {unit === "second"
            ? "总价 = 当前规格单价 × 视频时长（秒）"
            : "总价 = 当前规格单次价格，一次提交计费一次"}
        </p>
        {specs ? (
          <div className="table-wrap specification-price-table">
            <table>
              <thead>
                <tr>
                  <th>规格 / 质量</th>
                  <th>单价（元 / {unitLabel[unit]}）</th>
                  <th>启用</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={index}>
                    <td>{specs[index].label}</td>
                    <td>
                      <input
                        type="number"
                        step="0.000001"
                        min="0"
                        max="1000000"
                        inputMode="decimal"
                        aria-label={specs[index].label + " 单价（元）"}
                        placeholder="未配置"
                        value={row.value}
                        onChange={(event) =>
                          setRows((current) =>
                            current.map((item, i) =>
                              i === index
                                ? { ...item, value: event.target.value }
                                : item,
                            ),
                          )
                        }
                      />
                    </td>
                    <td>
                      <input
                        type="checkbox"
                        aria-label={"启用 " + specs[index].label}
                        checked={row.enabled}
                        onChange={(event) =>
                          setRows((current) =>
                            current.map((item, i) =>
                              i === index
                                ? { ...item, enabled: event.target.checked }
                                : item,
                            ),
                          )
                        }
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="inline-error">
            规格组合超过 200 个，请使用「新增价格规则」按需设置。
          </p>
        )}
        <p className="muted small pricing-spec-help">
          规格来自当前模型参数，1K、2K、4K
          可分别定价。留空不新建或删除价格；原有其他价格规则继续参与匹配。
        </p>
        <div className="modal-footer dialog-actions">
          <button className="button" type="button" onClick={onClose}>
            取消
          </button>
          <button
            className="button primary"
            disabled={
              busy || !specs || !rows.some((row) => row.value.trim() !== "")
            }
          >
            {busy ? "保存中…" : "保存规格价格"}
          </button>
        </div>
      </Form>
    </Modal>
  );
}
