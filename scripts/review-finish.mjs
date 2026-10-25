import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";

const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const output = process.env.REVIEW_OUTPUT || "test-results/visual-finish";
await mkdir(output, { recursive: true });
const browser = await launchBrowser();
const checks = [];
const errors = [];
function luminance(hex) {
  const raw = hex.replace("#", "");
  const normalized =
    raw.length === 3 ? [...raw].map((value) => value + value).join("") : raw;
  const rgb = normalized.match(/.{2}/g).map((value) => {
    const channel = parseInt(value, 16) / 255;
    return channel <= 0.04045
      ? channel / 12.92
      : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
}
function contrast(a, b) {
  const values = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (values[0] + 0.05) / (values[1] + 0.05);
}
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
  page.on("pageerror", (error) => errors.push(error.message));
  for (const theme of ["dark", "light"]) {
    await page.goto(base);
    await page.evaluate(
      (value) => localStorage.setItem("pf-cinematic-theme-v3", value),
      theme,
    );
    await page.goto(base + "/generate?model=sd2.0-933-op", {
      waitUntil: "networkidle",
    });
    const palette = await page.evaluate(() => {
      const style = getComputedStyle(document.documentElement);
      return Object.fromEntries(
        [
          "text",
          "muted",
          "panel",
          "panel-light",
          "input",
          "on-action",
          "action",
          "action-end",
          "action-hover",
        ].map((key) => [key, style.getPropertyValue("--" + key).trim()]),
      );
    });
    for (const [foreground, background] of [
      ["muted", "panel-light"],
      ["muted", "input"],
      ["text", "panel"],
      ["on-action", "action"],
      ["on-action", "action-end"],
      ["on-action", "action-hover"],
    ]) {
      const ratio = contrast(palette[foreground], palette[background]);
      assert.ok(
        ratio >= 4.5,
        `${theme}: ${foreground} on ${background} is ${ratio.toFixed(2)}`,
      );
      checks.push({
        theme,
        foreground,
        background,
        contrast: +ratio.toFixed(2),
      });
    }
    const composer = page.locator(".surface-composer");
    const bounds = await composer.boundingBox();
    await page.mouse.move(bounds.x + 130, bounds.y + 10);
    await page.waitForFunction(
      () =>
        document.querySelector(".surface-composer").dataset.illuminated ===
        "true",
    );
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${output}/reflection-${theme}.png` });
    await page.mouse.move(1, 1);
    assert.equal(await composer.getAttribute("data-illuminated"), null);
    await page
      .getByRole("textbox", { name: "提示词", exact: true })
      .fill("清晨薄雾中的山间美术馆，玻璃外墙映着松林，柔和自然光，建筑摄影。");
    await page.waitForFunction(
      () => !document.querySelector(".studio-generate").disabled,
    );
    await page.locator(".studio-generate").hover();
    assert.equal(
      await page
        .locator(".studio-generate")
        .evaluate((node) => getComputedStyle(node, "::after").animationName),
      "action-reflection",
    );
    await page.screenshot({
      path: `${output}/workspace-${theme}.png`,
      animations: "disabled",
    });
    await page.getByRole("combobox", { name: "模型", exact: true }).click();
    const popup = await page.locator(".ui-select-popup").boundingBox();
    assert.ok(popup.x >= 0 && popup.y >= 0 && popup.y + popup.height <= 900);
    await page.screenshot({
      path: `${output}/model-picker-${theme}.png`,
      animations: "disabled",
    });
    await page.keyboard.press("Escape");
    await page
      .getByRole("button", { name: "添加参考内容", exact: true })
      .click();
    await page.getByRole("dialog").waitFor();
    await page.screenshot({
      path: `${output}/upload-${theme}.png`,
      animations: "disabled",
    });
    await page.keyboard.press("Escape");
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        ),
    );
    await page.mouse.move(bounds.x + 100, bounds.y + 12);
    assert.equal(await composer.getAttribute("data-illuminated"), null);
    assert.equal(
      await composer.evaluate(
        (node) => getComputedStyle(node, "::before").display,
      ),
      "none",
    );
    await page.locator(".studio-generate").hover();
    assert.equal(
      await page
        .locator(".studio-generate")
        .evaluate((node) => getComputedStyle(node, "::after").display),
      "none",
    );
    await page.emulateMedia({ reducedMotion: "no-preference" });
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        ),
    );
    await page.mouse.move(bounds.x + 100, bounds.y + 12);
    await page.waitForFunction(
      () =>
        document.querySelector(".surface-composer").dataset.illuminated ===
        "true",
    );
    await page
      .getByRole("link", { name: "模型与价格", exact: true })
      .first()
      .click();
    await page.locator(".catalog-card").first().waitFor();
    await page.screenshot({
      path: `${output}/catalog-${theme}.png`,
      animations: "disabled",
    });
    await page.goto(base + "/developer", { waitUntil: "networkidle" });
    await page.screenshot({
      path: `${output}/platform-${theme}.png`,
      animations: "disabled",
    });
    checks.push({
      theme,
      pointerReflection: true,
      clearsOnLeave: true,
      reducedMotion: true,
      menus: true,
    });
  }
  const touch = await browser.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
  });
  assert.ok(
    (
      await touch.request.post(base + "/api/auth/login", { data: credentials })
    ).ok(),
  );
  const mobile = await touch.newPage();
  mobile.on("pageerror", (error) => errors.push(error.message));
  for (const theme of ["dark", "light"]) {
    await mobile.goto(base);
    await mobile.evaluate(
      (value) => localStorage.setItem("pf-cinematic-theme-v3", value),
      theme,
    );
    for (const [width, height] of [
      [390, 844],
      [320, 568],
    ]) {
      await mobile.setViewportSize({ width, height });
      await mobile.goto(base + "/generate?model=sd2.0-933-op", {
        waitUntil: "networkidle",
      });
      await mobile
        .getByRole("textbox", { name: "提示词", exact: true })
        .fill("清晨薄雾中的山间美术馆，柔和自然光。");
      await mobile.locator(".creation-heading").tap();
      await mobile.mouse.move(120, 180);
      assert.equal(await mobile.locator("[data-illuminated]").count(), 0);
      const toggle = await mobile
        .locator(".settings-mobile-toggle")
        .boundingBox();
      const footer = await mobile.locator(".surface-footer").boundingBox();
      assert.ok(toggle.y + toggle.height <= footer.y + 1);
      assert.equal(
        await mobile.evaluate(() => document.body.scrollWidth > innerWidth),
        false,
      );
      await mobile.screenshot({
        path: `${output}/mobile-${theme}-${width}.png`,
        animations: "disabled",
      });
      checks.push({
        theme,
        width,
        touchReflectionDisabled: true,
        parameterToggleReachable: true,
        overflow: false,
      });
    }
  }
  const anonymous = await browser.newContext({
    viewport: { width: 1440, height: 900 },
  });
  const login = await anonymous.newPage();
  for (const theme of ["dark", "light"]) {
    await login.goto(base + "/login", { waitUntil: "networkidle" });
    await login.evaluate(
      (value) => localStorage.setItem("pf-cinematic-theme-v3", value),
      theme,
    );
    await login.reload({ waitUntil: "networkidle" });
    assert.match(
      await login
        .locator(".auth-submit")
        .evaluate((node) => getComputedStyle(node).backgroundImage),
      /linear-gradient/,
    );
    await login.screenshot({
      path: `${output}/login-${theme}.png`,
      animations: "disabled",
    });
  }
  assert.deepEqual(errors, []);
  await writeFile(
    `${output}/finish-report.json`,
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checks, errors }));
} finally {
  await browser.close();
}
