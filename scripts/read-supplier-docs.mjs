import {mkdir,writeFile} from "node:fs/promises";
import {launchBrowser} from "./browser.mjs";
await mkdir("test-results/suppliers/docs",{recursive:true});
const browser=await launchBrowser();
try{
 const sites=[
  ["openvideo","https://openaivideo.top/"],
  ["suda","https://api-docs.sudashuiapi.com/"],
  ["modelhub","https://modelhub.cc/quick-start"],
  ["gway","https://gway.dev"],
  ["catcoder","https://video.catcode.top/"],
  ["wxart","https://wxart.space/console/docs"],
 ];
 const results=await Promise.all(sites.map(async([name,url])=>{const page=await browser.newPage();try{const response=await page.goto(url,{waitUntil:"domcontentloaded",timeout:25000});await page.waitForTimeout(2000);const content=await page.locator("body").innerText();await writeFile(`test-results/suppliers/docs/${name}.txt`,content);await writeFile(`test-results/suppliers/docs/${name}.html`,await page.content());const links=await page.locator("a[href]").evaluateAll(elements=>elements.map(a=>({text:a.textContent?.trim().slice(0,55),href:a.href})).filter(a=>a.href.startsWith("http")).slice(0,35));return{name,status:response?.status(),title:await page.title(),characters:content.length,links};}catch{return{name,error:"Page unavailable"};}finally{await page.close();}}));console.log(JSON.stringify(results));
}finally{await browser.close();}
