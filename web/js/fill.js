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
  renderQuizIntro();
}

function renderQuizIntro() {
  document.getElementById('questions').textContent = '';
  document.getElementById('actions').textContent = '';
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
    const it = widgets[quiz.cur];
    box.appendChild(it.w.root);
    it.w.root.style.display = '';
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
    const next = document.createElement('button');
    next.className = 'btn btn-primary btn-lg';
    next.textContent = quiz.cur === widgets.length - 1 ? (isPreview ? '结 束' : '交 卷') : '下一题';
    next.addEventListener('click', () => {
      if (quiz.cur === widgets.length - 1) {
        submitQuiz(false);
      } else {
        quiz.cur++;
        renderQuizQuestions();
        window.scrollTo({ top: 0 });
      }
    });
    nav.append(prev, info, next);
    actions.appendChild(nav);
    return;
  }
  widgets.forEach((it) => box.appendChild(it.w.root));
  if (!isPreview) {
    const btn = document.createElement('button');
    btn.className = 'btn btn-primary btn-lg';
    btn.id = 'submitBtn';
    btn.textContent = '交 卷';
    btn.addEventListener('click', () => submitQuiz(false));
    actions.appendChild(btn);
  }
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
  const btn = document.querySelector('#actions button.btn-primary');
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
    if (btn) {
      btn.disabled = false;
      btn.textContent = '交 卷';
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
      lb.appendChild(table);
    }).catch((e) => {
      lb.textContent = e.message;
    });
  }

  window.scrollTo(0, 0);
}

boot();
