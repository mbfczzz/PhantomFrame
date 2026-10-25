import ts from "typescript";
import {readFile,writeFile} from "node:fs/promises";
import {launchBrowser} from "./browser.mjs";
const content=await readFile("test-results/suppliers/docs/wxart-bundle-3.js","utf8");
const source=ts.createSourceFile("wxart.js",content,ts.ScriptTarget.Latest,true,ts.ScriptKind.JS);
const examples=[];
const visit=node=>{const relevant=ts.isVariableDeclaration(node)&&["sd","sc","su"].includes(node.name.getText(source))||ts.isObjectLiteralExpression(node)&&node.properties.some(p=>ts.isPropertyAssignment(p)&&["model","model_name"].includes(p.name.getText(source))&&ts.isStringLiteral(p.initializer)&&/seedance|gpt-image|nano-banana|veo/.test(p.initializer.text));if(relevant&&node.getText(source).length<3000)examples.push(node.getText(source));ts.forEachChild(node,visit);};visit(source);await writeFile("test-results/suppliers/docs/wxart-examples.txt",examples.join("\n\n"));
const browser=await launchBrowser();try{const page=await browser.newPage();await page.goto("https://openaivideo.top/",{waitUntil:"domcontentloaded"});await writeFile("test-results/suppliers/docs/openvideo-examples.txt",(await page.locator("pre").allTextContents()).join("\n\n---\n\n"));}finally{await browser.close();}
