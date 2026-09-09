// 题库管理页：题库增删改名 + 库内题目（题型/题干/分值/正确答案）增删改
import { api, toast } from '/js/api.js';

const $ = (id) => document.getElementById(id);

const state = {
  banks: [],
  current: null,     // 选中的题库
  questions: [],
  editing: null,     // 正在编辑的题目（null=新增，对象=编辑）
  aiEnabled: false,  // AI 服务是否开通
};

const TYPE_LABEL = {
  single_choice: '单选题',
  multiple_choice: '多选题',
  dropdown: '下拉题',
  text: '填空题',
};

/* ---------- 启动 ---------- */

async function boot() {
  try {
    const me = await api('/api/auth/me');
    $('userName').textContent = me.username;
  } catch (e) {
    return; // 401 已跳登录
  }
  try {
    const st = await api('/api/ai/status');
    state.aiEnabled = !!st.enabled;
  } catch (e) {
    state.aiEnabled = false;
  }
  await loadBanks();
}

async function loadBanks(selectId) {
  state.banks = await api('/api/banks');
  const box = $('bankList');
  box.textContent = '';
  state.banks.forEach((b) => {
    const item = document.createElement('div');
    item.className = 'survey-item' + (state.current && b.id === state.current.id ? ' active' : '');
    const name = document.createElement('span');
    name.className = 's-name';
    name.textContent = b.name;
    item.appendChild(name);
    const cnt = document.createElement('span');
    cnt.className = 'badge badge-draft';
    cnt.textContent = b.question_count + ' 题';
    item.appendChild(cnt);
    item.addEventListener('click', () => selectBank(b.id));
    box.appendChild(item);
  });
  if (selectId) {
    await selectBank(selectId);
  } else if (!state.current && state.banks.length > 0) {
    await selectBank(state.banks[0].id);
  } else if (!state.current) {
    $('bankEmpty').style.display = '';
    $('bankDetail').style.display = 'none';
  }
}

