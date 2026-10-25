import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const output = "test-results/account-services";
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
  const ledger = await context.request.get(base + "/api/transactions?limit=1");
  assert.equal(ledger.status(), 200);
  assert.equal((await ledger.json()).summary.unit, "CNY");
  assert.equal(
    (await context.request.get(base + "/api/tickets")).status(),
    200,
  );
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const checks = [];
  for (const theme of ["dark", "light"]) {
    await page.goto(base + "/tickets", { waitUntil: "networkidle" });
    await page.evaluate(
      (value) => localStorage.setItem("pf-cinematic-theme-v3", value),
      theme,
    );
    for (const [width, height] of [
      [1440, 1000],
      [390, 844],
      [320, 568],
    ]) {
      await page.setViewportSize({ width, height });
      for (const [path, name] of [
        ["tickets", "tickets"],
        ["tickets/new?category=billing", "ticket-create"],
        ["transactions", "ledger"],
        ["redeem", "redeem"],
        ["admin/tickets", "admin-tickets"],
      ]) {
        await page.goto(base + "/" + path, { waitUntil: "networkidle" });
        assert.equal(
          await page.evaluate(() => document.body.scrollWidth > innerWidth),
          false,
          `${theme} ${width} ${name} overflows`,
        );
        await page.screenshot({
          path: `${output}/${name}-${theme}-${width}.png`,
          fullPage: true,
          animations: "disabled",
        });
        checks.push({ page: name, theme, width, overflow: false });
      }
    }
  }
  assert.deepEqual(errors, []);
  await writeFile(
    output + "/report.json",
    JSON.stringify({ checks, errors }, null, 2),
  );
  console.log(JSON.stringify({ checked: checks.length, errors }));
} finally {
  await browser.close();
}
