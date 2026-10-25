import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const base = process.env.PREVIEW_URL || "http://127.0.0.1:5173";
const output = "test-results/model-pricing";
await mkdir(output, {recursive:true});
const browser = await launchBrowser();
const checks = [], errors = [];
try {
  const context = await browser.newContext({viewport:{width:1440,height:1000}});
  assert.ok((await context.request.post(base+"/api/auth/login",{data:JSON.parse(await readFile(".local-credentials.json","utf8"))})).ok());
  const models = await (await context.request.get(base+"/api/admin/models")).json();
  const model = models.find(item=>item.id==="sd2.0-933-op") || models[0];
  assert.ok(model?.prices?.length);
  const page = await context.newPage();
  page.on("pageerror",error=>errors.push(error.message));
  await page.route("**/api/admin/**",route=>{
    if(route.request().method()!=="GET") { errors.push("Unexpected write during read-only review"); return route.abort(); }
    return route.continue();
  });
  for(const theme of ["dark","light"]) {
    await page.goto(base+"/admin/models",{waitUntil:"networkidle"});
    await page.evaluate(value=>localStorage.setItem("pf-cinematic-theme-v3",value),theme);
    for(const [width,height] of [[1440,1000],[390,844],[320,568]]) {
      await page.setViewportSize({width,height});
      await page.goto(base+"/admin/prices",{waitUntil:"networkidle"});
      await page.waitForURL("**/admin/models");
      assert.equal(await page.getByRole("link",{name:"价格管理",exact:true}).count(),0);
      assert.equal(await page.evaluate(()=>document.body.scrollWidth>innerWidth),false);
      await page.getByRole("button",{name:"配置价格 "+model.name,exact:true}).click();
      const parent=page.getByRole("dialog",{name:"编辑模型",exact:true});
      await parent.getByRole("button",{name:"新增价格规则",exact:true}).waitFor();
      assert.equal(await parent.getByRole("tab",{name:"计费规则",exact:true}).getAttribute("aria-selected"),"true");
      assert.equal(await parent.locator(".model-pricing-section tbody tr").count(),model.prices.length);
      await page.waitForTimeout(240);
      const bounds=await parent.boundingBox();
      assert.ok(bounds.x>=0&&bounds.y>=0&&bounds.x+bounds.width<=width+1&&bounds.y+bounds.height<=height+1);
      await page.screenshot({path:`${output}/rules-${theme}-${width}.png`,animations:"disabled"});
      await parent.getByRole("button",{name:"新增价格规则",exact:true}).click();
      const child=page.getByRole("dialog",{name:"新增价格规则",exact:true});
      await child.waitFor();
      assert.equal(await child.getByLabel("模型",{exact:true}).inputValue(),model.name);
      assert.equal(await child.getByLabel("模型",{exact:true}).isDisabled(),true);
      await page.waitForTimeout(240);
      const childBounds=await child.boundingBox();
      const footer=await child.locator(".modal-footer").boundingBox();
      assert.ok(childBounds.y>=0&&childBounds.y+childBounds.height<=height+1);
      assert.ok(footer.y>=childBounds.y&&footer.y+footer.height<=childBounds.y+childBounds.height+1);
      await page.screenshot({path:`${output}/new-rule-${theme}-${width}.png`,animations:"disabled"});
      await page.keyboard.press("Escape");
      await child.waitFor({state:"hidden"});
      await parent.waitFor();
      assert.equal(await page.evaluate(()=>document.body.style.overflow),"hidden");
      await parent.getByRole("tab",{name:"基本信息",exact:true}).click();
      assert.equal(await parent.getByLabel("模型 ID",{exact:true}).inputValue(),model.id);
      await parent.getByRole("tab",{name:"计费规则",exact:true}).click();
      await parent.getByRole("button",{name:"完成",exact:true}).click();
      await parent.waitFor({state:"hidden"});
      assert.notEqual(await page.evaluate(()=>document.body.style.overflow),"hidden");
      checks.push({theme,width,scopedPrices:model.prices.length,lockedModel:true,dialogFits:true,legacyRouteRedirects:true});
    }
  }
  assert.deepEqual(errors,[]);
  await writeFile(output+"/report.json",JSON.stringify({checks,errors},null,2));
  console.log(JSON.stringify({checks,errors}));
} finally { await browser.close(); }
