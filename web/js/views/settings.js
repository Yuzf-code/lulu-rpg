// 全局设置：思考档位等运行时参数。
// 档位是全局的——影响剧情、蒸馏、风格、灵感等所有生成；
// 选择同时保存到服务端（数据库，重启保留）与 localStorage（界面记忆）。
import { api } from '../api.js';
import { state } from '../app.js';
import { esc, toast } from '../util.js';

const THINK_KEY = 'reasoning_effort';
const LEVELS = [
  { v: 'none', label: '关闭', desc: '不思考，速度最快，质量一般' },
  { v: 'low', label: '低', desc: '简短思考，速度与质量均衡偏快' },
  { v: 'medium', label: '中（推荐）', desc: '常规思考深度' },
  { v: 'high', label: '高', desc: '最长思考，质量优先，速度最慢' },
];

export async function render(root) {
  let current = localStorage.getItem(THINK_KEY) || 'medium';
  let serverErr = '';
  try {
    const s = await api.getSettings();
    if (s.reasoning_effort) current = s.reasoning_effort; // 服务端为准
  } catch (e) { serverErr = e.message; }

  const cfg = state.config || {};

  root.innerHTML = `
    <div class="page narrow">
      <header class="page-head">
        <div><h1>⚙️ 设置</h1><p class="sub">全局参数，对所有生成生效</p></div>
      </header>
      <div class="editor">
        <div class="field">
          <span>🧠 思考档位</span>
          <select id="think-sel">
            ${LEVELS.map((l) => `<option value="${l.v}" ${l.v === current ? 'selected' : ''}>${l.label}</option>`).join('')}
          </select>
          <p class="hint" id="think-desc">${esc(levelDesc(current))}</p>
          <p class="hint">档位越高，剧情与蒸馏的质量通常越好，但耗时明显增加；即时生效，无需重启。</p>
        </div>

        <div class="draft-card">
          <div class="draft-head"><b>模型信息</b></div>
          <p><i>剧情写手</i>${cfg.llm?.provider === 'mock' ? '内置演示模式（Mock）' : esc(cfg.llm?.model || '未配置')}</p>
          <p><i>回合配图</i>${cfg.image?.enabled ? esc(cfg.image.model || '已启用') : '未启用'}</p>
        </div>
        ${serverErr ? `<div class="banner warn">无法读取服务端设置：${esc(serverErr)}（当前显示的是本机缓存值）</div>` : ''}
      </div>
    </div>`;

  const sel = root.querySelector('#think-sel');
  const desc = root.querySelector('#think-desc');
  sel.addEventListener('change', async () => {
    const v = sel.value;
    localStorage.setItem(THINK_KEY, v);
    desc.textContent = levelDesc(v);
    try {
      await api.updateSettings({ reasoning_effort: v });
      toast('思考档位已切换：' + (LEVELS.find((l) => l.v === v) || {}).label);
    } catch (e) {
      toast(e.message, 'error');
    }
  });
}

function levelDesc(v) {
  return (LEVELS.find((l) => l.v === v) || {}).desc || '';
}
