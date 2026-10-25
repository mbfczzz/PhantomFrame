import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
import { chooseOption } from "./ui-controls.mjs";
const output = "test-results/independent-layout";
await mkdir(output, { recursive: true });
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const browser = await launchBrowser();
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
  const checks = [];
  const moduleNames = [
    "创作",
    "生成记录",
    "模型与价格",
    "资产库",
    "开放平台",
    "提交工单",
    "资金流水",
    "兑换充值",
  ];
  for (const [width, height] of [
    [1440, 900],
    [1024, 768],
    [768, 1024],
    [390, 844],
    [320, 568],
  ]) {
    await page.setViewportSize({ width, height });
    await page.goto(base + "/generate?model=sd2.0-933-op", {
      waitUntil: "networkidle",
    });
    assert.equal(await page.locator(".sidebar").count(), 0);
    assert.equal(
      await page.evaluate(() => document.body.scrollWidth > innerWidth),
      false,
    );
    const masthead = await page.locator(".app-masthead").boundingBox();
    const title = await page.locator(".creation-heading").boundingBox();
    assert.ok(
      title.y >= masthead.y + masthead.height,
      "Heading must clear the sticky navigation",
    );
    if (width >= 768) {
      const editor = await page.locator(".surface-composer").boundingBox();
      const settings = await page.locator(".studio-settings").boundingBox();
      assert.ok(
        editor.x + editor.width < settings.x,
        "Settings must be beside the content editor",
      );
      const nav = await page.locator(".primary-nav").boundingBox();
      assert.ok(nav.y < masthead.height);
    } else {
      const toggle = await page
        .locator(".settings-mobile-toggle")
        .boundingBox();
      const footer = await page.locator(".surface-footer").boundingBox();
      if (toggle.y + toggle.height > footer.y + 1) {
        await page.screenshot({path:`${output}/parameter-fit-${width}.png`,animations:'disabled'});
        console.log(JSON.stringify({width,height,toggle,footer}));
      }
      assert.ok(
        toggle.y + toggle.height <= footer.y + 1,
        "Parameter toggle must be above the fixed submit bar",
      );
      await page.locator(".settings-mobile-toggle").click();
      await chooseOption(page, "分辨率", "480p");
      await page.waitForFunction(
        () =>
          parseFloat(
            document.querySelector(".studio-price strong").textContent.replace(/[^0-9.]/g,""),
          ) === 30,
      );
      await chooseOption(page, "分辨率", "720p");
      await page.locator(".settings-mobile-toggle").click();
      await page.getByRole("button", { name: "打开导航", exact: true }).click();
      assert.deepEqual(
        await page.locator(".workspace-menu nav a").allTextContents(),
        moduleNames,
      );
      await page.screenshot({
        path: `${output}/navigation-menu-${width}.png`,
        animations: "disabled",
      });
      await page.keyboard.press("Escape");
      await page.locator(".workspace-menu").waitFor({ state: "detached" });
      assert.equal(
        await page
          .getByRole("button", { name: "打开导航", exact: true })
          .evaluate((element) => element === document.activeElement),
        true,
      );
    }
    for (const [path, name] of [
      ["models", "models"],
      ["developer", "platform"],
      ["transactions", "ledger"],
      ["tickets", "tickets"],
    ]) {
      await page.goto(base + "/" + path, { waitUntil: "networkidle" });
      assert.equal(
        await page.evaluate(() => document.body.scrollWidth > innerWidth),
        false,
        `${path} must fit ${width}px`,
      );
      await page.screenshot({
        path: `${output}/${name}-${width}.png`,
        animations: "disabled",
      });
    }
    checks.push({
      width,
      height,
      horizontalNavigation: true,
      overflow: false,
      mobileSettingsReachable: width < 768 ? true : undefined,
    });
  }
  await page.evaluate(() =>
    localStorage.setItem("pf-cinematic-theme-v3", "light"),
  );
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(base + "/generate", { waitUntil: "networkidle" });
  await page.screenshot({
    path: output + "/workspace-light.png",
    animations: "disabled",
  });
  assert.deepEqual(errors, []);
  await writeFile(
    output + "/layout-report.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checks, errors }));
} finally {
  await browser.close();
}
