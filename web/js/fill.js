// 填答页：/s/{id} 匿名填答；/preview/{id} 登录预览（提交禁用）
// 普通问卷：逻辑跳转（条件显隐）+ 分页（按「此后分页」标记逐页作答）
// 答题卷（kind=1）：个人信息（可选）→ 倒计时 → 列表/分页作答 → 成绩 + 答案回顾（可选）+ 排行榜（可选）
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

// ---- 答题卷状态 ----
let quiz = null; // {cfg, cur, total, left, timerId, userAnswers}

async function boot() {
  if (!surveyID) {
    showError('链接无效');
    return;
  }
  let data;
  if (isPreview) {
    try {
      const d = await api('/api/surveys/' + surveyID);
      data = { survey: { title: d.survey.title, description: d.survey.description, kind: d.survey.kind, quiz_config: d.survey.quiz_config }, questions: d.questions };
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

  if ((data.survey.kind === 1 || data.survey.kind === '1')) {
    bootQuiz(data);
    return;
  }
  bootSurvey(data);
}

/* ==================== 普通问卷 ==================== */

function bootSurvey(data) {
  const box = document.getElementById('questions');
  box.textContent = '';
  widgets = data.questions.map((q) => {
    const w = renderFillQuestion(q);
    return { w, q, visible: true };
  });

  widgets.forEach((it) => {
    it.w.onChange = () => recalcVisibility();
  });

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
    const note = document.createElement('p');
    note.style.cssText = 'color:var(--text-3);font-size:13px;margin:10px 0';
    note.textContent = '预览模式：此处为填写者看到的最终效果';
    box.parentNode.insertBefore(note, box);
  } else {
    const sb = document.getElementById('submitBtn');
    if (sb) {
      sb.addEventListener('click', submit);
    }
  }
}

// 条件显隐：仅依据依赖题（按题序索引）的当前答案推导。
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
  applyDisplay();
}

function applyDisplay() {
  const pageSet = new Set(pages[curPage] || widgets.map((_, i) => i));
  widgets.forEach((it, i) => {
    const inPage = pageSet.has(i);
    it.w.root.style.display = (inPage && it.visible) ? '' : 'none';
  });
}

