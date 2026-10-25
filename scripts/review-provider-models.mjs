import assert from "node:assert/strict";
import {readFile,mkdir,writeFile} from "node:fs/promises";
import {launchBrowser} from "./browser.mjs";
const base=process.env.PREVIEW_URL||"http://127.0.0.1:5173";
const output="test-results/provider-models";
await mkdir(output,{recursive:true});
const browser=await launchBrowser();
const checks=[],errors=[];
try {
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 assert.ok((await context.request.post(base+"/api/auth/login",{data:JSON.parse(await readFile(".local-credentials.json","utf8"))})).ok());
 const providers=await(await context.request.get(base+"/api/admin/providers")).json();
 const provider=providers.find(item=>item.protocol==="demo")||providers[0];
 const before=await(await context.request.get(base+"/api/admin/models")).json();
 const page=await context.newPage();page.on("pageerror",error=>errors.push(error.message));
 await page.route("**/api/admin/**",route=>{if(route.request().method()!=="GET"){errors.push("Unexpected write during visual review");return route.abort();}return route.continue();});
 for(const theme of ["dark","light"]) {
  await page.goto(base+"/admin/providers",{waitUntil:"networkidle"});
  await page.evaluate(value=>localStorage.setItem("pf-cinematic-theme-v3",value),theme);
  for(const [width,height] of [[1440,1000],[390,844],[320,568]]) {
   await page.setViewportSize({width,height});
   await page.goto(base+"/admin/providers",{waitUntil:"networkidle"});
   assert.equal(await page.evaluate(()=>document.body.scrollWidth>innerWidth),false);
   await page.screenshot({path:`${output}/channels-${theme}-${width}.png`,animations:"disabled"});
   await page.getByRole("button",{name:"管理模型 "+provider.name,exact:true}).click();
   const catalog=page.getByRole("dialog",{name:"渠道模型",exact:true});
   await catalog.getByRole("button",{name:"添加支持模型",exact:true}).waitFor();
   await page.waitForTimeout(240);
   assert.equal(await catalog.evaluate(node=>node.scrollWidth>node.clientWidth),false);
   const bounds=await catalog.boundingBox();assert.ok(bounds.y>=0&&bounds.y+bounds.height<=height+1);
   await page.screenshot({path:`${output}/catalog-${theme}-${width}.png`,animations:"disabled"});
   await catalog.getByRole("button",{name:"添加支持模型",exact:true}).click();
   const form=page.getByRole("dialog",{name:"添加支持模型",exact:true});
   await form.getByLabel("上游模型 ID",{exact:true}).waitFor();
   await page.waitForTimeout(240);
   const formBounds=await form.boundingBox();const footer=await form.locator(".modal-footer").boundingBox();
   assert.ok(formBounds.y>=0&&formBounds.y+formBounds.height<=height+1);
   assert.ok(footer.y+footer.height<=formBounds.y+formBounds.height+1);
   assert.equal(await form.getByLabel("图片数量上限",{exact:true}).inputValue(),"");
   await page.screenshot({path:`${output}/new-model-${theme}-${width}.png`,animations:"disabled"});
   await page.keyboard.press("Escape");await form.waitFor({state:"hidden"});await catalog.waitFor();
   if(await catalog.getByRole("link",{name:"前往模型管理",exact:true}).count()) {
    await catalog.getByRole("link",{name:"前往模型管理",exact:true}).first().click();
    await page.waitForURL(/\/admin\/models\?model=/);
    await page.getByRole("dialog",{name:"编辑模型",exact:true}).waitFor();
    assert.equal(await page.getByRole("tab",{name:"计费规则",exact:true}).getAttribute("aria-selected"),"true");
    await page.keyboard.press("Escape");
   } else {await page.keyboard.press("Escape");}
   checks.push({theme,width,catalogFits:true,draftFormFits:true});
  }
 }
 assert.deepEqual(await(await context.request.get(base+"/api/admin/models")).json(),before);
 assert.deepEqual(errors,[]);
 await writeFile(output+"/report.json",JSON.stringify({checks,errors},null,2));console.log(JSON.stringify({checks,errors}));
} finally {await browser.close();}
