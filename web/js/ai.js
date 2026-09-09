// AI 能力：入口按钮、AI 创建问卷、Agent 对话面板
import { api, toast } from '/js/api.js';
import { renderFillQuestion, TYPE_LABEL } from '/js/renderers.js';

// console.js 调用：注入共享状态与回调
let state = null;
let hooks = null;

export function initAI(s, h) {
  state = s;
  hooks = h;
  mountEntryButton();
  if (state.ai.enabled) {
    updateQuotaHint();
  }
}

// 唯一入口按钮「AI 助手」；未配置时面板可打开并给出提示
function mountEntryButton() {
  if (document.getElementById('aiPanelBtn')) {
    return;
  }
  const box = document.querySelector('.topbar .spacer');
  const wrap = document.createElement('div');
  wrap.className = 'ai-btns';
  const panelBtn = document.createElement('button');
  panelBtn.id = 'aiPanelBtn';
  panelBtn.className = 'btn btn-sm';
  panelBtn.textContent = 'AI 助手';
  panelBtn.addEventListener('click', () => {
    const p = document.getElementById('aiPanel');
    p.classList.toggle('open');
    document.body.classList.toggle('ai-open', p.classList.contains('open'));
    if (p.classList.contains('open') && !state.ai.enabled && !p.dataset.warned) {
      p.dataset.warned = '1';
      addMsg('AI 助手', 'AI 服务尚未开通：请联系管理员在设置页配置 OpenAI 兼容服务后即可使用。');
      document.getElementById('aiSendBtn').disabled = true;
    }
  });
  wrap.appendChild(panelBtn);
  box.after(wrap);

  document.getElementById('aiPanelClose').addEventListener('click', () => {
    document.getElementById('aiPanel').classList.remove('open');
    document.body.classList.remove('ai-open');
  });
  document.getElementById('aiTabEdit').addEventListener('click', () => switchAITab('edit'));
  document.getElementById('aiTabCreate').addEventListener('click', () => {
    switchAITab('create');
    updateQuotaHint();
  });
  document.getElementById('aiTabQuiz').addEventListener('click', () => {
    switchAITab('quiz');
    updateQuotaHint(true);
  });
}

function switchAITab(tab) {
  document.getElementById('aiTabEdit').classList.toggle('active', tab === 'edit');
  document.getElementById('aiTabCreate').classList.toggle('active', tab === 'create');
  document.getElementById('aiTabQuiz').classList.toggle('active', tab === 'quiz');
  document.getElementById('aiEditView').style.display = tab === 'edit' ? 'flex' : 'none';
  document.getElementById('aiCreateView').style.display = tab === 'create' ? 'flex' : 'none';
  document.getElementById('aiCreateQuizView').style.display = tab === 'quiz' ? 'flex' : 'none';
}

function updateQuotaHint(isQuiz) {
  const hint = document.getElementById(isQuiz ? 'aiQuizQuotaHint' : 'aiQuotaHint');
  if (hint) {
    hint.textContent = '今日已用 ' + state.ai.usedToday + ' / ' + state.ai.quota + ' 次';
  }
}

function afterAICall() {
  state.ai.usedToday += 1;
  updateQuotaHint();
}

/* ---------- AI 创建问卷 ---------- */

let generated = null;

