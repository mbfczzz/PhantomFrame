import {mkdir,writeFile} from "node:fs/promises";
import {launchBrowser} from "./browser.mjs";
const root="test-results/suppliers/docs";
await mkdir(root,{recursive:true});
const browser=await launchBrowser();
try{
 const urls=[
  ["catcoder-docs","https://video.catcode.top/docs"],
  ["openvideo-docs","https://openaivideo.top/docs"],
  ["openvideo-openapi","https://openaivideo.top/openapi.json"],
  ["modelhub-spec","https://modelhub.cc/llms.txt"],
  ["gway-docs","https://gway.dev/docs"],
  ["wxart-docs","https://api.wxart.space/docs"],
 ];
 console.log(JSON.stringify(await Promise.all(urls.map(async([name,url])=>{const page=await browser.newPage();try{const response=await page.goto(url,{waitUntil:"domcontentloaded",timeout:20000});await page.waitForTimeout(800);const text=await page.locator("body").innerText();await writeFile(`${root}/${name}.txt`,text);await writeFile(`${root}/${name}.html`,await page.content());return{name,status:response?.status(),characters:text.length,title:await page.title()};}catch{return{name,error:"unavailable"};}finally{await page.close();}}))));
 const page=await browser.newPage();await page.goto("https://modelhub.cc/quick-start",{waitUntil:"networkidle"});
 for(const [label,file] of [["图片生成","image"],["模型接口","models"]]){await page.getByText(label,{exact:true}).first().click();await page.waitForTimeout(300);await writeFile(`${root}/modelhub-${file}.txt`,await page.locator("body").innerText());}
}finally{await browser.close();}
