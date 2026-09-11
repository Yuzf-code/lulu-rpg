// 后端 API 封装 + SSE 流式读取。

async function req(method, url, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const resp = await fetch(url, opt);
  let data = null;
  try { data = await resp.json(); } catch { /* 空响应体 */ }
  if (!resp.ok) {
    throw new Error((data && data.error) || `请求失败（HTTP ${resp.status}）`);
  }
  return data;
}

// route(method, url) 返回懒执行的请求函数（不会在模块加载时发请求）。
function route(method, url) {
  return (body) => req(method, url, body);
}

export const api = {
  config: route('GET', '/api/config'),

  // 角色卡
  listCharacters: route('GET', '/api/characters'),
  getCharacter: (id) => req('GET', `/api/characters/${id}`),
  createCharacter: (body) => req('POST', '/api/characters', body),
  updateCharacter: (id, body) => req('PUT', `/api/characters/${id}`, body),
  deleteCharacter: (id) => req('DELETE', `/api/characters/${id}`),

  // 角色档案
  listPersonas: route('GET', '/api/personas'),
  createPersona: (body) => req('POST', '/api/personas', body),
  updatePersona: (id, body) => req('PUT', `/api/personas/${id}`, body),
  deletePersona: (id) => req('DELETE', `/api/personas/${id}`),
  setDefaultPersona: (id) => req('POST', `/api/personas/${id}/default`, {}),

  // 会话
  listSessions: route('GET', '/api/sessions'),
  createSession: (body) => req('POST', '/api/sessions', body),
  getSession: (id) => req('GET', `/api/sessions/${id}`),
  updateSession: (id, body) => req('PATCH', `/api/sessions/${id}`, body),
  deleteSession: (id) => req('DELETE', `/api/sessions/${id}`),
  listMessages: (id) => req('GET', `/api/sessions/${id}/messages?limit=500`),

  // 图片
  uploadImage: (data_url) => req('POST', '/api/uploads', { data_url }),
  generateAvatar: (name, appearance) => req('POST', '/api/avatars/generate', { name, appearance }),
  generateImage: (prompt) => req('POST', '/api/images/generate', { prompt }),

  // 蒸馏 / 草稿 / 灵感
  distill: (text, subject, targets) => req('POST', '/api/characters/distill', { text, subject, targets }),
  distillInto: (id, text, subject) => req('POST', `/api/characters/${id}/distill`, { text, subject }),
  generateCardDraft: (kind, seed) => req('POST', '/api/characters/generate-draft', { kind, ...seed }),
  inspiration: (sessionId) => req('POST', `/api/sessions/${sessionId}/inspiration`, {}),

  // 写作风格
  listStyles: () => req('GET', '/api/styles'),
  createStyle: (body) => req('POST', '/api/styles', body),
  deleteStyle: (id) => req('DELETE', `/api/styles/${id}`),

  // SSE 流（opening / turn）
  streamSession: stream,
};

/**
 * 读取会话 SSE 流。path 为 /api/sessions/{id}/turn|opening。
 * onEvent 收到已解析的事件对象；signal 用于“停止生成”。
 */
export async function stream(path, body, onEvent, signal) {
  const resp = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
    signal,
  });
  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`;
    try { const j = await resp.json(); msg = j.error || msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buf.indexOf('\n\n')) >= 0) {
      const frame = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      for (const line of frame.split('\n')) {
        if (!line.startsWith('data:')) continue;
        const payload = line.slice(5).trim();
        if (!payload) continue;
        try {
          onEvent(JSON.parse(payload));
        } catch (e) {
          console.warn('事件解析失败', payload, e);
        }
      }
    }
  }
}
