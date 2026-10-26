import { useEffect, useState } from "react";
import {
  api,
  post,
  useApi,
  useRefresh,
  useAction,
  money,
  type User,
  type RecordData,
  ApiError,
} from "../lib";

type Operation = { key: string; path: string; body: RecordData };
const prefix = (uid: string) => `pf-money:${uid}:`;
const signal = () => window.dispatchEvent(new Event("pf-money-change"));
const inFlight = new Map<string, Promise<unknown>>();

async function moneyLock<T>(
  storageKey: string,
  action: () => T | Promise<T>,
): Promise<T> {
  if (!navigator.locks)
    return Promise.reject(
      new Error("当前浏览器不支持跨页面资金操作保护，请使用新版浏览器。"),
    );
  return await navigator.locks.request(storageKey, action);
}

async function clearPending(storageKey: string, operationKey: string) {
  await moneyLock(storageKey, () => {
    const saved = localStorage.getItem(storageKey);
    if (saved && JSON.parse(saved).key === operationKey)
      localStorage.removeItem(storageKey);
  });
  signal();
}

async function execute(uid: string, operation: Operation, replay: boolean) {
  const storageKey = prefix(uid) + operation.path;
  const flightKey = prefix(uid) + operation.key;
  const running = inFlight.get(flightKey);
  if (running) return running;
  const action = (async () => {
    try {
      let receipt;
      if (replay) {
        try {
          receipt = await api(
            `/money-operations/${encodeURIComponent(operation.key)}`,
          );
        } catch (error) {
          if (!(error instanceof ApiError) || error.status !== 404) throw error;
        }
      }
      receipt ??= await moneyLock(storageKey, () => {
        const saved = localStorage.getItem(storageKey);
        if (!saved || JSON.parse(saved).key !== operation.key)
          throw new ApiError("待确认操作已变更，请刷新页面后核对最新记录", 409);
        return post(operation.path, operation.body, "POST", {
          "Idempotency-Key": operation.key,
        });
      });
      if (receipt?.operationId !== operation.key || receipt?.ok !== true)
        throw new ApiError("资金回执不完整，请沿用原操作核对结果", 502);
      await clearPending(storageKey, operation.key);
      return receipt;
    } catch (error) {
      // A validation/insufficient-funds rejection is definite. Network/5xx errors
      // are ambiguous; retain the same operation across dialogs, reloads and tabs.
      if (error instanceof ApiError && error.status === 400)
        await clearPending(storageKey, operation.key);
      signal();
      throw error;
    } finally {
      inFlight.delete(flightKey);
    }
  })();
  inFlight.set(flightKey, action);
  return action;
}

export function useMoneyMutation() {
  const { data: user } = useApi<User>("/me");
  const refresh = useRefresh();
  return async (path: string, body: RecordData) => {
    if (!user) throw new Error("请先确认登录状态");
    const key = prefix(user.id) + path;
    const { operation, replay } = await moneyLock(key, () => {
      const saved = localStorage.getItem(key);
      if (saved) {
        const operation: Operation = JSON.parse(saved);
        if (JSON.stringify(operation.body) !== JSON.stringify(body))
          throw new Error(
            "此账户有待确认的资金操作，请先在页面顶部核对结果，再提交新的金额。",
          );
        return { operation, replay: true };
      }
      const operation = { key: crypto.randomUUID(), path, body };
      // Allocate atomically across tabs, and persist before any possible debit.
      localStorage.setItem(key, JSON.stringify(operation));
      return { operation, replay: false };
    });
    try {
      return await execute(user.id, operation, replay);
    } finally {
      refresh(
        "/me",
        "/sub-users",
        "/transactions",
        "/admin/users",
        "/admin/transactions",
      );
    }
  };
}

export function MoneyRecovery({ userId }: { userId: string }) {
  const [pending, setPending] = useState<Operation[]>([]);
  const { act, busy } = useAction();
  const refresh = useRefresh();
  useEffect(() => {
    const read = () => {
      const operations: Operation[] = [];
      try {
        for (let i = 0; i < localStorage.length; i++) {
          const key = localStorage.key(i)!;
          if (key.startsWith(prefix(userId))) {
            const operation = JSON.parse(localStorage.getItem(key)!);
            if (
              typeof operation.key === "string" &&
              /^\/(sub-users\/[^/]+\/transfer|admin\/users\/[^/]+\/credits)$/.test(
                operation.path,
              ) &&
              operation.body
            )
              operations.push(operation);
          }
        }
      } catch {
        /* The mutation itself reports storage failures before any debit. */
      }
      setPending(operations);
    };
    read();
    window.addEventListener("pf-money-change", read);
    window.addEventListener("storage", read);
    return () => {
      window.removeEventListener("pf-money-change", read);
      window.removeEventListener("storage", read);
    };
  }, [userId]);
  return (
    <>
      {pending.map((operation) => (
        <div className="money-recovery" role="status" key={operation.key}>
          <span>
            {operation.path.startsWith("/sub-users") ? "转账" : "调账"}{" "}
            {money(operation.body.credits ?? operation.body.amount)}{" "}
            的结果待确认。核对会沿用原操作编号，避免重复划款。
          </span>
          <button
            className="button"
            disabled={busy}
            onClick={() =>
              act(async () => {
                try {
                  await execute(userId, operation, true);
                } finally {
                  refresh();
                }
              }, "资金操作已确认，请查看最新余额与流水")
            }
          >
            核对并重试
          </button>
        </div>
      ))}
    </>
  );
}
