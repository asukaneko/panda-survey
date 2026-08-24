// 管理后台：总览 / AI 服务 / 用户管理 / 问卷管理 / 关于
import { api, toast, fmtTime } from '/js/api.js';
import { initAbout } from '/js/about.js';

const $ = (id) => document.getElementById(id);

/* ---------- 标签切换 ---------- */

const views = { tabOverview: 'viewOverview', tabAI: 'viewAI', tabUsers: 'viewUsers', tabSurveys: 'viewSurveys', tabAbout: 'viewAbout' };
Object.keys(views).forEach((tabId) => {
  $(tabId).addEventListener('click', () => {
    Object.entries(views).forEach(([t, v]) => {
      $(t).classList.toggle('active', t === tabId);
      $(v).style.display = t === tabId ? '' : 'none';
    });
    if (tabId === 'tabOverview') {
      loadOverview();
    }
    if (tabId === 'tabUsers') {
      loadUsers();
    }
    if (tabId === 'tabSurveys') {
      loadSurveys();
    }
    if (tabId === 'tabAbout') {
      initAbout('0.2.0'); // 本地版本号（每次发版需同步修改）
    }
  });
});

/* ---------- 总览 ---------- */

async function loadOverview() {
  let ov;
  try {
    ov = await api('/api/admin/overview');
  } catch (e) {
    toast(e.message, true);
    return;
  }
  const box = $('ovTiles');
  box.textContent = '';
  const tiles = [
    [ov.users, '注册用户'],
    [ov.surveys, '问卷总数'],
    [ov.published, '发布中'],
    [ov.responses, '累计答卷'],
    [ov.ai_calls, 'AI 调用次数'],
  ];
  tiles.forEach(([num, lbl]) => {
    const t = document.createElement('div');
    t.className = 'stat-tile';
    const n = document.createElement('div');
    n.className = 'num';
    n.textContent = num;
    const l = document.createElement('div');
    l.className = 'lbl';
    l.textContent = lbl;
    t.append(n, l);
    box.appendChild(t);
  });
}

/* ---------- 用户管理 ---------- */

async function loadUsers() {
  const table = $('userTable');
  table.textContent = '';
  const head = ['用户名', '角色', '问卷数', '注册时间', '状态', '操作'].map((h) => h);
  const thead = document.createElement('thead');
  const trh = document.createElement('tr');
  head.forEach((h) => {
    const th = document.createElement('th');
    th.textContent = h;
    trh.appendChild(th);
  });
  thead.appendChild(trh);
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  table.appendChild(tbody);
  let users;
  try {
    users = await api('/api/admin/users');
  } catch (e) {
    toast(e.message, true);
    return;
  }
  const me = await api('/api/auth/me').catch(() => null);
  users.forEach((u) => {
    const tr = document.createElement('tr');
    const tdName = document.createElement('td');
    tdName.textContent = u.username;
    const tdRole = document.createElement('td');
    tdRole.textContent = u.role === 1 ? '管理员' : '普通用户';
    const tdCount = document.createElement('td');
    tdCount.textContent = u.survey_count;
    const tdTime = document.createElement('td');
    tdTime.textContent = fmtTime(u.created_at);
    const tdStatus = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = 'badge ' + (u.status === 0 ? 'badge-published' : 'badge-stopped');
    badge.textContent = u.status === 0 ? '正常' : '已封禁';
    tdStatus.appendChild(badge);
    const tdOp = document.createElement('td');
    const isSelf = me && me.id === u.id;
    if (isSelf) {
      tdOp.textContent = '（当前账号）';
    } else {
      const btn = document.createElement('button');
      const banned = u.status !== 0;
      btn.className = 'btn btn-sm ' + (banned ? '' : 'btn-danger');
      btn.textContent = banned ? '解封' : '封禁';
      btn.addEventListener('click', async () => {
        const action = banned ? '解封' : '封禁';
        if (!confirm('确定' + action + '用户「' + u.username + '」？')) {
          return;
        }
        try {
          await api('/api/admin/users/' + u.id + (banned ? '/unban' : '/ban'), { method: 'PUT', body: {} });
          toast('已' + action);
          loadUsers();
        } catch (e) {
          toast(e.message, true);
        }
      });
tdOp.appendChild(btn);
	      const del = document.createElement('button');
      del.className = 'btn btn-sm btn-danger';
      del.textContent = '删除';
      del.addEventListener('click', async () => {
        if (!confirm('永久删除用户「' + u.username + '」及其全部问卷、答卷？此操作不可恢复！')) {
          return;
        }
        if (!confirm('再次确认：将彻底删除该用户所有数据，确定删除？')) {
          return;
        }
        try {
          await api('/api/admin/users/' + u.id, { method: 'DELETE', body: {} });
          toast('已删除用户');
          loadUsers();
        } catch (e) {
          toast(e.message, true);
        }
      });
      tdOp.appendChild(del);
    }
    tr.append(tdName, tdRole, tdCount, tdTime, tdStatus, tdOp);
    tbody.appendChild(tr);
  });
}

