const state = document.querySelector('#state');
document.querySelector('#extension-id').textContent = chrome.runtime.id;
async function refresh() {
  const {run,connection} = await chrome.storage.session.get(['run','connection']);
  state.textContent = connection === false ? '本机桥接尚未连接，请先安装并配对 Native Host。' :
    run?.phase === 'account-check' ? 'CloudCone 已有登录会话，但账号身份尚未适配核验。请先在网站退出，再用本次授权登录。' :
    run?.phase === 'challenge' ? '密码已填写，请在 CloudCone 标签页处理验证码 / MFA 并提交登录。' :
    run ? `当前任务：${run.id} · ${run.phase}` : '没有正在处理的浏览器登录';
}
document.querySelector('#check').addEventListener('click',async () => {
  state.textContent = '正在检查…';
  await chrome.runtime.sendMessage({action:'check'}).catch(() => {});
  await refresh();
});
refresh();
