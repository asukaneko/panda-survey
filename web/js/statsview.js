// 统计视图：console 内嵌标签页、独立 /stats 页、/share 只读页共用
// opts: { sid } 或 { shareToken }；aiEnabled / readonly 由调用方决定
import { api, toast, fmtTime } from '/js/api.js';

export async function renderStatsView(container, opts) {
  const readonly = !!opts.shareToken;
  const endpoint = opts.shareToken
    ? '/api/share/' + opts.shareToken + '/stats'
    : '/api/surveys/' + opts.sid + '/stats';

  container.textContent = '';
  const tip = document.createElement('p');
  tip.style.color = 'var(--text-2)';
  tip.textContent = '加载中……';
  container.appendChild(tip);

  let data;
  try {
    data = await api(endpoint);
  } catch (e) {
    tip.textContent = e.message;
    return;
  }
  const questions = data.questions || [];

  const build = async () => {
    container.textContent = '';

    const h2 = document.createElement('h2');
    h2.style.cssText = 'margin:18px 0 4px';
    h2.textContent = data.survey.title;
    container.appendChild(h2);
    const meta = document.createElement('p');
    meta.style.cssText = 'color:var(--text-2);margin-bottom:16px';
    if (readonly) {
      meta.textContent = '只读统计分享';
    } else {
      const statusName = { 0: '未发布', 1: '发布中', 2: '已停止' }[data.survey.status];
      meta.textContent = '状态：' + statusName + ' · 创建于 ' + fmtTime(data.survey.created_at);
    }
    container.appendChild(meta);

    // 概览
    const overview = document.createElement('div');
    overview.className = 'stats-overview';
    const tile = (num, lbl) => {
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
    overview.appendChild(tile(data.total, '已回收答卷'));
    overview.appendChild(tile(questions.length, '题目数量'));
    container.appendChild(overview);

    // 工具条：导出 + 明细开关（只读分享不提供）
    if (!readonly) {
      const bar = document.createElement('div');
      bar.className = 'toolbar';
      const aDetail = document.createElement('a');
      aDetail.className = 'btn';
      aDetail.textContent = '导出明细 CSV';
      aDetail.href = '/api/surveys/' + opts.sid + '/export?mode=detail';
      const aSummary = document.createElement('a');
      aSummary.className = 'btn';
      aSummary.textContent = '导出汇总 CSV';
      aSummary.href = '/api/surveys/' + opts.sid + '/export?mode=summary';
      const toggle = document.createElement('button');
      toggle.className = 'btn';
      toggle.textContent = '展开 / 收起明细答卷';
      bar.append(aDetail, aSummary, toggle);
      container.appendChild(bar);

      const detailCard = document.createElement('div');
      detailCard.className = 'card table-wrap';
      detailCard.style.display = 'none';
      const detailTitle = document.createElement('h4');
      detailTitle.style.marginBottom = '10px';
      detailTitle.textContent = '明细答卷';
      detailCard.appendChild(detailTitle);
      const table = document.createElement('table');
      table.className = 'table';
      detailCard.appendChild(table);
      container.appendChild(detailCard);
      toggle.addEventListener('click', () => {
        const show = detailCard.style.display === 'none';
        detailCard.style.display = show ? '' : 'none';
      });
      await loadResponses(opts.sid, table, build);
    }

    // 逐题统计
    const charts = document.createElement('div');
    container.appendChild(charts);
    let seq = 0;
    questions.forEach((st) => {
      const q = st.question;
      const card = document.createElement('div');
      card.className = 'card chart-card';
      const h = document.createElement('h4');
      h.textContent = q.sort_order + '. ' + q.title + '（作答 ' + st.answered + '）';
      card.appendChild(h);
      charts.appendChild(card);

      if (st.choices && st.choices.length > 0) {
        addBarChart(card, 'chart-' + (seq++), st.choices.map((c) => c.label), st.choices.map((c) => c.count));
      } else if (st.distribution) {
        addAvg(card, st.avg);
        const labels = Object.keys(st.distribution).sort();
        addBarChart(card, 'chart-' + (seq++), labels.map((k) => k + ' 分'), labels.map((k) => st.distribution[k]));
      } else if (st.rows && st.rows.length > 0) {
        // 矩阵题：逐行条形图
        st.rows.forEach((row) => {
          const rt = document.createElement('div');
          rt.style.cssText = 'font-size:13px;color:var(--text-2);margin:10px 0 2px';
          rt.textContent = row.row;
          card.appendChild(rt);
          addBarChart(card, 'chart-' + (seq++), row.counts.map((c) => c.label), row.counts.map((c) => c.count), 100);
        });
      } else if (st.rankings && st.rankings.length > 0) {
        // 排序题：平均名次表
        const t = document.createElement('table');
        t.className = 'table';
        const thead = document.createElement('thead');
        const hr = document.createElement('tr');
        ['选项', '平均名次', '排第一次数'].forEach((x) => {
          const th = document.createElement('th');
          th.textContent = x;
          hr.appendChild(th);
        });
        thead.appendChild(hr);
        t.appendChild(thead);
        const tb = document.createElement('tbody');
        [...st.rankings].sort((a, b) => a.avg - b.avg).forEach((rk) => {
          const tr = document.createElement('tr');
          [rk.label, rk.avg, rk.first].forEach((v) => {
            const td = document.createElement('td');
            td.textContent = v;
            tr.appendChild(td);
          });
          tb.appendChild(tr);
        });
        t.appendChild(tb);
        card.appendChild(t);
      } else if (st.texts) {
        const ul = document.createElement('ul');
        ul.className = 'text-list';
        if (st.texts.length === 0) {
          const li = document.createElement('li');
          li.textContent = '暂无作答';
          ul.appendChild(li);
        } else {
          st.texts.forEach((t) => {
            const li = document.createElement('li');
            li.textContent = t;
            ul.appendChild(li);
          });
        }
        card.appendChild(ul);
        // AI 摘要（仅文本题、AI 开通、非只读）
        if (opts.aiEnabled && !readonly && (q.type === 'text' || q.type === 'textarea')) {
          const btn = document.createElement('button');
          btn.className = 'btn btn-sm';
          btn.style.marginTop = '8px';
          btn.textContent = 'AI 摘要';
          btn.addEventListener('click', async () => {
            btn.disabled = true;
            btn.textContent = '总结中……';
            try {
              const res = await api('/api/ai/summarize-answers', {
                method: 'POST',
                body: { survey_id: opts.sid, question_id: q.id },
              });
              const box = document.createElement('div');
              box.style.cssText = 'margin-top:10px;padding:10px;background:var(--primary-light);border-radius:8px;font-size:13px;white-space:pre-line';
              box.textContent = res.summary;
              card.appendChild(box);
              btn.textContent = '重新摘要';
            } catch (e) {
              toast(e.message, true);
              btn.textContent = 'AI 摘要';
            } finally {
              btn.disabled = false;
            }
          });
          card.appendChild(btn);
        }
      }
    });
  };
  await build();
}

function addAvg(card, avg) {
  const avgLine = document.createElement('div');
  avgLine.className = 'avg-line';
  avgLine.appendChild(document.createTextNode('平均分 '));
  const b = document.createElement('b');
  b.textContent = avg !== undefined && avg !== null ? avg : '-';
  avgLine.appendChild(b);
  card.appendChild(avgLine);
}

function addBarChart(card, id, labels, values, height) {
  if (typeof Chart === 'undefined') {
    const pre = document.createElement('pre');
    pre.style.cssText = 'font-size:13px;color:var(--text-2)';
    pre.textContent = labels.map((l, i) => l + '：' + values[i]).join('\n');
    card.appendChild(pre);
    return;
  }
  const canvas = document.createElement('canvas');
  canvas.id = id;
  canvas.height = height || Math.max(120, labels.length * 34);
  card.appendChild(canvas);
  new Chart(canvas.getContext('2d'), {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: '人次',
        data: values,
        backgroundColor: 'rgba(43, 162, 69, 0.75)',
        borderColor: '#2BA245',
        borderWidth: 1,
        borderRadius: 4,
      }],
    },
    options: {
      indexAxis: 'y',
      plugins: { legend: { display: false } },
      scales: { x: { ticks: { precision: 0 }, grid: { color: '#f0f1f2' } }, y: { grid: { display: false } } },
    },
  });
}

