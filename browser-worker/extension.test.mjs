import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import {chromium} from 'playwright';

const source=await readFile(new URL('../extension/background.js',import.meta.url),'utf8');
const id='login_0123456789abcdef';
const origin='https://app.cloudcone.com';
const password='FakeOnly-Pass-582';
async function fixture(t,{challenge=false}={}) {
  const browser=await chromium.launch({headless:true});t.after(()=>browser.close());
  const page=await browser.newPage();
  await page.route('**/*',route=>route.fulfill({contentType:'text/html',body:new URL(route.request().url()).pathname==='/'?
    `<form id="login-form"><input name="email"><input name="password" type="password">${challenge?'<input name="captcha">':''}<button id="login-form-btn" type="button">Login</button></form><script>document.querySelector('button').onclick=()=>location.href='/cloud';</script>`:
    '<a href="/logout">Logout</a><a href="/billing">Billing</a><table><tr><td>CPU</td><td>2 cores</td></tr><tr><td>Price</td><td>'+password+'</td></tr><tr><td>Password</td><td>'+password+'</td></tr></table>'}));
  await page.goto(origin+'/');
  const storage={},messages=[],badges=[];
  const deadline=new Date(Date.now()+600000).toISOString();
  let job={id,origin,read_path:'/vps/12345/manage',status:'extension_pending',deadline};
  const listener={addListener() {}};
  const chrome={
    runtime:{onInstalled:listener,onStartup:listener,onMessage:listener,sendNativeMessage:async(host,message)=>{
      assert.equal(host,'com.grantide.login');messages.push(message);
      if(message.action==='poll')return job;
      if(message.action==='claim'){assert.equal(job.status,'extension_pending');job={...job,status:'extension_claimed'};return {origin,username:'fake@example.test',password,read_path:job.read_path,deadline};}
      if(message.action==='complete'){job=null;return {ok:true};}
      assert.fail('unexpected native action');
    }},
    storage:{session:{get:async()=>storage,set:async(value)=>Object.assign(storage,value),remove:async(key)=>{delete storage[key];}}},
    action:{setBadgeBackgroundColor:async()=>{},setBadgeText:async({text})=>badges.push(text)},
    alarms:{create:()=>{},onAlarm:listener},
    tabs:{query:async()=>[{id:7,url:page.url(),status:'complete'}],get:async()=>({id:7,url:page.url(),status:'complete'}),create:async()=>assert.fail('existing tab was not reused'),update:async(tab,{url})=>{assert.equal(tab,7);await page.goto(url);},onUpdated:listener},
    scripting:{executeScript:async({target,func,args})=>{
      assert.equal(target.tabId,7);
      return [{result:await page.evaluate(({source,args})=>{const fn=(0,eval)('('+source+')');return fn(...args);},{source:func.toString(),args})}];
    }},
  };
  const context=vm.createContext({chrome,URL,Error,Date});vm.runInContext(source,context);
  return {page,storage,messages,badges,poll:()=>vm.runInContext('poll()',context),job:()=>job};
}
test('extension reuses the existing tab, claims once and exports bounded fields',async t=>{
  const f=await fixture(t);
  await f.poll();await f.page.waitForURL(origin+'/cloud');
  assert.ok(!JSON.stringify(f.storage).includes(password));
  await f.poll();assert.equal(f.page.url(),origin+'/vps/12345/manage');
  await f.poll();
  const complete=f.messages.find(m=>m.action==='complete');
  assert.equal(complete.status,'completed');assert.deepEqual(JSON.parse(JSON.stringify(complete.fields)),{cpu:'2 cores'});
  assert.equal(f.messages.filter(m=>m.action==='claim').length,1);
  assert.ok(!JSON.stringify(f.messages).includes(password));assert.equal(f.storage.run,undefined);
});
test('extension fills credentials but pauses on CAPTCHA until website authentication',async t=>{
  const f=await fixture(t,{challenge:true});await f.poll();
  assert.equal(await f.page.locator('[name=password]').inputValue(),password);
  assert.equal(f.page.url(),origin+'/');assert.equal(f.badges.at(-1),'CAP');
  await f.poll();assert.equal(f.messages.filter(m=>m.action==='claim').length,1);
  assert.equal(f.messages.filter(m=>m.action==='complete').length,0);
  await f.page.locator('[name=captcha]').fill('FAKE-ONLY');await f.page.locator('button').click();await f.page.waitForURL(origin+'/cloud');
  await f.poll();await f.poll();assert.equal(f.messages.at(-1).status,'completed');
});
test('an existing authenticated session is not treated as the saved grant account',async t=>{
  const f=await fixture(t);await f.page.goto(origin+'/cloud');await f.poll();
  assert.equal(f.badges.at(-1),'ACCT');assert.equal(f.storage.run.phase,'account-check');
  assert.equal(f.messages.filter(m=>m.action==='claim').length,0);
  assert.equal(f.messages.filter(m=>m.action==='complete').length,0);
});
