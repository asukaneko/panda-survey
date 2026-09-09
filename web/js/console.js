// 管理端工作台：问卷列表 + 编辑器 + 发布/分享 + 统计标签页（含答题卷编辑）
import { api, toast, fmtTime } from '/js/api.js';
import { TYPES, QUIZ_TYPES, TYPE_LABEL, newQuestion, newQuizQuestion, renderEditQuestion, renderFillQuestion } from '/js/renderers.js';
import { initAI } from '/js/ai.js';
import { renderStatsView } from '/js/statsview.js';

const state = {
  me: null,
  surveys: [],
  current: null,   // {id,title,description,status,kind,quiz_config,updated_at,...}
  questions: [],   // [{id,type,title,required,config}]
  dirty: false,
  activeTab: 'design', // design | stats
  ai: { enabled: false, usedToday: 0, quota: 50 },
};

const $ = (id) => document.getElementById(id);

function isQuiz() {
  return !!state.current && state.current.kind === 1;
}

/* ---------- 顶栏 ---------- */

function renderTopActions() {
  const box = $('topActions');
  box.textContent = '';
  if (!state.current) {
    return;
  }
  const st = state.current.status;
  const mk = (txt, cls, fn, disabled) => {
    const b = document.createElement('button');
    b.className = 'btn btn-sm ' + (cls || '');
    b.textContent = txt;
    b.disabled = !!disabled;
    b.addEventListener('click', fn);
    return b;
  };
  // 任意状态（草稿 / 发布中 / 已停止）都可直接保存
  box.appendChild(mk('保存', 'btn-primary', saveSurvey, !state.dirty));
  if (st === 0) {
    box.appendChild(mk('发布', '', openPublish));
  } else if (st === 1) {
    box.appendChild(mk('停止', 'btn-danger', stopSurvey));
  } else {
    box.appendChild(mk('重新发布', '', openPublish));
  }
  box.appendChild(mk('预览', '', () => window.open('/preview/' + state.current.id, '_blank')));
  if (st === 1) {
    box.appendChild(mk('分享', 'btn-primary', openShare));
  }
  box.appendChild(mk('复制', '', copySurvey));
  box.appendChild(mk('删除', 'btn-danger', deleteSurvey));
}

/* ---------- 侧栏列表 ---------- */

function statusBadge(st) {
  const names = { 0: '未发布', 1: '已发布', 2: '已停止' };
  const cls = { 0: 'badge-draft', 1: 'badge-published', 2: 'badge-stopped' };
  const b = document.createElement('span');
  b.className = 'badge ' + cls[st];
  b.textContent = names[st];
  return b;
}

function kindTag(s) {
  if (s.kind !== 1) {
    return null;
  }
  const t = document.createElement('span');
  t.className = 'quiz-tag';
  t.textContent = '答题';
  return t;
}

async function loadList(selectId) {
  state.surveys = await api('/api/surveys');
  const box = $('surveyList');
  box.textContent = '';
  state.surveys.forEach((s) => {
    const item = document.createElement('div');
    item.className = 'survey-item' + (state.current && s.id === state.current.id ? ' active' : '');
    const name = document.createElement('span');
    name.className = 's-name';
    name.textContent = s.title || '未命名问卷';
    name.title = s.title;
    item.appendChild(name);
    const tag = kindTag(s);
    if (tag) {
      item.appendChild(tag);
    }
    item.appendChild(statusBadge(s.status));
    item.addEventListener('click', async () => {
      closeDrawer();
      await selectSurvey(s.id);
    });
    box.appendChild(item);
  });
  if (selectId) {
    await selectSurvey(selectId);
  } else if (!state.current && state.surveys.length > 0) {
    await selectSurvey(state.surveys[0].id);
  } else if (!state.current) {
    showEmpty();
  }
}

function refreshSidebarActive() {
  document.querySelectorAll('.survey-item').forEach((el, i) => {
    el.classList.toggle('active', !!state.current && state.surveys[i] && state.surveys[i].id === state.current.id);
  });
}