async function loadResponses(sid, table, refresh) {
  let list;
  try {
    list = await api('/api/surveys/' + sid + '/responses');
  } catch (e) {
    return;
  }
  table.textContent = '';
  if (list.length === 0) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.textContent = '暂无答卷';
    tr.appendChild(td);
    table.appendChild(tr);
    return;
  }
  const thead = document.createElement('thead');
  const headRow = document.createElement('tr');
  ['提交时间', '耗时(秒)', '答案摘要', '操作'].forEach((h) => {
    const th = document.createElement('th');
    th.textContent = h;
    headRow.appendChild(th);
  });
  thead.appendChild(headRow);
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  list.forEach((r) => {
    const tr = document.createElement('tr');
    const tdTime = document.createElement('td');
    tdTime.textContent = fmtTime(r.created_at);
    const tdDur = document.createElement('td');
    tdDur.textContent = r.duration;
    const tdAns = document.createElement('td');
    tdAns.textContent = Object.values(r.answers).map((v) => String(v).slice(0, 40)).join('；') || '-';
    const tdOp = document.createElement('td');
    const del = document.createElement('button');
    del.className = 'btn btn-sm btn-danger';
    del.textContent = '删除';
    del.addEventListener('click', async () => {
      if (!confirm('确定删除这份答卷？删除后统计将回减。')) {
        return;
      }
      try {
        await api('/api/surveys/' + sid + '/responses/' + r.id, { method: 'DELETE' });
        toast('已删除');
        refresh();
      } catch (e) {
        toast(e.message, true);
      }
    });
    tdOp.appendChild(del);
    tr.append(tdTime, tdDur, tdAns, tdOp);
    tbody.appendChild(tr);
  });
  table.appendChild(tbody);
}
