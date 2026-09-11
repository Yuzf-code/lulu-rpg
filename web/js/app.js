// 应用入口：加载全局配置 + hash 路由 + 底部导航。
import { api } from './api.js';
import { toast } from './util.js';
import * as lobby from './views/lobby.js';
import * as characters from './views/characters.js';
import * as personas from './views/personas.js';
import * as chat from './views/chat.js';
import * as distill from './views/distill.js';
import * as settings from './views/settings.js';

const app = document.getElementById('app');
const nav = document.getElementById('nav');

export const state = {
  config: { llm: { provider: 'mock' }, image: { enabled: false } },
};

const routes = [
  { re: /^#\/distill$/, nav: 'characters', view: () => distill.render(app) },
  { re: /^#\/characters\/new$/, nav: 'characters', view: () => characters.renderEditor(app, null) },
  { re: /^#\/characters\/([\w-]+)$/, nav: 'characters', view: (m) => characters.renderEditor(app, m[1]) },
  { re: /^#\/characters$/, nav: 'characters', view: () => characters.renderList(app) },
  { re: /^#\/personas$/, nav: 'personas', view: () => personas.render(app) },
  { re: /^#\/settings$/, nav: 'settings', view: () => settings.render(app) },
  { re: /^#\/chat\/([\w-]+)$/, nav: null, view: (m) => chat.render(app, m[1]) },
  { re: /^#?\/?$/, nav: 'sessions', view: () => lobby.render(app) },
];

let cleanup = null;

async function route() {
  if (typeof cleanup === 'function') {
    try { cleanup(); } catch { /* ignore */ }
    cleanup = null;
  }
  const hash = location.hash || '#/';
  for (const r of routes) {
    const m = hash.match(r.re);
    if (!m) continue;
    nav.classList.toggle('hidden', r.nav === null);
    document.querySelectorAll('#nav a').forEach((a) => {
      a.classList.toggle('active', a.dataset.nav === r.nav);
    });
    try {
      cleanup = (await r.view(m)) || null;
    } catch (e) {
      console.error(e);
      app.innerHTML = `<div class="page-error"><p>😭 ${e.message || '页面加载失败'}</p><a class="btn" href="#/">返回会话列表</a></div>`;
    }
    return;
  }
  location.hash = '#/';
}

window.addEventListener('hashchange', route);

(async function boot() {
  try {
    state.config = await api.config();
  } catch (e) {
    toast('无法连接服务器：' + e.message, 'error');
  }
  await route();
})();
