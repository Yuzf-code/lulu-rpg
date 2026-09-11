// 角色档案：用户在故事中代表自己的身份。
import { api } from '../api.js';
import { esc, toast, avatarHTML, modal, onActions, fileToDataURL } from '../util.js';

export async function render(root) {
  const personas = await api.listPersonas();
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div><h1>🪪 角色档案</h1><p class="sub">每局游戏中「你」的身份设定</p></div>
        <button class="btn primary" data-action="new">＋ 新建档案</button>
      </header>
      <div class="persona-list">
        ${personas.length === 0 ? `
          <div class="empty-state wide">
            <div class="empty-ico">🪪</div>
            <p>还没有档案。</p>
            <p class="sub">创建一个「你」，写手会据此决定如何呼应你的言行。</p>
          </div>` : personas.map((p) => `
          <div class="persona-card ${p.is_default ? 'default' : ''}" data-action="edit" data-id="${esc(p.id)}" role="button" tabindex="0">
            ${avatarHTML(p.avatar_path, p.name, 'lg')}
            <div class="persona-main">
              <div class="persona-name">${esc(p.name)}
                ${p.is_default ? '<span class="badge">默认</span>' : ''}
              </div>
              <div class="persona-desc">${esc(p.description || '（暂无描述）')}</div>
            </div>
            <div class="persona-side">
              ${p.is_default ? '' : `<button class="btn small ghost" data-action="set-default" data-id="${esc(p.id)}">设为默认</button>`}
            </div>
          </div>`).join('')}
      </div>
    </div>`;

  onActions(root, {
    new: () => editorModal(null, root),
    edit: (t) => editorModal(personas.find((p) => p.id === t.dataset.id), root),
    'set-default': async (t, e) => {
      e.stopPropagation();
      try {
        await api.setDefaultPersona(t.dataset.id);
        render(root);
      } catch (err) { toast(err.message, 'error'); }
    },
  });
}

function editorModal(persona, root) {
  let avatarPath = persona ? persona.avatar_path : '';
  const m = modal(`
    <h2>${persona ? '编辑档案' : '新建档案'}</h2>
    <div class="avatar-editor">
      <div class="avatar-preview" id="p-avatar">${avatarHTML(avatarPath, persona ? persona.name : '?', 'xl')}</div>
      <div class="avatar-actions">
        <label class="btn small">📁 上传图片<input type="file" accept="image/*" hidden id="p-file" /></label>
        <button type="button" class="btn small" data-action="gen">🪄 AI 生成</button>
      </div>
    </div>
    <label class="field"><span>名称 *</span><input id="p-name" required maxlength="40" value="${esc(persona ? persona.name : '')}" placeholder="你的名字，例如：林远" /></label>
    <label class="field"><span>描述（性格 / 身份 / 特征）</span>
      <textarea id="p-desc" rows="4" placeholder="写手会把这个档案当作「玩家角色」来配合，不会替你发言">${esc(persona ? persona.description : '')}</textarea></label>
    <label class="switch-row"><span>设为默认档案</span><input type="checkbox" id="p-default" ${persona && persona.is_default ? 'checked' : ''} /><i class="switch"></i></label>
    <div class="modal-actions">
      ${persona ? `<button class="btn danger-ghost" data-action="del">删除</button>` : ''}
      <span style="flex:1"></span>
      <button class="btn" data-close>取消</button>
      <button class="btn primary" data-action="save">保存</button>
    </div>`);

  m.root.addEventListener('click', async (e) => {
    if (e.target.matches('[data-close]')) return m.close();

    if (e.target.closest('[data-action="gen"]')) {
      const name = m.root.querySelector('#p-name').value.trim();
      if (!name) return toast('先填一下名称', 'error');
      try {
        const r = await api.generateAvatar(name, m.root.querySelector('#p-desc').value);
        avatarPath = r.url;
        m.root.querySelector('#p-avatar').innerHTML = avatarHTML(avatarPath, name, 'xl');
      } catch (err) { toast(err.message, 'error'); }
    }

    if (e.target.closest('[data-action="save"]')) {
      const body = {
        name: m.root.querySelector('#p-name').value,
        description: m.root.querySelector('#p-desc').value,
        avatar_path: avatarPath,
        is_default: m.root.querySelector('#p-default').checked,
      };
      try {
        if (persona) await api.updatePersona(persona.id, body);
        else await api.createPersona(body);
        m.close();
        render(root);
      } catch (err) { toast(err.message, 'error'); }
    }

    if (e.target.closest('[data-action="del"]')) {
      if (!confirm(`删除档案「${persona.name}」？`)) return;
      try {
        await api.deletePersona(persona.id);
        m.close();
        render(root);
      } catch (err) { toast(err.message, 'error'); }
    }
  });

  m.root.querySelector('#p-file').addEventListener('change', async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    try {
      const dataURL = await fileToDataURL(file);
      const r = await api.uploadImage(dataURL);
      avatarPath = r.url;
      m.root.querySelector('#p-avatar').innerHTML = avatarHTML(avatarPath, '?', 'xl');
    } catch (err) { toast(err.message, 'error'); }
    e.target.value = '';
  });
}
