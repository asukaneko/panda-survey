// 填答页：/s/{id} 匿名填答；/preview/{id} 登录预览（提交禁用）
// 支持逻辑跳转（条件显隐）与分页（按「此后分页」标记分组逐页作答）
import { api, toast, fmtTime } from '/js/api.js';
import { renderFillQuestion } from '/js/renderers.js';

const path = location.pathname;
const isPreview = path.startsWith('/preview/');
const m = path.match(/^\/(?:s|preview)\/(\d+)/);
const surveyID = m ? m[1] : null;
const startAt = Date.now();

let widgets = [];   // [{w, q, visible}]
let pages = [];     // [[widgetIdx...], ...]
let curPage = 0;

async function boot() {
  if (!surveyID) {
    showError('链接无效');
    return;
  }
  let data;
  if (isPreview) {
    try {
      const d = await api('/api/surveys/' + surveyID);
      data = { survey: { title: d.survey.title, description: d.survey.description }, questions: d.questions };
    } catch (e) {
      showError(e.message);
      return;
    }
  } else {
    try {
      data = await api('/api/surveys/' + surveyID + '/public');
    } catch (e) {
      showError(e.message);
      return;
    }
  }
  document.getElementById('sTitle').textContent = data.survey.title || '问卷调查';
  document.getElementById('sDesc').textContent = data.survey.description || '';

  // 渲染全部题目控件（隐藏分页外的）
  const box = document.getElementById('questions');
  box.textContent = '';
  widgets = data.questions.map((q) => {
    const w = renderFillQuestion(q);
    return { w, q, visible: true };
  });

  // 依赖题变化时重算条件显隐
  widgets.forEach((it) => {
    it.w.onChange = () => recalcVisibility();
  });

  // 分页：按 page_break_after 分组
  pages = [];
  let cur = [];
  widgets.forEach((it, i) => {
    cur.push(i);
    if (it.q.config && it.q.config.page_break_after) {
      pages.push(cur);
      cur = [];
    }
  });
  if (cur.length > 0) {
    pages.push(cur);
  }

  widgets.forEach((it) => box.appendChild(it.w.root));
  recalcVisibility();
  renderPage();

  if (isPreview) {
    const actions = document.getElementById('actions');
    actions.textContent = '';
    const note = document.createElement('p');
    note.style.color = 'var(--text-3)';
    note.textContent = '预览模式：此处为填写者看到的最终效果';
    actions.appendChild(note);
  } else {
    document.getElementById('submitBtn').addEventListener('click', submit);
  }
}

// 条件显隐：仅依据依赖题（按题序索引）的当前答案推导
function recalcVisibility() {
  widgets.forEach((it, idx) => {
    const vi = it.q.config && it.q.config.visible_if;
    if (!vi) {
      it.visible = true;
      return;
    }
    const dep = widgets[vi.question_index];
    let vis = false;
    if (dep) {
      const got = dep.w.collect();
      if (got && vi.option_ids.includes(got.value)) {
        vis = true;
      }
    }
    it.visible = vis;
  });
  widgets.forEach((it) => {
    it.w.root.style.display = it.visible ? '' : 'none';
  });
}

function renderPage() {
  const pageSet = new Set(pages[curPage] || []);
  widgets.forEach((it, i) => {
    const inPage = pageSet.has(i);
    it.w.root.style.display = (inPage && it.visible) ? '' : 'none';
  });
  const actions = document.getElementById('actions');
  actions.textContent = '';
  if (pages.length <= 1) {
    if (!isPreview) {
      const btn = document.createElement('button');
      btn.className = 'btn btn-primary btn-lg';
      btn.id = 'submitBtn';
      btn.textContent = '提 交';
      btn.addEventListener('click', submit);
      actions.appendChild(btn);
    }
    return;
  }
  const nav = document.createElement('div');
  nav.className = 'page-nav';
  const prev = document.createElement('button');
  prev.className = 'btn btn-lg';
  prev.textContent = '上一页';
  prev.disabled = curPage === 0;
  prev.addEventListener('click', () => {
    if (validatePage()) {
      curPage--;
      renderPage();
      window.scrollTo({ top: 0 });
    }
  });
  const info = document.createElement('span');
  info.className = 'page-info';
  info.textContent = '第 ' + (curPage + 1) + ' / ' + pages.length + ' 页';
  const next = document.createElement('button');
  next.className = 'btn btn-primary btn-lg';
  next.textContent = curPage === pages.length - 1 ? '提 交' : '下一页';
  next.addEventListener('click', () => {
    if (!validatePage()) {
      return;
    }
    if (curPage === pages.length - 1) {
      submit();
    } else {
      curPage++;
      renderPage();
      window.scrollTo({ top: 0 });
    }
  });
  nav.append(prev, info, next);
  actions.appendChild(nav);
}

function showError(msg) {
  const app = document.getElementById('app');
  app.textContent = '';
  const t = document.createElement('div');
  t.className = 'thanks';
  const h = document.createElement('h2');
  h.textContent = msg || '问卷不可用';
  const p = document.createElement('p');
  p.textContent = '请核对链接是否正确，或联系问卷发起人';
  t.appendChild(h);
  t.appendChild(p);
  app.appendChild(t);
}

// 校验当前页可见题的必答；标红并定位第一处未答
function validatePage() {
  const tipBar = document.getElementById('tipBar');
  tipBar.style.display = 'none';
  const pageSet = new Set(pages[curPage] || widgets.map((_, i) => i));
  let firstMiss = null;
  for (const i of pageSet) {
    const it = widgets[i];
    if (!it.visible) {
      it.w.root.classList.remove('error');
      continue;
    }
    it.w.root.classList.remove('error');
    const got = it.w.collect();
    if (it.q.required && !got) {
      it.w.root.classList.add('error');
      if (!firstMiss) {
        firstMiss = it;
      }
    }
  }
  if (firstMiss) {
    tipBar.textContent = '第 ' + firstMiss.q.sort_order + ' 题为必答，请完成后再继续';
    tipBar.style.display = 'block';
    firstMiss.w.root.scrollIntoView({ behavior: 'smooth', block: 'center' });
    return false;
  }
  return true;
}

async function submit() {
  // 逐页校验（条件隐藏题跳过）
  for (let p = 0; p < pages.length; p++) {
    const saved = curPage;
    curPage = p;
    const pass = validatePage();
    curPage = saved;
    if (!pass) {
      curPage = p;
      renderPage();
      return;
    }
  }
  const answers = [];
  for (const it of widgets) {
    if (!it.visible) {
      continue;
    }
    const got = it.w.collect();
    if (got) {
      answers.push(got);
    }
  }
  const btn = document.getElementById('submitBtn');
  if (btn) {
    btn.disabled = true;
    btn.textContent = '提交中……';
  }
  try {
    await api('/api/surveys/' + surveyID + '/responses', {
      method: 'POST',
      body: { answers, duration: Math.round((Date.now() - startAt) / 1000) },
    });
    document.getElementById('app').style.display = 'none';
    document.getElementById('thanks').style.display = '';
    window.scrollTo(0, 0);
  } catch (e) {
    if (btn) {
      btn.disabled = false;
      btn.textContent = '提 交';
    }
    toast(e.message, true);
    if (e.code === 1004) {
      showError('问卷已停止回收');
    }
  }
}

boot();
