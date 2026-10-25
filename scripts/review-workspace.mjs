import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const output = "test-results/workspace-refinement";
const focused = process.argv.includes("--focused");
await mkdir(output, { recursive: true });
const browser = await launchBrowser();
const errors = [],
  checks = [];
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
  });
  const credentials = JSON.parse(
    await readFile(".local-credentials.json", "utf8"),
  );
  assert.ok(
    (
      await context.request.post(base + "/api/auth/login", {
        data: credentials,
      })
    ).ok(),
  );
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("dialog", async (dialog) => {
    errors.push("Unexpected native dialog");
    await dialog.dismiss();
  });
  await page.route("**/api/**", (route) => {
    const request = route.request();
    if (
      !["GET", "HEAD", "OPTIONS"].includes(request.method()) &&
      !request.url().endsWith("/pricing/quote")
    ) {
      errors.push("Unexpected mutation during visual review");
      return route.abort();
    }
    return route.continue();
  });
  const capture = async (name, width, theme) => {
    assert.equal(
      await page.evaluate(() => document.body.scrollWidth > innerWidth),
      false,
      name + " page overflow",
    );
    await page.screenshot({
      path: `${output}/${name}-${theme}-${width}.png`,
      animations: "disabled",
    });
    checks.push({ name, theme, width });
  };
  const reviewDialog = async (name, width, height, theme, focusLabel) => {
    const dialog = page.getByRole("dialog");
    await dialog.waitFor();
    await page.waitForTimeout(240);
    const bounds = await dialog.boundingBox();
    assert.ok(
      bounds.x >= 0 &&
        bounds.y >= 0 &&
        bounds.x + bounds.width <= width + 1 &&
        bounds.y + bounds.height <= height + 1,
      name + " bounds",
    );
    assert.equal(
      await dialog.evaluate((node) => node.scrollWidth > node.clientWidth),
      false,
      name + " horizontal overflow",
    );
    if (focusLabel)
      await page.waitForFunction(
        (label) =>
          document.activeElement?.getAttribute("aria-labelledby") &&
          document.getElementById(
            document.activeElement.getAttribute("aria-labelledby"),
          )?.textContent === label,
        focusLabel,
      );
    const footer = dialog.locator(".modal-footer").last();
    if (await footer.count()) {
      const footerBox = await footer.boundingBox();
      if (
        !(
          footerBox.y >= bounds.y &&
          footerBox.y + footerBox.height <= bounds.y + bounds.height + 1
        )
      ) {
        console.log(
          JSON.stringify({
            name,
            width,
            bounds,
            footerBox,
            body: await dialog.locator(".modal-body").boundingBox(),
          }),
        );
        await page.screenshot({
          path: `${output}/failure-${name}-${width}.png`,
          animations: "disabled",
        });
      }
      assert.ok(
        footerBox.y >= bounds.y &&
          footerBox.y + footerBox.height <= bounds.y + bounds.height + 1,
        name + " footer visible",
      );
    }
    await capture(name, width, theme);
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    assert.notEqual(
      await page.evaluate(() => document.body.style.overflow),
      "hidden",
    );
  };
  for (const theme of ["dark", "light"]) {
    await page.goto(base + "/history", { waitUntil: "networkidle" });
    await page.evaluate(
      (value) => localStorage.setItem("pf-cinematic-theme-v3", value),
      theme,
    );
    for (const [width, height] of [
      [1440, 900],
      [390, 844],
      [320, 568],
    ]) {
      await page.setViewportSize({ width, height });
      for (const [route, name] of [
        ["history", "history"],
        ["assets", "assets"],
        ["models", "models"],
        ["developer", "platform"],
        ["tickets", "tickets"],
        ["tickets/new", "ticket-form"],
        ["transactions", "ledger"],
        ["redeem", "redeem"],
        ["profile", "profile"],
      ]) {
        if (focused && !["models", "tickets/new"].includes(route)) continue;
        await page.goto(base + "/" + route, { waitUntil: "networkidle" });
        await capture(name, width, theme);
        if (route === "developer") {
          await page
            .getByRole("button", { name: "创建 Key", exact: true })
            .click();
          await reviewDialog("create-key", width, height, theme, "Key 名称");
        }
        if (route === "models") {
          await page
            .getByRole("button", { name: "完整参数", exact: true })
            .first()
            .click();
          await reviewDialog("model-detail", width, height, theme);
        }
        if (route === "profile") {
          await page
            .getByRole("tab", { name: "编辑资料", exact: true })
            .click();
          await capture("profile-edit", width, theme);
          await page
            .getByRole("tab", { name: "我的子账户", exact: true })
            .click();
          await page
            .getByRole("button", { name: "添加子账户", exact: true })
            .click();
          await reviewDialog("create-subuser", width, height, theme, "用户名");
        }
      }
      await page.goto(base + "/admin/models", { waitUntil: "networkidle" });
      await page.getByRole("button", { name: /^编辑 / }).first().click();
      await reviewDialog("model-form", width, height, theme, "显示名称");
      await page
        .getByRole("button", { name: /^删除 / })
        .first()
        .click();
      await page.waitForFunction(
        () => document.activeElement?.textContent === "取消",
      );
      await reviewDialog("delete-confirm", width, height, theme);
      await page.goto(
        base + "/history?search=no-matching-record-visual-review",
        { waitUntil: "networkidle" },
      );
      await page.getByRole("button", { name: "清空筛选", exact: true }).click();
      assert.equal(
        await page.getByPlaceholder("搜索提示词、模型或任务 ID").inputValue(),
        "",
      );
    }
  }
  assert.deepEqual(errors, []);
  await writeFile(
    `${output}/${focused ? "focused-report" : "report"}.json`,
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ reviewed: checks.length, errors }));
} finally {
  await browser.close();
}
