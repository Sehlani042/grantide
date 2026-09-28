export const origin = 'https://app.cloudcone.com';
export const overviewPath = /^\/(compute|vps)\/[0-9]+$/;
export function allowedRequest(raw, method, phase, readPath, resourceType = 'document') {
  let u; try { u = new URL(raw); } catch { return false; }
  if (u.origin !== origin || u.username || u.password || u.hash || /[%\\]/.test(u.pathname)) return false;
  if (method === 'POST') return phase === 'login' && u.pathname === '/ajax/visitor' && !u.search;
  if (method !== 'GET') return false;
  if (u.pathname.startsWith('/assets/')) return ['script','stylesheet','image','font'].includes(resourceType);
  if (u.pathname === '/favicon.ico') return resourceType === 'image';
  if (phase === 'login' && u.pathname === '/captcha') return resourceType === 'image' && [...u.searchParams.keys()].every(k=>['_CAPTCHA','t'].includes(k));
  if (u.search) return false;
  if (['/', '/compute', '/vps'].includes(u.pathname)) return true;
  if (u.pathname === '/login') return phase === 'login';
  return phase === 'read' && overviewPath.test(readPath) && u.pathname === readPath;
}
export async function isAuthenticated(page) {
  if (new URL(page.url()).origin !== origin) return false;
  if (await page.locator('#login-form').isVisible()) return false;
  // Require both authenticated navigation and a logout control, never URL alone.
  return await page.locator('a[href="/logout"], a[href="https://app.cloudcone.com/logout"]').count() > 0 &&
    await page.locator('a[href="/billing"], a[href="https://app.cloudcone.com/billing"]').count() > 0;
}
export async function fillLogin(page, input) {
  if (new URL(page.url()).origin !== origin) throw new Error('adapter');
  const email=page.locator('#login-form input[name="email"]');
  const password=page.locator('#login-form input[name="password"][type="password"]');
  const button=page.locator('#login-form-btn');
  if(await email.count()!==1||await password.count()!==1||await button.count()!==1) throw new Error('adapter');
  await email.fill(input.username);
  // Recheck destination immediately before handing the secret to the browser.
  if (new URL(page.url()).origin !== origin) throw new Error('adapter');
  await password.fill(input.password);
}
export async function hasChallenge(page) {
  return await page.locator('#login-form input[name="captcha"]').isVisible() ||
    await page.locator('#login-form input[name="2fa_code"]').isVisible();
}
export async function readInventory(page) {
  // Return only reviewed label/value pairs. Never return body text, HTML, inputs, scripts or attributes.
  return page.locator('tr, dl').evaluateAll(nodes => {
    const names={'cpu':'cpu','cpu cores':'cpu','vcpus':'cpu','memory':'memory','ram':'memory','disk':'disk','disk space':'disk','bandwidth':'bandwidth','traffic':'traffic','monthly traffic':'traffic','price':'price','next due date':'next_due','auto renew':'auto_renew','auto renewal':'auto_renew'};
    const out={};
    for(const node of nodes){
      const cells=node.matches('tr')?[...node.querySelectorAll(':scope > td, :scope > th')]:[...node.querySelectorAll(':scope > dt, :scope > dd')];
      if(cells.length!==2||cells.some(c=>c.querySelector('input,textarea,script,style')))continue;
      const key=names[cells[0].textContent.trim().replace(/:$/,'').toLowerCase()];
      const value=cells[1].textContent.trim();
      if(key && value.length<=64)out[key]=value;
    }
    return out;
  });
}
export const fieldFormats = {
 cpu:/^[0-9]{1,4}(?:\s*(?:v?cpus?|cores?))?$/i,
 memory:/^[0-9.]{1,10}\s*(?:[KMGT]i?B)$/i,
 disk:/^[0-9.]{1,10}\s*(?:[KMGT]i?B)(?:\s+(?:SSD|HDD|NVMe))?$/i,
 bandwidth:/^[0-9.]{1,10}\s*(?:[KMGT]i?(?:B|bit)\/?s|[KMGT]bps)$/i,
 traffic:/^[0-9.]{1,10}\s*(?:[KMGT]i?B)(?:\s*\/\s*month)?$/i,
 price:/^(?:\$|USD\s*)[0-9.,]{1,15}(?:\s*\/\s*(?:year|month|quarter))?$/i,
 next_due:/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/,
 auto_renew:/^(?:enabled|disabled|on|off|yes|no)$/i,
};
export function filterInventory(values, input) {
  return Object.fromEntries(Object.entries(values).filter(([k,v])=>fieldFormats[k]?.test(v)&&!v.includes(input.password)&&!v.includes(input.username)));
}
