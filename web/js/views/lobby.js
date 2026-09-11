// 会话大厅：游戏实例列表 + 开新游戏。
import { api } from '../api.js';
import { state } from '../app.js';
import { esc, fmtTime, toast, avatarHTML, modal, onActions } from '../util.js';

export async function render(root) {
  const [sessions, personas, characters, styles] = await Promise.all([
    api.listSessions(), api.listPersonas(), api.listCharacters(), api.listStyles(),
  ]);

  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>🗓️ 冒险目录</h1>
          <p class="sub">每个对话都是一段独立的旅程</p>
        </div>
        <button class="btn primary" data-action="new-game">＋ 新游戏</button>
      </header>
      ${mockBanner()}
      ${characters.length === 0 ? emptyCharHint() : ''}
      <div class="session-list">
        ${sessions.length === 0 ? `
          <div class="empty-state">
            <div class="empty-ico">🌙</div>
            <p>还没有冒险。</p>
            <p class="sub">选几位角色，开始你的第一个故事吧。</p>
          </div>` : sessions.map(sessionCard).join('')}
      </div>
    </div>`;

  onActions(root, {
    'new-game': () => newGameModal(personas, characters, styles || []),
    'open-session': (t) => { location.hash = '#/chat/' + t.dataset.id; },
    'del-session': async (t, e) => {
      e.stopPropagation();
      if (!confirm('删除这个会话？聊天记录与配图都会一并删除，不可恢复。')) return;
      try {
        await api.deleteSession(t.dataset.id);
        toast('已删除');
        render(root);
      } catch (err) { toast(err.message, 'error'); }
    },
  });
}

function mockBanner() {
  if (state.config.llm.provider !== 'mock') return '';
  return `<div class="banner">⚗️ 当前为<b>内置演示模式</b>：剧情由本地 Mock 写手生成、配图为占位画。配置 <code>LLM_PROVIDER=openai</code>、<code>LLM_MODEL</code> 等环境变量即可接入真实模型。</div>`;
}

function emptyCharHint() {
  return `<div class="banner warn">还没有角色卡。先去 <a href="#/characters">🎭 角色</a> 页创建或了解示例角色。</div>`;
}

function sessionCard(s) {
  const chars = s.characters || [];
  const derived = s.parent_id ? '<span class="badge">私聊</span>' : '';
  const img = s.auto_image ? '<span class="badge soft">🎨 自动配图</span>' : '';
  return `
    <div class="session-card" data-action="open-session" data-id="${esc(s.id)}" role="button" tabindex="0">
      <div class="session-avatars">${chars.slice(0, 3).map((c) => avatarHTML(c.avatar_path, c.name, 'sm')).join('')}</div>
      <div class="session-main">
        <div class="session-title">${esc(s.title)} ${derived}${img}</div>
        <div class="session-preview">${esc(s.preview || '（尚未开始）')}</div>
      </div>
      <div class="session-side">
        <span class="session-time">${fmtTime(s.updated_at)}</span>
        <button class="icon-btn danger" data-action="del-session" data-id="${esc(s.id)}" title="删除">✕</button>
      </div>
    </div>`;
}

// ---- 开新游戏 ----

function newGameModal(personas, characters, styles) {
  if (characters.length === 0) {
    toast('请先创建角色卡', 'error');
    location.hash = '#/characters';
    return;
  }
  const defaultPersona = personas.find((p) => p.is_default) || personas[0];
  const m = modal(`
    <h2>🎬 开始新游戏</h2>
    <div class="field"><span>你的角色档案</span>
      <div class="persona-pick">
        ${personas.length === 0 ? '<p class="sub">尚未创建档案，本局将以「旅行者」身份登场。可稍后在「档案」页创建。</p>'
          : personas.map((p) => `
            <label class="pick-item">
              <input type="radio" name="persona" value="${esc(p.id)}" ${p.id === (defaultPersona && defaultPersona.id) ? 'checked' : ''} />
              ${avatarHTML(p.avatar_path, p.name, 'sm')}
              <span>${esc(p.name)}</span>
            </label>`).join('')}
      </div>
    </div>
    <div class="field"><span>出场角色（可多选）</span>
      <div class="char-pick">
        ${characters.map((c) => `
          <button type="button" class="pick-chip" data-id="${esc(c.id)}">
            ${avatarHTML(c.avatar_path, c.name, 'sm')}<span>${esc(c.name)}</span>
          </button>`).join('')}
      </div>
    </div>
    ${styles.length ? `
    <div class="field"><span>写作风格（可选，可先到 🧪 蒸馏页从文本提炼）</span>
      <select name="style_id">
        <option value="">不指定</option>
        ${styles.map((st) => `<option value="${esc(st.id)}">${esc(st.name)}</option>`).join('')}
      </select>
    </div>` : ''}
    <div class="field"><span>世界 / 场景设定（可选）</span>
      <textarea name="scenario" rows="3" placeholder="例如：架空的东方王朝，江湖门派林立；或直接留空由写手发挥"></textarea>
    </div>
    <div class="field"><span>会话标题（可选）</span>
      <input name="title" placeholder="留空自动生成" />
    </div>
    ${state.config.image.enabled ? `
    <label class="switch-row">
      <span>🎨 每轮自动生成配图</span>
      <input type="checkbox" name="auto_image" checked /><i class="switch"></i>
    </label>` : ''}
    <div class="modal-actions">
      <button type="button" class="btn" data-close>取消</button>
      <button type="button" class="btn primary" data-action="start">开始冒险</button>
    </div>`);

  const selected = new Set();
  m.root.addEventListener('click', (e) => {
    const chip = e.target.closest('.pick-chip');
    if (chip) {
      const id = chip.dataset.id;
      selected.has(id) ? selected.delete(id) : selected.add(id);
      chip.classList.toggle('on', selected.has(id));
    }
  });

  m.root.querySelector('[data-action="start"]').addEventListener('click', async () => {
    if (selected.size === 0) return toast('请至少选择一位角色', 'error');
    const persona = m.root.querySelector('input[name="persona"]:checked');
    const styleSel = m.root.querySelector('[name="style_id"]');
    const body = {
      character_ids: [...selected],
      persona_id: persona ? persona.value : '',
      style_id: styleSel ? styleSel.value : '',
      scenario: m.root.querySelector('[name="scenario"]').value.trim(),
      title: m.root.querySelector('[name="title"]').value.trim(),
      auto_image: !!m.root.querySelector('[name="auto_image"]')?.checked,
    };
    const btn = m.root.querySelector('[data-action="start"]');
    btn.disabled = true;
    try {
      const s = await api.createSession(body);
      m.close();
      location.hash = '#/chat/' + s.id;
    } catch (err) {
      toast(err.message, 'error');
      btn.disabled = false;
    }
  });
}