/* ---------- 问卷管理 ---------- */

const surveyStatus = { 0: '未发布', 1: '发布中', 2: '已停止' };
const surveyBadge = { 0: 'badge-draft', 1: 'badge-published', 2: 'badge-stopped' };

async function loadSurveys() {
  const table = $('surveyTable');
  table.textContent = '';
  const thead = document.createElement('thead');
  const trh = document.createElement('tr');
  ['问卷', '所有者', '状态', '回收量', '更新时间', '操作'].forEach((h) => {
    const th = document.createElement('th');
    th.textContent = h;
    trh.appendChild(th);
  });
  thead.appendChild(trh);
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  table.appendChild(tbody);
  let surveys;
  try {
    surveys = await api('/api/admin/surveys');
  } catch (e) {
    toast(e.message, true);
    return;
  }
  if (surveys.length === 0) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.textContent = '暂无问卷';
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }
  surveys.forEach((s) => {
    const tr = document.createElement('tr');
    const tdTitle = document.createElement('td');
    tdTitle.textContent = s.title || '未命名问卷';
    const tdOwner = document.createElement('td');
    tdOwner.textContent = s.username;
    const tdStatus = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = 'badge ' + surveyBadge[s.status];
    badge.textContent = surveyStatus[s.status];
    tdStatus.appendChild(badge);
    const tdCount = document.createElement('td');
    tdCount.textContent = s.response_count;
    const tdTime = document.createElement('td');
    tdTime.textContent = fmtTime(s.updated_at);
    const tdOp = document.createElement('td');
    if (s.status === 1) {
      const stop = document.createElement('button');
      stop.className = 'btn btn-sm';
      stop.textContent = '下架';
      stop.addEventListener('click', async () => {
        if (!confirm('确定下架问卷「' + (s.title || '未命名') + '」？填写者将立即无法提交。')) {
          return;
        }
        try {
          await api('/api/admin/surveys/' + s.id + '/stop', { method: 'POST', body: {} });
          toast('已下架');
          loadSurveys();
        } catch (e) {
          toast(e.message, true);
        }
      });
      tdOp.appendChild(stop);
    }
    const del = document.createElement('button');
    del.className = 'btn btn-sm btn-danger';
    del.textContent = '删除';
    del.addEventListener('click', async () => {
      if (!confirm('确定删除问卷「' + (s.title || '未命名') + '」？历史数据保留但不可恢复查看。')) {
        return;
      }
      try {
        await api('/api/admin/surveys/' + s.id, { method: 'DELETE' });
        toast('已删除');
        loadSurveys();
      } catch (e) {
        toast(e.message, true);
      }
    });
    tdOp.appendChild(del);
    tr.append(tdTitle, tdOwner, tdStatus, tdCount, tdTime, tdOp);
    tbody.appendChild(tr);
  });
}

/* ---------- AI 配置 ---------- */

async function loadAIConfig() {
  let data;
  try {
    data = await api('/api/admin/ai-config');
  } catch (e) {
    if (e.status === 403) {
      document.querySelector('main').textContent = '';
      const d = document.createElement('div');
      d.className = 'thanks';
      const h = document.createElement('h2');
      h.textContent = '需要管理员权限';
      d.appendChild(h);
      document.querySelector('main').appendChild(d);
      return;
    }
    toast(e.message, true);
    return;
  }
  const cfg = data.config;
  $('cfgBaseURL').value = cfg.base_url || '';
  $('cfgModel').value = cfg.model || '';
  $('cfgTimeout').value = cfg.timeout_sec || 60;
  $('cfgQuota').value = cfg.daily_quota || 50;
  if (data.configured) {
    $('cfgAPIKey').value = cfg.api_key; // 掩码值
    $('keyHint').textContent = '已保存（' + cfg.api_key + '），留空或保持掩码不变则不修改';
  }
}

function collectAIConfig() {
  return {
    base_url: $('cfgBaseURL').value.trim(),
    api_key: $('cfgAPIKey').value.trim(),
    model: $('cfgModel').value.trim(),
    timeout_sec: parseInt($('cfgTimeout').value, 10) || 60,
    daily_quota: parseInt($('cfgQuota').value, 10) || 50,
  };
}

$('saveBtn').addEventListener('click', async () => {
  try {
    const saved = await api('/api/admin/ai-config', { method: 'POST', body: collectAIConfig() });
    $('cfgAPIKey').value = saved.api_key;
    toast('配置已保存');
  } catch (e) {
    toast(e.message, true);
  }
});

$('testBtn').addEventListener('click', async () => {
  const out = $('testResult');
  out.textContent = '测试中……';
  out.style.color = 'var(--text-2)';
  try {
    await api('/api/admin/ai-config/test', { method: 'POST', body: collectAIConfig() });
    out.textContent = '连接成功，模型可用';
    out.style.color = 'var(--primary)';
  } catch (e) {
    out.textContent = '失败：' + e.message;
    out.style.color = 'var(--danger)';
  }
});

/* ---------- 启动 ---------- */

loadAIConfig();
loadOverview();
