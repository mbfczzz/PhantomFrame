import assert from "node:assert/strict";
import {readFile,writeFile} from "node:fs/promises";
const base="http://127.0.0.1:3001/api";
const login=await fetch(base+"/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:await readFile(".local-credentials.json","utf8")});
assert.ok(login.ok);
const cookie=login.headers.get("set-cookie").split(";")[0];
const get=async path=>{const response=await fetch(base+path,{headers:{Cookie:cookie}});assert.ok(response.ok,path);return response.json();};
const all=async path=>{const rows=[];for(let page=1;;page++){const result=await get(path+`?page=${page}&limit=100`);rows.push(...result.items);if(rows.length>=result.total)return rows;}};
const [users,prices,codes,transactions,stats]=await Promise.all([get("/admin/users"),get("/admin/prices"),all("/admin/codes"),all("/admin/transactions"),get("/admin/stats")]);
const select=(rows,keys)=>rows.map(row=>Object.fromEntries(keys.map(key=>[key,row[key]]))).sort((a,b)=>a.id.localeCompare(b.id));
const snapshot={users:select(users,["id","balance"]),prices:select(prices,["id","credits"]),codes:select(codes,["id","credits"]),transactions:select(transactions,["id","amount","balance"])};
const path="test-results/currency-preservation.json";
if(process.argv.includes("--before")) {assert.equal(stats.activeJobs,0,"Wait for live jobs before replacing the API");await writeFile(path,JSON.stringify(snapshot));}
else {assert.deepEqual(snapshot,JSON.parse(await readFile(path,"utf8")));const config=await get("/config");assert.equal(config.currency,"CNY");assert.equal(config.creditValueInYuan,1);}
console.log(JSON.stringify({phase:process.argv.includes("--before")?"before":"after",users:users.length,prices:prices.length,codes:codes.length,transactions:transactions.length,amountsPreserved:!process.argv.includes("--before")}));
