import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { Script } from "node:vm";
import { execFileSync } from "node:child_process";
import { launchBrowser } from "./browser.mjs";
const output = "test-results/open-platform";
await mkdir(output, { recursive: true });
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const browser = await launchBrowser();
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
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
  assert.equal(
    (await context.request.get(base + "/api/usage?source=invalid")).status(),
    400,
  );
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const checks = [];
  for (const [width, height] of [
    [1440, 1000],
    [390, 844],
    [320, 568],
  ]) {
    await page.setViewportSize({ width, height });
    await page.goto(base + "/developer", { waitUntil: "networkidle" });
    for (const [label, name] of [
      ["API 密钥", "keys"],
      ["快速接入", "quickstart"],
      ["调用记录", "records"],
    ]) {
      await page.getByRole("tab", { name: label, exact: true }).click();
      await page.waitForTimeout(200);
      assert.equal(
        await page.evaluate(() => document.body.scrollWidth > innerWidth),
        false,
        `${width} ${label} overflows`,
      );
      await page.screenshot({
        path: `${output}/${name}-${width}.png`,
        fullPage: true,
        animations: "disabled",
      });
      checks.push({ width, view: name, overflow: false });
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole("tab", { name: "快速接入", exact: true }).click();
  for (const [label, name] of [
    ["图片生成", "image"],
    ["视频生成", "video"],
    ["查询结果", "query"],
  ]) {
    await page.getByRole("tab", { name: label, exact: true }).click();
    await page.getByRole("tab", { name: "Node.js", exact: true }).click();
    const js = await page.locator(".platform-code").innerText();
    new Script(`(async()=>{${js}\n})()`);
    await page.getByRole("tab", { name: "Python", exact: true }).click();
    const python = await page.locator(".platform-code").innerText();
    const file = `${output}/${name}-example.py`;
    await writeFile(file, python);
    execFileSync("python", ["-m", "py_compile", file], {
      stdio: "pipe",
      windowsHide: true,
    });
    checks.push({
      example: name,
      javascriptSyntaxValid: true,
      pythonSyntaxValid: true,
    });
  }
  assert.deepEqual(errors, []);
  await writeFile(
    output + "/report.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checks, errors }));
} finally {
  await browser.close();
}
