// 文本蒸馏（独立入口）：粘贴原文 → 提取角色卡草稿 + 写作风格草稿 → 一键入库。
// 蒸馏结果不会自动保存，逐项确认后再写入。
import { api } from '../api.js';
import { esc, toast } from '../util.js';

export async function render(root) {
  root.innerHTML = `
    <div class="page narrow">
      <header class="page-head">
        <a class="icon-btn" href="#/characters">←</a>
        <div><h1>🧪 文本蒸馏</h1><p class="sub">从小说 / 跑团记录 / 设定文档中提取角色卡与写作风格</p></div>
      </header>
      <div class="editor">
        <div class="field"><span>原文 *</span>
          <textarea id="ds-text" rows="10" placeholder="粘贴包含目标人物的文本……（上限 16000 字）"></textarea></div>
        <div class="field"><span>蒸馏对象（逗号分隔；留空由 AI 自动识别，最多 5 个）</span>
          <input id="ds-targets" placeholder="例如：艾莉娅，老巴德" /></div>
        <div class="field"><span>主体（可选：蒸馏对象对其的态度/关系，如你的档案名或另一角色名）</span>
          <input id="ds-subject" placeholder="例如：林远" /></div>
        <label class="switch-row"><span>同时提炼写作风格（开局新游戏时可选）</span>
          <input type="checkbox" id="ds-style" checked /><i class="switch"></i></label>
        <div class="modal-actions" style="justify-content:flex-end">
          <button type="button" class="btn primary" id="ds-run">🧪 开始蒸馏</button>
        </div>
      </div>
      <div id="ds-results"></div>
    </div>`;

  const $ = (sel) => root.querySelector(sel);
  const resultsBox = $('#ds-results');

  $('#ds-run').addEventListener('click', async () => {
    const text = $('#ds-text').value.trim();
    if (!text) return toast('请粘贴原文', 'error');
    const targets = $('#ds-targets').value.split(/[，,]/).map((s) => s.trim()).filter(Boolean);
    const subject = $('#ds-subject').value.trim();
    const wantStyle = $('#ds-style').checked;

    const btn = $('#ds-run');
    btn.disabled = true; btn.textContent = '🧪 蒸馏中…（多对象并行，请稍候）';
    resultsBox.innerHTML = '';
    try {
      const out = await api.distill(text, subject, targets);
      let html = '';
      if (wantStyle && out.style) html += styleCard(out.style);
      if (wantStyle && out.style_error) html += `<div class="banner warn">风格提炼失败：${esc(out.style_error)}</div>`;
      html += (out.drafts || []).map((d, i) => draftCard(d, i)).join('');
      resultsBox.innerHTML = html || '<div class="empty-state"><p>没有产出结果。</p></div>';
      wireSaves(resultsBox, out);
    } catch (err) {
      toast(err.message, 'error');
    }
    btn.disabled = false; btn.textContent = '🧪 开始蒸馏';
  });

  function styleCard(st) {
    return `
      <div class="draft-card" data-kind="style">
        <div class="draft-head"><b>🖋 写作风格：${esc(st.name)}</b></div>
        <p>${esc(st.description)}</p>
        <div class="modal-actions"><button class="btn small primary" data-save-style>📥 保存为风格</button></div>
      </div>`;
  }

  function draftCard(d, i) {
    if (!d.character) {
      return `<div class="draft-card"><div class="draft-head">⚠ ${esc(d.target)}：${esc(d.error || '蒸馏失败')}</div></div>`;
    }
    const c = d.character;
    return `
      <div class="draft-card" data-kind="char" data-idx="${i}">
        <div class="draft-head"><b>🎭 ${esc(c.name || d.target)}</b><span class="tag">${esc(c.title || '无头衔')}</span></div>
        ${c.appearance ? `<p><i>外貌</i>${esc(c.appearance)}</p>` : ''}
        ${c.personality ? `<p><i>性格</i>${esc(c.personality)}</p>` : ''}
        ${c.background ? `<p><i>背景</i>${esc(c.background)}</p>` : ''}
        ${c.greeting ? `<p><i>开场白</i>${esc(c.greeting)}</p>` : ''}
        ${(c.relationships || []).map((r) => `<p><i>对${esc(r.subject)}</i>${esc(r.text)}</p>`).join('')}
        <div class="modal-actions"><button class="btn small primary" data-save-char="${i}">📥 保存为角色卡</button></div>
      </div>`;
  }

  function wireSaves(box, out) {
    box.addEventListener('click', async (e) => {
      const styleBtn = e.target.closest('[data-save-style]');
      if (styleBtn && out.style) {
        styleBtn.disabled = true;
        try {
          await api.createStyle({ name: out.style.name, description: out.style.description });
          styleBtn.textContent = '✓ 已保存，开局时可选';
        } catch (err) {
          toast(err.message, 'error');
          styleBtn.disabled = false;
        }
      }
      const charBtn = e.target.closest('[data-save-char]');
      if (charBtn) {
        const d = out.drafts[Number(charBtn.dataset.saveChar)];
        if (!d || !d.character) return;
        charBtn.disabled = true;
        const c = d.character;
        try {
          await api.createCharacter({
            name: c.name || d.target,
            title: c.title,
            appearance: c.appearance,
            personality: c.personality,
            background: c.background,
            greeting: c.greeting,
            tags: c.tags || [],
            example_dialogues: c.example_dialogues || [],
            relationships: c.relationships || [],
          });
          charBtn.textContent = '✓ 已保存到角色卡';
        } catch (err) {
          toast(err.message, 'error');
          charBtn.disabled = false;
        }
      }
    }, { once: false });
  }
}
