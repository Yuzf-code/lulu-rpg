// 通用工具：转义、时间格式化、toast、图片压缩、DOM 小助手。

export function esc(s) {
  return String(s ?? '')
    .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

export function fmtTime(ts) {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  const diff = (Date.now() - d.getTime()) / 1000;
  if (diff < 60) return '刚刚';
  if (diff < 3600) return Math.floor(diff / 60) + ' 分钟前';
  if (diff < 86400) return Math.floor(diff / 3600) + ' 小时前';
  if (diff < 86400 * 7) return Math.floor(diff / 86400) + ' 天前';
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// 轻量 toast。
export function toast(msg, type = 'info') {
  const root = document.getElementById('toast-root');
  const el = document.createElement('div');
  el.className = `toast toast-${type}`;
  el.textContent = msg;
  root.appendChild(el);
  requestAnimationFrame(() => el.classList.add('show'));
  setTimeout(() => {
    el.classList.remove('show');
    setTimeout(() => el.remove(), 300);
  }, 3200);
}

// 首字母头像（NPC 或未设头像时用）。
export function initialAvatar(name, cls = '') {
  const ch = [...String(name || '?')][0] || '?';
  const hues = [262, 199, 152, 24, 336, 47, 0, 280];
  let h = 0;
  for (const c of String(name || '')) h = (h * 31 + c.codePointAt(0)) >>> 0;
  const hue = hues[h % hues.length];
  return `<span class="initial-avatar ${cls}" style="--h:${hue}">${esc(ch)}</span>`;
}

// 头像 HTML：有图用图，无图用首字母。
export function avatarHTML(path, name, cls = '') {
  if (path) return `<img class="avatar ${cls}" src="${esc(path)}" alt="${esc(name)}" loading="lazy" />`;
  return initialAvatar(name, cls);
}

// 全屏图片查看。
export function lightbox(src) {
  let lb = document.getElementById('lightbox');
  if (!lb) {
    lb = document.createElement('div');
    lb.id = 'lightbox';
    lb.className = 'lightbox';
    lb.addEventListener('click', () => lb.classList.remove('open'));
    document.body.appendChild(lb);
  }
  lb.innerHTML = '';
  const img = document.createElement('img');
  img.src = src;
  lb.appendChild(img);
  lb.classList.add('open');
}

// 用户选择的本地图片 → 压缩为 data URL（上传前减小体积）。
export function fileToDataURL(file, maxSize = 768, quality = 0.88) {
  return new Promise((resolve, reject) => {
    if (!file || !file.type.startsWith('image/')) return reject(new Error('请选择图片文件'));
    if (file.size > 8 * 1024 * 1024) return reject(new Error('图片不能超过 8MB'));
    const reader = new FileReader();
    reader.onerror = () => reject(new Error('读取图片失败'));
    reader.onload = () => {
      const raw = String(reader.result);
      // 小图直接原样上传，避免二次压缩损失。
      if (file.size < 180 * 1024 && (file.type === 'image/png' || file.type === 'image/jpeg' || file.type === 'image/webp')) {
        return resolve(raw);
      }
      const img = new Image();
      img.onerror = () => reject(new Error('图片解析失败'));
      img.onload = () => {
        const scale = Math.min(1, maxSize / Math.max(img.width, img.height));
        const w = Math.max(1, Math.round(img.width * scale));
        const h = Math.max(1, Math.round(img.height * scale));
        const canvas = document.createElement('canvas');
        canvas.width = w; canvas.height = h;
        canvas.getContext('2d').drawImage(img, 0, 0, w, h);
        resolve(canvas.toDataURL('image/jpeg', quality));
      };
      img.src = raw;
    };
    reader.readAsDataURL(file);
  });
}

// 事件委托绑定：container 上监听，按 data-action 分发。
export function onActions(container, handlers) {
  container.addEventListener('click', (e) => {
    const t = e.target.closest('[data-action]');
    if (!t || !container.contains(t)) return;
    const fn = handlers[t.dataset.action];
    if (fn) fn(t, e);
  });
}

// 简易模态框。返回 {root, wrap, close}。
// 内置两类关闭行为：点击遮罩、点击任意 [data-close] 元素（用 closest
// 匹配，保证点中按钮内部任何子元素都生效）。
export function modal(html, cls = '') {
  const wrap = document.createElement('div');
  wrap.className = `modal-wrap ${cls}`;
  wrap.innerHTML = `<div class="modal">${html}</div>`;
  document.body.appendChild(wrap);
  requestAnimationFrame(() => wrap.classList.add('open'));
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    wrap.classList.remove('open');
    setTimeout(() => wrap.remove(), 220);
  };
  wrap.addEventListener('click', (e) => {
    if (e.target === wrap) return close();
    if (e.target.closest('[data-close]')) close();
  });
  return { root: wrap.querySelector('.modal'), wrap, close };
}

// 底部抽屉（移动端更友好）。
export function sheet(html) {
  const m = modal(html, 'sheet-wrap');
  m.root.parentElement.classList.add('as-sheet');
  return m;
}
