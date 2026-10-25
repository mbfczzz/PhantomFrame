import { useState, useEffect } from "react";
import { Link, useNavigate, useLocation } from "react-router-dom";
import { Eye, EyeOff, ArrowUpRight } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useApi, post, Field, Form, type RecordData, type User } from "../lib";
import { Brand } from "../components/Brand";
import { preferredTheme } from "../theme";
export default function Auth() {
  const register = useLocation().pathname === "/register";
  const { data: config } = useApi<RecordData>("/config");
  const [username, setUsername] = useState(""),
    [password, setPassword] = useState(""),
    [showPassword, setShowPassword] = useState(false);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const navigate = useNavigate();
  const client = useQueryClient();
  const brandLabel =
    !config?.siteName || config.siteName === "PhantomFrame"
      ? "幻帧"
      : config.siteName;
  useEffect(() => {
    document.documentElement.dataset.theme = preferredTheme();
  }, []);
  useEffect(() => setError(""), [register]);
  const finish = (data: User) => {
    client.clear();
    client.setQueryData(["/me"], data);
    navigate("/generate");
  };
  const submit = async (demo = false) => {
    setError("");
    setBusy(true);
    try {
      finish(
        await post(
          demo ? "/auth/demo" : register ? "/auth/register" : "/auth/login",
          demo ? {} : { username: username.trim(), password },
        ),
      );
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="auth-page">
      <div className="auth-card">
        <section className="auth-art">
          <img
            className="auth-hero-image"
            src="/images/login-light-study.jpg"
            alt="阳光与栏杆的影子落在混凝土台阶上，摄影：Henry Fraczek"
            fetchPriority="high"
          />
          <div className="auth-art-shade" />
          <div className="auth-brand">
            <Brand name={config?.siteName} />
          </div>
          <div className="auth-art-copy">
            <span className="auth-photo-label">光与结构</span>
            <a
              className="auth-photo-credit"
              href="https://unsplash.com/photos/3FsWmv2fBaE"
              target="_blank"
              rel="noreferrer"
            >
              Henry Fraczek / Unsplash <ArrowUpRight size={12} />
            </a>
          </div>
        </section>
        <section className="auth-form">
          <div className="auth-mobile-brand">
            <Brand name={config?.siteName} />
          </div>
          <h2>{register ? "创建账号" : `登录${brandLabel}`}</h2>
          <p>
            {register
              ? "注册后即可创作图片和视频，管理生成记录与资产。"
              : config?.subtitle &&
                  config.subtitle !== "让每一个想法，成为独特的一帧。"
                ? config.subtitle
                : "输入账号，继续上次的创作。"}
          </p>
          <Form onSubmit={() => submit()}>
            <Field label="用户名">
              <input
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value);
                  setError("");
                }}
                placeholder="输入你的用户名"
                autoComplete="username"
                required
                minLength={3}
                aria-invalid={!!error}
                aria-describedby={error ? "auth-error" : undefined}
              />
            </Field>
            <Field label="密码">
              <div className="input-icon">
                <input
                  aria-label="密码"
                  type={showPassword ? "text" : "password"}
                  value={password}
                  onChange={(e) => {
                    setPassword(e.target.value);
                    setError("");
                  }}
                  placeholder={
                    register ? "设置密码，至少 8 位" : "输入你的密码"
                  }
                  autoComplete={register ? "new-password" : "current-password"}
                  required
                  minLength={register ? 8 : 1}
                  aria-invalid={!!error}
                  aria-describedby={error ? "auth-error" : undefined}
                />
                <button
                  type="button"
                  className="icon-button"
                  onClick={() => setShowPassword(!showPassword)}
                  aria-label={showPassword ? "隐藏密码" : "显示密码"}
                >
                  {showPassword ? <EyeOff size={17} /> : <Eye size={17} />}
                </button>
              </div>
            </Field>
            {error && (
              <div className="auth-error" role="alert" id="auth-error">
                {error}
              </div>
            )}
            <button className="button primary full auth-submit" disabled={busy}>
              {busy ? "请稍候…" : register ? "注册" : "登录"}
            </button>
          </Form>
          {(register || config?.registrationEnabled !== "false") && (
            <div className="auth-switch">
              {register ? "已经有账号？" : "还没有账号？"}{" "}
              <Link to={register ? "/login" : "/register"}>
                {register ? "登录" : "注册账号"}
              </Link>
            </div>
          )}
          {config?.demoMode && !register && (
            <>
              <div className="auth-divider">
                <span>或</span>
              </div>
              <button
                className="button full auth-demo"
                disabled={busy}
                onClick={() => submit(true)}
              >
                试用演示账号
              </button>
            </>
          )}
          <Link className="auth-help-link" to="/docs">
            使用说明
          </Link>
        </section>
      </div>
      <footer className="auth-footer">
        <span>PHANTOMFRAME © {new Date().getFullYear()}</span>
        <span>图片 · 视频创作</span>
      </footer>
    </div>
  );
}
