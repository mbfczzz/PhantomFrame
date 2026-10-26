import { useState, useEffect, useRef } from "react";
import {
  Navigate,
  Outlet,
  NavLink,
  Link,
  useNavigate,
  useLocation,
} from "react-router-dom";
import {
  Aperture,
  Boxes,
  History,
  FolderOpen,
  Code2,
  LifeBuoy,
  ReceiptText,
  Wallet,
  Sun,
  Moon,
  LogOut,
  Plus,
  Menu,
  X,
  BookOpen,
  MessageCircle,
  ChevronRight,
  ShieldCheck,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import {
  useApi,
  useToast,
  copy,
  post,
  money,
  Loading,
  ErrorState,
  type User,
  type RecordData,
} from "../lib";
import { MoneyRecovery } from "./MoneyRecovery";
import { Brand } from "./Brand";
import { preferredTheme, THEME_STORAGE_KEY } from "../theme";

const links = [
  ["/generate", "创作", Aperture],
  ["/history", "生成记录", History],
  ["/models", "模型与价格", Boxes],
  ["/assets", "资产库", FolderOpen],
  ["/developer", "开放平台", Code2],
] as const;
const serviceLinks = [
  ["/tickets", "提交工单", LifeBuoy],
  ["/transactions", "资金流水", ReceiptText],
  ["/redeem", "兑换充值", Wallet],
] as const;

export default function Shell() {
  const {
    data: user,
    isLoading,
    error: userError,
    refetch: refetchUser,
  } = useApi<User>("/me", {
    refetchOnWindowFocus: true,
  });
  const { data: config } = useApi<RecordData>("/config");
  const [menuOpen, setMenuOpen] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const logoutLock = useRef(false);
  const [theme, setTheme] = useState(preferredTheme);
  const menu = useRef<HTMLDivElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null);
  const location = useLocation(),
    navigate = useNavigate(),
    client = useQueryClient();
  const toast = useToast();
  const copySupport = () =>
    copy(config?.support || "")
      .then(() => toast("客服联系方式已复制"))
      .catch(() => toast(config?.support || "复制失败", true));
  const current =
    [...links, ...serviceLinks].find(([path]) =>
      location.pathname.startsWith(path),
    )?.[1] ||
    (location.pathname.startsWith("/admin") ? "管理后台" : "账户与余额");
  const switchTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    localStorage.setItem(THEME_STORAGE_KEY, next);
    setTheme(next);
  };
  const logout = async () => {
    if (logoutLock.current) return;
    logoutLock.current = true;
    setLoggingOut(true);
    try {
      await post("/auth/logout");
      client.clear();
      navigate("/login");
    } catch {
      toast("退出未完成，会话仍可能有效，请稍后再次退出。", true);
    } finally {
      logoutLock.current = false;
      setLoggingOut(false);
    }
  };
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);
  useEffect(() => {
    setMenuOpen(false);
    window.scrollTo({ top: 0, left: 0, behavior: "auto" });
  }, [location.pathname]);
  useEffect(() => {
    document.title = `${current} · ${config?.siteName || "PhantomFrame"}`;
  }, [current, config?.siteName]);
  useEffect(() => {
    if (!menuOpen) return;
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setMenuOpen(false);
        menuButton.current?.focus();
      }
      if (event.key === "Tab") {
        const items = Array.from(
          menu.current?.querySelectorAll<HTMLElement>(
            "a[href],button:not(:disabled)",
          ) || [],
        ).filter((element) => element.getClientRects().length);
        if (event.shiftKey && document.activeElement === items[0]) {
          event.preventDefault();
          items.at(-1)?.focus();
        } else if (!event.shiftKey && document.activeElement === items.at(-1)) {
          event.preventDefault();
          items[0]?.focus();
        }
      }
    };
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    menu.current?.querySelector<HTMLElement>("a[href]")?.focus();
    document.addEventListener("keydown", keyboard);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", keyboard);
    };
  }, [menuOpen]);
  if (isLoading) return <Loading />;
  if (userError?.status === 401)
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  if (userError || !user)
    return (
      <ErrorState
        error={userError || new Error("暂时无法确认登录状态")}
        retry={refetchUser}
      />
    );
  return (
    <div
      className={`app-shell horizontal-shell ${location.pathname === "/generate" ? "is-studio" : ""}`}
    >
      <a className="skip-to-content" href="#workspace-content">
        跳转到内容
      </a>
      <header className="app-masthead">
        <div className="app-topbar">
          <Brand name={config?.siteName} />
          <nav className="primary-nav" aria-label="工作空间导航">
            {links.map(([path, title]) => (
              <NavLink key={path} to={path}>
                {title}
              </NavLink>
            ))}
          </nav>
          <div className="topbar-actions">
            {user.role === "admin" && (
              <NavLink
                to="/admin"
                className="topbar-admin"
                title="管理后台"
                aria-label="管理后台"
              >
                <ShieldCheck size={16} />
                <span>管理后台</span>
              </NavLink>
            )}
            <Link className="topbar-balance" to="/redeem" aria-label="兑换充值">
              <Wallet size={15} />
              <strong>{money(user.balance)}</strong>
              <Plus size={13} />
            </Link>
            <button
              type="button"
              className="topbar-theme"
              aria-label={theme === "light" ? "深色主题" : "浅色主题"}
              title={theme === "light" ? "深色主题" : "浅色主题"}
              onClick={switchTheme}
            >
              {theme === "light" ? <Moon size={17} /> : <Sun size={17} />}
            </button>
            <NavLink
              className="account-switch topbar-account"
              to="/profile"
              aria-label="账户与余额"
            >
              <span className="avatar small">
                {user.displayName?.[0]?.toUpperCase()}
              </span>
              <span className="topbar-account-name">{user.displayName}</span>
            </NavLink>
            <button
              type="button"
              className="navigation-toggle"
              ref={menuButton}
              aria-label={menuOpen ? "关闭导航" : "打开导航"}
              aria-expanded={menuOpen}
              aria-controls="workspace-menu"
              onClick={() => setMenuOpen(!menuOpen)}
            >
              {menuOpen ? <X size={21} /> : <Menu size={21} />}
            </button>
          </div>
        </div>
        <nav className="mobile-tabs" aria-label="手机导航">
          {links.map(([path, title]) => (
            <NavLink key={path} to={path}>
              <span>{title}</span>
            </NavLink>
          ))}
        </nav>
      </header>
      <div className="workspace-subbar">
        <div className="workspace-location">
          <span>工作空间</span>
          <ChevronRight size={12} />
          <strong>{current}</strong>
        </div>
        <nav className="service-nav" aria-label="账户与支持导航">
          {serviceLinks.map(([path, title, Icon]) => (
            <NavLink key={path} to={path}>
              <Icon size={14} />
              {title}
            </NavLink>
          ))}
          <Link to="/docs">
            <BookOpen size={14} />
            使用文档
          </Link>
          {config?.support && (
            <button
              type="button"
              aria-label="复制客服联系方式"
              title="联系客服"
              onClick={copySupport}
            >
              <MessageCircle size={15} />
            </button>
          )}
          <button
            type="button"
            aria-label="退出登录"
            title="退出登录"
            onClick={logout}
            disabled={loggingOut}
          >
            <LogOut size={15} />
          </button>
        </nav>
      </div>
      {menuOpen && (
        <>
          <div
            className="workspace-menu-scrim"
            onClick={() => {
              setMenuOpen(false);
              menuButton.current?.focus();
            }}
          />
          <div
            id="workspace-menu"
            className="workspace-menu"
            ref={menu}
            role="dialog"
            aria-modal="true"
            aria-label="导航菜单"
          >
            <div className="workspace-menu-heading">
              <span>工作空间</span>
              <button
                type="button"
                className="icon-button"
                aria-label="收起导航"
                onClick={() => {
                  setMenuOpen(false);
                  menuButton.current?.focus();
                }}
              >
                <X size={18} />
              </button>
            </div>
            <nav aria-label="全部模块">
              {[...links, ...serviceLinks].map(([path, title, Icon]) => (
                <NavLink
                  key={path}
                  to={path}
                  onClick={() => setMenuOpen(false)}
                >
                  <Icon size={18} />
                  <span>{title}</span>
                </NavLink>
              ))}
            </nav>
            <div className="workspace-menu-tools">
              {user.role === "admin" && (
                <Link to="/admin" onClick={() => setMenuOpen(false)}>
                  <ShieldCheck size={15} />
                  管理后台
                </Link>
              )}
              <Link to="/profile" onClick={() => setMenuOpen(false)}>
                账户与余额
              </Link>
              <Link to="/docs">使用文档</Link>
              {config?.support && (
                <button type="button" onClick={copySupport}>
                  <MessageCircle size={15} />
                  联系客服
                </button>
              )}
              <button type="button" onClick={switchTheme}>
                {theme === "light" ? <Moon size={15} /> : <Sun size={15} />}
                <span>{theme === "light" ? "深色主题" : "浅色主题"}</span>
              </button>
              <button type="button" onClick={logout} disabled={loggingOut}>
                <LogOut size={15} />
                退出登录
              </button>
            </div>
          </div>
        </>
      )}
      <main id="workspace-content" className="main-content" tabIndex={-1}>
        <MoneyRecovery userId={user.id} />
        <Outlet />
      </main>
    </div>
  );
}
