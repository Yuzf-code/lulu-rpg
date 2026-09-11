// 文本蒸馏（独立入口）：粘贴原文 → 前端逐个请求蒸馏（一个对象一次请求）
// → 每成功一个立即渲染并可保存，顺序与重试由前端控制。
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
      <div id="ds-progress" class="sub"></div>
      <div id="ds-results"></div>
    </div>`;

  const $ = (sel) => root.querySelector(sel);
  const resultsBox = $('#ds-results');
  const progress = $('#ds-progress');
  const setProgress = (s) => { progress.textContent = s; };

  $('#ds-run').addEventListener('click', async () => {
    const text = $('#ds-text').value.trim();
    if (!text) return toast('请粘贴原文', 'error');
    if (text.length > 16000) return toast('原文过长（上限 16000 字），请分段蒸馏', 'error');
    let targets = $('#ds-targets').value.split(/[，,]/).map((s) => s.trim()).filter(Boolean).slice(0, 5);
    const subject = $('#ds-subject').value.trim();
    const wantStyle = $('#ds-style').checked;

    const btn = $('#ds-run');
    btn.disabled = true;
    resultsBox.innerHTML = '';

    try {
      // 自动识别人物（一次轻量请求）。
      if (targets.length === 0) {
        setProgress('🔍 正在从原文中识别人物…');
        targets = (await api.detectTargets(text)).targets || [];
        if (targets.length === 0) {
          toast('未能从原文中识别出人物，请手动指定对象', 'error');
          btn.disabled = false; setProgress('');
          return;
        }
        setProgress(`识别到 ${targets.length} 个人物：${targets.join('、')}`);
      }

      // 逐个蒸馏：一次请求一个对象，成功即渲染。
      for (let i = 0; i < targets.length; i++) {
        const name = targets[i];
        setProgress(`🧪 正在蒸馏 ${i + 1}/${targets.length}：${name} …`);
        try {
          const r = await api.distillCharacter(text, subject, name);
          resultsBox.insertAdjacentHTML('beforeend', draftCard(r.character));
          wireDraftCard(resultsBox.lastElementChild, r.character);
        } catch (err) {
          resultsBox.insertAdjacentHTML('beforeend',
            `<div class="draft-card"><div class="draft-head">⚠ ${esc(name)}：${esc(err.message)}</div></div>`);
        }
      }

      // 写作风格。
      if (wantStyle) {
        setProgress('🖋 正在提炼写作风格…');
        try {
          const r = await api.distillStyle(text);
          resultsBox.insertAdjacentHTML('afterbegin', styleCard(r.style));
          wireStyleCard(resultsBox.querySelector('[data-kind="style"]'), r.style);
        } catch (err) {
          resultsBox.insertAdjacentHTML('afterbegin',
            `<div class="banner warn">风格提炼失败：${esc(err.message)}</div>`);
        }
      }
      setProgress('✅ 蒸馏完成，结果可逐项保存');
    } catch (err) {
      toast(err.message, 'error');
      setProgress('');
    }
    btn.disabled = false;
  });

  function styleCard(st) {
    return `
      <div class="draft-card" data-kind="style">
        <div class="draft-head"><b>🖋 写作风格：${esc(st.name)}</b></div>
        <p>${esc(st.description)}</p>
        <div class="modal-actions"><button type="button" class="btn small primary" data-save-style>📥 保存为风格</button></div>
      </div>`;
  }

  function draftCard(c) {
    return `
      <div class="draft-card" data-kind="char">
        <div class="draft-head"><b>🎭 ${esc(c.name || '未命名')}</b><span class="tag">${esc(c.title || '无头衔')}</span></div>
        ${c.appearance ? `<p><i>外貌</i>${esc(c.appearance)}</p>` : ''}
        ${c.personality ? `<p><i>性格</i>${esc(c.personality)}</p>` : ''}
        ${c.background ? `<p><i>背景</i>${esc(c.background)}</p>` : ''}
        ${c.greeting ? `<p><i>开场白</i>${esc(c.greeting)}</p>` : ''}
        ${(c.relationships || []).map((r) => `<p><i>对${esc(r.subject)}</i>${esc(r.text)}</p>`).join('')}
        <div class="modal-actions"><button type="button" class="btn small primary" data-save-char>📥 保存为角色卡</button></div>
      </div>`;
  }

  function wireStyleCard(cardEl, st) {
    const btn = cardEl.querySelector('[data-save-style]');
    btn.addEventListener('click', async () => {
      btn.disabled = true;
      try {
        await api.createStyle({ name: st.name, description: st.description });
        btn.textContent = '✓ 已保存，开局时可选';
      } catch (err) {
        toast(err.message, 'error');
        btn.disabled = false;
      }
    });
  }

  function wireDraftCard(cardEl, c) {
    const btn = cardEl.querySelector('[data-save-char]');
    btn.addEventListener('click', async () => {
      btn.disabled = true;
      try {
        await api.createCharacter({
          name: c.name,
          title: c.title,
          appearance: c.appearance,
          personality: c.personality,
          background: c.background,
          greeting: c.greeting,
          tags: c.tags || [],
          example_dialogues: c.example_dialogues || [],
          relationships: c.relationships || [],
        });
        btn.textContent = '✓ 已保存到角色卡';
      } catch (err) {
        toast(err.message, 'error');
        btn.disabled = false;
      }
    });
  }
}
