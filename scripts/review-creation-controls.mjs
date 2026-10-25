import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const directory = process.env.REVIEW_OUTPUT || "test-results/creation-refined";
await mkdir(directory, { recursive: true });
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const browser = await launchBrowser();
const checks = [];
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
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  for (const [width, height] of [
    [1440, 900],
    [1280, 800],
    [390, 844],
    [320, 568],
  ]) {
    await page.setViewportSize({ width, height });
    await page.goto(base + "/generate?model=sd2.0-933-op", {
      waitUntil: "networkidle",
    });
    await page.getByRole("combobox", { name: "模型", exact: true }).click();
    await page.getByRole("listbox", { name: "模型", exact: true }).waitFor();
    await page.waitForTimeout(160);
    const popup = await page.locator(".ui-select-popup").boundingBox();
    assert.ok(
      popup.x >= 0 &&
        popup.x + popup.width <= width &&
        popup.y >= 0 &&
        popup.y + popup.height <= height + 1,
      "Menu must stay in viewport",
    );
    await page.screenshot({ path: `${directory}/model-menu-${width}.png` });
    await page.keyboard.press("Escape");
    await page
      .getByRole("button", { name: "添加参考内容", exact: true })
      .click();
    await page
      .getByRole("dialog", { name: "添加参考内容", exact: true })
      .waitFor();
    const dialog = await page.getByRole("dialog").boundingBox();
    assert.ok(
      dialog.x >= 0 &&
        dialog.x + dialog.width <= width &&
        dialog.y >= 0 &&
        dialog.y + dialog.height <= height + 1,
      "Upload dialog must fit screen",
    );
    await page.screenshot({
      path: `${directory}/reference-upload-${width}.png`,
      animations: "disabled",
    });
    await page
      .getByRole("button", { name: "从资产库选择", exact: false })
      .click();
    await page
      .getByRole("dialog", { name: "从资产库选择", exact: true })
      .waitFor();
    await page.getByRole("button", { name: "关闭", exact: true }).click();
    await page
      .getByRole("button", { name: "展开生成预览", exact: true })
      .click();
    await page.waitForTimeout(600);
    await page.screenshot({ path: `${directory}/preview-open-${width}.png` });
    const editor = await page.locator(".surface-composer").boundingBox();
    const settings = await page.locator(".studio-settings").boundingBox();
    const preview = await page.locator("#generation-preview").boundingBox();
    const button = await page.locator(".studio-generate").boundingBox();
    assert.ok(
      preview.y >=
        Math.max(editor.y + editor.height, settings.y + settings.height),
      "Gallery must stay below the editor and settings",
    );
    if (width < 768)
      assert.ok(
        button.y >= 0 && button.y + button.height <= height,
        "Mobile generate action remains accessible",
      );
    if (width < 1280)
      assert.ok(
        preview.y >= 0 && preview.y < height - 100,
        "Opening the gallery must reveal its heading",
      );
    assert.equal(
      await page.evaluate(() => document.body.scrollWidth > innerWidth),
      false,
    );
    await page
      .getByRole("button", { name: "关闭生成预览", exact: true })
      .click();
    assert.equal(
      await page
        .getByRole("button", { name: "展开生成预览", exact: true })
        .evaluate((el) => el === document.activeElement),
      true,
    );
    checks.push({
      width,
      height,
      menuInViewport: true,
      uploadDialogFits: true,
      previewOverlaps: false,
      mobileGenerateVisible: width < 768 ? true : undefined,
      mobilePreviewRevealed: width < 1280 ? true : undefined,
      closeRestoresFocus: true,
    });
  }
  await page.evaluate(() =>
    localStorage.setItem("pf-cinematic-theme-v3", "light"),
  );
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.reload({ waitUntil: "networkidle" });
  await page.getByRole("combobox", { name: "模型", exact: true }).click();
  await page.screenshot({ path: directory + "/model-menu-light.png" });
  assert.deepEqual(errors, []);
  await writeFile(
    directory + "/controls-report.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checks, errors }));
} finally {
  await browser.close();
}
