import { chromium } from 'playwright';
import readline from 'node:readline';
import { origin, overviewPath } from './adapter.mjs';
import { loginWorkflow } from './workflow.mjs';

// No console/error forwarding, tracing, screenshots, storage-state import/export or debug server.
let browser, context, page, input, started=false, stopped=false, waiting=false, resumeResolve, timeout;
const emit = status => process.stdout.write(JSON.stringify(status)+'\n');
const lines=readline.createInterface({input:process.stdin,crlfDelay:Infinity});
async function close() {
  if(stopped)return;stopped=true;clearTimeout(timeout);
  resumeResolve?.(false);
  try {await context?.close();await browser?.close();}catch{}
  lines.close();process.stdin.destroy();
}
async function finish(event){await close();emit(event);}
async function handoff(originalPage) {
  page=originalPage;
  await page.bringToFront();
  waiting=true;
  const result=new Promise(resolve=>{resumeResolve=resolve});
  emit({status:'human_required',authenticated:false});
  const resumed=await result;
  waiting=false;resumeResolve=undefined;
  // No page observations while the human is entering a challenge.
  return resumed&&!stopped;
}
async function run(incoming){
  input=incoming;
  if(input.origin!==origin||typeof input.username!=='string'||!input.username||typeof input.password!=='string'||!input.password||
     typeof input.read_path!=='string'||(input.read_path&&!overviewPath.test(input.read_path)))return finish({status:'adapter_mismatch',authenticated:false});
  const duration=new Date(input.deadline).getTime()-Date.now();
  if(!Number.isFinite(duration)||duration<=0||duration>600500)return finish({status:'adapter_mismatch',authenticated:false});
  timeout=setTimeout(()=>{close().finally(()=>process.exit(0));},duration);
  try{
    browser=await chromium.launch({headless:false,chromiumSandbox:process.platform==='linux'});
    if(stopped){await browser.close();return;}
    context=await browser.newContext({acceptDownloads:false,serviceWorkers:'block',permissions:[]});
    if(stopped){await context.close();await browser.close();return;}
    const result=await loginWorkflow(context,input,{human:handoff,stopped:()=>stopped});
    if(!stopped)await finish(result);
  }catch{
    if(!stopped)await finish({status:'adapter_mismatch',authenticated:false});
  }finally{input=undefined;}
}
lines.on('line',line=>{
  if(line.length>16384){close();return;}
  let msg;try{msg=JSON.parse(line);}catch{close();return;}
  if(!started){started=true;void run(msg);return;}
  if(msg.command==='cancel'){void close();return;}
  if(msg.command==='resume'&&waiting){resumeResolve?.(true);}
});
lines.on('close',()=>{if(!stopped)void close();});
process.on('SIGTERM',()=>{void close();});
process.on('SIGINT',()=>{void close();});
process.on('unhandledRejection',()=>{void close();});
