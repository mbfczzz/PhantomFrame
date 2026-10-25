import assert from "node:assert/strict";
import {readFile,writeFile} from "node:fs/promises";
const root="http://127.0.0.1:3001/api";
const login=await fetch(root+"/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:await readFile(".local-credentials.json","utf8")});assert.ok(login.ok);
const cookie=login.headers.get("set-cookie").split(";")[0];
const call=async(path,method="GET",body)=>{const response=await fetch(root+path,{method,headers:{Cookie:cookie,"Content-Type":"application/json"},body:body?JSON.stringify(body):undefined});const value=await response.json();if(!response.ok)throw new Error(value.message||`HTTP ${response.status}`);return value;};
const providers=await call("/admin/providers");
const storage=providers.find(p=>p.config?.vendor==="flowlight-upload");assert.ok(storage?.hasKey);
await call("/admin/providers/"+storage.id,"PUT",{...storage,protocol:"media-upload",apiKey:"",enabled:true,config:{...storage.config,integrationStatus:"configured"}});
const upload=await call("/admin/providers/"+storage.id+"/test-upload","POST");
console.log(JSON.stringify({service:"Flowlight upload",ok:upload.ok,publicHost:upload.host}));
const reports=[];
for(const vendor of ["openvideo","suda","modelhub","gway","catcoder","wxart"]){
 const provider=providers.find(p=>p.config?.vendor===vendor);assert.ok(provider?.hasKey);
 const config={...provider.config,pollInterval:vendor==="catcoder"?10000:5000,integrationStatus:"configured",generationVerified:false};
 if(["catcoder","wxart"].includes(vendor)) config.uploadProviderId=storage.id;
 if(vendor==="suda")config.uploadBaseURL="https://files.sudashuiapi.com";
 if(vendor==="catcoder")config.docsUrl="https://video.catcode.top/docs";
 const body={...provider,protocol:vendor,apiKey:"",enabled:false,config,base_url:vendor==="modelhub"?"https://api.modelhub.cc/v1":provider.base_url};
 await call("/admin/providers/"+provider.id,"PUT",body);
 try{
  const imported=await call("/admin/providers/"+provider.id+"/sync-models","POST");
  await call("/admin/providers/"+provider.id,"PUT",{...body,enabled:true,config:{...config,integrationStatus:"ready",modelsVerified:true}});
  reports.push({vendor,providerId:provider.id,enabled:true,...imported});
 }catch(error){reports.push({vendor,providerId:provider.id,enabled:false,error:error.message});}
}
await writeFile("test-results/suppliers/final-report.json",JSON.stringify({upload:{ok:upload.ok,host:upload.host},providers:reports},null,2));console.log(JSON.stringify(reports));
