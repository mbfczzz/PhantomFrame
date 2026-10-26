import {
  createContext,
  useContext,
  useState,
  useEffect,
  useRef,
  useId,
  useCallback,
  Children,
  isValidElement,
  cloneElement,
  type ReactNode,
  type FormEvent,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  X,
  Loader2,
  Search,
  ChevronLeft,
  ChevronRight,
  Check,
  AlertCircle,
} from "lucide-react";
export type RecordData = Record<string, any>;
export type User = {
  id: string;
  username: string;
  displayName: string;
  role: string;
  balance: number;
  status: string;
  parentId?: string;
  createdAt: string;
};
export type Parameter = {
  name: string;
  label: string;
  type: string;
  default?: any;
  options?: any[];
  required?: boolean;
  integer?: boolean;
  min?: number | null;
  max?: number | null;
  itemType?: string;
  maxTotal?: number;
  maxByResolution?: Record<string, number>;
  limitSource?: string;
};
export type Price = {
  id: string;
  model_id: string;
  label: string;
  unit: string;
  credits: number;
  conditions: RecordData;
  enabled: boolean;
  modelName?: string;
  extras?: {
    imagePrice?: number;
    includedImages?: number;
    videoSecondPrice?: number;
    videoOutputSecondPrice?: number;
    videoRounding?: "total" | "each";
  };
};
export type Model = {
  inventory_route_id?: string;
  id: string;
  name: string;
  type: string;
  provider_id: string;
  providerName: string;
  upstream_model: string;
  description: string;
  category: string;
  enabled: boolean;
  sort_order: number;
  tags: string[];
  parameters: Parameter[];
  prices: Price[];
  pricingVersion: string;
  demo: boolean;
  maxOutputsPerRequest?: number;
};
export type Asset = {
  id: string;
  name: string;
  type: string;
  mime: string;
  size: number;
  url: string;
  downloadUrl?: string;
  jobId?: string;
  createdAt: string;
};
export type Job = {
  id: string;
  modelId: string;
  modelName: string;
  type: string;
  prompt: string;
  params: RecordData;
  status: string;
  cost: number;
  outputs: {
    assetId: string;
    url: string;
    downloadUrl?: string;
    urlExpiresAt?: string;
    storage?: string;
    type: string;
    name: string;
  }[];
  error?: string;
  source: string;
  demo: boolean;
  refunded: boolean;
  createdAt: string;
  finishedAt?: string;
  username?: string;
};
export type PageData<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
};
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function api<T = any>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const res = await fetch("/api" + path, {
    credentials: "include",
    ...options,
    headers: {
      ...(!(options.body instanceof FormData)
        ? { "Content-Type": "application/json" }
        : {}),
      ...options.headers,
    },
  }).catch(() => {
    throw new ApiError("网络连接中断，请检查网络后重试", 0);
  });
  const result = await res.json().catch(() => {
    throw new ApiError(
      "服务器返回了无效响应，请稍后重试",
      res.ok ? 502 : res.status,
    );
  });
  if (!res.ok) throw new ApiError(result.message || "请求失败", res.status);
  return result;
}
export const post = <T = any,>(
  path: string,
  data: unknown = {},
  method = "POST",
  headers?: Record<string, string>,
) => api<T>(path, { method, body: JSON.stringify(data), headers });
export function useApi<T = any>(path: string, options: RecordData = {}) {
  return useQuery<T, ApiError>({
    queryKey: [path],
    queryFn: () => api<T>(path),
    retry: false,
    ...options,
  });
}
export function useRefresh() {
  const client = useQueryClient();
  return useCallback(
    (...paths: string[]) => {
      if (!paths.length) return client.invalidateQueries();
      paths.forEach((path) =>
        client.invalidateQueries({
          predicate: (q) => String(q.queryKey[0]).startsWith(path),
        }),
      );
    },
    [client],
  );
}
export const number = (v: number | undefined) =>
  Number(v || 0).toLocaleString("zh-CN", { maximumFractionDigits: 2 });
