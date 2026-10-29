import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";

const output = resolve("test-results/opencrow-ui");
await mkdir(output, { recursive: true });
const database = "phantomframe_test_opencrow_ui_" + Date.now();
const dir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4194",
  upstream = "http://127.0.0.1:4195";
const video = await readFile("server/demo-video.webm");
const keys = [
  "gpt-5.4",
  "gpt-image-2",
  "wan3.0-video",
  "qwen3-tts-flash",
  "kling-video-motion-control-v3.0",
  "text-embedding-3-large",
];
const checks = [],
  errors = [],
  requests = [];
let app,
  browser,
  page,
  serverOutput = "",
  created = false;
const mock = createServer(async (req, res) => {
  const data = [];
  for await (const chunk of req) data.push(chunk);
  requests.push({ method: req.method, path: req.url });
  res.setHeader("Content-Type", "application/json");
  if (req.url === "/v1/models")
    return res.end(JSON.stringify({ data: keys.map((id) => ({ id })) }));
  if (req.url === "/result") {
    res.setHeader("Content-Type", "video/webm");
    return res.end(video);
  }
  if (req.url === "/v1/videos/generations")
    return res.end(
      JSON.stringify({ id: "local-video-task", status: "submitted" }),
    );
  if (req.url === "/v1/videos/generations/local-video-task")
    return res.end(
      JSON.stringify({
        id: "local-video-task",
        status: "succeeded",
        content: [
          { type: "video_url", video_url: { url: upstream + "/result" } },
        ],
      }),
    );
  res.statusCode = 404;
  res.end(JSON.stringify({ error: "unexpected local endpoint" }));
});
const delay = (ms) => new Promise((r) => setTimeout(r, ms));
async function api(ctx, path, method = "GET", body) {
  const r = await ctx.request.fetch(base + "/api" + path, {
    method,
    data: body,
  });
  const value = await r.json();
  assert.ok(r.ok(), `${path}: ${r.status()} ${JSON.stringify(value)}`);
  return value;
}
function check(name) {
  checks.push(name);
  console.log("PASS", name);
}
try {
  execFileSync("go", ["run", "./cmd/testdb", "create", database], {
    stdio: "pipe",
    windowsHide: true,
  });
  created = true;
  await new Promise((r) => mock.listen(4195, "127.0.0.1", r));
  app = spawn(
    resolve(`bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`),
    [],
    {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        APP_ENV: "test",
        HOST: "127.0.0.1",
        PORT: "4194",
        APP_URL: base,
        MYSQL_DATABASE: database,
        REDIS_PREFIX: database + ":",
        DATA_DIR: dir,
        STORAGE_DRIVER: "local",
        DEMO_MODE: "true",
        COOKIE_SECURE: "false",
        ADMIN_USERNAME: "opencrow-ui-admin",
        ADMIN_PASSWORD: "OpenCrow-ui-password-2026",
        ENCRYPTION_KEY: "a".repeat(64),
      },
    },
  );
  app.stdout.on("data", (d) => (serverOutput += d));
  app.stderr.on("data", (d) => (serverOutput += d));
  let ready = false;
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(base + "/api/health")).ok) {
        ready = true;
        break;
      }
    } catch {}
    await delay(150);
  }
  assert.ok(ready, serverOutput);
  // Legacy demo scenarios prepare explicit fixtures; production startup stays empty.
  execFileSync("go", ["run", "./cmd/testdb", "seed-demo", database], {
    stdio: "pipe",
    windowsHide: true,
  });
  browser = await launchBrowser();
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await ctx.route("**/*", (route) =>
    new URL(route.request().url()).hostname === "127.0.0.1"
      ? route.continue()
      : route.abort(),
  );
  await api(ctx, "/auth/login", "POST", {
    username: "opencrow-ui-admin",
    password: "OpenCrow-ui-password-2026",
  });
  page = await ctx.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/admin/providers", { waitUntil: "networkidle" });
  const startupProviders = (await api(ctx, "/admin/providers")).filter(
    (p) => p.protocol === "opencrow",
  );
  assert.equal(startupProviders.length, 1);
  assert.equal(startupProviders[0].hasKey, false);
  assert.equal(requests.length, 0);
  await page.getByRole("button", { name: "配置 API Key", exact: true }).click();
  let form = page.getByRole("dialog", { name: "编辑供应商渠道", exact: true });
  assert.equal(
    await form.getByLabel("接入协议", { exact: true }).inputValue(),
    "opencrow",
  );
  assert.equal(
    await form.getByLabel("渠道名称", { exact: true }).inputValue(),
    "OpenCrow",
  );
  assert.equal(
    await form.getByLabel("API 基础地址", { exact: true }).inputValue(),
    "https://api.router.ai/v1",
  );
  await page.screenshot({
    path: join(output, "startup-defaults-form.png"),
    fullPage: false,
    animations: "disabled",
  });
  await form.getByLabel("API 基础地址", { exact: true }).fill(upstream);
  await form
    .getByLabel("API Key", { exact: true })
    .fill("opencrow-browser-test-key");
  await form.getByRole("button", { name: "保存渠道", exact: true }).click();
  await form.waitFor({ state: "hidden" });
  const provider = (await api(ctx, "/admin/providers")).find(
    (p) => p.protocol === "opencrow",
  );
  assert.ok(provider);
  assert.ok(!JSON.stringify(provider).includes("opencrow-browser-test-key"));
  check(
    "explicit test fixture configures one OpenCrow account; key editing does not request upstream generation",
  );
  await page
    .getByRole("button", { name: "管理模型 OpenCrow", exact: true })
    .click();
  const catalog = page.getByRole("dialog", { name: "渠道模型", exact: true });
  await catalog
    .getByRole("button", { name: "拉取模型列表", exact: true })
    .click();
  await catalog
    .getByRole("button", { name: "发布 wan3.0-video", exact: true })
    .waitFor();
  assert.equal(
    await catalog.getByText("text-embedding-3-large", { exact: true }).count(),
    0,
  );
  await catalog
    .getByRole("button", { name: "发布 wan3.0-video", exact: true })
    .click();
  form = page.getByRole("dialog", { name: "发布到模型管理", exact: true });
  await form.getByLabel("站内模型 ID", { exact: true }).fill("opencrow-wan-ui");
  await form.getByRole("button", { name: "确认发布", exact: true }).click();
  await form.waitFor({ state: "hidden" });
  let model = (await api(ctx, "/admin/models")).find(
    (m) => m.id === "opencrow-wan-ui",
  );
  assert.equal(model.enabled, false);
  assert.equal(model.prices.length, 0);
  assert.equal(requests.filter((r) => r.method === "POST").length, 0);
  check(
    "authenticated catalog sync recognizes modalities, excludes embeddings and publishes unpriced models disabled",
  );
  await api(ctx, "/admin/prices", "POST", {
    model_id: model.id,
    label: "local fixture",
    unit: "request",
    credits: 0.25,
  });
  await api(ctx, "/admin/models/" + model.id, "PUT", {
    ...model,
    enabled: true,
  });
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await chooseOption(page, "生成类型", "video");
  await chooseOption(page, "模型", model.id);
  await page
    .getByLabel("提示词", { exact: true })
    .fill("OpenCrow 本地视频模拟");
  await page.getByRole("button", { name: "开始生成", exact: true }).click();
  await page.waitForFunction(() =>
    Array.from(document.querySelectorAll(".job-card")).some(
      (el) =>
        el.textContent.includes("OpenCrow 本地视频模拟") &&
        el.querySelector(".badge.succeeded"),
    ),
  );
  assert.equal((await api(ctx, "/me")).balance, 999.75);
  assert.equal(
    requests.filter(
      (r) => r.method === "POST" && r.path === "/v1/videos/generations",
    ).length,
    1,
  );
  check(
    "OpenCrow video submission, polling, nested output saving and one-time billing work through the studio",
  );
  const entries = await api(
    ctx,
    `/admin/providers/${provider.id}/models?limit=100`,
  );
  const motionEntry = entries.items.find(
    (item) => item.upstreamModel === "kling-video-motion-control-v3.0",
  );
  const publishedMotion = await api(
    ctx,
    `/admin/providers/${provider.id}/models/${motionEntry.id}/publish`,
    "POST",
    {
      modelId: "opencrow-motion-ui",
      name: "OpenCrow 动作控制测试",
      type: "video",
    },
  );
  await api(ctx, "/admin/prices", "POST", {
    model_id: "opencrow-motion-ui",
    label: "local motion fixture",
    unit: "request",
    credits: 0.25,
  });
  await api(ctx, "/admin/models/opencrow-motion-ui", "PUT", {
    ...publishedMotion.model,
    enabled: true,
  });
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await chooseOption(page, "生成类型", "video");
  await chooseOption(page, "模型", "opencrow-motion-ui");
  await page
    .getByLabel("提示词", { exact: true })
    .fill("不合格动作素材必须在扣费前拦截");
  await page
    .locator('input[type="file"]')
    .first()
    .setInputFiles([
      {
        name: "small.png",
        mimeType: "image/png",
        buffer: Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=",
          "base64",
        ),
      },
      {
        name: "motion.mp4",
        mimeType: "video/mp4",
        buffer: await readFile(
          "internal/api/testdata/opencrow-motion-valid.mp4",
        ),
      },
    ]);
  await page.getByText(/动作控制形象图宽高至少 300 像素/).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "开始生成", exact: true })
      .isDisabled(),
    true,
  );
  assert.equal((await api(ctx, "/me")).balance, 999.75);
  assert.equal(
    requests.filter(
      (r) => r.method === "POST" && r.path === "/v1/videos/generations",
    ).length,
    1,
  );
  await page.setViewportSize({ width: 390, height: 900 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await page.screenshot({
    path: join(output, "motion-rejection-mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
  check(
    "invalid motion media shows a specific error, disables generation and makes no upstream call or debit",
  );
  await page.goto(base + "/admin/providers", { waitUntil: "networkidle" });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.screenshot({
      path: join(output, `providers-${width}.png`),
      fullPage: true,
      animations: "disabled",
    });
  }
  assert.deepEqual(errors, []);
} catch (error) {
  console.error(error);
  console.error(serverOutput);
  process.exitCode = 1;
  if (page)
    await page
      .screenshot({ path: join(output, "failure.png"), fullPage: true })
      .catch(() => {});
} finally {
  await writeFile(
    join(output, "results.json"),
    JSON.stringify(
      {
        checks,
        pageErrors: errors,
        mockRequests: requests,
        realModelCalls: 0,
        realPaymentCalls: 0,
      },
      null,
      2,
    ),
  );
  await browser?.close();
  if (app && app.exitCode === null) {
    const stopped = new Promise((r) => app.once("exit", r));
    app.kill();
    await stopped;
  }
  if (mock.listening) await new Promise((r) => mock.close(r));
  if (created)
    execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
      stdio: "pipe",
      windowsHide: true,
    });
}