/* ---------- 编辑器 ---------- */

/* ---------- 主区视图切换（编辑器 / 统计 / 空提示互斥） ---------- */

function renderMain() {
  const has = !!state.current;
  $('emptyHint').style.display = has ? 'none' : '';
  const showEditor = has && state.activeTab === 'design';
  const showStats = has && state.activeTab === 'stats';
  $('editor').style.display = showEditor ? '' : 'none';
  $('statsArea').style.display = showStats ? '' : 'none';
  $('quizSettings').style.display = (showEditor && isQuiz()) ? '' : 'none';
  if (has && state.activeTab === 'design') {
    renderQuizSettings();
  }
  if (!has && state.activeTab !== 'design') {
    setTab('design');
  }
}

function setTab(tab) {
  state.activeTab = tab;
  $('tabDesign').classList.toggle('active', tab === 'design');
  $('tabStats').classList.toggle('active', tab === 'stats');
  renderMain();
  if (tab === 'stats' && state.current) {
    renderStatsView($('statsArea'), { sid: state.current.id, aiEnabled: state.ai.enabled });
  }
}

function showEmpty() {
  renderMain();
  renderTopActions();
}

async function selectSurvey(id) {
  if (state.dirty && !confirm('当前问卷未保存，切换将丢失修改，确定继续？')) {
    return;
  }
  const data = await api('/api/surveys/' + id);
  state.current = data.survey;
  state.questions = (data.questions || []).map((q) => ({
    id: q.id, type: q.type, title: q.title, required: q.required, config: q.config || {},
  }));
  state.dirty = false;
  renderMain();
  bindHead();
  renderQuestions();
  renderAddBar();
  renderTopActions();
  refreshSidebarActive();
  if (state.activeTab === 'stats') {
    renderStatsView($('statsArea'), { sid: state.current.id, aiEnabled: state.ai.enabled });
  }
  const idx = state.surveys.findIndex((s) => s.id === id);
  if (idx >= 0) {
    state.surveys[idx] = { ...state.surveys[idx], ...state.current, response_count: state.surveys[idx].response_count };
    // 状态可能变化，重绘侧栏徽标
    loadListKeepCurrent();
  }
}

// 仅重绘侧栏（不重新选中）
async function loadListKeepCurrent() {
  const cur = state.current && state.current.id;
  state.surveys = await api('/api/surveys');
  const box = $('surveyList');
  box.textContent = '';
  state.surveys.forEach((s) => {
    const item = document.createElement('div');
    item.className = 'survey-item' + (cur === s.id ? ' active' : '');
    const name = document.createElement('span');
    name.className = 's-name';
    name.textContent = s.title || '未命名问卷';
    item.appendChild(name);
    const tag = kindTag(s);
    if (tag) {
      item.appendChild(tag);
    }
    item.appendChild(statusBadge(s.status));
    item.addEventListener('click', () => selectSurvey(s.id));
    box.appendChild(item);
  });
}

function bindHead() {
  const t = $('sTitle');
  const d = $('sDesc');
  t.value = state.current.title;
  d.value = state.current.description;
  t.oninput = () => { state.current.title = t.value; markDirty(); };
  d.oninput = () => { state.current.description = d.value; markDirty(); };
}

function markDirty() {
  state.dirty = true;
  renderTopActions();
}

function renderQuestions() {
  const box = $('questionList');
  box.textContent = '';
  const quiz = isQuiz();
  const handlers = {
    onChange: markDirty,
    onStruct: quiz ? () => { markDirty(); renderQuestions(); } : null,
    onMove: (idx, dir) => {
      const to = idx + dir;
      if (to < 0 || to >= state.questions.length) {
        return;
      }
      const arr = state.questions;
      [arr[idx], arr[to]] = [arr[to], arr[idx]];
      markDirty();
      renderQuestions();
    },
    onDelete: (idx) => {
      if (!confirm('确定删除第 ' + (idx + 1) + ' 题？')) {
        return;
      }
      state.questions.splice(idx, 1);
      markDirty();
      renderQuestions();
    },
    onAI: state.ai.enabled ? optimizeQuestionAt : null,
  };
  state.questions.forEach((q, i) => {
    box.appendChild(renderEditQuestion(q, i, handlers, state.questions, { quiz }));
  });
}

