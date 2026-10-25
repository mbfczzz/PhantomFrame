import {readFile,writeFile} from "node:fs/promises";
import {launchBrowser} from "./browser.mjs";
const origins={openvideo:"https://openaivideo.top",catcoder:"https://video.catcode.top",wxart:"https://wxart.space",gway:"https://gway.dev"};
for(const [name,origin] of Object.entries(origins)){
 const html=await readFile(`test-results/suppliers/docs/${name}.html`,"utf8");
 const scripts=[...html.matchAll(/<script[^>]+src=["']([^"']+)/g)].map(match=>new URL(match[1],origin).href);
 for(let i=0;i<scripts.length;i++){
  try{const response=await fetch(scripts[i],{signal:AbortSignal.timeout(20000)});const content=await response.text();await writeFile(`test-results/suppliers/docs/${name}-bundle-${i}.js`,content);const paths=[...new Set([...content.matchAll(/["'`]((?:https?:\/\/[^"'`\s]+)?\/(?:api|v1|openapi|docs)[^"'`\s]{0,160})["'`]/g)].map(m=>m[1]))];console.log(JSON.stringify({name,script:i,length:content.length,paths:paths.slice(0,90)}));}catch{console.log(JSON.stringify({name,error:"bundle unavailable"}));}
 }
}
const browser=await launchBrowser();
try{
 const page=await browser.newPage();await page.goto("https://modelhub.cc/quick-start",{waitUntil:"networkidle"});
 for(const [label,file] of [["图片生成","image"],["模型接口","models"]]){
  await page.getByRole("button",{name:label,exact:true}).click();await page.waitForTimeout(400);await writeFile(`test-results/suppliers/docs/modelhub-${file}.txt`,await page.locator("body").innerText());
 }
}finally{await browser.close();}
