const ORIGIN = 'https://app.cloudcone.com';
const HOST = 'com.grantide.login';
let busy = false;

async function native(message) {
  const answer = await chrome.runtime.sendNativeMessage(HOST, message);
  if (answer === undefined) throw new Error('native host unavailable');
  return answer;
}
async function state() { return (await chrome.storage.session.get('run')).run || null; }
async function remember(run) { await chrome.storage.session.set({run}); }
async function badge(text, color = '#355747') {
  await chrome.action.setBadgeBackgroundColor({color});
  await chrome.action.setBadgeText({text});
}
async function clear() { await chrome.storage.session.remove('run'); await badge(''); }
function fixedURL(raw, readPath = '') {
  try { const u = new URL(raw); return u.origin === ORIGIN && !u.username && !u.password && !u.search && !u.hash &&
    (['/','/login','/cloud','/compute','/vps'].includes(u.pathname) || (readPath && u.pathname === readPath)); }
  catch { return false; }
}
function inspectPage(readPath) {
  if (location.origin !== 'https://app.cloudcone.com' || location.search || location.hash ||
    !(['/','/login','/cloud','/compute','/vps'].includes(location.pathname) || (readPath && location.pathname===readPath))) return {kind:'mismatch'};
  if (document.querySelectorAll('#login-form').length > 1) return {kind:'mismatch'};
  const login = document.querySelector('#login-form');
  const auth = !login && !!document.querySelector('a[href="/logout"],a[href="https://app.cloudcone.com/logout"]') &&
    !!document.querySelector('a[href="/billing"],a[href="https://app.cloudcone.com/billing"]');
  if (auth && readPath && location.pathname !== readPath) return {kind:'navigate'};
  if (auth) {
    const names = {'cpu':'cpu','cpu cores':'cpu','vcpus':'cpu','memory':'memory','ram':'memory','disk':'disk','disk space':'disk','bandwidth':'bandwidth','traffic':'traffic','monthly traffic':'traffic','price':'price','next due date':'next_due','auto renew':'auto_renew','auto renewal':'auto_renew'};
    const formats = {cpu:/^[0-9]{1,4}(?:\s*(?:v?cpus?|cores?))?$/i,memory:/^[0-9.]{1,10}\s*(?:[KMGT]i?B)$/i,disk:/^[0-9.]{1,10}\s*(?:[KMGT]i?B)(?:\s+(?:SSD|HDD|NVMe))?$/i,bandwidth:/^[0-9.]{1,10}\s*(?:[KMGT]i?(?:B|bit)\/?s|[KMGT]bps)$/i,traffic:/^[0-9.]{1,10}\s*(?:[KMGT]i?B)(?:\s*\/\s*month)?$/i,price:/^(?:\$|USD\s*)[0-9.,]{1,15}(?:\s*\/\s*(?:year|month|quarter))?$/i,next_due:/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/,auto_renew:/^(?:enabled|disabled|on|off|yes|no)$/i};
    const fields = {};
    if (readPath) for (const row of document.querySelectorAll('tr,dl')) {
      const cells = row.matches('tr') ? [...row.querySelectorAll(':scope > td,:scope > th')] : [...row.querySelectorAll(':scope > dt,:scope > dd')];
      if (cells.length !== 2 || cells.some(c => c.querySelector('input,textarea,script,style'))) continue;
      const key = names[cells[0].textContent.trim().replace(/:$/,'').toLowerCase()];
      const value = cells[1].textContent.trim();
      if (key && value.length <= 64 && formats[key].test(value)) fields[key] = value;
    }
    return {kind:'authenticated',fields};
  }
  if (!login) return {kind:'waiting'};
  if (login.querySelectorAll('input[name="email"]').length !== 1 || login.querySelectorAll('input[name="password"][type="password"]').length !== 1) return {kind:'mismatch'};
  const challenge = [...login.querySelectorAll('input[name="captcha"],input[name="2fa_code"]')].some(e => e.getClientRects().length > 0);
  return {kind:challenge?'challenge':'login'};
}
function fillPage(username, password) {
  if (location.origin !== 'https://app.cloudcone.com' || !['/','/login'].includes(location.pathname) || location.search || location.hash) return {kind:'mismatch'};
  if (document.querySelectorAll('#login-form').length !== 1 || document.querySelectorAll('#login-form-btn').length !== 1) return {kind:'mismatch'};
  const form = document.querySelector('#login-form');
  const email = form?.querySelector('input[name="email"]');
  const secret = form?.querySelector('input[name="password"][type="password"]');
  const button = document.querySelector('#login-form-btn');
  if (!form || !email || !secret || !button || !username || !password ||
    form.querySelectorAll('input[name="email"]').length !== 1 || form.querySelectorAll('input[name="password"][type="password"]').length !== 1) return {kind:'mismatch'};
  const set = (element, value) => { element.value = value; element.dispatchEvent(new Event('input',{bubbles:true})); element.dispatchEvent(new Event('change',{bubbles:true})); };
  set(email,username);
  if (location.origin !== 'https://app.cloudcone.com' || !['/','/login'].includes(location.pathname) || location.search || location.hash) return {kind:'mismatch'};
  set(secret,password);
  const challenge = [...form.querySelectorAll('input[name="captcha"],input[name="2fa_code"]')].some(e => e.getClientRects().length > 0);
  if (!challenge) button.click();
  return {kind:challenge?'challenge':'submitted'};
}
async function injected(tabId, func, args = []) {
  const out = await chrome.scripting.executeScript({target:{tabId},func,args});
  return out?.[0]?.result || {kind:'waiting'};
}
async function findTab(readPath) {
  const tabs = await chrome.tabs.query({url:'https://app.cloudcone.com/*'});
  const existing = tabs.find(t => t.active && t.url && fixedURL(t.url,readPath)) || tabs.find(t => t.url && fixedURL(t.url,readPath));
  return existing || await chrome.tabs.create({url:ORIGIN+'/',active:false});
}
async function finish(run, status, fields = {}) {
  await native({action:'complete',id:run.id,status,fields});
  await clear();
}
async function processJob(job) {
  if (!job || !/^login_[a-f0-9]{16}$/.test(job.id) || job.origin !== ORIGIN ||
      (job.read_path && !/^\/(?:compute\/[0-9]+|vps\/[0-9]+(?:\/manage)?)$/.test(job.read_path)) ||
      !['extension_pending','extension_claimed'].includes(job.status) || !Number.isFinite(Date.parse(job.deadline)) || Date.parse(job.deadline) <= Date.now()) {
    await badge('ERR','#a43d37'); return;
  }
  let run = await state();
  if (run && run.id !== job.id) { await clear(); run = null; }
  if (!run && job.status === 'extension_claimed') { await badge('ERR','#a43d37'); return; }
  if (!run) {
    const tab = await findTab(job.read_path||'');
    run = {id:job.id,tabId:tab.id,phase:'opening',readPath:job.read_path||''};
    await remember(run);
  }
  let tab;
  try { tab = await chrome.tabs.get(run.tabId); }
  catch { if(job.status==='extension_claimed') await finish(run,'adapter_mismatch'); else await clear(); return; }
  if (!fixedURL(tab.url,run.readPath)) { if(job.status==='extension_claimed') await finish(run,'adapter_mismatch'); else await clear(); return; }
  if (tab.status !== 'complete') { await badge('…'); return; }
  let page;
  try { page = await injected(tab.id,inspectPage,[run.readPath]); }
  catch { await badge('…'); return; }
  if (page.kind === 'mismatch') { if(job.status==='extension_claimed') await finish(run,'adapter_mismatch'); else await badge('ERR','#a43d37'); return; }
  if (job.status === 'extension_pending') {
    if (page.kind === 'waiting') { await badge('…'); return; }
    // Authentication markers alone do not identify the account. Never treat
    // an existing session as proof of login with the grant's saved account.
    if (page.kind === 'authenticated' || page.kind === 'navigate') {
      run.phase='account-check'; await remember(run); await badge('ACCT','#9b743e'); return;
    }
    // Claim only when the exact provider page is visible. The host issues the
    // saved credential once; it never enters extension storage or logs.
    const credentials = await native({action:'claim',id:job.id});
    if (credentials.origin !== ORIGIN || (credentials.read_path||'') !== run.readPath || !Number.isFinite(Date.parse(credentials.deadline)) || Date.parse(credentials.deadline) <= Date.now()) throw new Error('claim scope mismatch');
    run.phase = 'claimed'; await remember(run);
    const result = await injected(tab.id,fillPage,[credentials.username,credentials.password]);
    if (result.kind === 'mismatch') { await finish(run,'adapter_mismatch'); return; }
    run.phase=result.kind==='challenge'?'challenge':'submitted'; await remember(run);
    await badge(result.kind === 'challenge' ? 'CAP' : '…', result.kind === 'challenge' ? '#9b743e' : '#355747');
    return;
  }
  if (!['challenge','submitted','reading'].includes(run.phase)) { await badge('ERR','#a43d37'); return; }
  if (page.kind === 'navigate') {
    run.phase = 'reading'; await remember(run);
    await chrome.tabs.update(tab.id,{url:ORIGIN+run.readPath});
    await badge('READ'); return;
  }
  if (page.kind === 'authenticated') { await finish(run,'completed',page.fields||{}); await badge('✓'); return; }
  if (page.kind === 'challenge') { await badge('CAP','#9b743e'); return; }
  await badge('…');
}
async function poll() {
  if (busy) return;
  busy = true;
  try {
    const job = await native({action:'poll'});
    await chrome.storage.session.set({connection:true});
    if (!job) { if (await state()) await clear(); return; }
    await processJob(job);
  } catch { await chrome.storage.session.set({connection:false}); await badge('ERR','#a43d37'); }
  finally { busy = false; }
}
chrome.runtime.onInstalled.addListener(() => { chrome.alarms.create('grantide-poll',{periodInMinutes:0.5}); poll(); });
chrome.runtime.onStartup.addListener(() => { chrome.alarms.create('grantide-poll',{periodInMinutes:0.5}); poll(); });
chrome.alarms.onAlarm.addListener(a => { if(a.name==='grantide-poll') poll(); });
chrome.tabs.onUpdated.addListener((_id,change) => { if(change.status==='complete') poll(); });
chrome.runtime.onMessage.addListener((m,_sender,sendResponse) => {
  if(m?.action !== 'check') return;
  poll().then(() => sendResponse({ok:true})).catch(() => sendResponse({ok:false}));
  return true;
});
