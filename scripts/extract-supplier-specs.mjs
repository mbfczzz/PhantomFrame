import ts from "typescript";
import {readFile,writeFile} from "node:fs/promises";
for(const [name,file] of [["wxart","wxart-bundle-3.js"],["gway","gway-bundle-4.js"]]){
 const content=await readFile("test-results/suppliers/docs/"+file,"utf8");
 const source=ts.createSourceFile(file,content,ts.ScriptTarget.Latest,true,ts.ScriptKind.JS);
 const strings=[];
 const visit=node=>{if(ts.isStringLiteral(node)||ts.isNoSubstitutionTemplateLiteral(node)){const value=node.text;if(value.length>70&&(/model|视频|图片|素材|上传|接口|API|任务|Bearer/.test(value)))strings.push(value);}else if(ts.isTemplateExpression(node)&&node.getText(source).includes('"model"'))strings.push(node.getText(source));ts.forEachChild(node,visit);};
 visit(source);await writeFile(`test-results/suppliers/docs/${name}-spec-strings.txt`,strings.join("\n\n---\n\n"));console.log(JSON.stringify({name,strings:strings.length}));
}
const html=await readFile("test-results/suppliers/docs/openvideo.html","utf8");
const paths=[...new Set([...html.matchAll(/["'`]([^"'`]*\/(?:v1|api)\/[^"'`\s]*)["'`]/g)].map(m=>m[1]))];
await writeFile("test-results/suppliers/docs/openvideo-inline.txt",html.replace(/<[^>]+>/g,"\n"));console.log(JSON.stringify({name:"openvideo",paths:paths.slice(0,70)}));