async function selectBank(id) {
  try {
    const data = await api('/api/banks/' + id + '/questions');
    state.current = data.bank;
    state.questions = data.questions || [];
    $('bankEmpty').style.display = 'none';
    $('bankDetail').style.display = '';
    $('bankName').value = state.current.name;
    renderQuestions();
    document.querySelectorAll('#bankList .survey-item').forEach((el, i) => {
      el.classList.toggle('active', !!state.banks[i] && state.banks[i].id === id);
    });
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 题库操作 ---------- */

$('newBankBtn').addEventListener('click', async () => {
  try {
    const b = await api('/api/banks', { method: 'POST', body: { name: '未命名题库' } });
    toast('已创建题库');
    await loadBanks(b.id);
  } catch (e) {
    toast(e.message, true);
  }
});

$('renameBankBtn').addEventListener('click', async () => {
  if (!state.current) {
    return;
  }
  const name = $('bankName').value.trim();
  if (!name) {
    toast('题库名称不能为空', true);
    return;
  }
  try {
    const b = await api('/api/banks/' + state.current.id, { method: 'PUT', body: { name } });
    state.current = { ...state.current, ...b };
    toast('已重命名');
    await loadBanks(state.current.id);
  } catch (e) {
    toast(e.message, true);
  }
});

$('deleteBankBtn').addEventListener('click', async () => {
  if (!state.current) {
    return;
  }
  if (!confirm('删除题库「' + state.current.name + '」？题库内题目将一并删除，已导入答题卷的题目不受影响。')) {
    return;
  }
  try {
    await api('/api/banks/' + state.current.id, { method: 'DELETE' });
    state.current = null;
    state.questions = [];
    $('bankDetail').style.display = 'none';
    $('bankEmpty').style.display = '';
    toast('已删除题库');
    await loadBanks();
  } catch (e) {
    toast(e.message, true);
  }
});

/* ---------- 题目列表 ---------- */

function correctSummary(q) {
  const c = q.config && q.config.correct;
  if (q.type === 'single_choice' || q.type === 'dropdown') {
    const hit = (q.config.options || []).find((o) => o.id === c);
    return '答案：' + (hit ? hit.label : String(c || '-'));
  }
  if (q.type === 'multiple_choice') {
    const opts = q.config.options || [];
    const labels = (Array.isArray(c) ? c : []).map((id) => {
      const hit = opts.find((o) => o.id === id);
      return hit ? hit.label : id;
    });
    return '答案：' + (labels.join('、') || '-');
  }
  return '答案：' + (Array.isArray(c) && c.length ? c.join(' / ') : '-');
}

function renderQuestions() {
  const box = $('bankQuestionList');
  box.textContent = '';
  if (state.questions.length === 0) {
    const p = document.createElement('div');
    p.className = 'card';
    p.style.cssText = 'text-align:center;color:var(--text-3);padding:30px';
    p.textContent = '暂无题目，点击下方「添加题目」开始录入';
    box.appendChild(p);
    return;
  }
  state.questions.forEach((q, i) => {
    const card = document.createElement('div');
    card.className = 'q-card';
    const head = document.createElement('div');
    head.className = 'q-head';
    const num = document.createElement('span');
    num.className = 'q-num';
    num.textContent = (i + 1) + '.';
    head.appendChild(num);
    const title = document.createElement('div');
    title.style.cssText = 'flex:1;font-weight:600';
    title.textContent = q.title;
    head.appendChild(title);
    const ops = document.createElement('div');
    ops.className = 'q-ops';
    const mkBtn = (txt, fn, cls) => {
      const b = document.createElement('button');
      b.className = 'btn btn-ghost btn-sm' + (cls ? ' ' + cls : '');
      b.textContent = txt;
      b.addEventListener('click', fn);
      return b;
    };
    ops.appendChild(mkBtn('编辑', () => openQModal(q)));
    ops.appendChild(mkBtn('删除', async () => {
      if (!confirm('确定删除第 ' + (i + 1) + ' 题？')) {
        return;
      }
      try {
        await api('/api/banks/' + state.current.id + '/questions/' + q.id, { method: 'DELETE' });
        toast('已删除');
        await selectBank(state.current.id);
      } catch (e) {
        toast(e.message, true);
      }
    }, 'btn-danger'));
    head.appendChild(ops);
    card.appendChild(head);

    const meta = document.createElement('div');
    meta.className = 'q-meta';
    meta.appendChild(Object.assign(document.createElement('label'), { textContent: TYPE_LABEL[q.type] || q.type }));
    meta.appendChild(Object.assign(document.createElement('label'), { textContent: (q.config.score || 1) + ' 分' }));
    meta.appendChild(Object.assign(document.createElement('label'), { textContent: correctSummary(q) }));
    card.appendChild(meta);

    if (q.type !== 'text' && q.config.options && q.config.options.length > 0) {
      const opts = document.createElement('div');
      opts.style.cssText = 'font-size:13px;color:var(--text-2);padding-left:28px';
      opts.textContent = '选项：' + q.config.options.map((o) => o.label).join(' / ');
      card.appendChild(opts);
    }
    box.appendChild(card);
  });
}

/* ---------- 题目编辑弹窗 ---------- */

let draft = null; // 编辑中的草稿 {type,title,config}

function openQModal(existing) {
  state.editing = existing || null;
  $('qModalTitle').textContent = existing ? '编辑题目' : '添加题目';
  if (existing) {
    draft = JSON.parse(JSON.stringify({ type: existing.type, title: existing.title, config: existing.config || {} }));
  } else {
    draft = { type: 'single_choice', title: '', config: { options: [{ id: 'o1', label: '' }, { id: 'o2', label: '' }], correct: 'o1', score: 1 } };
  }
  $('qType').value = draft.type;
  $('qTitle').value = draft.title;
  $('qScore').value = draft.config.score > 0 ? draft.config.score : 1;
  $('qTextCorrect').value = draft.type === 'text' && Array.isArray(draft.config.correct) ? draft.config.correct.join(',') : '';
  $('qType').disabled = !!existing;
  renderOptionsEditor();
  $('qModal').classList.add('open');
}

function renderOptionsEditor() {
  const box = $('qOptionsBox');
  box.textContent = '';
  const isText = draft.type === 'text';
  $('qTextBox').style.display = isText ? '' : 'none';
  box.style.display = isText ? 'none' : '';
  if (isText) {
    return;
  }
  if (!Array.isArray(draft.config.options) || draft.config.options.length < 2) {
    draft.config.options = [{ id: 'o1', label: '' }, { id: 'o2', label: '' }];
    if (draft.type === 'multiple_choice') {
      draft.config.correct = Array.isArray(draft.config.correct) ? draft.config.correct : ['o1'];
    } else {
      draft.config.correct = typeof draft.config.correct === 'string' ? draft.config.correct : 'o1';
    }
  }
  const multi = draft.type === 'multiple_choice';
  const wrap = document.createElement('div');
  wrap.className = 'field';
  const lab = document.createElement('label');
  lab.textContent = '选项（勾选' + (multi ? '勾选框' : '圆点') + '设置正确答案）';
  wrap.appendChild(lab);
  draft.config.options.forEach((o, oi) => {
    const row = document.createElement('div');
    row.style.cssText = 'display:flex;gap:8px;align-items:center;margin-bottom:6px';
    const input = document.createElement('input');
    input.type = multi ? 'checkbox' : 'radio';
    input.name = 'bankCorrect';
    input.checked = multi
      ? Array.isArray(draft.config.correct) && draft.config.correct.includes(o.id)
      : draft.config.correct === o.id;
    input.addEventListener('change', () => {
      if (multi) {
        const ids = new Set(Array.isArray(draft.config.correct) ? draft.config.correct : []);
        if (input.checked) {
          ids.add(o.id);
        } else {
          ids.delete(o.id);
        }
        draft.config.correct = Array.from(ids);
      } else if (input.checked) {
        draft.config.correct = o.id;
      }
    });
    row.appendChild(input);
    const text = document.createElement('input');
    text.type = 'text';
    text.className = 'input';
    text.value = o.label;
    text.placeholder = '选项 ' + (oi + 1);
    text.addEventListener('input', () => {
      o.label = text.value;
    });
    row.appendChild(text);
    if (draft.config.options.length > 2) {
      const del = document.createElement('button');
      del.className = 'btn btn-ghost btn-sm';
      del.type = 'button';
      del.textContent = '删除';
      del.addEventListener('click', () => {
        draft.config.options.splice(oi, 1);
        if (!multi && draft.config.correct === o.id) {
          draft.config.correct = draft.config.options[0].id;
        }
        if (multi && Array.isArray(draft.config.correct)) {
          draft.config.correct = draft.config.correct.filter((id) => id !== o.id);
        }
        renderOptionsEditor();
      });
      row.appendChild(del);
    }
    wrap.appendChild(row);
  });
  if (draft.config.options.length < 26) {
    const add = document.createElement('button');
    add.className = 'btn btn-sm';
    add.type = 'button';
    add.textContent = '+ 添加选项';
    add.style.marginLeft = '22px';
    add.addEventListener('click', () => {
      let n = 1;
      while (draft.config.options.some((o) => o.id === 'o' + n)) {
        n++;
      }
      draft.config.options.push({ id: 'o' + n, label: '' });
      renderOptionsEditor();
    });
    wrap.appendChild(add);
  }
  box.appendChild(wrap);
}

$('qType').addEventListener('change', () => {
  draft.type = $('qType').value;
  if (draft.type === 'text') {
    delete draft.config.options;
    draft.config.correct = [];
  } else {
    delete draft.config.correct;
    if (!Array.isArray(draft.config.options)) {
      draft.config.options = [{ id: 'o1', label: '' }, { id: 'o2', label: '' }];
    }
    draft.config.correct = draft.type === 'multiple_choice' ? ['o1'] : draft.config.options[0].id;
  }
  renderOptionsEditor();
});

$('qTextCorrect').addEventListener('input', () => {
  draft.config.correct = $('qTextCorrect').value.split(/[,，、;；]/).map((s) => s.trim()).filter((s) => s !== '');
});

$('qCancel').addEventListener('click', () => $('qModal').classList.remove('open'));

$('qSave').addEventListener('click', async () => {
  draft.title = $('qTitle').value.trim();
  let score = parseInt($('qScore').value, 10);
  if (isNaN(score) || score < 1) {
    score = 1;
  }
  if (score > 1000) {
    score = 1000;
  }
  draft.config.score = score;
  if (!draft.title) {
    toast('题干不能为空', true);
    return;
  }
  if (draft.type !== 'text') {
    const filled = draft.config.options.filter((o) => o.label.trim() !== '');
    if (filled.length < 2) {
      toast('至少需要 2 个非空选项', true);
      return;
    }
  } else if (!Array.isArray(draft.config.correct) || draft.config.correct.length === 0) {
    toast('请填写正确答案', true);
    return;
  }
  try {
    if (state.editing) {
      await api('/api/banks/' + state.current.id + '/questions/' + state.editing.id, {
        method: 'PUT',
        body: { type: draft.type, title: draft.title, config: draft.config },
      });
      toast('已保存');
    } else {
      await api('/api/banks/' + state.current.id + '/questions', {
        method: 'POST',
        body: { type: draft.type, title: draft.title, config: draft.config },
      });
      toast('已添加题目');
    }
    $('qModal').classList.remove('open');
    await selectBank(state.current.id);
  } catch (e) {
    toast(e.message, true);
  }
});

$('addQBtn').addEventListener('click', () => {
  if (!state.current) {
    toast('请先选择题库', true);
    return;
  }
  openQModal(null);
});

/* ---------- AI 生成题目 ---------- */

let aiGenerated = null; // 生成的题目数组（含勾选状态由 DOM 决定）

$('aiQBtn').addEventListener('click', () => {
  if (!state.current) {
    toast('请先选择题库', true);
    return;
  }
  if (!state.aiEnabled) {
    toast('AI 服务未配置，请联系管理员在设置页配置后使用', true);
    return;
  }
  $('aiQPrompt').value = '';
  $('aiQPreview').textContent = '';
  $('aiQImport').style.display = 'none';
  $('aiQModal').classList.add('open');
});

$('aiQGenerateBtn').addEventListener('click', async () => {
  const prompt = $('aiQPrompt').value.trim();
  if (!prompt) {
    toast('请先描述需求', true);
    return;
  }
  const btn = $('aiQGenerateBtn');
  btn.disabled = true;
  const box = $('aiQPreview');
  box.textContent = '正在生成，可能需要 1-3 分钟，请耐心等待……';
  const controller = new AbortController();
  let waited = 0;
  const timer = setInterval(() => {
    waited += 1;
    btn.textContent = '生成中 ' + waited + 's';
  }, 1000);
  const killer = setTimeout(() => controller.abort(), 5 * 60 * 1000);
  try {
    const res = await api('/api/ai/generate-bank-questions', {
      method: 'POST',
      body: { prompt },
      signal: controller.signal,
    });
    aiGenerated = res.questions || [];
    renderAIGenerated(box);
    $('aiQImport').style.display = aiGenerated.length ? '' : 'none';
  } catch (e) {
    box.textContent = '生成失败：' + e.message;
    toast(e.message, true);
  } finally {
    clearInterval(timer);
    clearTimeout(killer);
    btn.disabled = false;
    btn.textContent = '生成预览';
  }
});

function renderAIGenerated(box) {
  box.textContent = '';
  if (!aiGenerated.length) {
    box.textContent = '未生成任何题目';
    return;
  }
  const head = document.createElement('div');
  head.style.cssText = 'color:var(--text-2);font-size:13px;margin-bottom:6px';
  head.textContent = '共 ' + aiGenerated.length + ' 题，勾选要导入的：';
  box.appendChild(head);
  aiGenerated.forEach((q, i) => {
    const lab = document.createElement('label');
    lab.className = 'bank-q-row';
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.checked = true;
    lab.appendChild(cb);
    const info = document.createElement('span');
    const cfg = q.config || {};
    const score = cfg.score || 1;
    const correctTxt = TYPE_LABEL[q.type] + ' · ' + score + ' 分 · ' + q.title;
    info.textContent = correctTxt;
    lab.appendChild(info);
    box.appendChild(lab);
  });
}

$('aiQImport').addEventListener('click', async () => {
  if (!state.current || !aiGenerated) {
    return;
  }
  const chosen = aiGenerated.filter((q, i) => {
    const cb = document.querySelectorAll('#aiQPreview input[type="checkbox"]')[i];
    return cb && cb.checked;
  });
  if (chosen.length === 0) {
    toast('请先勾选题目', true);
    return;
  }
  const btn = $('aiQImport');
  btn.disabled = true;
  let ok = 0;
  let fail = 0;
  try {
    for (const q of chosen) {
      try {
        await api('/api/banks/' + state.current.id + '/questions', {
          method: 'POST',
          body: { type: q.type, title: q.title, config: q.config },
        });
        ok++;
      } catch (e) {
        fail++;
        toast('第 ' + (chosen.indexOf(q) + 1) + ' 题导入失败：' + e.message, true);
      }
    }
    aiGenerated = null;
    $('aiQModal').classList.remove('open');
    toast('已导入 ' + ok + ' 题' + (fail ? '，失败 ' + fail + ' 题' : ''));
    await selectBank(state.current.id);
  } finally {
    btn.disabled = false;
  }
});

$('aiQCancel').addEventListener('click', () => $('aiQModal').classList.remove('open'));

$('logoutBtn').addEventListener('click', async () => {
  await api('/api/auth/logout', { method: 'POST', body: {} }).catch(() => {});
  location.href = '/login';
});

document.querySelectorAll('.modal-mask').forEach((m) => {
  m.addEventListener('click', (ev) => {
    if (ev.target === m) {
      m.classList.remove('open');
    }
  });
});

boot();
