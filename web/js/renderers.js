// 题型渲染器：填答态与编辑态共用；全部使用 DOM API（textContent）渲染，
// 用户输入绝不拼进 innerHTML。

export const TYPES = [
  { type: 'single_choice', label: '单选题' },
  { type: 'multiple_choice', label: '多选题' },
  { type: 'text', label: '填空题' },
  { type: 'textarea', label: '简答题' },
  { type: 'dropdown', label: '下拉题' },
  { type: 'rating', label: '评分题' },
  { type: 'date', label: '日期题' },
  { type: 'matrix', label: '矩阵题' },
  { type: 'sorting', label: '排序题' },
];

export const TYPE_LABEL = Object.fromEntries(TYPES.map((t) => [t.type, t.label]));

export function newQuestion(type) {
  const q = { type, title: '', required: true, config: {} };
  if (type === 'single_choice' || type === 'multiple_choice' || type === 'dropdown' || type === 'sorting') {
    q.config = {
      options: [
        { id: 'o1', label: '选项一' },
        { id: 'o2', label: '选项二' },
      ],
    };
    if (type === 'multiple_choice') {
      q.config.min_select = 0;
      q.config.max_select = 0;
    }
  } else if (type === 'rating') {
    q.config = { max: 5 };
  } else if (type === 'text') {
    q.config = { max_len: 100 };
  } else if (type === 'textarea') {
    q.config = { max_len: 500 };
  } else if (type === 'matrix') {
    q.config = {
      rows: [
        { id: 'r1', label: '行一' },
        { id: 'r2', label: '行二' },
      ],
      cols: [
        { id: 'c1', label: '列一' },
        { id: 'c2', label: '列二' },
      ],
    };
  } else {
    q.config = {};
  }
  return q;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) {
    e.className = cls;
  }
  if (text !== undefined) {
    e.textContent = text;
  }
  return e;
}

function starSVG() {
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  const path = document.createElementNS(ns, 'path');
  path.setAttribute('d', 'M12 2l2.9 6.3 6.9.8-5.1 4.7 1.4 6.8L12 17.2 5.9 20.6l1.4-6.8L2.2 9.1l6.9-.8z');
  svg.appendChild(path);
  return svg;
}

/* ============ 填答态 ============ */

