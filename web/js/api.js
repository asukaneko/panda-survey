// 统一 API 封装：JSON 序列化、401 跳登录、错误抛出
async function api(path, opts = {}) {
  const init = {
    method: opts.method || 'GET',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
  };
  if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body);
  }
  if (opts.signal) {
    init.signal = opts.signal;
  }
  let resp;
  try {
    resp = await fetch(path, init);
  } catch (e) {
    if (e && e.name === 'AbortError') {
      throw new Error('请求超时，请重试');
    }
    throw new Error('网络请求失败，请检查连接');
  }
  let data = null;
  try {
    data = await resp.json();
  } catch (e) {
    data = null;
  }
  const onAuthPage = location.pathname === '/login' || location.pathname === '/register';
  if (resp.status === 401 && !onAuthPage && !path.startsWith('/api/auth/')) {
    location.href = '/login';
    throw new Error('登录已过期');
  }
  if (!data || data.code !== 0) {
    const err = new Error((data && data.msg) || '请求失败（HTTP ' + resp.status + '）');
    err.code = data && data.code;
    err.status = resp.status;
    throw err;
  }
  return data.data;
}

// 全局提示条
let toastTimer = null;
function ensureToast() {
  let el = document.querySelector('.toast');
  if (!el) {
    el = document.createElement('div');
    el.className = 'toast';
    document.body.appendChild(el);
  }
  return el;
}
function toast(msg, isError) {
  const el = ensureToast();
  el.textContent = msg;
  el.className = 'toast show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    el.className = 'toast' + (isError ? ' err' : '');
  }, 2600);
}

// UTC 时间字符串转本地显示
function fmtTime(utc) {
  if (!utc) {
    return '';
  }
  const d = new Date(utc);
  if (isNaN(d.getTime())) {
    return utc;
  }
  const p = (n) => (n < 10 ? '0' + n : '' + n);
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) +
    ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
}

export { api, toast, fmtTime };
