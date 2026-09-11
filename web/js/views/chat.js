// 聊天 / 游戏主界面：左侧最新配图 + 右侧消息流与输入框（移动端上下堆叠）。
// 支持 SSE 流式渲染分段（旁白 / 台词 / 动作 / 内心）、台词与导演两种输入模式、
// 派生私聊、会话设置与自动配图开关。
import { api, stream } from '../api.js';
import { state } from '../app.js';
import { esc, toast, avatarHTML, lightbox, sheet } from '../util.js';

let activeAbort = null;

function hueOf(str) {
  let h = 0;
  for (const c of String(str || '')) h = (h * 31 + c.codePointAt(0)) >>> 0;
  return h % 360;
}

export async function render(root, sessionId) {
  const [detail, payload] = await Promise.all([
    api.getSession(sessionId),
    api.listMessages(sessionId),
  ]);
  const session = detail.session;
  const privateChats = detail.private_chats || [];
  const messages = payload.messages || [];

  const charById = new Map((session.characters || []).map((c) => [c.id, c]));
  const persona = session.persona;
  const personaName = persona ? persona.name : '旅行者';

  root.innerHTML = `
    <div class="chat-page">
      <header class="chat-head">
        <a class="icon-btn" href="#/">←</a>
        <div class="chat-head-main" id="head-main" role="button" tabindex="0">
          <div class="head-avatars">${(session.characters || []).slice(0, 4).map((c) => avatarHTML(c.avatar_path, c.name, 'sm')).join('')}</div>
          <div class="head-text">
            <div class="head-title"><span id="head-title">${esc(session.title)}</span>
              ${session.parent_id ? '<span class="badge">私聊</span>' : ''}</div>
            <div class="head-sub">${esc((session.characters || []).map((c) => c.name).join('、') || '无角色')}${privateChats.length ? ` · 🔗 ${privateChats.length} 段私聊记忆` : ''}</div>
          </div>
        </div>
        <button class="icon-btn" id="btn-derive" title="发起私聊">💬</button>
        <button class="icon-btn" id="btn-settings" title="会话设置">⚙️</button>
      </header>
      <div class="chat-body">
        <aside class="art-panel" id="art-panel">
          <div class="art-main" id="art-main"></div>
          <div class="art-thumbs" id="art-thumbs"></div>
        </aside>
        <div class="chat-main">
          <div class="messages" id="messages"></div>
          <div class="composer">
            <div id="insp-panel" class="insp-panel hidden"></div>
            ${state.config.llm.provider === 'mock' ? '<div class="mock-tip">⚗️ 演示模式：剧情由内置 Mock 写手生成</div>' : ''}
            <div class="composer-mode">
              <button class="mode-btn on" data-mode="say">🗣 台词</button>
              <button class="mode-btn" data-mode="direct">🎬 导演</button>
              <span class="mode-hint" id="mode-hint">以你的角色身份说话</span>
              <button class="mode-btn idea-btn" id="btn-inspire" title="根据当前剧情生成行动灵感">✨ 灵感</button>
            </div>
            <div class="composer-row">
              <textarea id="input" rows="1" placeholder="说点什么，推进剧情…"></textarea>
              <button id="btn-send" class="send-btn" title="发送">➤</button>
              <button id="btn-stop" class="send-btn stop hidden" title="停止生成">■</button>
            </div>
          </div>
        </div>
      </div>
    </div>`;

  const $ = (sel) => root.querySelector(sel);
  const flow = $('#messages');
  const input = $('#input');
  let streaming = false;
  let mode = 'say';
  const segments = new Map(); // segment_idx → {node, body, raw}
  let openErrNode = null;

  // ---- 渲染工具 ----

  function speakerInfo(m) {
    if (m.speaker_type === 'character') {
      const c = charById.get(m.character_id);
      return { name: (c && c.name) || m.speaker_name, avatar: avatarHTML(c ? c.avatar_path : '', (c && c.name) || m.speaker_name, 'xs'), hue: hueOf(m.character_id) };
    }
    return { name: m.speaker_name || '???', avatar: avatarHTML('', m.speaker_name || '???', 'xs'), hue: hueOf(m.speaker_name || 'npc') };
  }

  function fmtBody(text) {
    return esc(text).replaceAll('\n', '<br>');
  }

  function msgNode(m) {
    const wrap = document.createElement('div');
    if (m.kind === 'user') {
      if (m.style === 'direct') {
        wrap.className = 'msg user-direct';
        wrap.innerHTML = `<div class="direct-box"><span class="dir-tag">🎬 导演指令</span><p>${fmtBody(m.content)}</p></div>`;
      } else {
        wrap.className = 'msg user-say';
        wrap.innerHTML = `
          <div class="who right">${esc(personaName)} ${avatarHTML(persona ? persona.avatar_path : '', personaName, 'xs')}</div>
          <div class="bubble user">${fmtBody(m.content)}</div>`;
      }
    } else if (m.kind === 'system') {
      wrap.className = 'msg sys';
      wrap.innerHTML = `<p>${fmtBody(m.content)}</p>`;
    } else if (m.speaker_type === 'narrator') {
      wrap.className = 'msg narration';
      wrap.innerHTML = `<p>${fmtBody(m.content)}</p>`;
    } else {
      const sp = speakerInfo(m);
      const who = `<span class="who" style="--h:${sp.hue}">${sp.avatar}<b>${esc(sp.name)}</b></span>`;
      if (m.style === 'dialogue') {
        wrap.className = 'msg dialogue';
        wrap.innerHTML = `${who}<div class="bubble">${fmtBody(m.content)}</div>`;
      } else if (m.style === 'thought') {
        wrap.className = 'msg side-block thought';
        wrap.innerHTML = `${who}<p>（${fmtBody(m.content.replace(/^[（(]|[）)]$/g, ''))}）</p>`;
      } else {
        wrap.className = 'msg side-block action';
        wrap.innerHTML = `${who}<p>${fmtBody(m.content)}</p>`;
      }
    }
    if (m.image) {
      wrap.appendChild(imgRowNode(m.image));
    }
    return wrap;
  }

  function imgRowNode(image) {
    const row = document.createElement('div');
    row.className = 'msg img-row';
    row.innerHTML = `<img src="${esc(image.path)}" alt="回合配图" loading="lazy" />`;
    row.querySelector('img').addEventListener('click', () => lightbox(image.path));
    return row;
  }

  function appendErrorChip(text) {
    if (openErrNode) openErrNode.remove();
    openErrNode = document.createElement('div');
    openErrNode.className = 'msg err-chip';
    openErrNode.textContent = '⚠ ' + text;
    flow.appendChild(openErrNode);
    scrollBottom(true);
  }

  // ---- 滚动 ----
  let pinned = true;
  flow.addEventListener('scroll', () => {
    pinned = flow.scrollHeight - flow.scrollTop - flow.clientHeight < 140;
  });
  function scrollBottom(force) {
    if (force || pinned) {
      requestAnimationFrame(() => { flow.scrollTop = flow.scrollHeight; });
    }
  }

  // ---- 历史渲染 ----
  for (const m of messages) {
    flow.appendChild(msgNode(m));
  }
  scrollBottom(true);

  // ---- 画板（左侧最新图） ----
  const artMain = $('#art-main');
  const artThumbs = $('#art-thumbs');
  const allImages = messages.map((m) => m.image).filter(Boolean);

  function setMainImage(img, animate) {
    artMain.innerHTML = `<img src="${esc(img.path)}" alt="最新配图" ${animate ? 'class="fade-in"' : ''} />`;
    artMain.querySelector('img').addEventListener('click', () => lightbox(img.path));
  }
  function addThumb(img) {
    const t = document.createElement('img');
    t.src = img.path;
    t.loading = 'lazy';
    t.title = '回合 ' + img.turn;
    t.addEventListener('click', () => { lightbox(img.path); });
    artThumbs.appendChild(t);
    artThumbs.scrollLeft = artThumbs.scrollWidth;
  }
  if (allImages.length > 0) {
    setMainImage(allImages[allImages.length - 1], false);
    allImages.forEach(addThumb);
  } else {
    artMain.innerHTML = `<div class="art-empty">${state.config.image.enabled ? '🎨<br/>故事开始后<br/>这里会浮现画面' : '🎨<br/>未启用自动配图<br/>可在会话设置或服务端开启'}</div>`;
  }

  // ---- 流式处理 ----
  function startSegment(ev) {
    const tmp = {
      id: '', session_id: sessionId, turn: currentTurn, kind: 'story',
      style: ev.kind, speaker_type: ev.speaker_type, character_id: ev.character_id,
      speaker_name: ev.speaker_name, content: '',
    };
    const node = msgNode(tmp);
    node.classList.add('streaming');
    const body = node.querySelector('p, .bubble');
    flow.appendChild(node);
    segments.set(ev.segment_idx, { node, body, raw: '' });
    if (openErrNode) { openErrNode = null; }
    scrollBottom();
  }
  function appendDelta(ev) {
    const seg = segments.get(ev.segment_idx);
    if (!seg) return;
    seg.raw += ev.text;
    seg.body.innerHTML = fmtBody(seg.raw.replace(/\n+$/, ''));
    scrollBottom();
  }
  function finishSegment(ev) {
    const seg = segments.get(ev.segment_idx);
    if (seg) {
      seg.node.classList.remove('streaming');
      seg.body.innerHTML = fmtBody(seg.raw.trim());
    }
  }

  let currentTurn = 0;
  function handleEvent(ev) {
    switch (ev.type) {
      case 'user_saved':
        currentTurn = ev.turn;
        flow.appendChild(msgNode(ev.message));
        scrollBottom(true);
        break;
      case 'segment_start': startSegment(ev); break;
      case 'delta': appendDelta(ev); break;
      case 'segment_end': finishSegment(ev); break;
      case 'image': {
        allImages.push(ev.image);
        setMainImage(ev.image, true);
        addThumb(ev.image);
        // 手机上侧栏隐藏：把配图同时插入消息流末尾。
        flow.appendChild(imgRowNode(ev.image));
        scrollBottom();
        break;
      }
      case 'image_failed':
        toast('配图失败：' + (ev.error || '未知原因'), 'error');
        break;
      case 'error':
        appendErrorChip(ev.error);
        break;
      case 'done':
        currentTurn = ev.turn || currentTurn;
        break;
    }
  }

  // ---- 流程控制 ----
  function setStreaming(on) {
    streaming = on;
    $('#btn-send').classList.toggle('hidden', on);
    $('#btn-stop').classList.toggle('hidden', !on);
    input.disabled = on;
    flow.classList.toggle('generating', on);
  }

  async function runStream(path, body) {
    setStreaming(true);
    activeAbort = new AbortController();
    try {
      await stream(path, body, handleEvent, activeAbort.signal);
    } catch (e) {
      if (e.name !== 'AbortError') {
        appendErrorChip(e.message);
        toast(e.message, 'error');
      }
    } finally {
      activeAbort = null;
      setStreaming(false);
      scrollBottom();
    }
  }

  async function send() {
    const content = input.value.trim();
    if (!content || streaming) return;
    input.value = '';
    autosize();
    openErrNode = null;
    await runStream(`/api/sessions/${sessionId}/turn`, { content, mode });
  }

  // ---- 输入区 ----
  function autosize() {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 132) + 'px';
  }
  input.addEventListener('input', autosize);
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    }
  });
  $('#btn-send').addEventListener('click', send);
  $('#btn-stop').addEventListener('click', () => { if (activeAbort) activeAbort.abort(); });

  root.querySelectorAll('.mode-btn[data-mode]').forEach((b) => {
    b.addEventListener('click', () => {
      mode = b.dataset.mode;
      root.querySelectorAll('.mode-btn[data-mode]').forEach((x) => x.classList.toggle('on', x === b));
      $('#mode-hint').textContent = mode === 'say'
        ? '以你的角色身份说话'
        : '向写手描述剧情走向（不会作为台词出现）';
      input.placeholder = mode === 'say' ? '说点什么，推进剧情…' : '例如：突然下起大雨，让队伍躲进山洞…';
    });
  });

  // ---- 灵感 ----
  const inspPanel = $('#insp-panel');
  const inspireBtn = $('#btn-inspire');

  function setMode(next) {
    const btn = root.querySelector(`.mode-btn[data-mode="${next}"]`);
    if (btn) btn.click();
  }

  function hideInsp() { inspPanel.classList.add('hidden'); }

  async function loadInspiration() {
    if (streaming) return toast('等这一轮结束后再试', 'error');
    inspPanel.classList.remove('hidden');
    inspPanel.innerHTML = '<div class="insp-loading">✨ 正在结合当前剧情寻找灵感…</div>';
    try {
      const r = await api.inspiration(sessionId);
      inspPanel.innerHTML = `
        <div class="insp-list">
          ${r.options.map((o) => `
            <button class="insp-opt" data-kind="${o.kind === 'say' ? 'say' : 'direct'}" data-text="${esc(o.text)}">
              <span class="insp-ico">${o.kind === 'say' ? '🗣' : '🎬'}</span><span>${esc(o.text)}</span>
            </button>`).join('')}
        </div>
        <div class="insp-foot">
          <button class="btn small ghost" data-insp="regen">🔄 换一批</button>
          <button class="btn small ghost" data-insp="close">收起</button>
        </div>`;
      inspPanel.querySelectorAll('.insp-opt').forEach((b) => {
        b.addEventListener('click', () => {
          setMode(b.dataset.kind);
          input.value = b.dataset.text;
          autosize();
          hideInsp();
          input.focus();
        });
      });
    } catch (e) {
      inspPanel.classList.add('hidden');
      toast(e.message, 'error');
    }
  }

  inspireBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    inspPanel.classList.contains('hidden') ? loadInspiration() : hideInsp();
  });
  inspPanel.addEventListener('click', (e) => {
    e.stopPropagation();
    if (e.target.closest('[data-insp="regen"]')) loadInspiration();
    if (e.target.closest('[data-insp="close"]')) hideInsp();
  });
  root.addEventListener('click', (e) => {
    if (!inspPanel.classList.contains('hidden') && !e.target.closest('#insp-panel') && !e.target.closest('#btn-inspire')) {
      hideInsp();
    }
  });

  // ---- 开场 ----
  if (messages.length === 0 && session.turn_seq === 0) {
    if ((session.characters || []).length === 0) {
      appendErrorChip('这个会话没有出场角色，无法开始。请删除后重新创建。');
    } else {
      await runStream(`/api/sessions/${sessionId}/opening`, {});
    }
  }

  // ---- 头部操作 ----
  $('#btn-derive').addEventListener('click', () => deriveSheet(session));
  $('#head-main').addEventListener('click', () => settingsSheet(session));
  $('#btn-settings').addEventListener('click', () => settingsSheet(session));

  function settingsSheet(sess) {
    const s = sheet(`
      <h2>⚙️ 会话设置</h2>
      <label class="field"><span>标题</span><input id="st-title" value="${esc(sess.title)}" /></label>
      <label class="field"><span>世界 / 场景设定</span><textarea id="st-scenario" rows="4">${esc(sess.scenario)}</textarea></label>
      ${state.config.image.enabled ? `
      <label class="switch-row"><span>🎨 每轮自动生成配图</span>
        <input type="checkbox" id="st-autoimg" ${sess.auto_image ? 'checked' : ''} /><i class="switch"></i></label>` : ''}
      <div class="modal-actions">
        <button class="btn danger-ghost" data-action="del">删除会话</button>
        <span style="flex:1"></span>
        <button class="btn" data-action="close">取消</button>
        <button class="btn primary" data-action="save">保存</button>
      </div>`);
    s.root.addEventListener('click', async (e) => {
      if (e.target.closest('[data-action="close"]')) return s.close();
      if (e.target.closest('[data-action="save"]')) {
        try {
          await api.updateSession(sess.id, {
            title: s.root.querySelector('#st-title').value.trim(),
            scenario: s.root.querySelector('#st-scenario').value,
            auto_image: !!s.root.querySelector('#st-autoimg')?.checked,
          });
          toast('已保存');
          s.close();
          render(root, sessionId); // 重载以刷新标题与开关
        } catch (err) { toast(err.message, 'error'); }
      }
      if (e.target.closest('[data-action="del"]')) {
        if (!confirm('删除这个会话？记录与配图都会一并删除，不可恢复。')) return;
        try {
          await api.deleteSession(sess.id);
          location.hash = '#/';
        } catch (err) { toast(err.message, 'error'); }
      }
    });
  }

  function deriveSheet(sess) {
    const chars = sess.characters || [];
    const picked = new Set();
    const s = sheet(`
      <h2>💬 发起私聊</h2>
      <p class="hint">从本局选择一位或几位角色单独开一个新的对话。私聊会<b>继承主线剧情记忆</b>；而你在私聊中的相处，也会成为对应角色带回主线的<b>私下记忆</b>——只有参与私聊的角色知道，其他角色不知情。</p>
      <div class="char-pick">
        ${chars.map((c) => `
          <button type="button" class="pick-chip" data-id="${esc(c.id)}">
            ${avatarHTML(c.avatar_path, c.name, 'sm')}<span>${esc(c.name)}</span>
          </button>`).join('')}
      </div>
      <div class="modal-actions">
        <button class="btn" data-action="close">取消</button>
        <button class="btn primary" data-action="go">开始私聊</button>
      </div>`);
    s.root.addEventListener('click', async (e) => {
      const chip = e.target.closest('.pick-chip');
      if (chip) {
        const id = chip.dataset.id;
        picked.has(id) ? picked.delete(id) : picked.add(id);
        chip.classList.toggle('on', picked.has(id));
      }
      if (e.target.closest('[data-action="close"]')) return s.close();
      if (e.target.closest('[data-action="go"]')) {
        if (picked.size === 0) return toast('请选择至少一位角色', 'error');
        if (picked.size === chars.length) {
          const ok = confirm('你选择了全部角色——这将是一个与主线并行的新分支，而不是私聊。确定继续？');
          if (!ok) return;
        }
        try {
          const ns = await api.createSession({
            parent_id: sess.id,
            character_ids: [...picked],
            auto_image: sess.auto_image,
          });
          s.close();
          location.hash = '#/chat/' + ns.id;
        } catch (err) { toast(err.message, 'error'); }
      }
    });
  }

  // 返回清理函数：离开页面时中断进行中的流。
  return () => { if (activeAbort) activeAbort.abort(); };
}