export const money = (v: number | undefined) => {
  const value = Number(v || 0);
  return `${value < 0 ? "−" : ""}¥${Math.abs(value).toLocaleString("zh-CN", { maximumFractionDigits: 6 })}`;
};
export const moneyChange = (v: number) =>
  `${v < 0 ? "−" : "+"}${money(Math.abs(v))}`;
export const transactionDescription = (value: string) =>
  (
    ({
      新用户注册积分: "新用户注册赠送",
      演示积分: "演示额度",
      "生成结束 · 退还积分": "生成结束 · 退款",
      向子账户分配积分: "向子账户转账",
      父账户分配积分: "主账户转入",
    }) as Record<string, string>
  )[value] || value;
export const date = (v?: string) =>
  v
    ? new Date(v.includes("T") ? v : v.replace(" ", "T") + "Z").toLocaleString(
        "zh-CN",
        {
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        },
      )
    : "—";
export const unitLabel: Record<string, string> = {
  request: "次",
  second: "秒",
  image: "张",
};
export const generationLabels: Record<string, string> = {
  image: "图片",
  video: "视频",
  text: "文本",
  audio: "音频",
  upscale: "视频超分",
};
export const generationOptions = Object.entries(generationLabels).map(
  ([value, label]) => ({ value, label }),
);
export const statusLabel: Record<string, string> = {
  queued: "排队中",
  running: "生成中",
  refund_pending: "退款处理中",
  succeeded: "已完成",
  failed: "失败",
  cancelled: "已取消",
  active: "正常",
  disabled: "已停用",
};
export function defaults(model?: Model) {
  return Object.fromEntries(
    (model?.parameters || []).map((p) => [
      p.name,
      p.default ??
        (p.type === "assets"
          ? []
          : p.type === "boolean"
            ? false
            : (p.options?.[0]?.v ?? p.options?.[0] ?? "")),
    ]),
  );
}
const ToastContext = createContext<(text: string, error?: boolean) => void>(
  () => {},
);
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<
    { id: number; text: string; error: boolean }[]
  >([]);
  const toast = (text: string, error = false) => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, text, error }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000);
  };
  return (
    <ToastContext.Provider value={toast}>
      {children}
      <div className="toast-stack">
        {toasts.map((t) => (
          <div
            className={"toast " + (t.error ? "error" : "")}
            role={t.error ? "alert" : "status"}
            key={t.id}
          >
            {t.error ? <AlertCircle size={18} /> : <Check size={18} />}
            <span>{t.text}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
export const useToast = () => useContext(ToastContext);
export function useTheme() {
  const [theme, setTheme] = useState<"light" | "dark">(
    document.documentElement.dataset.theme === "dark" ? "dark" : "light",
  );
  useEffect(() => {
    const observer = new MutationObserver(() =>
      setTheme(
        document.documentElement.dataset.theme === "dark" ? "dark" : "light",
      ),
    );
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);
  return theme;
}
export function useAction() {
  const pending = useRef(false);
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  return {
    busy,
    act: async (action: () => Promise<unknown>, message?: string) => {
      if (pending.current) return false;
      pending.current = true;
      setBusy(true);
      try {
        await action();
        if (message) toast(message);
        return true;
      } catch (e) {
        toast((e as Error).message, true);
        return false;
      } finally {
        pending.current = false;
        setBusy(false);
      }
    },
  };
}
const dialogStack: HTMLElement[] = [];
let previousBodyOverflow = "";
function updateDialogStack() {
  dialogStack.forEach((dialog, index) => {
    const inactive = index !== dialogStack.length - 1;
    dialog.parentElement!.inert = inactive;
    dialog.setAttribute("aria-modal", String(!inactive));
    if (inactive) dialog.setAttribute("aria-hidden", "true");
    else dialog.removeAttribute("aria-hidden");
  });
}
export function Modal({
  title,
  description,
  icon,
  children,
  onClose,
  wide = false,
  className = "",
  initialFocusRef,
}: {
  title: string;
  description?: string;
  icon?: ReactNode;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
  className?: string;
  initialFocusRef?: RefObject<HTMLElement | null>;
}) {
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const titleId = useId();
  useEffect(() => {
    const dialog = dialogRef.current!;
    const previous = document.activeElement as HTMLElement | null;
    if (!dialogStack.length) {
      previousBodyOverflow = document.body.style.overflow;
      document.body.style.overflow = "hidden";
    }
    dialogStack.push(dialog);
    updateDialogStack();
    const getFocusable = () =>
      Array.from(
        dialog.querySelectorAll<HTMLElement>(
          "button,input,textarea,select,a[href],[tabindex]",
        ),
      ).filter(
        (element) =>
          !element.hasAttribute("disabled") &&
          element.tabIndex >= 0 &&
          element.getClientRects().length > 0,
      );
    const listener = (e: KeyboardEvent) => {
      if (dialogStack.at(-1) !== dialog || e.defaultPrevented) return;
      if (e.key === "Escape") {
        e.preventDefault();
        closeRef.current();
      }
      if (e.key === "Tab") {
        // Select's portal owns its keyboard navigation until it closes.
        if ((e.target as HTMLElement).closest(".ui-select-popup")) return;
        const focusable = getFocusable();
        const first = focusable[0],
          last = focusable.at(-1);
        if (!first) {
          e.preventDefault();
          dialog.focus();
        } else if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last?.focus();
        } else if (
          (!e.shiftKey && document.activeElement === last) ||
          !dialog.contains(document.activeElement)
        ) {
          e.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener("keydown", listener);
    const focusTimer = requestAnimationFrame(() => {
      if (dialogStack.at(-1) !== dialog) return;
      // Respect focus acquired by a click or typing before the first paint.
      if (
        document.activeElement !== dialog &&
        dialog.contains(document.activeElement)
      )
        return;
      const controls = getFocusable();
      const firstField = controls.find((element) =>
        element.matches("input:not([type=checkbox]),textarea,select"),
      );
      (initialFocusRef?.current || firstField || controls[0] || dialog).focus({
        preventScroll: true,
      });
    });
    return () => {
      cancelAnimationFrame(focusTimer);
      document.removeEventListener("keydown", listener);
      const wasTop = dialogStack.at(-1) === dialog;
      const index = dialogStack.indexOf(dialog);
      if (index >= 0) dialogStack.splice(index, 1);
      updateDialogStack();
      if (!dialogStack.length)
        document.body.style.overflow = previousBodyOverflow;
      if (wasTop && previous?.isConnected && !previous.closest("[inert]"))
        previous.focus({ preventScroll: true });
    };
  }, []);
  return createPortal(
    <div
      className="modal-backdrop"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <section
        ref={dialogRef}
        className={`modal ${wide ? "wide" : ""} ${className}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? titleId + "-description" : undefined}
        tabIndex={-1}
      >
        <header>
          {icon && (
            <span className="modal-heading-icon" aria-hidden="true">
              {icon}
            </span>
          )}
          <div className="modal-heading-copy">
            <h2 id={titleId}>{title}</h2>
            {description && <p id={titleId + "-description"}>{description}</p>}
          </div>
          <button
            type="button"
            className="icon-button"
            aria-label="关闭"
            onClick={onClose}
          >
            <X size={20} />
          </button>
        </header>
        <div className="modal-body">{children}</div>
      </section>
    </div>,
    document.body,
  );
}

type ConfirmationOptions = { title?: string; confirmLabel?: string };
type Confirmation = { text: string; options: ConfirmationOptions };
const ConfirmContext = createContext<
  (text: string, options?: ConfirmationOptions) => Promise<boolean>
>(() => Promise.resolve(false));
export function useConfirm() {
  return useContext(ConfirmContext);
}
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<Confirmation | null>(null);
  const resolver = useRef<((answer: boolean) => void) | null>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  const confirm = useCallback(
    (text: string, options: ConfirmationOptions = {}) => {
      if (resolver.current) return Promise.resolve(false);
      return new Promise<boolean>((resolve) => {
        resolver.current = resolve;
        setPending({ text, options });
      });
    },
    [],
  );
  const settle = (answer: boolean) => {
    const resolve = resolver.current;
    resolver.current = null;
    setPending(null);
    resolve?.(answer);
  };
  useEffect(
    () => () => {
      resolver.current?.(false);
      resolver.current = null;
    },
    [],
  );
  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && (
        <Modal
          title={pending.options.title || "确认操作"}
          icon={<AlertCircle size={21} />}
          className="confirmation-dialog"
          onClose={() => settle(false)}
          initialFocusRef={cancelButton}
        >
          <p className="confirmation-message">{pending.text}</p>
          <div className="modal-footer">
            <button
              ref={cancelButton}
              type="button"
              className="button"
              onClick={() => settle(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="button confirmation-accept"
              onClick={() => settle(true)}
            >
              {pending.options.confirmLabel || "确认继续"}
            </button>
          </div>
        </Modal>
      )}
    </ConfirmContext.Provider>
  );
}
export function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  const fieldId = useId(),
    labelId = fieldId + "-label";
  let labelled = false;
  const attach = (nodes: ReactNode): ReactNode =>
    Children.map(nodes, (node) => {
      if (!isValidElement<RecordData>(node)) return node;
      if (
        !labelled &&
        typeof node.type === "string" &&
        ["input", "select", "textarea"].includes(node.type)
      ) {
        labelled = true;
        return cloneElement(node, {
          id: fieldId,
          "aria-labelledby": labelId,
          ...(hint
            ? {
                "aria-describedby": [
                  node.props["aria-describedby"],
                  fieldId + "-hint",
                ]
                  .filter(Boolean)
                  .join(" "),
              }
            : {}),
        });
      }
      return node.props.children
        ? cloneElement(node, {}, attach(node.props.children))
        : node;
    });
  return (
    <div className="field">
      <label id={labelId} htmlFor={fieldId}>
        {label}
      </label>
      {attach(children)}
      {hint && <small id={fieldId + "-hint"}>{hint}</small>}
    </div>
  );
}
export function Empty({
  title = "暂无内容",
  text,
  icon,
  children,
}: {
  title?: string;
  text?: string;
  icon?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <div className="empty-icon">{icon || <Search size={28} />}</div>
      <h3>{title}</h3>
      {text && <p>{text}</p>}
      {children}
    </div>
  );
}
export function Loading() {
  return (
    <div className="loading">
      <Loader2 size={22} className="spin" />
      <span>正在加载…</span>
    </div>
  );
}
export function ErrorState({
  error,
  retry,
}: {
  error: Error;
  retry?: () => void;
}) {
  return (
    <Empty
      title="加载失败"
      text={error.message}
      icon={<AlertCircle size={26} />}
    >
      <button className="button" onClick={retry}>
        重新加载
      </button>
    </Empty>
  );
}
export function PageHeader({
  eyebrow,
  title,
  description,
  children,
}: {
  eyebrow?: string;
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        {eyebrow && <span className="eyebrow">{eyebrow}</span>}
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      <div className="header-actions">{children}</div>
    </header>
  );
}
export function Tabs({
  value,
  onChange,
  items,
  panelId,
}: {
  value: string;
  onChange: (v: string) => void;
  items: { value: string; label: ReactNode }[];
  panelId?: string;
}) {
  const root = useRef<HTMLDivElement>(null);
  return (
    <div
      className="tabs"
      ref={root}
      role={panelId ? "tablist" : "group"}
      aria-label={panelId ? "内容分页" : "筛选选项"}
    >
      {items.map((i) => (
        <button
          type="button"
          role={panelId ? "tab" : undefined}
          id={panelId ? `${panelId}-tab-${i.value}` : undefined}
          aria-controls={panelId}
          aria-selected={panelId ? value === i.value : undefined}
          aria-pressed={panelId ? undefined : value === i.value}
          tabIndex={panelId && value !== i.value ? -1 : 0}
          onKeyDown={(event) => {
            if (!panelId) return;
            const index = items.findIndex((item) => item.value === i.value);
            const rtl =
              getComputedStyle(event.currentTarget).direction === "rtl";
            let next = index;
            if (event.key === "ArrowRight") next += rtl ? -1 : 1;
            else if (event.key === "ArrowLeft") next += rtl ? 1 : -1;
            else if (event.key === "Home") next = 0;
            else if (event.key === "End") next = items.length - 1;
            else return;
            event.preventDefault();
            next = (next + items.length) % items.length;
            onChange(items[next].value);
            root.current
              ?.querySelectorAll<HTMLButtonElement>("button")
              [next]?.focus();
          }}
          className={value === i.value ? "active" : ""}
          key={i.value}
          onClick={() => onChange(i.value)}
        >
          {i.label}
        </button>
      ))}
    </div>
  );
}
export function SearchBox({
  value,
  onChange,
  placeholder = "搜索",
  onCompositionChange,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  onCompositionChange?: (composing: boolean) => void;
}) {
  return (
    <div className="searchbox">
      <Search size={16} />
      <input
        aria-label={placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onCompositionStart={() => onCompositionChange?.(true)}
        onCompositionEnd={() => onCompositionChange?.(false)}
      />
      {value && (
        <button
          className="icon-button"
          onClick={() => onChange("")}
          aria-label="清空搜索"
        >
          <X size={14} />
        </button>
      )}
    </div>
  );
}
export function Pagination({
  page,
  total,
  limit = 24,
  onChange,
}: {
  page: number;
  total?: number;
  limit?: number;
  onChange: (p: number) => void;
}) {
  const last =
    total === undefined ? undefined : Math.max(1, Math.ceil(total / limit));
  useEffect(() => {
    if (last !== undefined && page > last) onChange(last);
  }, [last, page, onChange]);
  if (total === undefined) return null;
  return total > limit ? (
    <div className="pagination">
      <span>共 {total} 条</span>
      <button
        className="icon-button"
        disabled={page === 1}
        onClick={() => onChange(page - 1)}
        aria-label="上一页"
      >
        <ChevronLeft size={17} />
      </button>
      <span>
        {page} / {Math.ceil(total / limit)}
      </span>
      <button
        className="icon-button"
        disabled={page * limit >= total}
        onClick={() => onChange(page + 1)}
        aria-label="下一页"
      >
        <ChevronRight size={17} />
      </button>
    </div>
  ) : null;
}
export function Badge({
  status,
  children,
}: {
  status?: string;
  children?: ReactNode;
}) {
  return (
    <span className={"badge " + (status || "")}>
      {children || statusLabel[status || ""] || status}
    </span>
  );
}
export function JsonEditor({
  value,
  onChange,
  label = "JSON 配置",
}: {
  value: string;
  onChange: (v: string) => void;
  label?: string;
}) {
  return (
    <Field label={label}>
      <textarea
        className="code-input"
        rows={8}
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}
export function Form({
  children,
  onSubmit,
}: {
  children: ReactNode;
  onSubmit: () => void;
}) {
  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      {children}
    </form>
  );
}
export async function copy(value: string) {
  await navigator.clipboard.writeText(value);
}
export function downloadJSON(value: unknown, name: string) {
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(value, null, 2)], { type: "application/json" }),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
