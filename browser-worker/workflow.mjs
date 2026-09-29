import {origin,allowedRequest,isAuthenticated,fillLogin,hasChallenge,readInventory,filterInventory} from './adapter.mjs';

// Native Chromium networking uses the operator's working network path. Pause
// responses before Chromium follows redirects, including redirect hops that
// Playwright's request router does not invoke again.
export async function installRedirectGuard(context,page) {
  const cdp=await context.newCDPSession(page);
  cdp.on('Fetch.requestPaused',async event=>{
    const id=event.requestId, status=event.responseStatusCode;
    try {
      if(typeof status!=='number') {
        // Chromium can surface an initial request-stage pause even with a
        // response-stage pattern. The request router already checked this URL;
        // still reject a redirect hop that changes origin before it proceeds.
        if(new URL(event.request.url).origin!==origin) {
          await cdp.send('Fetch.failRequest',{requestId:id,errorReason:'BlockedByClient'});
        }else{
          await cdp.send('Fetch.continueRequest',{requestId:id});
        }
      }else if(status>=300&&status<400) {
        await cdp.send('Fetch.failRequest',{requestId:id,errorReason:'BlockedByClient'});
      }else{
        await cdp.send('Fetch.continueResponse',{requestId:id});
      }
    }catch{
      await cdp.send('Fetch.failRequest',{requestId:id,errorReason:'BlockedByClient'}).catch(()=>{});
    }
  });
  await cdp.send('Fetch.enable',{patterns:[{urlPattern:'*',requestStage:'Response'}]});
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
export async function loginWorkflow(context,input,{human,stopped=()=>false,dispatch=route=>route.continue()}) {
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
  await installRedirectGuard(context,page);
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
