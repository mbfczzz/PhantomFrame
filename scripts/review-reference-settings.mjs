import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { launchBrowser } from "./browser.mjs";
const base=process.env.PREVIEW_URL||"http://127.0.0.1:5173";
const output="test-results/reference-settings";
await mkdir(output,{recursive:true});
const browser=await launchBrowser();
const errors=[],checks=[];
try {
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  assert.ok((await context.request.post(base+"/api/auth/login",{data:JSON.parse(await readFile(".local-credentials.json","utf8"))})).ok());
  const original=await(await context.request.get(base+"/api/admin/models")).json();
  const model=original.find(item=>item.id==="sd2.0-933-op");
  assert.ok(model);
  const page=await context.newPage();
  page.on("pageerror",error=>errors.push(error.message));
  await page.route("**/api/admin/**",route=>{
    if(route.request().method()!=="GET") {errors.push("Unexpected configuration write");return route.abort();}
    return route.continue();
  });
  for(const theme of ["dark","light"]) {
    await page.goto(base+"/admin/models",{waitUntil:"networkidle"});
    await page.evaluate(value=>localStorage.setItem("pf-cinematic-theme-v3",value),theme);
    for(const [width,height] of [[1440,1000],[390,844],[320,568]]) {
      await page.setViewportSize({width,height});
      await page.goto(base+"/admin/models",{waitUntil:"networkidle"});
      await page.getByRole("button",{name:"编辑 "+model.name,exact:true}).click();
      const dialog=page.getByRole("dialog",{name:"编辑模型",exact:true});
      const settings=dialog.getByRole("region",{name:"参考素材配置"});
      await settings.scrollIntoViewIfNeeded();
      for(const [type,label] of [["image","图片"],["video","视频"],["audio","音频"]]) {
        assert.equal(await settings.getByRole("checkbox",{name:`支持${label}参考`,exact:true}).isChecked(),true);
        const field=model.parameters.find(item=>item.type==="assets"&&item.itemType===type);
        assert.equal(await settings.getByLabel(`${label}数量上限`,{exact:true}).inputValue(),String(field.max??""));
      }
      assert.equal(await dialog.evaluate(node=>node.scrollWidth>node.clientWidth),false);
      await page.screenshot({path:`${output}/editor-${theme}-${width}.png`,animations:"disabled"});
      await settings.getByLabel("图片数量上限",{exact:true}).fill("");
      await dialog.locator(".advanced-model-parameters > summary").click();
      const raw=JSON.parse(await dialog.getByLabel("生成参数定义（JSON）",{exact:true}).inputValue());
      assert.equal(raw.find(item=>item.type==="assets"&&item.itemType==="image").max,undefined);
      const videoField=raw.find(item=>item.type==="assets"&&item.itemType==="video");
      videoField.max=5;
      await dialog.getByLabel("生成参数定义（JSON）",{exact:true}).fill(JSON.stringify(raw,null,2));
      assert.equal(await settings.getByLabel("视频数量上限",{exact:true}).inputValue(),"5");
      await page.keyboard.press("Escape");
      await dialog.waitFor({state:"hidden"});
      await page.goto(base+"/models",{waitUntil:"networkidle"});
      const card=page.locator(".catalog-card").filter({has:page.getByRole("heading",{name:model.name,exact:true})});
      assert.match(await card.innerText(),/图片 9 张/);
      assert.match(await card.innerText(),/视频 3 个/);
      assert.match(await card.innerText(),/音频 3 段/);
      assert.equal(await page.evaluate(()=>document.body.scrollWidth>innerWidth),false);
      await page.screenshot({path:`${output}/catalog-${theme}-${width}.png`,animations:"disabled"});
      checks.push({theme,width,referenceEditorFits:true,jsonSync:true,catalogFromConfiguration:true});
    }
  }
  const after=await(await context.request.get(base+"/api/admin/models")).json();
  assert.deepEqual(after,original);
  assert.deepEqual(errors,[]);
  await writeFile(output+"/report.json",JSON.stringify({checks,errors},null,2));
  console.log(JSON.stringify({checks,errors}));
} finally {await browser.close();}