document.getElementById('aiGenerateBtn').addEventListener('click', async () => {
  if (!state.ai.enabled) {
    toast('AI 服务未配置，请联系管理员在设置页配置后使用', true);
    return;
  }
  const prompt = document.getElementById('aiCreatePrompt').value.trim();
  if (!prompt) {
    toast('请先描述需求', true);
    return;
  }
  const btn = document.getElementById('aiGenerateBtn');
  btn.disabled = true;
  const box = document.getElementById('aiPreview');
  box.textContent = '正在生成，大模型生成整卷问卷通常需要 1-3 分钟，请耐心等待……';
  // 等待计时 + 前端 5 分钟超时
  const controller = new AbortController();
  let waited = 0;
  const timer = setInterval(() => {
    waited += 1;
    btn.textContent = '生成中 ' + waited + 's';
  }, 1000);
  const killer = setTimeout(() => controller.abort(), 5 * 60 * 1000);
  try {
    generated = await api('/api/ai/generate-survey', {
      method: 'POST',
      body: { prompt },
      signal: controller.signal,
    });
    renderGeneratedPreview(box, generated);
    document.getElementById('aiCreateConfirm').style.display = '';
    afterAICall();
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

function renderGeneratedPreview(box, gen) {
  box.textContent = '';
  const head = document.createElement('div');
  head.style.marginBottom = '10px';
  const t = document.createElement('b');
  t.textContent = gen.title;
  head.appendChild(t);
  if (gen.description) {
    const d = document.createElement('div');
    d.style.cssText = 'color:var(--text-2);font-size:13px';
    d.textContent = gen.description;
    head.appendChild(d);
  }
  head.appendChild(document.createTextNode(gen.questions.length + ' 道题：'));
  box.appendChild(head);
  gen.questions.forEach((q, i) => {
    const fake = { id: 'gen' + i, ...q };
    const item = renderFillQuestion(fake);
    item.root.classList.remove('fill-card');
    item.root.style.cssText = 'border:1px solid var(--border);padding:10px;margin-bottom:8px;border-radius:8px';
    box.appendChild(item.root);
  });
}

document.getElementById('aiCreateConfirm').addEventListener('click', async () => {
  if (!generated) {
    return;
  }
  try {
    const s = await api('/api/surveys', {
      method: 'POST',
      body: { title: generated.title, description: generated.description },
    });
    await api('/api/surveys/' + s.id, {
      method: 'PUT',
      body: { title: generated.title, description: generated.description, updated_at: s.updated_at, questions: generated.questions },
    });
    generated = null;
    document.getElementById('aiPreview').textContent = '';
    document.getElementById('aiCreateConfirm').style.display = 'none';
    document.getElementById('aiCreatePrompt').value = '';
    switchAITab('edit');
    document.getElementById('aiPanel').classList.remove('open');
    document.body.classList.remove('ai-open');
    toast('AI 问卷已创建为草稿，可继续调整');
    hooks.reload(s.id);
  } catch (e) {
    toast(e.message, true);
  }
});

/* ---------- Agent 面板 ---------- */

let proposal = null;

function addMsg(who, text, isUser) {
  const log = document.getElementById('aiLog');
  const msg = document.createElement('div');
  msg.className = 'ai-msg' + (isUser ? ' user' : '');
  const w = document.createElement('div');
  w.className = 'who';
  w.textContent = who;
  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.textContent = text;
  msg.appendChild(w);
  msg.appendChild(bubble);
  log.appendChild(msg);
  log.scrollTop = log.scrollHeight;
  return bubble;
}

document.getElementById('aiSendBtn').addEventListener('click', sendAgentInstruction);
document.getElementById('aiInput').addEventListener('keydown', (ev) => {
  if (ev.key === 'Enter' && !ev.shiftKey) {
    ev.preventDefault();
    sendAgentInstruction();
  }
});

async function sendAgentInstruction() {
  if (!state.ai.enabled) {
    toast('AI 服务未配置，请联系管理员在设置页配置后使用', true);
    return;
  }
  const input = document.getElementById('aiInput');
  const text = input.value.trim();
  if (!text) {
    return;
  }
  if (!state.current) {
    toast('请先选择一份问卷', true);
    return;
  }
  if (state.dirty) {
    toast('请先保存当前修改，再让 AI 编辑', true);
    return;
  }
  input.value = '';
  addMsg('我', text, true);
  const bubble = addMsg('AI 助手', '正在思考并执行修改，可能需要 1-3 分钟，请耐心等待……');
  const btn = document.getElementById('aiSendBtn');
  btn.disabled = true;
  try {
    const res = await api('/api/ai/agent-edit', {
      method: 'POST',
      body: { survey_id: state.current.id, instruction: text },
    });
    // 步骤展示
    const steps = document.createElement('div');
    steps.className = 'ai-steps';
    res.steps.forEach((st) => {
      const row = document.createElement('div');
      row.className = 'ai-step ' + (st.success ? 'ok' : 'fail');
      const dot = document.createElement('span');
      dot.className = 'dot';
      const txt = document.createElement('span');
      txt.className = 'txt';
      txt.textContent = st.detail;
      row.appendChild(dot);
      row.appendChild(txt);
      steps.appendChild(row);
    });
    bubble.textContent = res.summary || '已完成修改';
    bubble.appendChild(steps);
    proposal = res;
    const pbox = document.getElementById('aiProposal');
    pbox.style.display = '';
    document.getElementById('aiLog').scrollTop = 99999;
    afterAICall();
  } catch (e) {
    bubble.textContent = e.message;
  } finally {
    btn.disabled = false;
  }
}

document.getElementById('aiApplyBtn').addEventListener('click', () => {
  if (!proposal || !state.current) {
    return;
  }
  state.current.title = proposal.survey.title;
  state.current.description = proposal.survey.description;
  document.getElementById('sTitle').value = state.current.title;
  document.getElementById('sDesc').value = state.current.description;
  state.questions = proposal.questions.map((q) => ({
    // 保留原题目 id（新增题为 0），保存时原地更新，历史答卷关联不失效
    id: q.id || 0, type: q.type, title: q.title, required: q.required, config: q.config || {},
  }));
  hooks.markDirty();
  hooks.renderQuestions();
  document.getElementById('aiProposal').style.display = 'none';
  toast('已应用到编辑器，请检查后手动保存');
});

/* ---------- AI 创建答题 ---------- */

let generatedQuiz = null;

document.getElementById('aiQuizGenerateBtn').addEventListener('click', async () => {
  if (!state.ai.enabled) {
    toast('AI 服务未配置，请联系管理员在设置页配置后使用', true);
    return;
  }
  const prompt = document.getElementById('aiQuizPrompt').value.trim();
  if (!prompt) {
    toast('请先描述需求', true);
    return;
  }
  const btn = document.getElementById('aiQuizGenerateBtn');
  btn.disabled = true;
  const box = document.getElementById('aiQuizPreview');
  box.textContent = '正在生成答题卷，大模型生成整卷题目通常需要 1-3 分钟，请耐心等待……';
  const controller = new AbortController();
  let waited = 0;
  const timer = setInterval(() => {
    waited += 1;
    btn.textContent = '生成中 ' + waited + 's';
  }, 1000);
  const killer = setTimeout(() => controller.abort(), 5 * 60 * 1000);
  try {
    generatedQuiz = await api('/api/ai/generate-quiz', {
      method: 'POST',
      body: { prompt },
      signal: controller.signal,
    });
    renderQuizPreview(box, generatedQuiz);
    document.getElementById('aiQuizCreateConfirm').style.display = '';
    afterAICall();
    updateQuotaHint(true);
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

// 答题配置转可读摘要
function quizConfigSummary(cfg) {
  const c = cfg || {};
  const parts = [];
  parts.push(c.duration_min > 0 ? '限时 ' + c.duration_min + ' 分钟' : '不限时');
  parts.push(c.display_mode === 'paged' ? '分页展示' : '列表展示');
  parts.push(c.question_order === 'random' ? '随机排序' : '顺序出题');
  if (c.show_answer) {
    parts.push('展示答案');
  }
  if (c.collect_profile) {
    parts.push('收集个人信息');
  }
  if (c.show_ranking) {
    parts.push('排行榜');
  }
  return parts.join(' · ');
}

function renderQuizPreview(box, gen) {
  box.textContent = '';
  const head = document.createElement('div');
  head.style.marginBottom = '10px';
  const t = document.createElement('b');
  t.textContent = gen.title;
  head.appendChild(t);
  if (gen.description) {
    const d = document.createElement('div');
    d.style.cssText = 'color:var(--text-2);font-size:13px';
    d.textContent = gen.description;
    head.appendChild(d);
  }
  const meta = document.createElement('div');
  meta.style.cssText = 'color:var(--text-2);font-size:12px;margin-top:2px';
  meta.textContent = gen.questions.length + ' 道题 · ' + quizConfigSummary(gen.quiz_config);
  head.appendChild(meta);
  box.appendChild(head);
  gen.questions.forEach((q, i) => {
    const fake = { id: 'genq' + i, ...q };
    const item = renderFillQuestion(fake);
    item.root.classList.remove('fill-card');
    item.root.style.cssText = 'border:1px solid var(--border);padding:10px;margin-bottom:8px;border-radius:8px';
    box.appendChild(item.root);
  });
}

document.getElementById('aiQuizCreateConfirm').addEventListener('click', async () => {
  if (!generatedQuiz) {
    return;
  }
  try {
    const s = await api('/api/surveys', {
      method: 'POST',
      body: { title: generatedQuiz.title, description: generatedQuiz.description, kind: 1 },
    });
    await api('/api/surveys/' + s.id, {
      method: 'PUT',
      body: {
        title: generatedQuiz.title,
        description: generatedQuiz.description,
        updated_at: s.updated_at,
        questions: generatedQuiz.questions,
        quiz_config: generatedQuiz.quiz_config,
      },
    });
    generatedQuiz = null;
    document.getElementById('aiQuizPreview').textContent = '';
    document.getElementById('aiQuizCreateConfirm').style.display = 'none';
    document.getElementById('aiQuizPrompt').value = '';
    switchAITab('edit');
    document.getElementById('aiPanel').classList.remove('open');
    document.body.classList.remove('ai-open');
    toast('AI 答题卷已创建为草稿，可继续调整');
    hooks.reload(s.id);
  } catch (e) {
    toast(e.message, true);
  }
});