// 返回 {root, collect, onChange}：collect() 得到 {question_id, value} 或 null（未作答）；
// onChange 在作答变化时回调（供填答端推导逻辑跳转显隐）
export function renderFillQuestion(q) {
  const card = el('div', 'fill-card');
  card.dataset.qid = q.id;
  const title = el('div', 'f-title');
  if (q.required) {
    title.appendChild(el('span', 'req', '*'));
  }
  title.appendChild(document.createTextNode(q.title));
  card.appendChild(title);

  let collect;
  let notify = () => {};
  const wire = (node) => {
    node.addEventListener('change', () => notify());
    return node;
  };

  switch (q.type) {
    case 'single_choice':
    case 'multiple_choice': {
      collect = renderChoices(card, q);
      card.querySelectorAll('input').forEach((i) => wire(i));
      break;
    }
    case 'dropdown': {
      const sel = el('select', 'input');
      const ph = el('option', '', '请选择');
      ph.value = '';
      sel.appendChild(ph);
      q.config.options.forEach((o) => {
        const opt = el('option', '', o.label);
        opt.value = o.id;
        sel.appendChild(opt);
      });
      card.appendChild(wire(sel));
      collect = () => (sel.value === '' ? null : { question_id: q.id, value: sel.value });
      break;
    }
    case 'text': {
      const input = el('input', 'input');
      input.type = 'text';
      if (q.config.max_len > 0) {
        input.maxLength = q.config.max_len;
      }
      card.appendChild(wire(input));
      collect = () => (input.value.trim() === '' ? null : { question_id: q.id, value: input.value });
      break;
    }
    case 'textarea': {
      const ta = el('textarea', 'input');
      if (q.config.max_len > 0) {
        ta.maxLength = q.config.max_len;
      }
      card.appendChild(wire(ta));
      collect = () => (ta.value.trim() === '' ? null : { question_id: q.id, value: ta.value });
      break;
    }
    case 'rating': {
      const max = q.config.max || 5;
      const row = el('div', 'stars');
      let chosen = 0;
      const btns = [];
      for (let i = 1; i <= max; i++) {
        const b = el('button', 'star-btn');
        b.type = 'button';
        b.title = i + ' 分';
        b.appendChild(starSVG());
        b.addEventListener('click', () => {
          chosen = chosen === i ? 0 : i;
          btns.forEach((btn, idx) => {
            btn.classList.toggle('on', idx + 1 <= chosen);
          });
          notify();
        });
        btns.push(b);
        row.appendChild(b);
      }
      card.appendChild(row);
      collect = () => (chosen === 0 ? null : { question_id: q.id, value: chosen });
      break;
    }
    case 'date': {
      const input = el('input', 'input');
      input.type = 'date';
      card.appendChild(wire(input));
      collect = () => (input.value === '' ? null : { question_id: q.id, value: input.value });
      break;
    }
    case 'matrix': {
      const wrap = el('div', 'table-wrap');
      const table = el('table', 'table matrix-table');
      const thead = el('thead');
      const hr = el('tr');
      hr.appendChild(el('th', '', ''));
      q.config.cols.forEach((c) => hr.appendChild(el('th', '', c.label)));
      thead.appendChild(hr);
      table.appendChild(thead);
      const tbody = el('tbody');
      const rowVals = {};
      q.config.rows.forEach((row) => {
        const tr = el('tr');
        tr.appendChild(el('td', 'm-row', row.label));
        q.config.cols.forEach((c) => {
          const td = el('td');
          const lab = el('label', 'm-cell');
          const input = document.createElement('input');
          input.type = 'radio';
          input.name = 'm' + q.id + '-' + row.id;
          input.value = c.id;
          wire(input);
          lab.appendChild(input);
          td.appendChild(lab);
          tr.appendChild(td);
        });
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      wrap.appendChild(table);
      card.appendChild(wrap);
      collect = () => {
        const out = {};
        let any = false;
        q.config.rows.forEach((row) => {
          const sel = wrap.querySelector('input[name="m' + q.id + '-' + row.id + '"]:checked');
          if (sel) {
            out[row.id] = sel.value;
            any = true;
          }
        });
        return any ? { question_id: q.id, value: out } : null;
      };
      break;
    }
    case 'sorting': {
      const list = el('div', '');
      const items = q.config.options.map((o, i) => ({ id: o.id, label: o.label }));
      const rerender = () => {
        list.textContent = '';
        items.forEach((it, i) => {
          const row = el('div', 'choice sort-item');
          row.appendChild(el('span', 'sort-num', String(i + 1)));
          row.appendChild(el('span', '', it.label));
          const up = el('button', 'btn btn-ghost btn-sm', '上移');
          up.type = 'button';
          up.disabled = i === 0;
          up.addEventListener('click', () => {
            if (i > 0) {
              [items[i - 1], items[i]] = [items[i], items[i - 1]];
              rerender();
              notify();
            }
          });
          const down = el('button', 'btn btn-ghost btn-sm', '下移');
          down.type = 'button';
          down.disabled = i === items.length - 1;
          down.addEventListener('click', () => {
            if (i < items.length - 1) {
              [items[i + 1], items[i]] = [items[i], items[i + 1]];
              rerender();
              notify();
            }
          });
          row.appendChild(up);
          row.appendChild(down);
          list.appendChild(row);
        });
      };
      rerender();
      card.appendChild(list);
      const hint = el('div', '', '请用上移/下移调整为你心中的优先级顺序');
      hint.style.cssText = 'font-size:12px;color:var(--text-3);margin-top:2px';
      card.appendChild(hint);
      collect = () => {
        const value = items.map((it) => it.id);
        // 与初始顺序一致且用户从未操作过时视为未作答
        const sameAsInit = q.config.options.every((o, i) => o.id === value[i]);
        return sameAsInit ? null : { question_id: q.id, value };
      };
      break;
    }
    default:
      collect = () => null;
  }
  return {
    root: card,
    collect,
    set onChange(fn) {
      notify = fn;
    },
  };
}

function renderChoices(card, q) {
  const multi = q.type === 'multiple_choice';
  const name = 'q' + q.id;
  const boxes = [];
  q.config.options.forEach((o) => {
    const lab = el('label', 'choice');
    const input = document.createElement('input');
    input.type = multi ? 'checkbox' : 'radio';
    input.name = name;
    input.value = o.id;
    lab.appendChild(input);
    lab.appendChild(el('span', '', o.label));
    input.addEventListener('change', () => {
      if (multi) {
        lab.classList.toggle('checked', input.checked);
      } else {
        card.querySelectorAll('.choice').forEach((c) => c.classList.remove('checked'));
        lab.classList.add('checked');
      }
    });
    boxes.push({ input, id: o.id });
    card.appendChild(lab);
  });
  if (multi) {
    const note = el('div', '', '');
    note.style.cssText = 'font-size:12px;color:var(--text-3);margin-top:2px';
    let txt = '可多选';
    if (q.config.min_select > 0) {
      txt += '，至少 ' + q.config.min_select + ' 项';
    }
    if (q.config.max_select > 0) {
      txt += '，最多 ' + q.config.max_select + ' 项';
    }
    note.textContent = txt;
    card.appendChild(note);
    return () => {
      const ids = boxes.filter((b) => b.input.checked).map((b) => b.id);
      return ids.length === 0 ? null : { question_id: q.id, value: ids };
    };
  }
  return () => {
    const hit = boxes.find((b) => b.input.checked);
    return hit ? { question_id: q.id, value: hit.id } : null;
  };
}

/* ============ 编辑态 ============ */

// 编辑卡片：直接修改传入的 q 对象；结构变化（增删选项/题）调用 onStruct 重渲染
export function renderEditQuestion(q, idx, handlers, allQuestions) {
  const card = el('div', 'q-card');
  card.dataset.idx = idx;

  const head = el('div', 'q-head');
  head.appendChild(el('span', 'q-num', (idx + 1) + '.'));
  const titleInput = el('input', 'q-title-input');
  titleInput.type = 'text';
  titleInput.placeholder = '请输入题干';
  titleInput.value = q.title;
  titleInput.addEventListener('input', () => {
    q.title = titleInput.value;
    handlers.onChange();
  });
  head.appendChild(titleInput);

  const ops = el('div', 'q-ops');
  const mkBtn = (txt, fn, cls) => {
    const b = el('button', 'btn btn-ghost btn-sm' + (cls ? ' ' + cls : ''), txt);
    b.type = 'button';
    b.title = txt;
    b.addEventListener('click', fn);
    return b;
  };
  ops.appendChild(mkBtn('上移', () => handlers.onMove(idx, -1)));
  ops.appendChild(mkBtn('下移', () => handlers.onMove(idx, 1)));
  if (handlers.onAI) {
    ops.appendChild(mkBtn('AI 优化', () => handlers.onAI(idx), 'ai-opt-btn'));
  }
  ops.appendChild(mkBtn('删除', () => handlers.onDelete(idx), 'btn-danger'));
  head.appendChild(ops);
  card.appendChild(head);

  // 题目属性行
  const meta = el('div', 'q-meta');
  const typeLab = el('label', '', TYPE_LABEL[q.type] || q.type);
  meta.appendChild(typeLab);

  const reqLab = el('label');
  const req = document.createElement('input');
  req.type = 'checkbox';
  req.checked = !!q.required;
  req.addEventListener('change', () => {
    q.required = req.checked;
    handlers.onChange();
  });
  reqLab.appendChild(req);
  reqLab.appendChild(document.createTextNode('必答'));
  meta.appendChild(reqLab);

  // 分页标记：该题之后分页
  const pbLab = el('label');
  const pb = document.createElement('input');
  pb.type = 'checkbox';
  pb.checked = !!q.config.page_break_after;
  pb.addEventListener('change', () => {
    q.config.page_break_after = pb.checked;
    handlers.onChange();
  });
  pbLab.appendChild(pb);
  pbLab.appendChild(document.createTextNode('此后分页'));
  meta.appendChild(pbLab);
  card.appendChild(meta);

  appendConfigEditor(card, q, handlers);

  // 显示条件（逻辑跳转）：依赖前面的单选/下拉题
  if (allQuestions) {
    appendVisibleIfEditor(card, q, idx, handlers, allQuestions);
  }
  return card;
}

function numControl(labelText, value, min, max, onSet) {
  const lab = el('label');
  lab.appendChild(document.createTextNode(labelText + ' '));
  const n = document.createElement('input');
  n.type = 'number';
  n.className = 'num';
  n.min = min;
  n.max = max;
  n.value = value;
  n.style.width = '70px';
  n.addEventListener('change', () => {
    let v = parseInt(n.value, 10);
    if (isNaN(v) || v < min) {
      v = min;
    }
    if (v > max) {
      v = max;
    }
    n.value = v;
    onSet(v);
  });
  lab.appendChild(n);
  return lab;
}

// 选项列表编辑器（单选/多选/下拉/排序共用）
function optionsEditor(q, handlers, rerender) {
  const wrap = el('div', '');
  const draw = () => {
    wrap.textContent = '';
    q.config.options.forEach((o, oi) => {
      const row = el('div', 'opt-row');
      const mark = document.createElement('span');
      mark.className = 'opt-radio';
      mark.style.cssText = 'border:1px solid var(--border);background:#fff;border-radius:50%';
      row.appendChild(mark);
      const input = el('input', 'input');
      input.type = 'text';
      input.value = o.label;
      input.placeholder = '选项 ' + (oi + 1);
      input.addEventListener('input', () => {
        o.label = input.value;
        handlers.onChange();
      });
      row.appendChild(input);
      if (q.config.options.length > 2) {
        const del = el('button', 'btn btn-ghost btn-sm', '删除');
        del.type = 'button';
        del.addEventListener('click', () => {
          q.config.options.splice(oi, 1);
          handlers.onChange();
          draw();
        });
        row.appendChild(del);
      }
      wrap.appendChild(row);
    });
    if (q.config.options.length < 26) {
      const add = el('button', 'btn btn-sm');
      add.type = 'button';
      add.textContent = '+ 添加选项';
      add.style.marginLeft = '28px';
      add.addEventListener('click', () => {
        let n = 1;
        while (q.config.options.some((o) => o.id === 'o' + n)) {
          n++;
        }
        q.config.options.push({ id: 'o' + n, label: '' });
        handlers.onChange();
        draw();
      });
      wrap.appendChild(add);
    }
  };
  draw();
  rerender && rerender();
  return wrap;
}

// 矩阵行/列编辑器
function matrixItemsEditor(q, key, what, handlers) {
  const wrap = el('div', '');
  const prefix = key === 'rows' ? 'r' : 'c';
  const draw = () => {
    wrap.textContent = '';
    const title = el('div', '', what + '：');
    title.style.cssText = 'font-size:13px;color:var(--text-2);margin-top:8px;padding-left:28px';
    wrap.appendChild(title);
    q.config[key].forEach((o, oi) => {
      const row = el('div', 'opt-row');
      const input = el('input', 'input');
      input.type = 'text';
      input.value = o.label;
      input.placeholder = what + ' ' + (oi + 1);
      input.addEventListener('input', () => {
        o.label = input.value;
        handlers.onChange();
      });
      row.appendChild(input);
      if (q.config[key].length > 2) {
        const del = el('button', 'btn btn-ghost btn-sm', '删除');
        del.type = 'button';
        del.addEventListener('click', () => {
          q.config[key].splice(oi, 1);
          handlers.onChange();
          draw();
        });
        row.appendChild(del);
      }
      wrap.appendChild(row);
    });
    if (q.config[key].length < 10) {
      const add = el('button', 'btn btn-sm');
      add.type = 'button';
      add.textContent = '+ 添加' + what;
      add.style.marginLeft = '28px';
      add.addEventListener('click', () => {
        let n = 1;
        while (q.config[key].some((o) => o.id === prefix + n)) {
          n++;
        }
        q.config[key].push({ id: prefix + n, label: '' });
        handlers.onChange();
        draw();
      });
      wrap.appendChild(add);
    }
  };
  draw();
  return wrap;
}

function appendConfigEditor(card, q, handlers) {
  const t = q.type;
  if (t === 'single_choice' || t === 'multiple_choice' || t === 'dropdown' || t === 'sorting') {
    card.appendChild(optionsEditor(q, handlers));
    if (t === 'multiple_choice') {
      const meta2 = el('div', 'q-meta');
      meta2.appendChild(numControl('最少选', q.config.min_select || 0, 0, q.config.options.length,
        (v) => { q.config.min_select = v; handlers.onChange(); }));
      meta2.appendChild(numControl('最多选(0 为不限)', q.config.max_select || 0, 0, q.config.options.length,
        (v) => { q.config.max_select = v; handlers.onChange(); }));
      card.appendChild(meta2);
    }
  } else if (t === 'rating') {
    const meta2 = el('div', 'q-meta');
    meta2.appendChild(numControl('评分上限', q.config.max || 5, 2, 10,
      (v) => { q.config.max = v; handlers.onChange(); }));
    card.appendChild(meta2);
  } else if (t === 'text' || t === 'textarea') {
    const meta2 = el('div', 'q-meta');
    meta2.appendChild(numControl('字数上限', q.config.max_len || (t === 'text' ? 100 : 500), 0, 2000,
      (v) => { q.config.max_len = v; handlers.onChange(); }));
    card.appendChild(meta2);
  } else if (t === 'matrix') {
    card.appendChild(matrixItemsEditor(q, 'rows', '行', handlers));
    card.appendChild(matrixItemsEditor(q, 'cols', '列', handlers));
  }
  // date 题无额外配置
}

// 显示条件编辑器：依赖题下拉 + 条件选项多选 + 清除
function appendVisibleIfEditor(card, q, idx, handlers, allQuestions) {
  const sources = [];
  allQuestions.forEach((prev, pi) => {
    if (pi < idx && (prev.type === 'single_choice' || prev.type === 'dropdown')) {
      sources.push({ index: pi, q: prev });
    }
  });
  const box = el('div', '');
  box.style.cssText = 'margin-top:8px;padding:8px 10px;border:1px dashed var(--border);border-radius:6px';

  const draw = () => {
    box.textContent = '';
    const head = el('div', '');
    head.style.cssText = 'font-size:13px;color:var(--text-2);margin-bottom:6px';
    head.textContent = '显示条件：';
    if (!q.config.visible_if) {
      head.textContent += '始终显示';
    } else {
      const src = sources.find((s) => s.index === q.config.visible_if.question_index);
      head.textContent += '当第 ' + (q.config.visible_if.question_index + 1) + ' 题（' + (src ? src.q.title : '?') + '）选中指定选项时显示';
    }
    box.appendChild(head);

    if (!q.config.visible_if) {
      if (sources.length === 0) {
        box.appendChild(el('div', '', '（前面暂无可依赖的单选/下拉题）')).style.cssText = 'font-size:12px;color:var(--text-3)';
        return;
      }
      const add = el('button', 'btn btn-sm', '+ 设置显示条件');
      add.type = 'button';
      add.addEventListener('click', () => {
        q.config.visible_if = { question_index: sources[0].index, option_ids: [sources[0].q.config.options[0].id] };
        handlers.onChange();
        draw();
      });
      box.appendChild(add);
      return;
    }

    const src = sources.find((s) => s.index === q.config.visible_if.question_index);
    if (src) {
      const row = el('div', 'opt-row');
      row.style.paddingLeft = '0';
      const sel = el('select', 'input');
      sel.style.maxWidth = '200px';
      sources.forEach((s) => {
        const opt = el('option', '', '第 ' + (s.index + 1) + ' 题：' + s.q.title);
        opt.value = String(s.index);
        sel.appendChild(opt);
      });
      sel.value = String(src.index);
      sel.addEventListener('change', () => {
        q.config.visible_if.question_index = parseInt(sel.value, 10);
        const ns = sources.find((s) => s.index === q.config.visible_if.question_index);
        q.config.visible_if.option_ids = [ns.q.config.options[0].id];
        handlers.onChange();
        draw();
      });
      row.appendChild(sel);
      box.appendChild(row);

      // 选项多选
      const srcQ = src.q;
      const optRow = el('div', '');
      optRow.style.cssText = 'display:flex;flex-wrap:wrap;gap:8px;margin-top:6px';
      srcQ.config.options.forEach((o) => {
        const lab = el('label', '');
        lab.style.cssText = 'display:flex;align-items:center;gap:4px;font-size:13px';
        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.checked = q.config.visible_if.option_ids.includes(o.id);
        cb.addEventListener('change', () => {
          const ids = new Set(q.config.visible_if.option_ids);
          if (cb.checked) {
            ids.add(o.id);
          } else {
            ids.delete(o.id);
          }
          q.config.visible_if.option_ids = Array.from(ids);
          handlers.onChange();
        });
        lab.appendChild(cb);
        lab.appendChild(document.createTextNode(o.label));
        optRow.appendChild(lab);
      });
      box.appendChild(optRow);
    }

    const clear = el('button', 'btn btn-ghost btn-sm', '清除条件');
    clear.type = 'button';
    clear.style.marginTop = '6px';
    clear.addEventListener('click', () => {
      q.config.visible_if = null;
      handlers.onChange();
      draw();
    });
    box.appendChild(clear);
  };
  draw();
  card.appendChild(box);
}
