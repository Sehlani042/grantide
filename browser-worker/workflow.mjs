import {origin,allowedRequest,isAuthenticated,fillLogin,hasChallenge,readInventory,filterInventory} from './adapter.mjs';

// Browser routing alone does not reliably intercept every server redirect hop.
// Fetch without redirects and never deliver a redirect for Chromium to follow.
export async function dispatchWithoutRedirects(route) {
  let response;
  try {
    response=await route.fetch({maxRedirects:0,maxRetries:0,timeout:30000});
    if(response.status()>=300&&response.status()<400)return await route.abort();
    await route.fulfill({response});
  }catch{
    await route.abort().catch(()=>{});
  }finally{
    await response?.dispose();
  }
}

async function validLoginPost(request,input) {
  try {
    const body=request.postDataBuffer();if(!body||body.length>16384)return false;
    const data=await new Request(origin+'/ajax/visitor',{method:'POST',headers:{'content-type':request.headers()['content-type']||''},body}).formData();
    const keys=[...data.keys()];
    if(new Set(keys).size!==keys.length||keys.some(k=>!['email','password','captcha','2fa_code','method','_token'].includes(k)))return false;
    return data.get('method')==='login'&&data.get('email')===input.username&&data.get('password')===input.password;
  }catch{return false;}
}
// Only the fixed entry point constructs this context. Injection here is for fake-site tests.
export async function loginWorkflow(context,input,{human,stopped=()=>false,dispatch=dispatchWithoutRedirects}) {
  let phase='login',loginPosts=0,page;
  await context.route('**/*',async route=>{
    const r=route.request();
    if(stopped()||!allowedRequest(r.url(),r.method(),phase,input.read_path,r.resourceType()))return route.abort();
    if(r.method()==='POST'&&(++loginPosts>3||!await validLoginPost(r,input)))return route.abort();
    return dispatch(route);
  });
  await context.routeWebSocket('**/*',ws=>ws.close());
  page=await context.newPage();
  context.on('page',p=>{if(p!==page)p.close().catch(()=>{});});
  page.setDefaultTimeout(10000);
  page.on('dialog',d=>d.dismiss().catch(()=>{}));
  await page.goto(origin+'/',{waitUntil:'domcontentloaded',timeout:30000});
  if(stopped())throw new Error('cancelled');
  await fillLogin(page,input);
  if(await hasChallenge(page)){
    if(!await human(page))throw new Error('cancelled');
  }else{
    await page.locator('#login-form-btn').click();
    // Wait for authenticated navigation or a challenge, without exporting observations.
    await page.waitForFunction(()=> !document.querySelector('#login-form') || [...document.querySelectorAll('input[name="captcha"],input[name="2fa_code"]')].some(e=>e.getClientRects().length>0),{},{timeout:10000}).catch(()=>{});
  }
  if(!await isAuthenticated(page)){
    if(await hasChallenge(page)){if(!await human(page))throw new Error('cancelled');}
    if(!await isAuthenticated(page))return {status:'login_failed',authenticated:false};
  }
  phase='read';
  let fields={};
  if(input.read_path){
    await page.goto(origin+input.read_path,{waitUntil:'domcontentloaded',timeout:30000});
    if(new URL(page.url()).pathname!==input.read_path||!await isAuthenticated(page))return {status:'adapter_mismatch',authenticated:false};
    fields=filterInventory(await readInventory(page),input);
  }
  return {status:'completed',authenticated:true,fields};
}
