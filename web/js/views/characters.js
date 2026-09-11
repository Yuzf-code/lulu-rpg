// 角色卡：列表 + 编辑器（上传/AI 生成头像、AI 草稿、文本蒸馏）。
import { api } from '../api.js';
import { esc, toast, avatarHTML, onActions, fileToDataURL, modal } from '../util.js';

export async function renderList(root) {
  const characters = await api.listCharacters();
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>🎭 角色卡</h1><p class="sub">他们将在你的故事里登场</p></div>
        <a class="btn primary" href="#/characters/new">＋ 创建</a>
      </header>
      <div class="char-grid">
        ${characters.length === 0 ? `
          <div class="empty-state wide">
            <div class="empty-ico">🎭</div>
            <p>还没有角色。</p>
            <p class="sub">创建第一张角色卡：性格、背景、形象，都由你定义；也可以直接从一段文本蒸馏。</p>
            <a class="btn primary" href="#/characters/new">＋ 创建角色卡</a>
          </div>` : characters.map((c) => `
          <div class="char-card" data-action="edit" data-id="${esc(c.id)}" role="button" tabindex="0">
            ${avatarHTML(c.avatar_path, c.name, 'lg')}
            <div class="char-name">${esc(c.name)}</div>
            <div class="char-title">${esc(c.title || '—')}</div>
            <div class="char-brief">${esc(c.personality || c.background || '').slice(0, 60)}</div>
            <div class="char-tags">${(parseTags(c.tags) || []).slice(0, 3).map((t) => `<span class="tag">${esc(t)}</span>`).join('')}</div>
          </div>`).join('')}
      </div>
    </div>`;
  onActions(root, {
    edit: (t) => { location.hash = '#/characters/' + t.dataset.id; },
  });
}

function parseTags(raw) {
  try { return typeof raw === 'string' ? JSON.parse(raw || '[]') : (raw || []); } catch { return []; }
}
function parseDialogues(raw) {
  try { return typeof raw === 'string' ? JSON.parse(raw || '[]') : (raw || []); } catch { return []; }
}
function parseRelationships(raw) {
  try { return typeof raw === 'string' ? JSON.parse(raw || '[]') : (raw || []); } catch { return []; }
}

// ---- 编辑器 ----

export async function renderEditor(root, id) {
  const card = id ? await api.getCharacter(id) : null;
  const dialogues = card ? parseDialogues(card.example_dialogues) : [];
  const relationships = card ? parseRelationships(card.relationships) : [];
  let avatarPath = card ? card.avatar_path : '';

  root.innerHTML = `
    <div class="page narrow">
      <header class="page-head">
        <a class="icon-btn" href="#/characters">←</a>
        <div><h1>${card ? '编辑角色' : '创建角色卡'}</h1></div>
        <button class="btn small" data-action="distill">🧪 文本蒸馏</button>
        ${card ? `<button class="btn danger-ghost" data-action="del">删除</button>` : ''}
      </header>
      <form class="editor" id="char-form">
        <div class="avatar-editor">
          <div class="avatar-preview" id="avatar-preview">${avatarHTML(avatarPath, card ? card.name : '?', 'xl')}</div>
          <div class="avatar-actions">
            <label class="btn small">📁 上传图片<input type="file" accept="image/*" hidden id="avatar-file" /></label>
            <button type="button" class="btn small" data-action="gen-avatar">🪄 AI 生成</button>
            ${avatarPath ? `<button type="button" class="btn small ghost" data-action="rm-avatar">移除</button>` : ''}
          </div>
          <p class="hint">AI 生成会参考「名称 + 外貌」描述；也可以先填好外貌再生成。</p>
        </div>

        <label class="field"><span>名称 *</span><input name="name" required maxlength="40" value="${esc(card ? card.name : '')}" placeholder="例如：艾莉娅·风语" /></label>
        <label class="field"><span>头衔 / 身份</span><input name="title" maxlength="60" value="${esc(card ? card.title : '')}" placeholder="例如：流浪剑士" /></label>
        <label class="field"><span>标签（逗号分隔）</span><input name="tags" value="${esc((parseTags(card ? card.tags : null) || []).join('，'))}" placeholder="佣兵，剑士" /></label>
        <label class="field"><span>外貌（用于写作与生图）</span>
          <textarea name="appearance" rows="3" placeholder="发色、瞳色、体型、标志性服饰与携带物…">${esc(card ? card.appearance : '')}</textarea></label>
        <label class="field"><span>性格</span>
          <textarea name="personality" rows="3" placeholder="说话方式、脾气、在意的事、雷区…">${esc(card ? card.personality : '')}</textarea></label>
        <label class="field"><span>背景故事</span>
          <textarea name="background" rows="4" placeholder="出身、经历、目标、秘密…">${esc(card ? card.background : '')}</textarea></label>

        <div class="field">
          <div class="field-head"><span>开场白（可选，仅单角色开局时直接使用）</span>
            <button type="button" class="btn small ghost" data-action="gen-greeting">🪄 AI 草稿</button></div>
          <textarea name="greeting" rows="3" placeholder="角色登场的第一句话/第一幕；留空则由写手自由开场">${esc(card ? card.greeting : '')}</textarea>
        </div>

        <div class="field"><span>关系 / 态度（可选，每行一条：「主体：描述」。主体通常是你的档案名或另一角色，写手会在同台时使用）</span>
          <textarea name="relationships" rows="2" placeholder="林远：戒备之中藏着一丝说不清的在意……">${esc(relationships.map((r) => `${r.subject}：${r.text}`).join('\n'))}</textarea></label>

        <div class="field">
          <div class="field-head"><span>对话示例（帮助写手学习语气，可选）</span>
            <button type="button" class="btn small ghost" data-action="gen-dialogues">🪄 AI 草稿</button></div>
          <div id="dialogues"></div>
          <button type="button" class="btn small ghost" data-action="add-dialogue">＋ 添加一组</button>
        </div>

        <div class="modal-actions">
          ${card ? '' : '<a class="btn" href="#/characters">取消</a>'}
          <button type="submit" class="btn primary">保存</button>
        </div>
      </form>
    </div>`;

  const dlgBox = root.querySelector('#dialogues');
  const addDialogueRow = (u = '', c = '') => {
    const row = document.createElement('div');
    row.className = 'dlg-row';
    row.innerHTML = `
      <input placeholder="玩家说…" value="${esc(u)}" />
      <input placeholder="${esc(card ? card.name : '角色')}答…" value="${esc(c)}" />
      <button type="button" class="icon-btn danger" title="删除">✕</button>`;
    row.querySelector('button').addEventListener('click', () => row.remove());
    dlgBox.appendChild(row);
  };
  dialogues.forEach((d) => addDialogueRow(d.user, d.char));
  if (dialogues.length === 0) addDialogueRow();

  const seedFromForm = () => ({
    name: f().name.value.trim(),
    title: f().title.value.trim(),
    appearance: f().appearance.value,
    personality: f().personality.value,
    background: f().background.value,
    scenario: '',
  });
  const f = () => root.querySelector('#char-form');
  const setBusy = (btn, busy, label) => {
    btn.disabled = busy;
    if (label) btn.textContent = label;
  };

  root.addEventListener('click', async (e) => {
    const act = (sel) => e.target.closest(`[data-action="${sel}"]`);

    if (act('add-dialogue')) addDialogueRow();

    if (act('rm-avatar')) {
      avatarPath = '';
      root.querySelector('#avatar-preview').innerHTML = avatarHTML('', '?', 'xl');
      e.target.remove();
    }

    if (act('gen-avatar')) {
      const btn = e.target.closest('button');
      const name = f().name.value.trim();
      if (!name) return toast('先填一下名称，AI 才知道画谁', 'error');
      setBusy(btn, true, '🎨 生成中…');
      try {
        const r = await api.generateAvatar(name, f().appearance.value);
        avatarPath = r.url;
        root.querySelector('#avatar-preview').innerHTML = avatarHTML(avatarPath, name, 'xl');
        if (!root.querySelector('[data-action="rm-avatar"]')) {
          root.querySelector('.avatar-actions').insertAdjacentHTML('beforeend',
            '<button type="button" class="btn small ghost" data-action="rm-avatar">移除</button>');
        }
      } catch (err) { toast(err.message, 'error'); }
      setBusy(btn, false, '🪄 AI 生成');
    }

    if (act('gen-greeting')) {
      const btn = e.target.closest('button');
      setBusy(btn, true, '✍️ 构思中…');
      try {
        const r = await api.generateCardDraft('greeting', seedFromForm());
        f().greeting.value = r.greeting;
        toast('开场白草稿已生成，可自由修改');
      } catch (err) { toast(err.message, 'error'); }
      setBusy(btn, false, '🪄 AI 草稿');
    }

    if (act('gen-dialogues')) {
      const btn = e.target.closest('button');
      setBusy(btn, true, '✍️ 构思中…');
      try {
        const r = await api.generateCardDraft('dialogues', seedFromForm());
        // 清掉空白占位行后追加草稿。
        dlgBox.querySelectorAll('.dlg-row').forEach((row) => {
          const inputs = row.querySelectorAll('input');
          if (!inputs[0].value.trim() && !inputs[1].value.trim()) row.remove();
        });
        r.example_dialogues.forEach((d) => addDialogueRow(d.user, d.char));
        toast('对话示例草稿已生成，可自由修改');
      } catch (err) { toast(err.message, 'error'); }
      setBusy(btn, false, '🪄 AI 草稿');
    }

    if (act('distill')) distillModal(card, root, { importDraft: (draft) => importIntoForm(draft, addDialogueRow) });

    if (act('del')) {
      if (!confirm(`删除角色「${card.name}」？历史剧情会保留，但该角色不再可选。`)) return;
      try {
        await api.deleteCharacter(card.id);
        location.hash = '#/characters';
      } catch (err) { toast(err.message, 'error'); }
    }
  });

  root.querySelector('#avatar-file').addEventListener('change', async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    try {
      const dataURL = await fileToDataURL(file);
      const r = await api.uploadImage(dataURL);
      avatarPath = r.url;
      root.querySelector('#avatar-preview').innerHTML = avatarHTML(avatarPath, '?', 'xl');
      if (!root.querySelector('[data-action="rm-avatar"]')) {
        root.querySelector('.avatar-actions').insertAdjacentHTML('beforeend',
          '<button type="button" class="btn small ghost" data-action="rm-avatar">移除</button>');
      }
    } catch (err) { toast(err.message, 'error'); }
    e.target.value = '';
  });

  root.querySelector('#char-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const form = e.target;
    const body = {
      name: form.name.value,
      title: form.title.value,
      tags: form.tags.value.split(/[，,]/).map((s) => s.trim()).filter(Boolean),
      appearance: form.appearance.value,
      personality: form.personality.value,
      background: form.background.value,
      greeting: form.greeting.value,
      avatar_path: avatarPath,
      example_dialogues: [...dlgBox.querySelectorAll('.dlg-row')].map((row) => ({
        user: row.querySelectorAll('input')[0].value,
        char: row.querySelectorAll('input')[1].value,
      })),
      relationships: form.relationships.value.split('\n')
        .map((line) => {
          const i = line.search(/[：:]/);
          return i > 0 ? { subject: line.slice(0, i).trim(), text: line.slice(i + 1).trim() } : null;
        })
        .filter((r) => r && r.subject && r.text),
    };
    try {
      if (card) await api.updateCharacter(card.id, body);
      else await api.createCharacter(body);
      toast('已保存');
      location.hash = '#/characters';
    } catch (err) { toast(err.message, 'error'); }
  });
}

// 把蒸馏草稿导入编辑器表单。
function importIntoForm(draft, addDialogueRow) {
  const form = document.querySelector('#char-form');
  if (!form) return;
  const hasContent = ['title', 'appearance', 'personality', 'background', 'greeting'].some((k) => form[k].value.trim());
  if (hasContent && !confirm('导入将覆盖表单中已填写的设定字段（对话示例与关系为追加），继续？')) return;
  if (draft.name) form.name.value = draft.name;
  if (draft.title) form.title.value = draft.title;
  if (draft.appearance) form.appearance.value = draft.appearance;
  if (draft.personality) form.personality.value = draft.personality;
  if (draft.background) form.background.value = draft.background;
  if (draft.greeting) form.greeting.value = draft.greeting;
  if (draft.tags && draft.tags.length) form.tags.value = draft.tags.join('，');
  if (draft.example_dialogues && draft.example_dialogues.length) {
    form.querySelectorAll('#dialogues .dlg-row').forEach((row) => {
      const inputs = row.querySelectorAll('input');
      if (!inputs[0].value.trim() && !inputs[1].value.trim()) row.remove();
    });
    draft.example_dialogues.forEach((d) => addDialogueRow(d.user, d.char));
  }
  if (draft.relationships && draft.relationships.length) {
    const existing = form.relationships.value.split('\n').filter((l) => l.trim());
    const lines = draft.relationships.map((r) => `${r.subject}：${r.text}`);
    form.relationships.value = [...new Set([...existing, ...lines])].join('\n');
  }
  toast('已导入到表单，请检查后保存');
}

// ---- 文本蒸馏 ----

function distillModal(card, root, { importDraft }) {
  const m = modal(`
    <h2>🧪 从文本蒸馏角色卡</h2>
    <p class="hint">粘贴小说、跑团记录、设定文档等原文，提取角色卡；编辑已有角色时可选择「增强合并」。</p>
    <label class="field"><span>原文 *</span>
      <textarea id="ds-text" rows="8" placeholder="粘贴包含目标人物的文本……（上限 16000 字）"></textarea></label>
    <label class="field"><span>蒸馏对象（逗号分隔；留空由 AI 自动识别，最多 5 个）</span>
      <input id="ds-targets" placeholder="${esc(card ? card.name : '例如：艾莉娅，老巴德')}" /></label>
    <label class="field"><span>主体（可选：蒸馏对象对其的态度/关系，如你的档案名或另一角色名）</span>
      <input id="ds-subject" placeholder="例如：林远" /></label>
    ${card ? `
    <label class="switch-row"><span>增强当前「${esc(card.name)}」（结果与现有设定合并）</span>
      <input type="checkbox" id="ds-enhance" checked /><i class="switch"></i></label>` : ''}
    <div class="modal-actions">
      <button class="btn" data-close>取消</button>
      <button class="btn primary" data-action="run">开始蒸馏</button>
    </div>`);

  m.root.addEventListener('click', async (e) => {
    if (e.target.matches('[data-close]')) return m.close();
    if (!e.target.closest('[data-action="run"]')) return;

    const text = m.root.querySelector('#ds-text').value.trim();
    if (!text) return toast('请粘贴原文', 'error');
    const subject = m.root.querySelector('#ds-subject').value.trim();
    const targets = m.root.querySelector('#ds-targets').value.split(/[，,]/).map((s) => s.trim()).filter(Boolean);
    const enhance = m.root.querySelector('#ds-enhance');

    const btn = m.root.querySelector('[data-action="run"]');
    btn.disabled = true; btn.textContent = '🧪 蒸馏中…';
    try {
      let drafts;
      if (enhance && enhance.checked) {
        const r = await api.distillInto(card.id, text, subject);
        drafts = [{ target: card.name, character: r.character }];
      } else {
        drafts = (await api.distill(text, subject, targets)).drafts;
      }
      m.close();
      draftsModal(drafts, importDraft);
    } catch (err) {
      toast(err.message, 'error');
      btn.disabled = false; btn.textContent = '开始蒸馏';
    }
  });
}

// 蒸馏结果预览：逐卡展示，确认后导入表单。
function draftsModal(drafts, importDraft) {
  const usable = drafts.filter((d) => d.character);
  const m = modal(`
    <h2>🧪 蒸馏结果（${usable.length}/${drafts.length}）</h2>
    ${drafts.map((d, i) => {
      if (!d.character) {
        return `<div class="draft-card"><div class="draft-head">⚠ ${esc(d.target)}：${esc(d.error || '蒸馏失败')}</div></div>`;
      }
      const c = d.character;
      return `
      <div class="draft-card">
        <div class="draft-head"><b>${esc(c.name || d.target)}</b><span class="tag">${esc(c.title || '无头衔')}</span></div>
        ${c.appearance ? `<p><i>外貌</i>${esc(c.appearance)}</p>` : ''}
        ${c.personality ? `<p><i>性格</i>${esc(c.personality)}</p>` : ''}
        ${c.background ? `<p><i>背景</i>${esc(c.background)}</p>` : ''}
        ${c.greeting ? `<p><i>开场白</i>${esc(c.greeting)}</p>` : ''}
        ${(c.relationships || []).map((r) => `<p><i>对${esc(r.subject)}</i>${esc(r.text)}</p>`).join('')}
        <div class="modal-actions"><button class="btn small primary" data-import="${i}">📥 导入到表单</button></div>
      </div>`;
    }).join('')}
    <p class="hint">导入后可在表单中继续调整，确认无误再保存。</p>
    <div class="modal-actions"><button class="btn" data-close>关闭</button></div>`);

  m.root.addEventListener('click', (e) => {
    if (e.target.matches('[data-close]')) return m.close();
    const btn = e.target.closest('[data-import]');
    if (btn) {
      importDraft(drafts[Number(btn.dataset.import)].character);
      m.close();
    }
  });
}
