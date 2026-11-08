// Utilidades e componentes de interface compartilhados (sem frameworks).

export const $ = (s, el = document) => el.querySelector(s);
export const $$ = (s, el = document) => [...el.querySelectorAll(s)];
export const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
export const html = (str) => { const t = document.createElement('template'); t.innerHTML = str.trim(); return t.content.firstElementChild; };
export const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
export const fmt = (n, d = 1) => Number(n || 0).toLocaleString('pt-BR', { minimumFractionDigits: d, maximumFractionDigits: d });
export const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };

const bus = new EventTarget();
export const on = (ev, fn) => { bus.addEventListener(ev, fn); return () => bus.removeEventListener(ev, fn); };
export const emit = (ev, detail) => bus.dispatchEvent(new CustomEvent(ev, { detail }));

export function fmtBytes(b, d = 1) {
  if (b == null || isNaN(b)) return '—';
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0, v = Number(b);
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${fmt(v, i === 0 ? 0 : v >= 100 ? 0 : d)} ${u[i]}`;
}
export function fmtRate(b) { return `${fmtBytes(b)}/s`; }
export function fmtDuration(sec) {
  sec = Math.floor(sec || 0);
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  if (d) return `${d} ${d === 1 ? 'dia' : 'dias'}, ${h} h`;
  if (h) return `${h} h ${m} min`;
  return `${m} min`;
}
const rtf = new Intl.RelativeTimeFormat('pt-BR', { numeric: 'auto' });
export function timeAgo(unix) {
  if (!unix) return '—';
  const diff = unix - Date.now() / 1000;
  const abs = Math.abs(diff);
  if (abs < 60) return 'agora';
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour');
  if (abs < 86400 * 7) return rtf.format(Math.round(diff / 86400), 'day');
  return new Date(unix * 1000).toLocaleDateString('pt-BR', { day: '2-digit', month: 'short', year: 'numeric' });
}

/* ── Ícones de traço (desenho próprio) ─────────────────────────── */
const P = {
  x: '<path d="M6 6l12 12M18 6L6 18"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/>',
  chevR: '<path d="M9 6l6 6-6 6"/>', chevL: '<path d="M15 6l-6 6 6 6"/>', chevU: '<path d="M6 15l6-6 6 6"/>', chevD: '<path d="M6 9l6 6 6-6"/>',
  arrowUp: '<path d="M12 19V5M6 11l6-6 6 6"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  sliders: '<path d="M4 6h9M19 6h1M4 12h3M13 12h7M4 18h11M21 18h-1"/><circle cx="16" cy="6" r="2.2"/><circle cx="10" cy="12" r="2.2"/><circle cx="18" cy="18" r="2.2"/>',
  activity: '<path d="M3 12h4l3-8 4 16 3-8h4"/>',
  cpu: '<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/>',
  memory: '<rect x="3" y="6" width="18" height="11" rx="2"/><path d="M7 10v3M11 10v3M15 10v3M7 17v3M12 17v3M17 17v3"/>',
  disk: '<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M3 14h18M16.5 17h.01"/>',
  disks: '<rect x="3" y="3" width="18" height="8" rx="2.5"/><rect x="3" y="13" width="18" height="8" rx="2.5"/><path d="M16.5 7h.01M16.5 17h.01"/>',
  plug: '<path d="M9 3v5M15 3v5M6.5 8h11v3.5a5.5 5.5 0 0 1-11 0z"/><path d="M12 17v4"/>',
  plugOff: '<path d="M9 3v5M15 3v5M6.5 8h11v3.5a5.5 5.5 0 0 1-11 0z"/><path d="M12 17v4M3 3l18 18"/>',
  bag: '<path d="M5.5 8h13l-1 12.5h-11z"/><path d="M9 10.5V7a3 3 0 0 1 6 0v3.5"/>',
  minus: '<path d="M5 12h14"/>',
  alert: '<path d="M12 3.5l9.5 16.5h-19z"/><path d="M12 10v4.5M12 17.5h.01"/>',
  caret: '<path d="M10 8l5 4-5 4z" fill="currentColor" stroke-linejoin="round"/>',
  temp: '<path d="M10 13.5V5a2 2 0 1 1 4 0v8.5a4 4 0 1 1-4 0z"/><path d="M12 9v8"/>',
  fan: '<circle cx="12" cy="12" r="2"/><path d="M12 10c0-4 1-7 4-7s3 4-2 7M14 12c4 0 7 1 7 4s-4 3-7-2M12 14c0 4-1 7-4 7s-3-4 2-7M10 12c-4 0-7-1-7-4s4-3 7 2"/>',
  battery: '<rect x="2" y="7" width="18" height="10" rx="2"/><path d="M22 11v2M6 10v4M10 10v4"/>',
  net: '<path d="M7 17V5M3 9l4-4 4 4M17 7v12M13 15l4 4 4-4"/>',
  home: '<path d="M4 11l8-7 8 7v8a1 1 0 0 1-1 1h-4v-6H9v6H5a1 1 0 0 1-1-1z"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  star: '<path d="M12 3l2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1-4.4-4.3 6.1-.9z"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3"/>',
  grid: '<rect x="4" y="4" width="6.5" height="6.5" rx="1.6"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.6"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.6"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.6"/>',
  list: '<path d="M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01"/>',
  menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
  upload: '<path d="M12 16V4M7 9l5-5 5 5M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/>',
  download: '<path d="M12 4v12M7 11l5 5 5-5M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/>',
  folderPlus: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M12 10v6M9 13h6"/>',
  power: '<path d="M12 3v9M6.3 6.3a8 8 0 1 0 11.4 0"/>',
  refresh: '<path d="M20 11a8 8 0 0 0-14.9-3M4 4v4h4M4 13a8 8 0 0 0 14.9 3M20 20v-4h-4"/>',
  lock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  logout: '<path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3M10 17l-5-5 5-5M5 12h11"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  eyeOff: '<path d="M3 3l18 18M10.6 5.1A10 10 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-3 3.9M6.6 6.6C3.9 8.4 2 12 2 12s3.5 7 10 7a9.6 9.6 0 0 0 5.4-1.6M9.9 9.9a3 3 0 0 0 4.2 4.2"/>',
  image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="M21 16l-5-5-9 9"/>',
  palette: '<path d="M12 3a9 9 0 1 0 0 18c1 0 1.5-.8 1.5-1.6 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.2 0-.9.7-1.5 1.6-1.5H16a5 5 0 0 0 5-5c0-4.1-4-7.5-9-7.5z"/><path d="M7.5 11.5h.01M10 7.5h.01M15 7.5h.01"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
  wifi: '<path d="M2 9a15 15 0 0 1 20 0M5 12.5a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0M12 19.5h.01"/>',
  terminal: '<path d="M5 7l5 5-5 5M12 17h7"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  check: '<path d="M5 12l5 5 9-10"/>',
  external: '<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 20a8 8 0 0 1 16 0"/>',
  key: '<circle cx="8" cy="15" r="4"/><path d="M11 12l9-9M17 6l3 3M14 9l2 2"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16z"/>',
  pin: '<path d="M9 4h6l-1 6 4 4H6l4-4zM12 14v6"/>',
  sparkles: '<path d="M12 3l1.8 4.7 4.7 1.8-4.7 1.8L12 16l-1.8-4.7-4.7-1.8 4.7-1.8z"/><path d="M19 15l.7 1.8 1.8.7-1.8.7L19 20l-.7-1.8-1.8-.7 1.8-.7z"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>',
  cut: '<circle cx="6" cy="18" r="3"/><circle cx="18" cy="18" r="3"/><path d="M8.5 16L18 4M15.5 16L6 4"/>',
  paste: '<rect x="6" y="4" width="12" height="17" rx="2"/><path d="M9 4V3h6v1M9 10h6M9 14h6"/>',
  layers: '<path d="M12 3l9 5-9 5-9-5z"/><path d="M3 13l9 5 9-5"/>',
  apps: '<rect x="4" y="4" width="6.5" height="6.5" rx="1.6"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.6"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.6"/><path d="M16.75 13.5v6.5M13.5 16.75H20"/>',
  network: '<rect x="9" y="3" width="6" height="5" rx="1"/><rect x="3" y="16" width="6" height="5" rx="1"/><rect x="15" y="16" width="6" height="5" rx="1"/><path d="M12 8v4M6 16v-2h12v2"/>',
  link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>',
  restore: '<path d="M4 12a8 8 0 1 0 2.3-5.7M4 4v4h4"/>',
  sort: '<path d="M7 4v16M3 16l4 4 4-4M17 20V4M13 8l4-4 4 4"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
  shield: '<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/>',
  box: '<path d="M21 8l-9-5-9 5 9 5zM3 8v8l9 5 9-5V8M12 13v8"/>',
  bell: '<path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.6 2H4.4z"/><path d="M10 21a2.2 2.2 0 0 0 4 0"/>',
  bolt: '<path d="M13 3L5 13.5h6L10 21l8-10.5h-6z"/>',
  more: '<circle cx="5" cy="12" r="1.4"/><circle cx="12" cy="12" r="1.4"/><circle cx="19" cy="12" r="1.4"/>',
  filePlus: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M12 11v6M9 14h6"/>',
  unzip: '<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M12 3v2M12 7v2M12 11v2"/><rect x="10" y="14" width="4" height="4" rx="1"/>',
  treemap: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M12 3v18M12 12h9M16.5 12v9"/>',
  dupes: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/><path d="M12 14h4"/>',
};
export const svg = (name, cls = '') => `<svg class="svg ${cls}" viewBox="0 0 24 24" aria-hidden="true">${P[name] || ''}</svg>`;

/* Pasta (usa a cor de destaque) e arquivos coloridos por tipo — desenho próprio */
document.body.insertAdjacentHTML('afterbegin', `<svg width="0" height="0" style="position:absolute" aria-hidden="true"><defs>
  <linearGradient id="fBack" x1="0" y1="0" x2="0" y2="1"><stop offset="0" style="stop-color:hsl(var(--bh) 75% 52%)"/><stop offset="1" style="stop-color:hsl(var(--bh) 80% 40%)"/></linearGradient>
  <linearGradient id="fFront" x1="0" y1="0" x2="0" y2="1"><stop offset="0" style="stop-color:hsl(var(--bh) 90% 72%)"/><stop offset="1" style="stop-color:hsl(var(--bh) 80% 56%)"/></linearGradient>
</defs></svg>`);
export const folderSVG = (cls = 'folder-ico') => `<svg class="${cls}" viewBox="0 0 54 46" aria-hidden="true"><path d="M4 7a4 4 0 0 1 4-4h11.5l5 5H46a4 4 0 0 1 4 4v28a4 4 0 0 1-4 4H8a4 4 0 0 1-4-4z" fill="url(#fBack)"/><rect x="2" y="13" width="50" height="31" rx="5" fill="url(#fFront)"/><path d="M7 13.6h40" stroke="rgba(255,255,255,.55)" stroke-width="1.2" stroke-linecap="round"/></svg>`;
const FILE_COLORS = { img: '#34b46a', vid: '#e8505b', doc: '#3d7ff0', pdf: '#e0453a', zip: '#d8962b', aud: '#d24fa3', code: '#7a8396', txt: '#8b93a6', sheet: '#1f9d55', slide: '#e57a2e' };
export function extKind(n) {
  const e = String(n).split('.').pop().toLowerCase();
  if (/^(jpe?g|png|heic|webp|gif|avif|bmp|svg|tiff?)$/.test(e)) return 'img';
  if (/^(mp4|mkv|mov|avi|webm|m4v|wmv|flv|ts)$/.test(e)) return 'vid';
  if (/^(xlsx?|ods|csv|tsv)$/.test(e)) return 'sheet';
  if (/^(pptx?|odp|key)$/.test(e)) return 'slide';
  if (/^(docx?|odt|rtf|pages)$/.test(e)) return 'doc';
  if (e === 'pdf') return 'pdf';
  if (/^(zip|rar|7z|tar|gz|bz2|xz|iso|dmg)$/.test(e)) return 'zip';
  if (/^(mp3|flac|wav|m4a|ogg|opus|aac)$/.test(e)) return 'aud';
  if (/^(py|js|ts|json|ya?ml|sh|go|rs|c|h|cpp|java|rb|php|sql|html?|css|r|m|toml|ini|conf|xml|env|dockerfile)$/.test(e)) return 'code';
  return 'txt';
}
export const KIND_LABEL = { img: 'Imagem', vid: 'Vídeo', doc: 'Documento', pdf: 'PDF', zip: 'Compactado', aud: 'Áudio', code: 'Código', txt: 'Arquivo', sheet: 'Planilha', slide: 'Apresentação' };
export function fileSVG(name, cls = 'file-ico') {
  const k = extKind(name);
  const parts = String(name).split('.');
  const ext = parts.length > 1 ? parts.pop().slice(0, 4).toUpperCase() : '';
  return `<svg class="${cls}" viewBox="0 0 40 46" aria-hidden="true"><path d="M7 2h19l11 11v29a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3V5a3 3 0 0 1 3-3z" fill="${FILE_COLORS[k]}"/><path d="M26 2v8a3 3 0 0 0 3 3h8" fill="rgba(255,255,255,.4)"/><text x="20.5" y="37" text-anchor="middle" font-family="Inter,sans-serif" font-size="8.5" font-weight="800" fill="#fff">${esc(ext)}</text></svg>`;
}

/* ── Notificações ──────────────────────────────────────────────── */
export function toast(title, sub = '', action, { error = false, ms } = {}) {
  const t = html(`<div class="toast ${error ? 'toast-error' : ''}" role="status"><span class="t-ico">${error ? svg('info') : '<span class="logo"></span>'}</span><span style="flex:1">${esc(title)}${sub ? `<br><span class="t-sub">${esc(sub)}</span>` : ''}</span>${action ? `<button class="pill-btn">${esc(action.label)}</button>` : ''}</div>`);
  const kill = () => { t.classList.add('out'); setTimeout(() => t.remove(), 250); };
  if (action) $('button', t).addEventListener('click', () => { action.run(); kill(); });
  t.addEventListener('click', (e) => { if (!e.target.closest('button')) kill(); });
  $('#toast-host').append(t);
  setTimeout(kill, ms || (action ? 9000 : error ? 6000 : 4200));
}
export const toastError = (err, title = 'Algo deu errado') => toast(title, err?.message || String(err), null, { error: true });

/* ── Menu de contexto ──────────────────────────────────────────── */
let menuCloser = null;
export function closeMenu() { menuCloser?.(); }
export function contextMenu(x, y, items, onClose) {
  closeMenu();
  const visible = items.filter((it) => it && (it === '-' || !it.hidden));
  const m = html(`<div class="ctx" role="menu">${visible.map((it, i) => it === '-' ? '<hr>' : `<button role="menuitem" data-i="${i}" ${it.disabled ? 'disabled' : ''} class="${it.danger ? 'danger' : ''}">${svg(it.icon || 'chevR')}<span>${esc(it.label)}</span>${it.kbd ? `<kbd>${esc(it.kbd)}</kbd>` : ''}</button>`).join('')}</div>`);
  $('#menu-host').append(m);
  const r = m.getBoundingClientRect();
  m.style.left = Math.max(8, Math.min(x, innerWidth - r.width - 8)) + 'px';
  m.style.top = Math.max(8, Math.min(y, innerHeight - r.height - 8)) + 'px';
  const close = () => { m.remove(); document.removeEventListener('mousedown', outside, true); removeEventListener('blur', close); menuCloser = null; onClose?.(); };
  const outside = (e) => { if (!m.contains(e.target)) close(); };
  setTimeout(() => { document.addEventListener('mousedown', outside, true); addEventListener('blur', close); });
  m.addEventListener('click', (e) => { const b = e.target.closest('[data-i]'); if (!b || b.disabled) return; close(); visible[+b.dataset.i].run(); });
  menuCloser = close;
}
export const menuOpen = () => !!menuCloser;

/* ── Diálogos ──────────────────────────────────────────────────── */
export const dialogs = [];
// beforeClose: função (pode ser async) que devolve false para não fechar (ex.: alterações não salvas).
export function openDialog(inner, { cls = '', wrapCls = '', closeBtn = true, onClose, width, backdropClose = true, beforeClose } = {}) {
  const wrap = html(`<div class="dlg-wrap ${wrapCls}"><div class="dialog ${cls}" role="dialog" aria-modal="true">${inner}</div>${closeBtn ? `<button class="dlg-x" aria-label="Fechar">${svg('x')}</button>` : ''}</div>`);
  const dlg = $('.dialog', wrap);
  if (width) dlg.style.width = `min(${width}px, 100%)`;
  const close = (force = false) => {
    if (wrap._closing) return;
    if (beforeClose && force !== true) { Promise.resolve(beforeClose()).then((ok) => { if (ok !== false) close(true); }); return; }
    wrap._closing = true;
    wrap.classList.add('closing');
    const i = dialogs.indexOf(close); if (i >= 0) dialogs.splice(i, 1);
    if (!dialogs.length) document.body.classList.remove('has-dialog');
    onClose?.();
    setTimeout(() => wrap.remove(), 180);
  };
  wrap.addEventListener('mousedown', (e) => { if (backdropClose && e.target === wrap) close(); });
  $('.dlg-x', wrap)?.addEventListener('click', () => close());
  $('#dialog-host').append(wrap);
  dialogs.push(close);
  document.body.classList.add('has-dialog');
  setTimeout(() => $('[autofocus]', dlg)?.focus(), 40);
  return { el: dlg, close };
}
export function confirmDialog(title, text, okLabel = 'Confirmar', { danger = false } = {}) {
  return new Promise((resolve) => {
    let done = false;
    const d = openDialog(`<h2>${esc(title)}</h2><p>${esc(text)}</p><div class="dlg-actions"><button class="pill-btn" data-c="no">Cancelar</button><button class="pill-btn ${danger ? 'danger-solid' : 'primary'}" data-c="ok" autofocus>${esc(okLabel)}</button></div>`,
      { closeBtn: false, width: 420, onClose: () => { if (!done) resolve(false); } });
    d.el.addEventListener('click', (e) => { const b = e.target.closest('[data-c]'); if (!b) return; done = true; resolve(b.dataset.c === 'ok'); d.close(); });
  });
}
export function promptDialog(title, { label = '', value = '', okLabel = 'Salvar', placeholder = '', select = true } = {}) {
  return new Promise((resolve) => {
    let done = false;
    const d = openDialog(`<h2>${esc(title)}</h2><form class="form-grid"><label>${esc(label)}<input class="text-input" name="v" value="${esc(value)}" placeholder="${esc(placeholder)}" autocomplete="off"></label>
      <div class="dlg-actions"><button type="button" class="pill-btn" data-c="no">Cancelar</button><button type="submit" class="pill-btn primary">${esc(okLabel)}</button></div></form>`,
      { closeBtn: false, width: 420, onClose: () => { if (!done) resolve(null); } });
    const input = $('input', d.el);
    setTimeout(() => {
      input.focus();
      if (select) { const dot = value.lastIndexOf('.'); input.setSelectionRange(0, dot > 0 ? dot : value.length); }
    }, 50);
    $('form', d.el).addEventListener('submit', (e) => { e.preventDefault(); done = true; resolve(input.value.trim()); d.close(); });
    $('[data-c="no"]', d.el).addEventListener('click', () => d.close());
  });
}
