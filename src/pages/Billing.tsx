import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
  ArrowDownLeft,
  ArrowUpRight,
  CheckCircle2,
  Copy,
  LifeBuoy,
  ReceiptText,
  RotateCcw,
  Wallet,
} from "lucide-react";
import { Select } from "../components/Select";
import {
  useApi,
  useRefresh,
  useToast,
  post,
  money,
  moneyChange,
  transactionDescription,
  date,
  copy,
  PageHeader,
  Field,
  Form,
  Loading,
  ErrorState,
  Empty,
  Pagination,
  SearchBox,
  type User,
  type RecordData,
  type PageData,
} from "../lib";
import { Stat, TransactionTable } from "./Account";

const kinds = [
  { value: "all", label: "全部类型" },
  { value: "redeem", label: "兑换充值" },
  { value: "generation", label: "生成扣费" },
  { value: "refund", label: "退款" },
  { value: "welcome", label: "注册赠送" },
  { value: "adjustment", label: "人工调账" },
  { value: "transfer", label: "余额划转" },
];
type LedgerData = PageData<RecordData> & {
  summary: { balance: number; income: number; expense: number; unit: string };
};

export function Ledger() {
  const [initial] = useSearchParams();
  const [filters, setFilters] = useState({
    kind: kinds.some((kind) => kind.value === initial.get("kind"))
      ? initial.get("kind")!
      : "all",
    direction: "all",
    from: "",
    to: "",
    search: "",
    page: 1,
  });
  const query = new URLSearchParams({
    ...filters,
    page: String(filters.page),
    limit: "20",
  });
  const { data, isLoading, error, refetch } = useApi<LedgerData>(
    "/transactions?" + query,
    { refetchInterval: 10000 },
  );
  const refresh = useRefresh();
  useEffect(() => {
    if (data?.summary) refresh("/me");
  }, [data?.summary.balance, refresh]);
  const change = (field: string, value: string) =>
    setFilters((current) => ({ ...current, [field]: value, page: 1 }));
  return (
    <div className="page billing-page">
      <PageHeader
        title="资金流水"
        description="查看兑换充值、生成扣费与退款等账户变动，金额单位为人民币（元）。"
      >
        <Link to="/redeem" className="button primary">
          <Wallet size={16} />
          兑换充值
        </Link>
      </PageHeader>
      <div className="stats-grid billing-stats">
        <Stat
          label="当前账户余额"
          value={data ? money(data.summary.balance) : "—"}
          icon={<Wallet size={17} />}
          sub="人民币（元）"
        />
        <Stat
          label="筛选范围内入账"
          value={data ? moneyChange(data.summary.income) : "—"}
          icon={<ArrowDownLeft size={17} />}
          sub="包含充值、退款与其他入账"
        />
        <Stat
          label="筛选范围内支出"
          value={data ? `−${money(data.summary.expense)}` : "—"}
          icon={<ArrowUpRight size={17} />}
          sub="包含生成扣费与其他支出"
        />
      </div>
      <section className="panel ledger-panel">
        <div className="ledger-filters">
          <div className="ledger-filter">
            <span>流水类型</span>
            <Select
              label="流水类型"
              value={filters.kind}
              options={kinds}
              onChange={(value) => change("kind", value)}
            />
          </div>
          <div className="ledger-filter">
            <span>收支方向</span>
            <Select
              label="收支方向"
              value={filters.direction}
              options={[
                { value: "all", label: "全部收支" },
                { value: "in", label: "入账" },
                { value: "out", label: "支出" },
              ]}
              onChange={(value) => change("direction", value)}
            />
          </div>
          <label className="ledger-filter">
            <span>开始日期</span>
            <input
              type="date"
              aria-label="流水开始日期"
              value={filters.from}
              onChange={(event) => change("from", event.target.value)}
            />
          </label>
          <label className="ledger-filter">
            <span>结束日期</span>
            <input
              type="date"
              aria-label="流水结束日期"
              value={filters.to}
              onChange={(event) => change("to", event.target.value)}
            />
          </label>
          <button
            type="button"
            className="button"
            onClick={() =>
              setFilters({
                kind: "all",
                direction: "all",
                from: "",
                to: "",
                search: "",
                page: 1,
              })
            }
          >
            <RotateCcw size={14} />
            重置筛选
          </button>
        </div>
        <div className="ledger-search">
          <SearchBox
            placeholder="搜索说明、流水号或任务 ID"
            value={filters.search}
            onChange={(value) => change("search", value)}
          />
          <span>共 {data?.total || 0} 条记录 · 日期按 UTC 筛选</span>
        </div>
        {isLoading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} retry={refetch} />
        ) : (
          <TransactionTable items={data?.items || []} details />
        )}
        <Pagination
          page={filters.page}
          limit={20}
          total={data?.total}
          onChange={(page) => setFilters((current) => ({ ...current, page }))}
        />
      </section>
      <p className="billing-help">
        对账单有疑问？
        <Link to="/tickets/new?category=billing">
          提交工单 <LifeBuoy size={13} />
        </Link>
      </p>
    </div>
  );
}

