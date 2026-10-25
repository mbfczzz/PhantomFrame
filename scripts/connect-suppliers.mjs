import assert from "node:assert/strict";
import {readFile,writeFile,mkdir} from "node:fs/promises";
// Credentials arrive on a non-echoing terminal, are never persisted here, and only go to their own origin.
if(process.stdin.isTTY) process.stdin.setRawMode(true);
process.stdin.resume();
process.stdout.write("READY: waiting for supplier credentials on hidden stdin\n");
let input="";
const payload=await new Promise((resolve,reject)=>{
 process.stdin.on("data",chunk=>{input+=chunk.toString();if(/[\r\n]/.test(input)){try{resolve(JSON.parse(input.trim()));}catch{reject(new Error("Invalid credential input"));}process.stdin.pause();}});
});
if(process.stdin.isTTY) process.stdin.setRawMode(false);
if(process.argv.includes("--stdin-test")){console.log("Validated hidden JSON input");process.exit(0);}
const vendors=[
 {vendor:"openvideo",name:"OpenVideo",origin:"https://openaivideo.top",docs:"https://openaivideo.top/"},
 {vendor:"suda",name:"Suda 速达",origin:"https://api.sudashuiapi.com",docs:"https://api-docs.sudashuiapi.com/"},
 {vendor:"modelhub",name:"ModelHub",origin:"https://modelhub.cc",docs:"https://modelhub.cc/quick-start"},
 {vendor:"gway",name:"Gway",origin:"https://gway.dev",docs:"https://gway.dev"},
 {vendor:"catcoder",name:"Catcoder",origin:"https://video.catcode.top",docs:""},
 {vendor:"wxart",name:"WXArt",origin:"https://api.wxart.space",docs:"https://wxart.space/console/docs"},
];
const local="http://127.0.0.1:3001/api";
const login=await fetch(local+"/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:await readFile(".local-credentials.json","utf8")});assert.ok(login.ok);
const cookie=login.headers.get("set-cookie").split(";")[0];
const localAPI=async(path,method="GET",body)=>{const response=await fetch(local+path,{method,headers:{Cookie:cookie,"Content-Type":"application/json"},body:body?JSON.stringify(body):undefined});if(!response.ok)throw new Error(`Local API HTTP ${response.status}`);return response.json();};
const existing=await localAPI("/admin/providers");
const report=[];
await mkdir("test-results/suppliers",{recursive:true});
for(const vendor of vendors){
 assert.equal(typeof payload[vendor.vendor],"string");assert.ok(payload[vendor.vendor].length>15);
 const old=existing.find(item=>item.config?.vendor===vendor.vendor||item.name===vendor.name&&item.base_url===vendor.origin+"/v1");
 const saved=await localAPI("/admin/providers"+(old?"/"+old.id:""),old?"PUT":"POST",{name:vendor.name,protocol:"json-async",base_url:vendor.origin+"/v1",apiKey:payload[vendor.vendor],enabled:false,config:{...old?.config,vendor:vendor.vendor,docsUrl:vendor.docs,integrationStatus:"configuring",preferred:vendor.vendor==="openvideo"}});
 report.push({vendor:vendor.vendor,providerId:saved.id,origin:vendor.origin});
}
await Promise.all(report.map(async item=>{
 try{
  const response=await fetch(item.origin+"/v1/models",{headers:{Authorization:"Bearer "+payload[item.vendor]},redirect:"manual",signal:AbortSignal.timeout(20000)});
  item.modelsHTTP=response.status;
  if(response.status>=300&&response.status<400){const location=response.headers.get("location");item.redirect=location?new URL(location,item.origin).origin+new URL(location,item.origin).pathname:undefined;return;}
  if(!response.ok)return;
  const body=await response.json();const rows=Array.isArray(body)?body:Array.isArray(body.data)?body.data:Array.isArray(body.models)?body.models:[];
  item.models=rows.length;item.responseKeys=Object.keys(body).slice(0,12);
  const sanitized=rows.map(row=>typeof row==="string"?{id:row}:Object.fromEntries(Object.entries(row).filter(([key])=>!["api_key","apiKey","token","secret","key"].includes(key))));
  await writeFile(`test-results/suppliers/${item.vendor}-models.json`,JSON.stringify(sanitized,null,2));
  item.exampleModels=sanitized.slice(0,8).map(row=>row.id);
 }catch{item.probeError="Network timeout or unreadable response";}
}));
input="";for(const key of Object.keys(payload))payload[key]="";
await writeFile("test-results/suppliers/connections.json",JSON.stringify(report,null,2));
console.log(JSON.stringify(report));
process.exit(0);
