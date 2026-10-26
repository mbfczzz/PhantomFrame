import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";

const output = resolve("test-results/upstream-ui");
await mkdir(output, { recursive: true });
const database = "phantomframe_test_upstream_ui_" + Date.now();
const dataDir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4184";
const fixture = await readFile("server/demo-video.webm");
const requests = [],
  errors = [],
  checks = [];
let app,
  browser,
  serverOutput = "",
  cookie = "";
const mock = createServer(async (req, res) => {
  for await (const _ of req) {
    /* Consume multipart input without sending it elsewhere. */
  }
  requests.push({ method: req.method, path: req.url });
  res.setHeader("X-Request-Id", "mock-http-" + requests.length);
  if (req.url.startsWith("/b/")) {
    res.writeHead(401, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({ error: "simulated provider authorization failure" }),
    );
  } else if (req.url.endsWith("/content")) {
    res.writeHead(200, { "Content-Type": "video/webm" });
    res.end(fixture);
  } else {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({
        id: "task_mock_video_very_long_id_0123456789abcdef",
        status: req.method === "POST" ? "queued" : "completed",
      }),
    );
  }
});
const delay = (ms) => new Promise((r) => setTimeout(r, ms));
async function api(path, method = "GET", data) {
  const response = await fetch(base + "/api" + path, {
    method,
    headers: {
      Cookie: cookie,
      "Content-Type": "application/json",
      "Idempotency-Key": crypto.randomUUID(),
    },
    body: data === undefined ? undefined : JSON.stringify(data),
  });
  if (response.headers.get("set-cookie"))
    cookie = response.headers.get("set-cookie").split(";")[0];
  const value = await response.json();
  assert.ok(
    response.ok,
    `${path}: ${response.status} ${JSON.stringify(value)}`,
  );
  return value;
}
try {
  execFileSync("go", ["run", "./cmd/testdb", "create", database], {
    windowsHide: true,
    stdio: "pipe",
  });
  await new Promise((r) => mock.listen(4185, "127.0.0.1", r));
  app = spawn(
    resolve(`bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`),
    [],
    {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        APP_ENV: "test",
        PORT: "4184",
        HOST: "127.0.0.1",
        APP_URL: base,
        DATA_DIR: dataDir,
        MYSQL_DATABASE: database,
        REDIS_PREFIX: database + ":",
        DEMO_MODE: "false",
        STORAGE_DRIVER: "local",
        ADMIN_USERNAME: "upstream-ui-admin",
        ADMIN_PASSWORD: "Upstream-UI-test-2026",
        ENCRYPTION_KEY: "e".repeat(64),
        COOKIE_SECURE: "false",
      },
    },
  );
  app.stdout.on("data", (d) => (serverOutput += d));
  app.stderr.on("data", (d) => (serverOutput += d));
  let ready = false;
  for (let n = 0; n < 120; n++) {
    try {
      if ((await fetch(base + "/api/health")).ok) {
        ready = true;
        break;
      }
    } catch {}
    await delay(150);
  }
  assert.ok(ready, serverOutput);
  const owner = await api("/auth/login", "POST", {
    username: "upstream-ui-admin",
    password: "Upstream-UI-test-2026",
  });
  await api(`/admin/users/${owner.id}/credits`, "POST", {
    amount: 100,
    reason: "本地模拟日志测试",
  });
  const keys = ["only-a-local-test-key-a", "only-a-local-test-key-b"];
  const providers = [];
  for (const [index, part] of ["a", "b"].entries()) {
    const provider = await api("/admin/providers", "POST", {
      name: index ? "模拟渠道 B · 失败" : "模拟渠道 A · 视频",
      protocol: "openai-video",
      base_url: `http://127.0.0.1:4185/${part}`,
      apiKey: keys[index],
      config: { pollInterval: 20 },
    });
    providers.push(provider);
    await api("/admin/models", "POST", {
      id: "mock-video-" + part,
      name: index ? "模拟视频 · HTTP 401" : "模拟视频 · 成功",
      type: "video",
      provider_id: provider.id,
      upstream_model: "video-mock-" + part,
      parameters: [
        {
          name: "duration",
          label: "生成时长",
          type: "number",
          default: 8,
          min: 1,
          max: 30,
        },
        {
          name: "resolution",
          label: "分辨率",
          type: "select",
          default: "720p",
          options: ["720p"],
        },
        {
          name: "api_key",
          label: "脱敏测试字段",
          type: "text",
          default: keys[index],
        },
      ],
    });
    await api("/admin/prices", "POST", {
      model_id: "mock-video-" + part,
      label: "按次",
      unit: "request",
      credits: 2.5,
    });
  }
  const success = await api("/jobs", "POST", {
    modelId: "mock-video-a",
    prompt: "Only a localhost mock response, not a real model call.",
  });
  const failed = await api("/jobs", "POST", {
    modelId: "mock-video-b",
    prompt: "Only a simulated HTTP failure.",
  });
  for (const [job, status] of [
    [success, "succeeded"],
    [failed, "failed"],
  ]) {
    let current;
    for (let n = 0; n < 160; n++) {
      current = await api("/jobs/" + job.id);
      if (current.status === status) break;
      await delay(80);
    }
    assert.equal(current.status, status);
  }
  const logs = await api("/admin/upstream-calls");
  assert.equal(logs.total, 2);
  assert.ok(keys.every((key) => !JSON.stringify(logs).includes(key)));
  checks.push(
    "two localhost channels produce independent successful/failed call logs without credentials",
  );
  browser = await launchBrowser();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    permissions: ["clipboard-read", "clipboard-write"],
  });
  await context.route("**/*", (route) =>
    new URL(route.request().url()).hostname === "127.0.0.1"
      ? route.continue()
      : route.abort(),
  );
  await context.request.post(base + "/api/auth/login", {
    data: { username: "upstream-ui-admin", password: "Upstream-UI-test-2026" },
  });
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/admin/upstream", { waitUntil: "networkidle" });
  await page
    .getByRole("button", { name: "查看上游调用 " + success.id })
    .waitFor();
  assert.equal(await page.locator(".upstream-log-table tbody tr").count(), 2);
  await page.getByText("HTTP 401", { exact: true }).waitFor();
  await page.screenshot({
    path: join(output, "channels-desktop-dark.png"),
    fullPage: true,
  });
  await chooseOption(page, "筛选渠道", providers[0].id);
  await page.waitForFunction(
    () =>
      document.querySelectorAll(".upstream-log-table tbody tr").length === 1,
  );
  await page
    .getByRole("button", { name: "查看上游调用 " + success.id })
    .click();
  let detail = page.getByRole("dialog", { name: "上游调用详情", exact: true });
  await detail
    .getByRole("button", { name: /复制上游任务 ID task_mock_video/ })
    .waitFor();
  await detail
    .getByRole("button", {
      name: "复制上游任务 ID task_mock_video_very_long_id_0123456789abcdef",
    })
    .click();
  assert.equal(
    await page.evaluate(() => navigator.clipboard.readText()),
    "task_mock_video_very_long_id_0123456789abcdef",
  );
  await detail.getByText("提交参数与本站任务 ID", { exact: true }).click();
  assert.match(await detail.locator("pre").innerText(), /已隐藏/);
  const parameterText = await detail.locator("pre").innerText();
  assert.ok(keys.every((key) => !parameterText.includes(key)));
  await page.screenshot({
    path: join(output, "success-detail-dark.png"),
    fullPage: true,
  });
  await page.keyboard.press("Escape");
  await chooseOption(page, "筛选渠道", providers[1].id);
  await page.waitForFunction(
    () =>
      document.querySelectorAll(".upstream-log-table tbody tr").length === 1,
  );
  await page.getByRole("button", { name: "查看上游调用 " + failed.id }).click();
  detail = page.getByRole("dialog", { name: "上游调用详情", exact: true });
  await detail.getByText("401", { exact: true }).waitFor();
  await detail.getByText(/费用已退回/).waitFor();
  await detail.getByText("提交上游", { exact: true }).waitFor();
  await page.screenshot({
    path: join(output, "failure-detail-dark.png"),
    fullPage: true,
  });
  checks.push(
    "channel filters, upstream task copying, sanitized parameters, HTTP status and refund stage are usable",
  );
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "重置筛选", exact: true }).click();
  await api("/jobs/" + success.id, "DELETE");
  await page.reload({ waitUntil: "networkidle" });
  await page
    .getByRole("button", { name: "查看上游调用 " + success.id })
    .click();
  await page
    .getByRole("button", { name: "查看生成任务", exact: true })
    .waitFor();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "浅色主题", exact: true }).click();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: width === 1440 ? 1000 : 844 });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.screenshot({
      path: join(output, `channels-light-${width}.png`),
      fullPage: true,
    });
  }
  checks.push(
    "logs remain after task deletion and fit desktop/mobile light theme",
  );
  assert.deepEqual(errors, []);
  assert.equal(requests.filter((r) => r.method === "POST").length, 2);
  await writeFile(
    join(output, "results.json"),
    JSON.stringify(
      { checks, requests, pageErrors: errors, realModelCalls: 0 },
      null,
      2,
    ),
  );
  console.log(
    "Upstream multi-channel UI checks passed:",
    checks.length,
    "; mock HTTP requests:",
    requests.length,
  );
} catch (error) {
  console.error(error);
  console.error(serverOutput);
  process.exitCode = 1;
} finally {
  await browser?.close();
  if (app && app.exitCode === null) {
    const exited = new Promise((r) => app.once("exit", r));
    app.kill();
    await exited;
  }
  await new Promise((r) => mock.close(r));
  execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
    windowsHide: true,
    stdio: "pipe",
  });
}
