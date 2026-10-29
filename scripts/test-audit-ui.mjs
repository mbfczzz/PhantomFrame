import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";

// Every fixture lives in its own disposable database. Browser requests cannot
// leave localhost, and no generation/payment actions are used by this suite.
const faultsOnly = process.argv.includes("--faults-only");
const output = resolve(
  "test-results/audit-fixes-ui" + (faultsOnly ? "/followup" : ""),
);
await mkdir(output, { recursive: true });
const database = "phantomframe_test_audit_fixes_" + Date.now();
const dataDir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4186";
const checks = [],
  matrix = [],
  violations = [],
  errors = [];
let app,
  browser,
  serverOutput = "";
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const check = (name) => {
  checks.push(name);
  console.log("PASS", name);
};
async function request(context, path, method = "GET", data) {
  const response = await context.request.fetch(base + "/api" + path, {
    method,
    data,
    headers: { "Idempotency-Key": crypto.randomUUID() },
  });
  const value = await response.json();
  assert.ok(
    response.ok(),
    `${method} ${path}: ${response.status()} ${JSON.stringify(value)}`,
  );
  return value;
}
async function context() {
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await ctx.route("**/*", (route) => {
    const url = new URL(route.request().url());
    return url.hostname === "127.0.0.1" ||
      ["data:", "blob:"].includes(url.protocol)
      ? route.continue()
      : route.abort();
  });
  await request(ctx, "/auth/login", "POST", {
    username: "audit-fixes-admin",
    password: "Audit-fixes-test-2026",
  });
  return ctx;
}
async function visit(page, path) {
  await page.goto(base + path, { waitUntil: "networkidle" });
}
try {
  execFileSync("go", ["run", "./cmd/testdb", "create", database], {
    windowsHide: true,
    stdio: "pipe",
  });
  app = spawn(
    resolve(`bin/phantomframe${process.platform === "win32" ? ".exe" : ""}`),
    [],
    {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        APP_ENV: "test",
        PORT: "4186",
        HOST: "127.0.0.1",
        APP_URL: base,
        DATA_DIR: dataDir,
        MYSQL_DATABASE: database,
        REDIS_PREFIX: database + ":",
        DEMO_MODE: "true",
        STORAGE_DRIVER: "local",
        ADMIN_USERNAME: "audit-fixes-admin",
        ADMIN_PASSWORD: "Audit-fixes-test-2026",
        ENCRYPTION_KEY: "f".repeat(64),
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
  // Legacy demo scenarios prepare explicit fixtures; production startup stays empty.
  execFileSync("go", ["run", "./cmd/testdb", "seed-demo", database], {
    stdio: "pipe",
    windowsHide: true,
  });
  browser = await launchBrowser();
  const ctx = await context();
  const user = await request(ctx, "/me");
  const child = await request(ctx, "/sub-users", "POST", {
    username: "audit-fixes-child",
    password: "Audit-child-test-2026",
  });
  const page = await ctx.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  await visit(page, "/profile");
  const firstTab = page.getByRole("tab", { name: "兑换充值", exact: true });
  await firstTab.focus();
  await page.keyboard.press("ArrowRight");
  assert.equal(
    await page.evaluate(() => document.activeElement.textContent),
    "资金流水",
  );
  assert.equal(
    await page.getByRole("tabpanel").getAttribute("aria-labelledby"),
    "profile-panel-tab-transactions",
  );
  assert.equal(await page.locator('[role="tab"][tabindex="0"]').count(), 1);
  await page.keyboard.press("End");
  assert.equal(
    await page.evaluate(() => document.activeElement.textContent),
    "我的子账户",
  );
  await page.keyboard.press("ArrowRight");
  assert.equal(
    await page.evaluate(() => document.activeElement.textContent),
    "兑换充值",
  );
  await page.keyboard.press("ArrowLeft");
  assert.equal(
    await page.evaluate(() => document.activeElement.textContent),
    "我的子账户",
  );
  check(
    "tabs support arrows, wrapping, Home/End, a single tab stop and a labelled panel",
  );

  await page
    .getByRole("row")
    .filter({ hasText: child.username })
    .getByRole("button", { name: "转账", exact: true })
    .click();
  let dialog = page.getByRole("dialog", {
    name: "转账给 " + child.username,
    exact: true,
  });
  await dialog.getByLabel("转账金额（元）").fill("5");
  let calls = 0;
  await page.route(
    "**/api/sub-users/" + child.id + "/transfer",
    async (route) => {
      calls++;
      if (calls === 1) {
        const response = await route.fetch();
        assert.equal(response.status(), 200);
        await route.abort("failed");
      } else await route.continue();
    },
  );
  await dialog.getByRole("button", { name: "确认分配", exact: true }).click();
  await page.getByText(/的结果待确认/).waitFor();
  assert.equal((await request(ctx, "/me")).balance, user.balance - 5);
  await dialog.getByLabel("转账金额（元）").fill("6");
  await dialog.getByRole("button", { name: "确认分配", exact: true }).click();
  await page
    .getByRole("alert")
    .filter({ hasText: "请先在页面顶部核对结果" })
    .waitFor();
  assert.equal(calls, 1);
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("button", { name: "核对并重试", exact: true }).click();
  await page.locator(".money-recovery").waitFor({ state: "hidden" });
  assert.equal((await request(ctx, "/me")).balance, user.balance - 5);
  assert.equal((await request(ctx, "/transactions?kind=transfer")).total, 1);
  check(
    "lost transfer responses survive changed input and reload, then reconcile without another debit",
  );

  // A second deliberate transfer with identical values is a distinct operation.
  await page.getByRole("tab", { name: "我的子账户", exact: true }).click();
  await page
    .getByRole("row")
    .filter({ hasText: child.username })
    .getByRole("button", { name: "转账", exact: true })
    .click();
  dialog = page.getByRole("dialog", {
    name: "转账给 " + child.username,
    exact: true,
  });
  await dialog.getByLabel("转账金额（元）").fill("5");
  await dialog.getByRole("button", { name: "确认分配", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  assert.equal((await request(ctx, "/me")).balance, user.balance - 10);
  check(
    "a confirmed operation allows a later intentional transfer of the same amount",
  );

  await page.unroute("**/api/sub-users/" + child.id + "/transfer");
  for (const mode of ["invalid-json", "incomplete-receipt", "before-commit"]) {
    const before = (await request(ctx, "/me")).balance;
    let attempts = 0;
    const path = "**/api/sub-users/" + child.id + "/transfer";
    await page.route(path, async (route) => {
      if (++attempts > 1) return route.continue();
      if (mode !== "before-commit")
        assert.equal((await route.fetch()).status(), 200);
      await route.fulfill({
        status: mode === "before-commit" ? 503 : 200,
        contentType: "application/json",
        body:
          mode === "invalid-json"
            ? "<html>invalid proxy response</html>"
            : "{}",
      });
    });
    await page
      .getByRole("row")
      .filter({ hasText: child.username })
      .getByRole("button", { name: "转账", exact: true })
      .click();
    dialog = page.getByRole("dialog", {
      name: "转账给 " + child.username,
      exact: true,
    });
    await dialog.getByLabel("转账金额（元）").fill("0.25");
    await dialog.getByRole("button", { name: "确认分配", exact: true }).click();
    await page.getByText(/的结果待确认/).waitFor();
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "核对并重试", exact: true }).click();
    await page.locator(".money-recovery").waitFor({ state: "hidden" });
    assert.equal((await request(ctx, "/me")).balance, before - 0.25);
    assert.equal(attempts, mode === "before-commit" ? 2 : 1);
    await page.unroute(path);
    check(mode + " reconciles the operation and debits exactly once");
  }

  await page.route("**/api/auth/logout", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ message: "simulated logout outage" }),
    }),
  );
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "退出未完成" }).waitFor();
  assert.ok(page.url().endsWith("/profile"));
  await page.unroute("**/api/auth/logout");
  check(
    "logout failure preserves the session and provides retry feedback without an unhandled exception",
  );

  const sessionCtx = await context();
  const sessionPage = await sessionCtx.newPage();
  sessionPage.on("pageerror", (e) => errors.push(e.message));
  for (const failure of ["503", "network"]) {
    await sessionPage.route("**/api/me", (route) =>
      failure === "network"
        ? route.abort("failed")
        : route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ message: "模拟会话服务故障" }),
          }),
    );
    await visit(sessionPage, "/transactions");
    assert.ok(sessionPage.url().endsWith("/transactions"));
    await sessionPage
      .getByRole("button", { name: "重新加载", exact: true })
      .waitFor();
    assert.equal(
      await sessionPage
        .getByRole("button", { name: "登录", exact: true })
        .count(),
      0,
    );
    await sessionPage.unroute("**/api/me");
    await sessionPage
      .getByRole("button", { name: "重新加载", exact: true })
      .click();
    await sessionPage.locator(".app-shell").waitFor();
  }
  await sessionPage.route("**/api/me", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ message: "请先登录" }),
    }),
  );
  await visit(sessionPage, "/transactions");
  await sessionPage.waitForURL("**/login");
  await sessionCtx.close();
  check(
    "session 503/network failures remain recoverable; only a confirmed 401 redirects to login",
  );

  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=",
    "base64",
  );
  for (let i = 0; i < 25; i++) {
    const response = await ctx.request.post(base + "/api/assets", {
      multipart: {
        file: { name: `audit-${i}.png`, mimeType: "image/png", buffer: png },
      },
    });
    assert.ok(response.ok(), await response.text());
  }
  await visit(page, "/assets");
  await page.waitForFunction(
    () => document.querySelectorAll(".asset-card").length === 24,
  );
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".asset-card").length === 1,
  );
  await page.getByTitle("删除素材", { exact: true }).click();
  await page
    .getByRole("dialog", { name: "删除素材", exact: true })
    .getByRole("button", { name: "删除素材", exact: true })
    .click();
  await page.waitForFunction(
    () => document.querySelectorAll(".asset-card").length === 24,
  );
  assert.equal(
    await page.getByText("资产库还是空的", { exact: true }).count(),
    0,
  );
  check(
    "deleting the only last-page asset returns to 24 existing assets on page one",
  );

  const routes = [
    "/login",
    "/register",
    "/generate",
    "/models",
    "/history",
    "/assets",
    "/profile",
    "/developer",
    "/transactions",
    "/redeem",
    "/tickets",
    "/docs",
    "/admin/overview",
    "/admin/models",
    "/admin/providers",
    "/admin/inventory",
    "/admin/users",
    "/admin/codes",
    "/admin/jobs",
    "/admin/upstream",
    "/admin/transactions",
    "/admin/settings",
    "/admin/audit",
  ];
  for (const theme of faultsOnly ? [] : ["dark", "light"]) {
    await page.evaluate((theme) => {
      localStorage.setItem("pf-cinematic-theme-v3", theme);
      document.documentElement.dataset.theme = theme;
    }, theme);
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
      for (const route of routes) {
        await visit(page, route);
        const layout = await page.evaluate(() => ({
          width: innerWidth,
          scrollWidth: document.documentElement.scrollWidth,
          theme: document.documentElement.dataset.theme,
        }));
        matrix.push({ theme, width, route, ...layout });
        if (width === 1440) {
          await page.addScriptTag({
            path: resolve("node_modules/axe-core/axe.min.js"),
          });
          const result = await page.evaluate(async () =>
            (
              await axe.run(document, { runOnly: ["color-contrast"] })
            ).violations.map((v) => ({
              id: v.id,
              nodes: v.nodes.map((n) => ({
                target: n.target,
                summary: n.failureSummary,
              })),
            })),
          );
          if (result.length) violations.push({ theme, route, result });
        }
        if (
          ["/login", "/profile", "/admin/users", "/assets"].includes(route) &&
          width !== 390
        )
          await page.screenshot({
            path: join(
              output,
              `${route.replaceAll("/", "_")}-${theme}-${width}.png`,
            ),
            fullPage: true,
          });
      }
      console.log("MATRIX", theme, width, routes.length, "routes");
    }
  }
  assert.deepEqual(
    matrix.filter((row) => row.scrollWidth > row.width),
    [],
    "horizontal overflow",
  );
  assert.deepEqual(violations, [], "contrast violations");
  assert.deepEqual(errors, [], "unhandled browser errors");
  if (!faultsOnly)
    check(
      `${matrix.length} route/theme/viewport checks and ${routes.length * 2} contrast scans pass`,
    );
} catch (error) {
  console.error(error);
  console.error(serverOutput);
  process.exitCode = 1;
} finally {
  await writeFile(
    join(output, "results.json"),
    JSON.stringify(
      {
        checks,
        matrix,
        violations,
        pageErrors: errors,
        realModelCalls: 0,
        realPaymentCalls: 0,
      },
      null,
      2,
    ),
  );
  await browser?.close();
  if (app && app.exitCode === null) {
    const exited = new Promise((resolve) => app.once("exit", resolve));
    app.kill();
    await exited;
  }
  execFileSync("go", ["run", "./cmd/testdb", "drop", database], {
    windowsHide: true,
    stdio: "pipe",
  });
}