function renderAddBar() {
  const bar = $('addBar');
  bar.textContent = '';
  const types = isQuiz() ? QUIZ_TYPES : TYPES;
  types.forEach((t) => {
    const b = document.createElement('button');
    b.className = 'btn btn-sm';
    b.textContent = '+ ' + t.label;
    b.addEventListener('click', () => {
      if (!state.current) {
        return;
      }
      state.questions.push(isQuiz() ? newQuizQuestion(t.type) : newQuestion(t.type));
      markDirty();
      renderQuestions();
      const cards = document.querySelectorAll('.q-card');
      if (cards.length > 0) {
        cards[cards.length - 1].scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    });
    bar.appendChild(b);
  });
}

/* ---------- 答题设置 ---------- */

// 归一化答题配置（缺失字段补默认值）
function quizCfg() {
  if (!state.current.quiz_config || typeof state.current.quiz_config !== 'object') {
    state.current.quiz_config = {};
  }
  const c = state.current.quiz_config;
  if (!c.display_mode) {
    c.display_mode = 'list';
  }
  if (!c.question_order) {
    c.question_order = 'sequential';
  }
  if (!Array.isArray(c.profile_fields)) {
    c.profile_fields = [];
  }
  return c;
}

function renderQuizSettings() {
  const box = $('quizSettings');
  if (!isQuiz()) {
    box.textContent = '';
    box.style.display = 'none';
    return;
  }
  const c = quizCfg();
  box.textContent = '';
  box.style.display = '';

  const card = document.createElement('div');
  card.className = 'card quiz-settings-card';

  const head = document.createElement('div');
  head.className = 'quiz-settings-head';
  const h = document.createElement('h4');
  h.textContent = '答题设置';
  head.appendChild(h);
  const pickBank = document.createElement('button');
  pickBank.className = 'btn btn-primary btn-sm';
  pickBank.textContent = '从题库选题';
  pickBank.addEventListener('click', openBankModal);
  head.appendChild(pickBank);
  card.appendChild(head);

  const meta = document.createElement('div');
  meta.className = 'q-meta';
  meta.style.flexWrap = 'wrap';

  // 倒计时
  const durLab = document.createElement('label');
  durLab.style.display = 'flex';
  durLab.style.alignItems = 'center';
  durLab.style.gap = '4px';
  durLab.appendChild(document.createTextNode('倒计时'));
  const dur = document.createElement('input');
  dur.type = 'number';
  dur.className = 'num';
  dur.min = 0;
  dur.max = 600;
  dur.style.width = '70px';
  dur.value = c.duration_min || 0;
  dur.title = '0 为不限时';
  dur.addEventListener('change', () => {
    let v = parseInt(dur.value, 10);
    if (isNaN(v) || v < 0) {
      v = 0;
    }
    if (v > 600) {
      v = 600;
    }
    dur.value = v;
    c.duration_min = v;
    markDirty();
  });
  durLab.appendChild(dur);
  durLab.appendChild(document.createTextNode('分钟（0 不限时）'));
  meta.appendChild(durLab);

  // 展示方式（互斥）：列表 / 分页
  const mkRadio = (name, value, checked, text, onSet) => {
    const lab = document.createElement('label');
    lab.style.cssText = 'display:flex;align-items:center;gap:4px';
    const input = document.createElement('input');
    input.type = 'radio';
    input.name = name;
    input.value = value;
    input.checked = checked;
    input.addEventListener('change', () => {
      if (input.checked) {
        onSet(input.value);
        markDirty();
      }
    });
    lab.appendChild(input);
    lab.appendChild(document.createTextNode(text));
    return lab;
  };
  const modeGroup = elDiv('');
  modeGroup.style.cssText = 'display:flex;gap:10px;align-items:center';
  modeGroup.appendChild(document.createTextNode('展示方式'));
  modeGroup.appendChild(mkRadio('quizMode', 'list', c.display_mode === 'list', '列表展示', (v) => { c.display_mode = v; }));
  modeGroup.appendChild(mkRadio('quizMode', 'paged', c.display_mode === 'paged', '分页展示（一题一页）', (v) => { c.display_mode = v; }));
  meta.appendChild(modeGroup);

  const orderGroup = elDiv('');
  orderGroup.style.cssText = 'display:flex;gap:10px;align-items:center';
  orderGroup.appendChild(document.createTextNode('题目顺序'));
  orderGroup.appendChild(mkRadio('quizOrder', 'sequential', c.question_order === 'sequential', '顺序', (v) => { c.question_order = v; }));
  orderGroup.appendChild(mkRadio('quizOrder', 'random', c.question_order === 'random', '随机', (v) => { c.question_order = v; }));
  meta.appendChild(orderGroup);

  const mkCheck = (checked, text, onSet) => {
    const lab = document.createElement('label');
    lab.style.cssText = 'display:flex;align-items:center;gap:4px';
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!checked;
    input.addEventListener('change', () => {
      onSet(input.checked);
      markDirty();
    });
    lab.appendChild(input);
    lab.appendChild(document.createTextNode(text));
    return lab;
  };
  meta.appendChild(mkCheck(c.show_answer, '提交后展示答案', (v) => { c.show_answer = v; }));
  meta.appendChild(mkCheck(c.show_ranking, '答题后可查看排行', (v) => { c.show_ranking = v; }));
  meta.appendChild(mkCheck(c.collect_profile, '答题前填写个人信息', (v) => {
    c.collect_profile = v;
    if (v && c.profile_fields.length === 0) {
      c.profile_fields.push({ label: '姓名', required: true }, { label: '手机号', required: false });
    }
    markDirty();
    renderQuizSettings();
  }));
  card.appendChild(meta);

  // 个人信息字段编辑
  if (c.collect_profile) {
    const pf = elDiv('');
    pf.style.cssText = 'margin-top:8px;padding:8px 10px;border:1px dashed var(--border);border-radius:6px';
    const pfTitle = elDiv('');
    pfTitle.style.cssText = 'font-size:13px;color:var(--text-2);margin-bottom:6px';
    pfTitle.textContent = '个人信息字段（最多 5 个）：';
    pf.appendChild(pfTitle);
    c.profile_fields.forEach((f, i) => {
      const row = elDiv('opt-row');
      row.style.paddingLeft = '0';
      const labelInput = document.createElement('input');
      labelInput.type = 'text';
      labelInput.className = 'input';
      labelInput.style.maxWidth = '180px';
      labelInput.value = f.label;
      labelInput.placeholder = '字段名，如 姓名';
      labelInput.addEventListener('input', () => {
        f.label = labelInput.value;
        markDirty();
      });
      row.appendChild(labelInput);
      row.appendChild(mkCheck(f.required, '必填', (v) => { f.required = v; }));
      const del = document.createElement('button');
      del.className = 'btn btn-ghost btn-sm';
      del.type = 'button';
      del.textContent = '删除';
      del.addEventListener('click', () => {
        c.profile_fields.splice(i, 1);
        markDirty();
        renderQuizSettings();
      });
      row.appendChild(del);
      pf.appendChild(row);
    });
    if (c.profile_fields.length < 5) {
      const add = document.createElement('button');
      add.className = 'btn btn-sm';
      add.type = 'button';
      add.textContent = '+ 添加字段';
      add.style.marginTop = '6px';
      add.addEventListener('click', () => {
        c.profile_fields.push({ label: '', required: false });
        markDirty();
        renderQuizSettings();
      });
      pf.appendChild(add);
    }
    card.appendChild(pf);
  }

  box.appendChild(card);
}

function elDiv(cls) {
  const d = document.createElement('div');
  if (cls) {
    d.className = cls;
  }
  return d;
}

/* ---------- 从题库选题 ---------- */

let bankQuestionsCache = []; // 当前所选题库的题目

async function openBankModal() {
  const sel = $('bankSelect');
  const list = $('bankQuestionList');
  list.innerHTML = '';
  list.appendChild(Object.assign(document.createElement('p'), {
    textContent: '加载中……', style: 'color:var(--text-2);font-size:13px',
  }));
  document.querySelectorAll('input[name="bankOrder"]').forEach((r) => {
    r.checked = r.value === quizCfg().question_order;
  });
  $('bankModal').classList.add('open');
  try {
    const banks = await api('/api/banks');
    sel.textContent = '';
    if (banks.length === 0) {
      list.innerHTML = '';
      list.appendChild(Object.assign(document.createElement('p'), {
        textContent: '还没有题库，请先到「题库管理」创建并录入题目',
        style: 'color:var(--text-2);font-size:13px',
      }));
      return;
    }
    banks.forEach((b) => {
      const opt = document.createElement('option');
      opt.value = b.id;
      opt.textContent = b.name + '（' + b.question_count + ' 题）';
      sel.appendChild(opt);
    });
    await loadBankQuestions(parseInt(sel.value, 10));
  } catch (e) {
    list.innerHTML = '';
    list.appendChild(Object.assign(document.createElement('p'), { textContent: e.message }));
  }
}

async function loadBankQuestions(bankId) {
  const list = $('bankQuestionList');
  bankQuestionsCache = [];
  list.innerHTML = '';
  list.appendChild(Object.assign(document.createElement('p'), {
    textContent: '加载中……', style: 'color:var(--text-2);font-size:13px',
  }));
  try {
    const data = await api('/api/banks/' + bankId + '/questions');
    bankQuestionsCache = data.questions || [];
    list.innerHTML = '';
    if (bankQuestionsCache.length === 0) {
      list.appendChild(Object.assign(document.createElement('p'), {
        textContent: '该题库暂无题目', style: 'color:var(--text-2);font-size:13px',
      }));
      return;
    }
    bankQuestionsCache.forEach((q) => {
      const lab = document.createElement('label');
      lab.className = 'bank-q-row';
      const cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.value = q.id;
      lab.appendChild(cb);
      const info = document.createElement('span');
      info.textContent = TYPE_LABEL[q.type] + ' · ' + (q.config.score || 1) + ' 分 · ' + q.title;
      lab.appendChild(info);
      list.appendChild(lab);
    });
  } catch (e) {
    list.innerHTML = '';
    list.appendChild(Object.assign(document.createElement('p'), { textContent: e.message }));
  }
}

function importFromBank() {
  const sel = $('bankSelect');
  const bankId = parseInt(sel.value, 10);
  const chosen = bankQuestionsCache.filter((q) => {
    const cb = document.querySelector('#bankQuestionList input[value="' + q.id + '"]');
    return cb && cb.checked;
  });
  if (chosen.length === 0) {
    toast('请先勾选题目', true);
    return;
  }
  const order = document.querySelector('input[name="bankOrder"]:checked');
  if (order) {
    quizCfg().question_order = order.value;
  }
  chosen.forEach((q) => {
    // 复制进答题卷：id 置 0 表示新题，config 深拷贝避免与题库联动
    state.questions.push({
      id: 0,
      type: q.type,
      title: q.title,
      required: false,
      config: JSON.parse(JSON.stringify(q.config || {})),
      _fromBank: bankId,
    });
  });
  markDirty();
  renderQuizSettings();
  renderQuestions();
  $('bankModal').classList.remove('open');
  toast('已导入 ' + chosen.length + ' 道题，记得保存');
}

/* ---------- 保存 / 状态操作 ---------- */

async function saveSurvey() {
  if (!state.current) {
    return;
  }
  const body = {
    title: state.current.title,
    description: state.current.description,
    updated_at: state.current.updated_at,
    questions: state.questions,
  };
  if (isQuiz()) {
    body.quiz_config = quizCfg();
  }
  try {
    const s = await api('/api/surveys/' + state.current.id, { method: 'PUT', body });
    state.current = { ...state.current, ...s };
    state.dirty = false;
    toast('已保存');
    renderTopActions();
    loadListKeepCurrent();
  } catch (e) {
    if (e.code === 1007) {
      if (confirm(e.message + '。点击确定刷新为最新版本（本地修改将丢失）')) {
        await selectSurvey(state.current.id);
      }
      return;
    }
    if (e.code === 1004) {
      toast(e.message, true);
      loadListKeepCurrent();
      return;
    }
    toast(e.message, true);
  }
}

function openPublish() {
  if (state.dirty) {
    toast('请先保存修改再发布', true);
    return;
  }
  $('publishModal').classList.add('open');
}

async function confirmPublish() {
  try {
    const body = {};
    if ($('pubDeadline').value) {
      body.deadline = $('pubDeadline').value;
    }
    if ($('pubMax').value) {
      body.max_responses = parseInt($('pubMax').value, 10);
    }
    const s = await api('/api/surveys/' + state.current.id + '/publish', { method: 'POST', body });
    state.current = { ...state.current, ...s };
    $('publishModal').classList.remove('open');
    toast('问卷已发布，可以分享了');
    renderTopActions();
    loadListKeepCurrent();
  } catch (e) {
    toast(e.message, true);
  }
}

async function stopSurvey() {
  if (!confirm('停止后填写者将无法提交，历史数据保留。确定停止？')) {
    return;
  }
  try {
    const s = await api('/api/surveys/' + state.current.id + '/stop', { method: 'POST', body: {} });
    state.current = { ...state.current, ...s };
    toast('问卷已停止');
    renderTopActions();
    loadListKeepCurrent();
  } catch (e) {
    toast(e.message, true);
  }
}

async function copySurvey() {
  try {
    const s = await api('/api/surveys/' + state.current.id + '/copy', { method: 'POST', body: {} });
    toast('已复制为新草稿');
    state.dirty = false;
    await loadList(s.id);
  } catch (e) {
    toast(e.message, true);
  }
}

async function deleteSurvey() {
  if (!confirm('删除后问卷进入回收状态，无法恢复。确定删除「' + (state.current.title || '未命名') + '」？')) {
    return;
  }
  try {
    await api('/api/surveys/' + state.current.id, { method: 'DELETE' });
    state.current = null;
    state.dirty = false;
    toast('已删除');
    await loadList();
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 分享 ---------- */

function shareURL() {
  return location.origin + '/s/' + state.current.id;
}

function openShare() {
  $('shareLink').value = shareURL();
  const box = $('qrBox');
  box.textContent = '';
  if (typeof QRCode !== 'undefined') {
    const holder = document.createElement('div');
    holder.style.background = '#fff';
    holder.style.padding = '8px';
    box.appendChild(holder);
    try {
      new QRCode(holder, { text: shareURL(), width: 160, height: 160, correctLevel: QRCode.CorrectLevel.M });
    } catch (e) {
      box.textContent = '二维码生成失败，请复制链接分享';
    }
  } else {
    box.textContent = '二维码组件未安装，请复制链接分享';
  }
  // 统计只读分享：默认收起，点生成时再开通
  $('shareStatsRow').style.display = 'none';
  $('closeStatsShareBtn').style.display = 'none';
  $('genStatsShareBtn').style.display = '';
  $('shareModal').classList.add('open');
}

/* ---------- AI 优化单题 ---------- */

async function optimizeQuestionAt(idx) {
  const q = state.questions[idx];
  $('aiOptBody').textContent = '正在请求 AI 优化第 ' + (idx + 1) + ' 题……';
  $('aiOptApply').style.display = 'none';
  $('aiOptModal').classList.add('open');
  try {
    const res = await api('/api/ai/optimize-question', {
      method: 'POST',
      body: { question: q },
    });
    const body = $('aiOptBody');
    body.textContent = '';
    const mkLine = (tag, text) => {
      const d = document.createElement('div');
      d.style.marginBottom = '10px';
      d.appendChild(Object.assign(document.createElement('b'), { textContent: tag + '：' }));
      d.appendChild(document.createTextNode(text || '（空）'));
      return d;
    };
    body.appendChild(mkLine('原题干', res.original.title));
    body.appendChild(mkLine('优化后', res.optimized.title));
    if (res.reason) {
      body.appendChild(mkLine('说明', res.reason));
    }
    if ((res.optimized.config.options || []).length > 0) {
      const ul = document.createElement('div');
      ul.style.cssText = 'color:var(--text-2);font-size:13px';
      ul.textContent = '选项：' + res.optimized.config.options.map((o) => o.label).join(' / ');
      body.appendChild(ul);
    }
    $('aiOptApply').onclick = () => {
      // 保留原题目 id，保存时原地更新，历史答卷关联不失效
      state.questions[idx] = { ...res.optimized, id: q.id };
      markDirty();
      renderQuestions();
      $('aiOptModal').classList.remove('open');
      toast('已应用优化，记得保存');
    };
    $('aiOptApply').style.display = '';
  } catch (e) {
    $('aiOptBody').textContent = e.message;
  }
}

/* ---------- 抽屉（移动端侧栏） ---------- */

function closeDrawer() {
  $('sidebar').classList.remove('drawer-open');
  $('drawerMask').classList.remove('open');
}

function openDrawer() {
  $('sidebar').classList.add('drawer-open');
  $('drawerMask').classList.add('open');
}

// 旋转 / 拉宽到桌面尺寸时自动收起抽屉
window.matchMedia('(min-width: 768px)').addEventListener('change', (e) => {
  if (e.matches) {
    closeDrawer();
  }
});

/* ---------- 启动 ---------- */

async function boot() {
  renderAddBar();
  try {
    state.me = await api('/api/auth/me');
  } catch (e) {
    return; // 401 已跳登录
  }
  $('userName').textContent = state.me.username;
  if (state.me.role === 1) {
    $('adminLink').style.display = '';
  }
  try {
    const st = await api('/api/ai/status');
    state.ai.enabled = !!st.enabled;
    state.ai.usedToday = st.used_today;
    state.ai.quota = st.quota;
  } catch (e) {
    state.ai.enabled = false;
  }
  initAI(state, { reload: selectSurvey, markDirty, renderQuestions, renderTopActions });
  await loadList();
}

$('newBtn').addEventListener('click', async () => {
  try {
    const s = await api('/api/surveys', { method: 'POST', body: { title: '未命名问卷', description: '', kind: 0 } });
    closeDrawer();
    await loadList(s.id);
  } catch (e) {
    toast(e.message, true);
  }
});
$('newQuizBtn').addEventListener('click', async () => {
  try {
    const s = await api('/api/surveys', { method: 'POST', body: { title: '未命名答题', description: '', kind: 1 } });
    closeDrawer();
    await loadList(s.id);
  } catch (e) {
    toast(e.message, true);
  }
});
$('banksBtn').addEventListener('click', () => {
  location.href = '/banks';
});
$('bankSelect').addEventListener('change', () => {
  loadBankQuestions(parseInt($('bankSelect').value, 10));
});
$('bankCancel').addEventListener('click', () => $('bankModal').classList.remove('open'));
$('bankImport').addEventListener('click', importFromBank);
$('logoutBtn').addEventListener('click', async () => {
  await api('/api/auth/logout', { method: 'POST', body: {} }).catch(() => {});
  location.href = '/login';
});
$('menuBtn').addEventListener('click', () => {
  if ($('drawerMask').classList.contains('open')) {
    closeDrawer();
  } else {
    openDrawer();
  }
});
$('drawerMask').addEventListener('click', closeDrawer);
$('tabDesign').addEventListener('click', () => setTab('design'));
$('tabStats').addEventListener('click', () => {
  if (!state.current) {
    toast('请先选择一份问卷', true);
    return;
  }
  if (state.dirty) {
    toast('有未保存的修改，请先保存', true);
    return;
  }
  setTab('stats');
});
$('pubCancel').addEventListener('click', () => $('publishModal').classList.remove('open'));
$('pubConfirm').addEventListener('click', confirmPublish);
$('shareClose').addEventListener('click', () => $('shareModal').classList.remove('open'));
$('copyLinkBtn').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(shareURL());
    toast('链接已复制');
  } catch (e) {
    $('shareLink').select();
    document.execCommand('copy');
    toast('链接已复制');
  }
});
$('genStatsShareBtn').addEventListener('click', async () => {
  try {
    const res = await api('/api/surveys/' + state.current.id + '/share-stats', { method: 'POST', body: {} });
    $('shareStatsLink').value = location.origin + res.path;
    $('shareStatsRow').style.display = '';
    $('closeStatsShareBtn').style.display = '';
    $('genStatsShareBtn').style.display = '';
    toast('统计只读链接已生成');
  } catch (e) {
    toast(e.message, true);
  }
});
$('copyStatsLinkBtn').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText($('shareStatsLink').value);
    toast('链接已复制');
  } catch (e) {
    $('shareStatsLink').select();
    document.execCommand('copy');
    toast('链接已复制');
  }
});
$('closeStatsShareBtn').addEventListener('click', async () => {
  if (!confirm('关闭后已分享的统计链接将立即失效，确定？')) {
    return;
  }
  try {
    await api('/api/surveys/' + state.current.id + '/share-stats', { method: 'DELETE' });
    $('shareStatsRow').style.display = 'none';
    $('closeStatsShareBtn').style.display = 'none';
    toast('已关闭统计分享');
  } catch (e) {
    toast(e.message, true);
  }
});

