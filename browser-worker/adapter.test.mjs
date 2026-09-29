import {test} from 'node:test';
import assert from 'node:assert/strict';
import {chromium} from 'playwright';
import {allowedRequest,origin} from './adapter.mjs';
import {loginWorkflow as workflow,dispatchWithoutRedirects} from './workflow.mjs';
import {createServer} from 'node:http';
// Fake pages exercise the policy/workflow; real HTTP tests below exercise transport.
const loginWorkflow=(context,input,options)=>workflow(context,input,{...options,dispatch:route=>route.fallback()});
const input={username:'fake@example.test',password:'FakeOnly-Pass-582',read_path:'/compute/12345'};
const accountHTML='<a href="/logout">Logout</a><a href="/billing">Billing</a>';
const form=(challenge=false)=>`<form id="login-form"><input name="email"><input name="password" type="password"><input name="method" value="login" type="hidden">${challenge?'<input name="captcha">':''}<button id="login-form-btn" type="button">Log in</button></form><script>document.querySelector('button').onclick=async()=>{const r=await fetch('/ajax/visitor',{method:'POST',body:new FormData(document.querySelector('form'))});if(r.ok)location.href='/cloud';};</script>`;
async function fixture(t,{challenge=false,redirect=false}={}){
 const browser=await chromium.launch({headless:true});const context=await browser.newContext({serviceWorkers:'block'});t.after(()=>browser.close());let posts=0;
 await context.route('**/*',async route=>{
  const r=route.request(),u=new URL(r.url());
  assert.equal(u.origin,origin,'external request escaped policy');
  if(u.pathname==='/ajax/visitor'){
   posts++;assert.equal(r.method(),'POST');assert.ok(r.postData().includes(input.password));
   return route.fulfill({status:redirect?307:200,headers:redirect?{location:'https://attacker.test/steal'}:{},body:'{}'});
  }
  const html=u.pathname==='/'?form(challenge):accountHTML+'<table><tr><td>CPU cores</td><td>2 cores</td></tr><tr><td>Memory</td><td>2 GB</td></tr><tr><td>Password</td><td>'+input.password+'</td></tr><tr><td>Price</td><td>'+input.password+'</td></tr></table>';
  return route.fulfill({contentType:'text/html',body:html});
 });
 return {context,posts:()=>posts};
}
test('strict origin, path, method and phase policy',()=>{
 for(const [url,method,phase] of [[origin+'/compute/12345/delete','GET','read'],[origin+'/billing','POST','read'],[origin+'/ajax/visitor','POST','read'],['https://app.cloudcone.com.evil.test/','GET','login'],['http://app.cloudcone.com/','GET','login'],[origin+'/compute/12345?delete=1','GET','read'],[origin+'/assets/../../logout','GET','read'],[origin+'/compute/999','GET','read']])assert.equal(allowedRequest(url,method,phase,input.read_path),false,url);
 assert.equal(allowedRequest(origin+'/ajax/visitor','POST','login',input.read_path),true);
 assert.equal(allowedRequest(origin+input.read_path,'GET','read',input.read_path),true);
 assert.equal(allowedRequest(origin+'/cloud','GET','login',''),true);
 assert.equal(allowedRequest(origin+'/cloud?delete=1','GET','login',''),false);
});
test('ordinary login autofills, authenticates and exports only inventory fields',async t=>{
 const f=await fixture(t);const result=await loginWorkflow(f.context,input,{human:async()=>assert.fail('unexpected human step')});
 assert.equal(f.posts(),1);assert.equal(result.status,'completed');assert.equal(result.authenticated,true);
 assert.deepEqual(result.fields,{cpu:'2 cores',memory:'2 GB'});assert.ok(!JSON.stringify(result).includes(input.password));
});
test('CAPTCHA pauses before submit and resumes only after human action',async t=>{
 const f=await fixture(t,{challenge:true});let handoffs=0;
 const result=await loginWorkflow(f.context,input,{human:async page=>{
  handoffs++;assert.equal(f.posts(),0);assert.equal(await page.locator('[name=password]').inputValue(),input.password);
  await page.locator('[name=captcha]').fill('FAKE-ONLY');await page.locator('#login-form-btn').click();await page.waitForURL(origin+'/cloud');return true;
 }});
 assert.equal(handoffs,1);assert.equal(result.status,'completed');assert.equal(f.posts(),1);
});
test('credential-bearing cross-origin redirect is blocked',async t=>{
 const f=await fixture(t,{redirect:true});const result=await loginWorkflow(f.context,input,{human:async()=>false});
 assert.equal(result.status,'login_failed');assert.equal(f.posts(),1);
});
test('false human confirmation does not prove login',async t=>{
 const f=await fixture(t,{challenge:true});const result=await loginWorkflow(f.context,input,{human:async()=>true});
 assert.equal(result.status,'login_failed');assert.equal(f.posts(),0);
});

test('real HTTP transport blocks redirects before any second-hop request',async t=>{
 const received=[];
 const server=createServer(async(req,res)=>{
  let body='';for await(const part of req)body+=part;
  received.push({url:req.url,body});
  if(req.url.startsWith('/redirect/')){
   res.writeHead(Number(req.url.split('/')[2]),{location:'/steal'});res.end();
  }else if(req.url==='/ok'){
   res.writeHead(200,{'content-type':'text/html','set-cookie':'fixture=yes; Path=/'});res.end('<p>OK</p>');
  }else{res.end('unexpected destination');}
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 t.after(()=>new Promise(resolve=>{server.close(resolve);server.closeAllConnections();}));
 const base=`http://127.0.0.1:${server.address().port}`;
 const browser=await chromium.launch({headless:true});t.after(()=>browser.close());
 const context=await browser.newContext({serviceWorkers:'block'});
 await context.route('**/*',dispatchWithoutRedirects);
 const page=await context.newPage();await page.goto(base+'/ok');
 assert.equal(await page.locator('p').textContent(),'OK');
 assert.ok((await context.cookies()).some(c=>c.name==='fixture'&&c.value==='yes'));
 for(const status of [301,302,303,307,308]){
  assert.equal(await page.evaluate(async({status,password})=>{
   try{await fetch('/redirect/'+status,{method:'POST',body:password});return 'followed';}catch{return 'blocked';}
  },{status,password:input.password}),'blocked');
 }
 assert.equal(received.filter(r=>r.url.startsWith('/redirect/')).length,5);
 assert.equal(received.filter(r=>r.url==='/steal').length,0);
 assert.ok(received.filter(r=>r.url.startsWith('/redirect/')).every(r=>r.body===input.password));
});
