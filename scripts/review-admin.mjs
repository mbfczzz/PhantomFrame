import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const output = "test-results/admin-management";
await mkdir(output, { recursive: true });
const browser = await launchBrowser();
const errors = [], checks = [];
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const login = await context.request.post(base + "/api/auth/login", { data: JSON.parse(await readFile(".local-credentials.json", "utf8")) });
  assert.ok(login.ok());
  assert.equal((await login.json()).role, "admin");
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.on("pageerror", error => errors.push(error.message));
  for (const [width, height] of [[1440,1000],[1024,768],[768,1024],[390,844],[320,568]]) {
    await page.setViewportSize({width,height});
    await page.goto(base + "/generate", {waitUntil:"networkidle"});
    if (width>=768) await page.locator(".topbar-admin").click();
    else {
      await page.getByRole("button", {name:"打开导航",exact:true}).click();
      await page.locator(".workspace-menu").getByRole("link",{name:"管理后台",exact:true}).click();
    }
    await page.getByRole("heading",{name:"管理后台",exact:true}).waitFor();
    for (const route of ["overview","models","providers","users","codes","jobs","transactions","audit","settings","tickets"]) {
      await page.goto(base + "/admin/" + route,{waitUntil:"networkidle"});
      assert.equal(await page.evaluate(()=>document.body.scrollWidth>innerWidth),false,route+" overflows at "+width);
      assert.equal(await page.getByRole("heading",{name:"加载失败",exact:true}).count(),0,route+" failed to load");
      await page.screenshot({path:`${output}/${route}-dark-${width}.png`,animations:"disabled"});
      checks.push({route,width,theme:"dark"});
    }
    await page.goto(base+"/admin/codes",{waitUntil:"networkidle"});
    await page.getByRole("button",{name:"生成兑换码",exact:true}).click();
    await page.waitForTimeout(240);
    const dialog=await page.getByRole("dialog").boundingBox();
    assert.ok(dialog.y>=0&&dialog.y+dialog.height<=height+1);
    await page.screenshot({path:`${output}/generate-codes-${width}.png`,animations:"disabled"});
    await page.keyboard.press("Escape");
  }
  await page.evaluate(()=>localStorage.setItem("pf-cinematic-theme-v3","light"));
  await page.setViewportSize({width:390,height:844});
  for (const route of ["jobs","codes","transactions"]) {
    await page.goto(base+"/admin/"+route,{waitUntil:"networkidle"});
    assert.equal(await page.evaluate(()=>document.body.scrollWidth>innerWidth),false);
    await page.screenshot({path:`${output}/${route}-light-390.png`,animations:"disabled"});
    checks.push({route,width:390,theme:"light"});
  }
  assert.deepEqual(errors,[]);
  await writeFile(output+"/report.json",JSON.stringify({checks,errors},null,2));
  console.log(JSON.stringify({pages:checks.length,errors}));
} finally { await browser.close(); }