/* ---------- 从模板创建 ---------- */

$('tplBtn').addEventListener('click', async () => {
  $('tplModal').classList.add('open');
  const box = $('tplList');
  box.textContent = '加载中……';
  try {
    const list = await api('/api/templates');
    box.textContent = '';
    list.forEach((t) => {
      const card = document.createElement('div');
      card.style.cssText = 'border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:10px';
      const name = document.createElement('b');
      name.textContent = t.name;
      card.appendChild(name);
      const desc = document.createElement('div');
      desc.style.cssText = 'color:var(--text-2);font-size:13px;margin:4px 0';
      desc.textContent = t.description + ' · ' + t.question_count + ' 题';
      card.appendChild(desc);
      const use = document.createElement('button');
      use.className = 'btn btn-primary btn-sm';
      use.textContent = '使用此模板';
      use.addEventListener('click', async () => {
        try {
          const s = await api('/api/surveys/from-template', {
            method: 'POST',
            body: { template_id: t.id },
          });
          $('tplModal').classList.remove('open');
          toast('已从模板创建草稿');
          closeDrawer();
          await loadList(s.id);
        } catch (e) {
          toast(e.message, true);
        }
      });
      card.appendChild(use);
      box.appendChild(card);
    });
  } catch (e) {
    box.textContent = e.message;
  }
});
$('tplClose').addEventListener('click', () => $('tplModal').classList.remove('open'));
$('aiOptClose').addEventListener('click', () => $('aiOptModal').classList.remove('open'));
document.querySelectorAll('.modal-mask').forEach((m) => {
  m.addEventListener('click', (ev) => {
    if (ev.target === m && m.id !== 'drawerMask') {
      m.classList.remove('open');
    }
  });
});
document.addEventListener('keydown', (ev) => {
  if ((ev.ctrlKey || ev.metaKey) && ev.key === 's') {
    ev.preventDefault();
    if (state.current && state.current.status === 0) {
      saveSurvey();
    }
  }
});
window.addEventListener('beforeunload', (ev) => {
  if (state.dirty) {
    ev.preventDefault();
    ev.returnValue = '';
  }
});

boot();