function renderPage() {
  applyDisplay();
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
  next.textContent = curPage === pages.length - 1 ? (isPreview ? '结 束' : '提 交') : '下一页';
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
  if (isPreview) {
    toast('预览模式不可提交，请返回设计页', true);
    return;
  }
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

/* ==================== 答题卷 ==================== */

function bootQuiz(data) {
  const cfg = data.survey.quiz_config || {};
  const box = document.getElementById('questions');
  box.textContent = '';
  widgets = data.questions.map((q) => {
    const w = renderFillQuestion(q);
    return { w, q, visible: true };
  });
  let total = 0;
  widgets.forEach((it) => {
    total += (it.q.config && it.q.config.score > 0) ? it.q.config.score : 1;
  });
  quiz = {
    cfg,
    total,
    cur: 0,
    left: (cfg.duration_min || 0) * 60,
    timerId: null,
    userAnswers: null,
  };
  widgets.forEach((it) => {
    it.w.onChange = () => updateQuizSheet();
  });
  renderQuizIntro();
}

function renderQuizIntro() {
  document.getElementById('questions').textContent = '';
  document.getElementById('actions').textContent = '';
  hideQuizSide(); // 未开始答题时不显示答题卡侧栏
  const intro = document.getElementById('quizIntro');
  intro.style.display = '';
  intro.textContent = '';

  const cfg = quiz.cfg;
  const card = document.createElement('div');
  card.className = 'fill-card quiz-intro-card';

  const h = document.createElement('h3');
  h.textContent = '答题说明';
  card.appendChild(h);

  const meta = document.createElement('div');
  meta.className = 'quiz-meta-row';
  const mkMeta = (num, lbl) => {
    const t = document.createElement('div');
    t.className = 'stat-tile';
    const n = document.createElement('div');
    n.className = 'num';
    n.textContent = num;
    const l = document.createElement('div');
    l.className = 'lbl';
    l.textContent = lbl;
    t.append(n, l);
    return t;
  };
  meta.appendChild(mkMeta(widgets.length, '题目数量'));
  meta.appendChild(mkMeta(quiz.total, '总分'));
  meta.appendChild(mkMeta(cfg.duration_min > 0 ? cfg.duration_min + ' 分钟' : '不限', '限时'));
  card.appendChild(meta);

  if (cfg.show_ranking) {
    const p = document.createElement('p');
    p.className = 'quiz-hint';
    p.textContent = '提交后可查看排行榜';
    card.appendChild(p);
  }

  // 个人信息
  if (cfg.collect_profile && Array.isArray(cfg.profile_fields) && cfg.profile_fields.length > 0) {
    const pfTitle = document.createElement('div');
    pfTitle.className = 'quiz-hint';
    pfTitle.style.marginTop = '14px';
    pfTitle.textContent = '请先填写个人信息';
    card.appendChild(pfTitle);
    quiz.profileInputs = [];
    cfg.profile_fields.forEach((f) => {
      const lab = document.createElement('label');
      lab.className = 'quiz-profile-field';
      const name = document.createElement('span');
      name.textContent = f.label + (f.required ? ' *' : '');
      lab.appendChild(name);
      const input = document.createElement('input');
      input.type = 'text';
      input.className = 'input';
      input.placeholder = '请输入' + f.label;
      lab.appendChild(input);
      card.appendChild(lab);
      quiz.profileInputs.push({ field: f, input });
    });
  } else {
    quiz.profileInputs = [];
  }

  const start = document.createElement('button');
  start.className = 'btn btn-primary btn-lg';
  start.textContent = isPreview ? '开 始（预览）' : '开始答题';
  start.addEventListener('click', () => {
    if (isPreview) {
      startQuiz();
      return;
    }
    if (quiz.profileInputs.some((p) => p.field.required && p.input.value.trim() === '')) {
      toast('请填写必填的个人信息', true);
      return;
    }
    startQuiz();
  });
  card.appendChild(start);
  intro.appendChild(card);
}

function startQuiz() {
  document.getElementById('quizIntro').style.display = 'none';
  const tipBar = document.getElementById('tipBar');
  tipBar.style.display = 'none';
  // 倒计时
  if (quiz.cfg.duration_min > 0 && !isPreview) {
    const bar = document.getElementById('timerBar');
    bar.style.display = '';
    document.body.classList.add('quiz-timed'); // 答题卡吸顶位置让开计时条
    renderTimer();
    quiz.timerId = setInterval(() => {
      quiz.left--;
      renderTimer();
      if (quiz.left <= 0) {
        clearInterval(quiz.timerId);
        quiz.timerId = null;
        toast('时间到，自动交卷');
        submitQuiz(true);
      }
    }, 1000);
  }
  renderQuizQuestions();
}

function renderTimer() {
  const bar = document.getElementById('timerBar');
  bar.textContent = '';
  const label = document.createElement('span');
  label.textContent = '剩余时间：';
  bar.appendChild(label);
  const time = document.createElement('b');
  const mm = String(Math.floor(Math.max(0, quiz.left) / 60)).padStart(2, '0');
  const ss = String(Math.max(0, quiz.left) % 60).padStart(2, '0');
  time.textContent = mm + ':' + ss;
  bar.appendChild(time);
  bar.classList.toggle('urgent', quiz.left <= 60);
}

// 答题卷题目渲染：list 全部展开；paged 一题一页
function renderQuizQuestions() {
  const box = document.getElementById('questions');
  box.textContent = '';
  const actions = document.getElementById('actions');
  actions.textContent = '';

  const isPaged = quiz.cfg.display_mode === 'paged';
  if (isPaged) {
    const perQ = isPerQ(); // 「提交后展示答案」= 逐题提交模式（仅分页 + 非预览）
    const it = widgets[quiz.cur];
    box.appendChild(it.w.root);
    it.w.root.style.display = '';
    if (it.locked) {
      it.w.root.classList.add('locked');
      if (it.feedback) {
        box.appendChild(renderFeedback(it));
      }
    }
    renderQuizSide(); // 侧栏答题卡：竖排题号，作答期间一直显示
    const nav = document.createElement('div');
    nav.className = 'page-nav';
    const prev = document.createElement('button');
    prev.className = 'btn btn-lg';
    prev.textContent = '上一题';
    prev.disabled = quiz.cur === 0;
    prev.addEventListener('click', () => {
      quiz.cur--;
      renderQuizQuestions();
      window.scrollTo({ top: 0 });
    });
    const info = document.createElement('span');
    info.className = 'page-info';
    info.textContent = '第 ' + (quiz.cur + 1) + ' / ' + widgets.length + ' 题';
    const last = quiz.cur === widgets.length - 1;
    const next = document.createElement('button');
    next.className = 'btn btn-primary btn-lg';
    if (perQ) {
      // 逐题提交：必须先「提交本题」才能进入下一题
      next.textContent = '下一题';
      next.disabled = last || !it.locked;
    } else {
      next.textContent = last ? (isPreview ? '结 束' : '交 卷') : '下一题';
    }
    next.addEventListener('click', () => {
      if (perQ) {
        if (!last && it.locked) {
          quiz.cur++;
          renderQuizQuestions();
          window.scrollTo({ top: 0 });
        }
      } else if (last) {
        submitQuiz(false);
      } else {
        quiz.cur++;
        renderQuizQuestions();
        window.scrollTo({ top: 0 });
      }
    });
    nav.append(prev, info, next);
    actions.appendChild(nav);
    // 「提交本题 / 查看成绩」：放在上一题/下一题按钮下方独立一行
    const subRow = document.createElement('div');
    subRow.className = 'quiz-submit-row';
    if (perQ && !it.locked) {
      // 逐题提交按钮：提交本题 -> 立即判分展示答案 -> 锁定不可修改
      const submitQ = document.createElement('button');
      submitQ.className = 'btn btn-primary btn-lg submit-quiz-btn';
      submitQ.textContent = '提交本题';
      submitQ.addEventListener('click', () => submitCurrent());
      subRow.appendChild(submitQ);
    }
    if (perQ && last) {
      if (allLocked()) {
        const finish = document.createElement('button');
        finish.className = 'btn btn-primary btn-lg submit-quiz-btn';
        finish.id = 'submitBtn';
        finish.textContent = '查看成绩';
        finish.addEventListener('click', () => submitQuiz(false));
        subRow.appendChild(finish);
      } else {
        const hint = document.createElement('div');
        hint.className = 'quiz-submit-hint';
        hint.textContent = '还有 ' + widgets.filter((w) => !w.locked).length + ' 题未提交，可点击右侧答题卡跳转补答';
        subRow.appendChild(hint);
      }
    }
    if (subRow.hasChildNodes()) {
      actions.appendChild(subRow);
    }
    return;
  }
  renderQuizSide(); // 列表展示同样常显侧栏答题卡（点击题号滚动定位）
  widgets.forEach((it) => box.appendChild(it.w.root));
  bindQuizScrollSpy();
  if (!isPreview) {
    const btn = document.createElement('button');
    btn.className = 'btn btn-primary btn-lg';
    btn.id = 'submitBtn';
    btn.textContent = '交 卷';
    btn.addEventListener('click', () => submitQuiz(false));
    actions.appendChild(btn);
  }
}

// 逐题提交模式（展示答案）：仅分页展示 + 非预览
function isPerQ() {
  return !isPreview && !!quiz && !!quiz.cfg && !!quiz.cfg.show_answer
    && quiz.cfg.display_mode === 'paged';
}

function allLocked() {
  return widgets.every((it) => it.locked);
}

// 答题卡标记语义：逐题提交模式按「已提交（锁定）」，普通模式按「已作答」
function sheetDone(it) {
  return isPerQ() ? !!it.locked : !!it.w.collect();
}

function sheetMarkText(done) {
  if (isPerQ()) {
    return done ? '（已提交）' : '（未提交）';
  }
  return done ? '（已答）' : '（未答）';
}

function sheetCountText() {
  return widgets.filter(sheetDone).length + '/' + widgets.length;
}

// 逐题提交（提交后展示答案）时该题已判分则返回对错，否则 null
function numVerdict(it) {
  if (isPerQ() && it.locked && it.feedback) {
    return it.feedback.correct;
  }
  return null;
}

// 题号样式：逐题提交模式按本题对错着色（绿=正确、红=错误），其余为已答/已提交
function numClass(it, i) {
  let cls = 'quiz-side-num';
  const verdict = numVerdict(it);
  if (verdict === true) {
    cls += ' ok';
  } else if (verdict === false) {
    cls += ' bad';
  } else if (sheetDone(it)) {
    cls += ' done';
  }
  if (i === quiz.cur) {
    cls += ' cur';
  }
  return cls;
}

function numTitle(it, i) {
  const verdict = numVerdict(it);
  let mark;
  if (verdict === true) {
    mark = '（正确）';
  } else if (verdict === false) {
    mark = '（错误）';
  } else {
    mark = sheetMarkText(sheetDone(it));
  }
  return '第 ' + (i + 1) + ' 题' + mark;
}

// 答题卡（左侧，5 列题号），作答期间一直显示，点击题号跳题
function renderQuizSide() {
  const side = document.getElementById('quizSide');
  if (!side) {
    return;
  }
  side.style.display = '';
  document.body.classList.add('quiz-has-sheet');
  side.textContent = '';

  const top = document.createElement('div');
  top.className = 'quiz-side-top';
  const head = document.createElement('div');
  head.className = 'quiz-side-head';
  head.textContent = '答题卡';
  top.appendChild(head);
  const cnt = document.createElement('div');
  cnt.className = 'quiz-side-count';
  cnt.title = isPerQ() ? '已提交 / 总题数' : '已答 / 总题数';
  cnt.textContent = sheetCountText();
  top.appendChild(cnt);
  side.appendChild(top);

  const nums = document.createElement('div');
  nums.className = 'quiz-side-nums';
  widgets.forEach((it, i) => {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = numClass(it, i);
    b.textContent = i + 1;
    b.title = numTitle(it, i);
    b.addEventListener('click', () => {
      quiz.cur = i;
      if (quiz.cfg.display_mode === 'paged') {
        renderQuizQuestions();
        window.scrollTo({ top: 0 });
      } else {
        // 列表展示：滚动定位到该题（留出倒计时条空间）
        updateQuizSideCurrent();
        const rect = it.w.root.getBoundingClientRect();
        window.scrollTo({ top: Math.max(0, window.scrollY + rect.top - 72), behavior: 'smooth' });
      }
    });
    nums.appendChild(b);
  });
  side.appendChild(nums);

  // 题号多时把当前题号滚动到可视区中间
  const cur = nums.querySelector('.quiz-side-num.cur');
  if (cur && nums.scrollHeight > nums.clientHeight) {
    nums.scrollTop = Math.max(0, cur.offsetTop - (nums.clientHeight - cur.offsetHeight) / 2);
  }
}

function hideQuizSide() {
  const side = document.getElementById('quizSide');
  document.body.classList.remove('quiz-has-sheet');
  document.body.classList.remove('quiz-timed');
  if (side) {
    side.style.display = 'none';
    side.textContent = '';
  }
}

// 仅刷新侧栏「当前题号」高亮
function updateQuizSideCurrent() {
  const nums = document.querySelectorAll('#quizSide .quiz-side-num');
  nums.forEach((b, i) => b.classList.toggle('cur', i === quiz.cur));
}

// 列表展示：按滚动位置高亮侧栏当前题号（rAF 节流）
let spyBound = false;
let spyPending = false;

function bindQuizScrollSpy() {
  if (spyBound) {
    return;
  }
  spyBound = true;
  window.addEventListener('scroll', () => {
    if (spyPending) {
      return;
    }
    spyPending = true;
    requestAnimationFrame(() => {
      spyPending = false;
      listScrollSpy();
    });
  }, { passive: true });
}

function listScrollSpy() {
  if (!quiz || quiz.cfg.display_mode === 'paged' || widgets.length === 0) {
    return;
  }
  const side = document.getElementById('quizSide');
  if (!side || side.style.display === 'none') {
    return;
  }
  let idx = 0;
  widgets.forEach((it, i) => {
    if (it.w.root.getBoundingClientRect().top <= 140) {
      idx = i;
    }
  });
  if (idx !== quiz.cur) {
    quiz.cur = idx;
    updateQuizSideCurrent();
  }
}

// 作答变化时仅刷新答题卡标记，不重建页面（避免打断输入）
function updateQuizSheet() {
  const side = document.getElementById('quizSide');
  if (!side || side.style.display === 'none') {
    return;
  }
  const nums = side.querySelectorAll('.quiz-side-num');
  widgets.forEach((it, i) => {
    const b = nums[i];
    if (b) {
      b.className = numClass(it, i);
      b.title = numTitle(it, i);
    }
  });
  const cnt = side.querySelector('.quiz-side-count');
  if (cnt) {
    cnt.textContent = sheetCountText();
  }
}

// 逐题提交：提交本题 -> 服务端判分 -> 展示正确答案与本题得分 -> 锁定不可修改
async function submitCurrent() {
  if (isPreview) {
    toast('预览模式不可提交，请返回设计页', true);
    return;
  }
  const it = widgets[quiz.cur];
  if (it.locked) {
    return;
  }
  const got = it.w.collect();
  if (!got) {
    toast('请先作答再提交本题', true);
    return;
  }
  // 先锁定，防止提交期间修改（判分返回前不写入 feedback，避免题号闪现错误色）
  const mine = displayAnswer(it.q, got.value);
  it.locked = true;
  it.feedback = null;
  it.w.root.classList.add('locked');
  it.w.root.querySelectorAll('input, select, textarea, button').forEach((el) => {
    el.disabled = true;
  });
  const btn = document.querySelector('#actions button.submit-quiz-btn');
  if (btn) {
    btn.disabled = true;
    btn.textContent = '提交中……';
  }
  try {
    const res = await api('/api/surveys/' + surveyID + '/grade-question', {
      method: 'POST',
      body: { question_id: it.q.id, value: got.value },
    });
    it.feedback = {
      correct: !!res.correct,
      awarded: res.awarded || 0,
      correct_data: res.correct_data,
      mine,
    };
    renderQuizQuestions();
    const fb = document.querySelector('#questions .review-card');
    if (fb) {
      fb.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  } catch (e) {
    it.locked = false;
    it.feedback = null;
    it.w.root.classList.remove('locked');
    it.w.root.querySelectorAll('input, select, textarea, button').forEach((el) => {
      el.disabled = false;
    });
    renderQuizQuestions();
    toast(e.message, true);
  }
}

// 单题反馈卡：✓/✗ + 你的答案 + 正确答案 + 本题得分
function renderFeedback(it) {
  const f = it.feedback;
  const card = document.createElement('div');
  card.className = 'fill-card review-card ' + (f.correct ? 'review-ok' : 'review-bad');
  const head = document.createElement('div');
  head.className = 'f-title';
  const mark = document.createElement('span');
  mark.className = 'review-mark';
  mark.textContent = f.correct ? '✓' : '✗';
  head.appendChild(mark);
  head.appendChild(document.createTextNode('本题答案'));
  card.appendChild(head);
  const row1 = document.createElement('div');
  row1.className = 'review-line';
  row1.appendChild(document.createTextNode('你的答案：'));
  const v1 = document.createElement('b');
  v1.textContent = f.mine || '未作答';
  row1.appendChild(v1);
  card.appendChild(row1);
  const row2 = document.createElement('div');
  row2.className = 'review-line';
  row2.appendChild(document.createTextNode('正确答案：'));
  const v2 = document.createElement('b');
  v2.textContent = displayAnswer(it.q, f.correct_data);
  row2.appendChild(v2);
  card.appendChild(row2);
  const row3 = document.createElement('div');
  row3.className = 'review-line';
  row3.appendChild(document.createTextNode('本题得分：'));
  const v3 = document.createElement('b');
  v3.textContent = f.awarded + ' 分';
  row3.appendChild(v3);
  card.appendChild(row3);
  return card;
}

// 锁定全部作答控件：交卷后不可再修改
function lockQuiz() {
  widgets.forEach((it) => {
    it.w.root.querySelectorAll('input, select, textarea, button').forEach((el) => {
      el.disabled = true;
    });
    it.w.root.classList.add('locked');
  });
}

function unlockQuiz() {
  widgets.forEach((it) => {
    it.w.root.querySelectorAll('input, select, textarea, button').forEach((el) => {
      el.disabled = false;
    });
    it.w.root.classList.remove('locked');
  });
}

function collectQuizAnswers() {
  const answers = [];
  const userAnswers = {};
  widgets.forEach((it) => {
    const got = it.w.collect();
    userAnswers[it.q.id] = got ? got.value : null;
    if (got) {
      answers.push(got);
    }
  });
  quiz.userAnswers = userAnswers;
  return answers;
}

async function submitQuiz(auto) {
  if (isPreview) {
    toast('预览模式不可提交，请返回设计页', true);
    return;
  }
  lockQuiz();
  const btn = document.getElementById('submitBtn') || document.querySelector('#actions button.btn-primary');
  const origText = btn ? btn.textContent : '';
  if (btn) {
    btn.disabled = true;
    btn.textContent = '交卷中……';
  }
  const answers = collectQuizAnswers();
  const profile = (quiz.profileInputs || []).map((p) => ({
    label: p.field.label,
    value: p.input.value.trim(),
  }));
  try {
    const res = await api('/api/surveys/' + surveyID + '/responses', {
      method: 'POST',
      body: { answers, profile, duration: Math.round((Date.now() - startAt) / 1000) },
    });
    if (quiz.timerId) {
      clearInterval(quiz.timerId);
      quiz.timerId = null;
    }
    showQuizResult(res);
  } catch (e) {
    unlockQuiz();
    if (btn) {
      btn.disabled = false;
      btn.textContent = origText;
    }
    toast(e.message, true);
    if (e.code === 1004) {
      showError('答题已停止回收');
    }
  }
}

// 答案值转可读文本（选项 id -> 标签）
function displayAnswer(q, value) {
  if (value === null || value === undefined) {
    return '未作答';
  }
  const cfg = q.config || {};
  if (q.type === 'single_choice' || q.type === 'dropdown') {
    const hit = (cfg.options || []).find((o) => o.id === value);
    return hit ? hit.label : String(value);
  }
  if (q.type === 'multiple_choice') {
    const opts = cfg.options || [];
    const labels = (Array.isArray(value) ? value : []).map((id) => {
      const hit = opts.find((o) => o.id === id);
      return hit ? hit.label : id;
    });
    return labels.length ? labels.join('、') : '未作答';
  }
  if (Array.isArray(value)) {
    return value.filter((s) => String(s).trim() !== '').join(' / ') || '未作答';
  }
  return String(value);
}

function showQuizResult(res) {
  hideQuizSide(); // 出成绩后收起答题卡侧栏
  document.getElementById('app').style.display = 'none';
  document.getElementById('timerBar').style.display = 'none';
  const box = document.getElementById('quizResult');
  box.style.display = '';
  box.textContent = '';

  // 成绩卡
  const scoreCard = document.createElement('div');
  scoreCard.className = 'fill-card quiz-score-card';
  const scoreRow = document.createElement('div');
  scoreRow.className = 'quiz-score-row';
  const num = document.createElement('div');
  num.className = 'quiz-score-num';
  num.textContent = res.score;
  scoreRow.appendChild(num);
  const denom = document.createElement('div');
  denom.className = 'quiz-score-denom';
  denom.textContent = '/ ' + res.total + ' 分';
  scoreRow.appendChild(denom);
  scoreCard.appendChild(scoreRow);
  const sub = document.createElement('p');
  sub.className = 'quiz-hint';
  const pct = res.total > 0 ? Math.round((res.score / res.total) * 100) : 0;
  const praise = pct >= 90 ? '太棒了！' : pct >= 70 ? '表现不错！' : pct >= 50 ? '再接再厉！' : '继续加油！';
  if (res.results && res.results.length > 0) {
    sub.textContent = praise + ' 你答对了 ' + res.results.filter((r) => r.correct).length + ' 题，正确率 ' + pct + '%';
  } else {
    sub.textContent = praise + ' 本场得分率 ' + pct + '%';
  }
  scoreCard.appendChild(sub);
  box.appendChild(scoreCard);

  // 答案回顾（show_answer）
  if (res.results && res.results.length > 0 && quiz.cfg.show_answer) {
    const byID = {};
    res.results.forEach((r) => {
      byID[r.question_id] = r;
    });
    const reviewTitle = document.createElement('h3');
    reviewTitle.className = 'quiz-review-title';
    reviewTitle.textContent = '答案回顾';
    box.appendChild(reviewTitle);
    widgets.forEach((it) => {
      const r = byID[it.q.id];
      if (!r) {
        return;
      }
      const card = document.createElement('div');
      card.className = 'fill-card review-card ' + (r.correct ? 'review-ok' : 'review-bad');
      const head = document.createElement('div');
      head.className = 'f-title';
      const mark = document.createElement('span');
      mark.className = 'review-mark';
      mark.textContent = r.correct ? '✓' : '✗';
      head.appendChild(mark);
      head.appendChild(document.createTextNode(it.q.title));
      card.appendChild(head);
      const myAns = displayAnswer(it.q, quiz.userAnswers ? quiz.userAnswers[it.q.id] : null);
      const row1 = document.createElement('div');
      row1.className = 'review-line';
      row1.appendChild(document.createTextNode('你的答案：'));
      const v1 = document.createElement('b');
      v1.textContent = myAns;
      row1.appendChild(v1);
      card.appendChild(row1);
      const row2 = document.createElement('div');
      row2.className = 'review-line';
      row2.appendChild(document.createTextNode('正确答案：'));
      const v2 = document.createElement('b');
      v2.textContent = displayAnswer(it.q, r.correct_data);
      row2.appendChild(v2);
      card.appendChild(row2);
      if (r.awarded > 0) {
        const row3 = document.createElement('div');
        row3.className = 'review-line';
        row3.appendChild(document.createTextNode('本题得分：'));
        const v3 = document.createElement('b');
        v3.textContent = r.awarded + ' 分';
        row3.appendChild(v3);
        card.appendChild(row3);
      }
      box.appendChild(card);
    });
  } else if (res.score !== undefined) {
    const note = document.createElement('p');
    note.className = 'quiz-hint';
    note.textContent = '本场不展示答案与逐题对错';
    box.appendChild(note);
  }

  // 排行榜（show_ranking）
  if (quiz.cfg.show_ranking) {
    const lbTitle = document.createElement('h3');
    lbTitle.className = 'quiz-review-title';
    lbTitle.textContent = '排行榜';
    box.appendChild(lbTitle);
    const lb = document.createElement('div');
    lb.textContent = '加载中……';
    lb.style.cssText = 'color:var(--text-2);font-size:13px';
    box.appendChild(lb);
    api('/api/surveys/' + surveyID + '/leaderboard').then((data) => {
      const entries = data.entries || [];
      lb.textContent = '';
      if (entries.length === 0) {
        lb.textContent = '暂无排行数据';
        return;
      }
      const table = document.createElement('table');
      table.className = 'table';
      const thead = document.createElement('thead');
      const hr = document.createElement('tr');
      ['排名', '姓名', '得分', '耗时(秒)', '提交时间'].forEach((x) => {
        const th = document.createElement('th');
        th.textContent = x;
        hr.appendChild(th);
      });
      thead.appendChild(hr);
      table.appendChild(thead);
      const tbody = document.createElement('tbody');
      entries.forEach((e) => {
        const tr = document.createElement('tr');
        if (e.response_id === res.response_id) {
          tr.className = 'lb-self';
        }
        [e.rank, e.name, e.score, e.duration, fmtTime(e.created_at)].forEach((v) => {
          const td = document.createElement('td');
          td.textContent = v;
          tr.appendChild(td);
        });
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      const wrap = document.createElement('div');
      wrap.className = 'table-wrap';
      wrap.appendChild(table);
      lb.appendChild(wrap);
    }).catch((e) => {
      lb.textContent = e.message;
    });
  }

  window.scrollTo(0, 0);
}

boot();
