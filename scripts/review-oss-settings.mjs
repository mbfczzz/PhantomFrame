import assert from 'node:assert/strict';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { launchBrowser } from './browser.mjs';
const base='http://127.0.0.1:5173', directory='test-results/oss-settings';
await mkdir(directory,{recursive:true});
const browser=await launchBrowser(), checks=[], errors=[];
try {
 const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
 assert.ok((await context.request.post(base+'/api/auth/login',{data:JSON.parse(await readFile('.local-credentials.json','utf8'))})).ok());
 const get=async()=>{const response=await context.request.get(base+'/api/admin/storage');assert.ok(response.ok());return response.json();};
 const before=await get();assert.ok(before.enabled&&before.hasCredentials);assert.equal(before.cdnDomain,'https://cdn.mbfczzzz.top');
 const page=await context.newPage();page.on('pageerror',error=>errors.push(error.message));
 await page.route('**/api/admin/**',route=>{if(route.request().method()!=='GET'){errors.push('Unexpected write');return route.abort();}return route.continue();});
 for(const width of [1440,390,320]){
  await page.setViewportSize({width,height:1000});await page.goto(base+'/admin/settings',{waitUntil:'networkidle'});
  const panel=page.locator('.storage-settings');await panel.getByRole('button',{name:'保存存储配置',exact:true}).waitFor();
  assert.equal(await panel.getByLabel('OSS AccessKey ID',{exact:true}).inputValue(),'');
  assert.equal(await panel.getByLabel('OSS AccessKey Secret',{exact:true}).inputValue(),'');
  assert.equal(await panel.getByLabel('CDN 域名',{exact:true}).inputValue(),before.cdnDomain);
  assert.equal(await panel.getByLabel('生成结果转存到阿里云 OSS',{exact:true}).isChecked(),true);
  assert.equal(await page.evaluate(()=>document.body.scrollWidth>innerWidth),false);
  await panel.screenshot({path:`${directory}/settings-${width}.png`,animations:'disabled'});
  checks.push({width,credentialsHidden:true,cdnConfigured:true,ossEnabled:true});
 }
 assert.deepEqual(await get(),before);assert.deepEqual(errors,[]);
 await writeFile(directory+'/report.json',JSON.stringify({checks,errors},null,2));console.log(JSON.stringify({checks,errors}));
}finally{await browser.close();}