export function RedeemPage() {
  const { data: user } = useApi<User>("/me");
  const { data: config } = useApi<RecordData>("/config");
  const {
    data: history,
    isLoading,
    error,
    refetch,
  } = useApi<PageData<RecordData>>("/transactions?kind=redeem&limit=5");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [receipt, setReceipt] = useState<{
    credits: number;
    balance: number;
    transactionId: string;
  } | null>(null);
  const refresh = useRefresh(),
    toast = useToast();
  const redeem = async () => {
    if (busy || !code.trim()) return;
    setBusy(true);
    setMessage("");
    setReceipt(null);
    try {
      const result = await post<{
        credits: number;
        balance: number;
        transactionId: string;
      }>("/redeem", { code: code.trim() });
      setReceipt(result);
      setCode("");
      refresh("/me", "/usage", "/transactions");
    } catch (error) {
      setMessage((error as Error).message || "兑换失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="page billing-page">
      <PageHeader
        title="兑换充值"
        description="使用兑换码为账户充值，可用于图片、视频和 API 调用。"
      >
        <Link className="button" to="/transactions">
          <ReceiptText size={16} />
          资金流水
        </Link>
      </PageHeader>
      <div className="recharge-layout">
        <section className="panel recharge-card">
          <div className="recharge-balance">
            <span>
              <Wallet size={16} />
              当前可用余额
            </span>
            <strong>{user ? money(user.balance) : "—"}</strong>
          </div>
          <Form onSubmit={redeem}>
            <Field
              label="兑换码"
              hint="每个兑换码仅可使用一次，成功后金额立即计入当前账户余额。"
            >
              <input
                autoComplete="off"
                autoCapitalize="characters"
                spellCheck={false}
                value={code}
                onChange={(event) => {
                  setCode(event.target.value);
                  setMessage("");
                }}
                placeholder="输入或粘贴兑换码"
                maxLength={100}
                required
                aria-invalid={!!message}
                aria-describedby={message ? "redeem-error" : undefined}
              />
            </Field>
            {message && (
              <p id="redeem-error" role="alert" className="redeem-error">
                {message}
              </p>
            )}
            <button
              className="button primary full"
              disabled={busy || !code.trim()}
            >
              {busy ? "正在兑换…" : "确认兑换"}
            </button>
          </Form>
          {receipt && (
            <div className="redeem-receipt" role="status">
              <CheckCircle2 size={22} />
              <div>
                <strong>兑换成功，已到账 {money(receipt.credits)}</strong>
                <p>兑换后余额：{money(receipt.balance)}</p>
                <button
                  type="button"
                  className="text-link"
                  onClick={() =>
                    copy(receipt.transactionId)
                      .then(() => toast("流水号已复制"))
                      .catch(() => toast("复制失败", true))
                  }
                >
                  <Copy size={13} />
                  复制本次流水号
                </button>
              </div>
            </div>
          )}
        </section>
        <aside className="panel recharge-guide">
          <h2>充值与帮助</h2>
          <p>
            金额统一按人民币计算，1 积分对应 ¥1。网页创作和 API
            调用共用余额，兑换、扣费和退款均可在资金流水中核对。
          </p>
          {config?.purchaseUrl && (
            <a
              className="button"
              href={config.purchaseUrl}
              target="_blank"
              rel="noreferrer"
            >
              获取兑换码 <ArrowUpRight size={15} />
            </a>
          )}
          <Link className="recharge-support" to="/tickets/new?category=billing">
            <LifeBuoy size={18} />
            <span>
              <strong>兑换遇到问题？</strong>
              <small>提交工单，查看客服回复与处理进度</small>
            </span>
            <ArrowUpRight size={15} />
          </Link>
        </aside>
      </div>
      <section className="panel">
        <div className="section-header">
          <h2>最近兑换</h2>
          <Link className="text-link" to="/transactions?kind=redeem">
            查看全部 <ArrowUpRight size={14} />
          </Link>
        </div>
        {isLoading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} retry={refetch} />
        ) : history?.items.length ? (
          <div className="recent-redemptions">
            {history.items.map((item) => (
              <div key={item.id}>
                <div>
                  <strong>{transactionDescription(item.description)}</strong>
                  <small>{date(item.created_at)}</small>
                </div>
                <strong className="green">{moneyChange(item.amount)}</strong>
                <span className="muted">余额 {money(item.balance)}</span>
              </div>
            ))}
          </div>
        ) : (
          <Empty
            title="暂无兑换记录"
            text="兑换成功后会在这里显示入账记录。"
            icon={<ReceiptText size={26} />}
          />
        )}
      </section>
    </div>
  );
}
