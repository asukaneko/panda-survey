// 独立统计页：/stats/{id} 本人视图；/share/{token} 匿名只读视图
import { renderStatsView } from '/js/statsview.js';
import { api } from '/js/api.js';

const shareMatch = location.pathname.match(/^\/share\/([0-9a-f]+)/);
const statsMatch = location.pathname.match(/^\/stats\/(\d+)/);
const view = document.getElementById('view');

async function boot() {
  if (shareMatch) {
    renderStatsView(view, { shareToken: shareMatch[1] });
    return;
  }
  if (!statsMatch) {
    view.textContent = '链接无效';
    return;
  }
  let aiEnabled = false;
  try {
    const st = await api('/api/ai/status');
    aiEnabled = !!st.enabled;
  } catch (e) {
    /* AI 未开通时静默 */
  }
  renderStatsView(view, { sid: statsMatch[1], aiEnabled });
}

boot();
