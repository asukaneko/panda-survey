// 关于页逻辑（检测更新 / 更新日志 / Bug 反馈）
// 依赖页面中存在以下 id 的 DOM：
//   currentVersion, checkBtn, checkMsg, updateStatus, releaseList,
//   fbForm, fbCategory, fbTitle, fbDesc, fbContact, fbResult
// 由 initAbout(localVer) 初始化，可重复调用（内部幂等，事件仅绑定一次）。

const ABOUT_UPDATE_URL = 'https://www.aykeji.cn/api/app-update/pandasurvey';
const ABOUT_FEEDBACK_URL = '/api/feedback';

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s == null ? '' : String(s);
  return d.innerHTML;
}

// 版本号逐段比较：返回 1 / 0 / -1
function cmpVersion(a, b) {
  const pa = (a || '').replace(/^v/i, '').split('.').map((n) => parseInt(n, 10) || 0);
  const pb = (b || '').replace(/^v/i, '').split('.').map((n) => parseInt(n, 10) || 0);
  const n = Math.max(pa.length, pb.length);
  for (let i = 0; i < n; i++) {
    const x = pa[i] || 0, y = pb[i] || 0;
    if (x > y) return 1;
    if (x < y) return -1;
  }
  return 0;
}

let aboutInited = false;

export function initAbout(localVer) {
  if (!aboutInited) {
    aboutInited = true;
    const $checkBtn = document.getElementById('checkBtn');
    if ($checkBtn) {
      $checkBtn.addEventListener('click', () => loadUpdates(localVer));
    }
    const $fbForm = document.getElementById('fbForm');
    if ($fbForm) {
      $fbForm.addEventListener('submit', (e) => submitFeedback(e, localVer));
    }
  }
  loadUpdates(localVer);
}

async function loadUpdates(localVer) {
  const $version = document.getElementById('currentVersion');
  const $checkMsg = document.getElementById('checkMsg');
  const $updateStatus = document.getElementById('updateStatus');
  const $list = document.getElementById('releaseList');
  if (!$version && !$checkMsg && !$list) return; // 本页无关于区块

  if ($checkMsg) {
    $checkMsg.className = 'check-msg';
    $checkMsg.textContent = '正在检查更新…';
  }
  try {
    const resp = await fetch(ABOUT_UPDATE_URL);
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    const data = await resp.json();
    const current = data.current || '';
    if (current && $version) $version.textContent = current;

    if ($checkMsg) {
      if (current && cmpVersion(current, localVer) > 0) {
        $checkMsg.innerHTML = '发现新版本 <span class="new-badge">v' + escapeHtml(current) + '</span>，请到 www.aykeji.cn 下载更新';
      } else if (current) {
        $checkMsg.textContent = '已是最新版本（v' + current + '）';
      } else {
        $checkMsg.textContent = '服务器暂无版本记录';
      }
    }

    if (data.releases && data.releases.length > 0 && $list) {
      if ($updateStatus) $updateStatus.textContent = '';
      $list.innerHTML = data.releases.map((r) => {
        const isCurrent = r.version === current;
        return `<li class="release-item">
          <div class="rv${isCurrent ? ' current' : ''}">
            v${escapeHtml(r.version)}${isCurrent ? '<span class="tag">当前版本</span>' : ''}
          </div>
          <div class="r-date">${escapeHtml(r.pub_date || '')}</div>
          ${r.title ? '<div class="r-title">' + escapeHtml(r.title) + '</div>' : ''}
          ${r.content ? '<div class="r-title">' + escapeHtml(r.content) + '</div>' : ''}
          ${r.download_url ? '<div class="r-dl"><a href="' + escapeHtml(r.download_url) + '" target="_blank" rel="noopener">下载此版本</a></div>' : ''}
        </li>`;
      }).join('');
    } else if ($updateStatus) {
      $updateStatus.textContent = '暂无更新记录';
    }
  } catch (e) {
    if ($checkMsg) {
      $checkMsg.className = 'check-msg err';
      $checkMsg.textContent = '检查更新失败，请访问 www.aykeji.cn';
    }
  }
}

// 收集前端诊断信息（与 qi-jing 反馈页一致：仅排障用，不含账号数据）
function collectLogs() {
  const lines = [
    'UA: ' + navigator.userAgent,
    '屏幕: ' + screen.width + 'x' + screen.height + ' dpr=' + window.devicePixelRatio,
    '语言: ' + navigator.language,
    '页面: ' + location.href,
    '时间: ' + new Date().toLocaleString(),
  ];
  return lines.join('\n');
}

async function submitFeedback(e, localVer) {
  e.preventDefault();
  const $version = document.getElementById('currentVersion');
  const $title = document.getElementById('fbTitle');
  const $result = document.getElementById('fbResult');
  const title = $title ? $title.value.trim() : '';
  if (!title) {
    if ($result) { $result.className = 'fb-result err'; $result.textContent = '请填写标题'; }
    return;
  }
  const includeLogs = !!(document.getElementById('fbLogs') || {}).checked;
  const body = {
    version: $version ? $version.textContent || localVer : localVer,
    category: (document.getElementById('fbCategory') || {}).value || '',
    title,
    description: (document.getElementById('fbDesc') || {}).value || '',
    contact: (document.getElementById('fbContact') || {}).value || '',
    logs: includeLogs ? collectLogs() : '',
  };
  if ($result) { $result.className = 'fb-result'; $result.textContent = '正在提交…'; }
  try {
    const resp = await fetch(ABOUT_FEEDBACK_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await resp.json().catch(() => null);
    if (!resp.ok || !data || !data.ok) {
      throw new Error((data && data.error) || ('HTTP ' + resp.status));
    }
    if ($result) { $result.className = 'fb-result ok'; $result.textContent = '反馈已提交，感谢你的帮助'; }
    const form = document.getElementById('fbForm');
    if (form) form.reset();
  } catch (err) {
    if ($result) { $result.className = 'fb-result err'; $result.textContent = '提交失败：' + err.message; }
  }
}
