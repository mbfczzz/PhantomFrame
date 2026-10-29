import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { launchBrowser } from "./browser.mjs";

// Every fixture lives in its own disposable database. Browser requests cannot
// leave localhost, and no generation/payment actions are used by this suite.
const faultsOnly = process.argv.includes("--faults-only");
const output = resolve(
  "test-results/recheck-ui" + (faultsOnly ? "/followup" : ""),
);
await mkdir(output, { recursive: true });
const database = "phantomframe_test_recheck_" + Date.now();
const dataDir = await mkdtemp(join(output, "data-"));
const base = "http://127.0.0.1:4183";
const checks = [],
  matrix = [],
  violations = [],
  errors = [];
let releaseOldReceipt = () => {};
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
        PORT: "4183",
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

  const transferPath = `/sub-users/${child.id}/transfer`;
  const oldKey = crypto.randomUUID();
  const receipt = await ctx.request.post(base + "/api" + transferPath, {
    data: { credits: 0.11 },
    headers: { "Idempotency-Key": oldKey },
  });
  assert.equal(receipt.status(), 200);
  await visit(page, "/profile");
  const storageKey = `pf-money:${user.id}:${transferPath}`;
  await page.evaluate(
    ({ storageKey, oldKey, transferPath }) =>
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          key: oldKey,
          path: transferPath,
          body: { credits: 0.11 },
        }),
      ),
    { storageKey, oldKey, transferPath },
  );
  await page.reload({ waitUntil: "networkidle" });
  const second = await ctx.newPage();
  second.setDefaultTimeout(15000);
  second.on("pageerror", (e) => errors.push(e.message));
  await visit(second, "/profile");
  let signalReceived;
  const received = new Promise((resolve) => (signalReceived = resolve));
  const release = new Promise((resolve) => (releaseOldReceipt = resolve));
  await page.route("**/api/money-operations/" + oldKey, async (route) => {
    const response = await route.fetch();
    signalReceived();
    await release;
    await route.fulfill({ response });
  });
  await page.getByRole("button", { name: "核对并重试", exact: true }).click();
  await received;
  // The other tab confirms the old operation, then creates a distinct uncertain one.
  await second.getByRole("button", { name: "核对并重试", exact: true }).click();
  await second.locator(".money-recovery").waitFor({ state: "hidden" });
  await second.getByRole("tab", { name: "我的子账户", exact: true }).click();
  await second
    .getByRole("row")
    .filter({ hasText: child.username })
    .getByRole("button", { name: "转账", exact: true })
    .click();
  const dialog = second.getByRole("dialog", {
    name: "转账给 " + child.username,
    exact: true,
  });
  await dialog.getByLabel("转账金额（元）").fill("0.12");
  await second.route("**/api" + transferPath, async (route) => {
    assert.equal((await route.fetch()).status(), 200);
    await route.abort("failed");
  });
  await dialog.getByRole("button", { name: "确认分配", exact: true }).click();
  await second.getByText(/的结果待确认/).waitFor();
  const pending = await second.evaluate(
    (key) => JSON.parse(localStorage.getItem(key)),
    storageKey,
  );
  assert.notEqual(pending.key, oldKey);
  releaseOldReceipt();
  await page
    .getByText("资金操作已确认，请查看最新余额与流水", { exact: true })
    .waitFor();
  assert.equal(
    await second.evaluate(
      (key) => JSON.parse(localStorage.getItem(key))?.key,
      storageKey,
    ),
    pending.key,
    "old tab must not remove the newer pending operation",
  );
  await second.keyboard.press("Escape");
  await second.getByRole("button", { name: "核对并重试", exact: true }).click();
  await second.locator(".money-recovery").waitFor({ state: "hidden" });
  assert.equal((await request(ctx, "/me")).balance, user.balance - 0.23);
  check(
    "a delayed receipt in another tab preserves the newer pending transfer and its exact balance",
  );
  await second.unroute("**/api" + transferPath);
  let signalPosted;
  const posted = new Promise((resolve) => (signalPosted = resolve));
  const releaseConcurrent = new Promise(
    (resolve) => (releaseOldReceipt = resolve),
  );
  let postedCount = 0;
  await page.route("**/api" + transferPath, async (route) => {
    postedCount++;
    const response = await route.fetch();
    signalPosted();
    await releaseConcurrent;
    await route.fulfill({ response });
  });
  await second.route("**/api" + transferPath, (route) => {
    postedCount++;
    return route.continue();
  });
  for (const target of [page, second]) {
    await target.getByRole("tab", { name: "我的子账户", exact: true }).click();
    await target
      .getByRole("row")
      .filter({ hasText: child.username })
      .getByRole("button", { name: "转账", exact: true })
      .click();
    await target
      .getByRole("dialog", { name: "转账给 " + child.username, exact: true })
      .getByLabel("转账金额（元）")
      .fill("0.13");
  }
  await page.getByRole("button", { name: "确认分配", exact: true }).click();
  await posted;
  await second.getByRole("button", { name: "确认分配", exact: true }).click();
  await second.waitForTimeout(150);
  assert.equal(postedCount, 1);
  releaseOldReceipt();
  await page
    .getByRole("dialog", { name: "转账给 " + child.username, exact: true })
    .waitFor({ state: "hidden" });
  await second
    .getByRole("dialog", { name: "转账给 " + child.username, exact: true })
    .waitFor({ state: "hidden" });
  assert.equal(postedCount, 1);
  assert.equal((await request(ctx, "/me")).balance, user.balance - 0.36);
  check(
    "simultaneous same-intent submissions in two tabs share one operation and one debit",
  );
  assert.deepEqual(errors, []);
} catch (error) {
  console.error(error);
  console.error(serverOutput);
  process.exitCode = 1;
} finally {
  releaseOldReceipt();
  await writeFile(
    join(output, "results.json"),
    JSON.stringify(
      { checks, pageErrors: errors, realModelCalls: 0, realPaymentCalls: 0 },
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
