// Internet: widget (latência e % no ar) e janela com o gráfico de 24 h e as quedas.
import { api } from './api.js';
import { mountChart, periodSeg, PERIOD_LONG } from './charts.js';
import { $, esc, fmt, svg, timeAgo, fmtDuration, openDialog } from './ui.js';

export const internetWidgetHTML = () => `<button class="widget w-internet" data-action="internet">
  <span class="w-caption">Internet</span>
  <span class="w-value"><span data-net="ms">—</span><span class="w-unit" data-net="unit"></span></span>
  <svg class="w-net-spark" viewBox="0 0 200 34" preserveAspectRatio="none"><path data-net="spark" fill="none" stroke="rgba(255,255,255,.85)" stroke-width="1.5" vector-effect="non-scaling-stroke"/></svg>
  <span class="w-sub" data-net="sub">medindo…</span></button>`;

// linha da latência; minutos sem internet quebram a linha
function sparkPath(points, w, h, pad = 3) {
  const ok = points.filter((p) => p.ms >= 0);
  if (ok.length < 2) return '';
  const t0 = points[0].t, t1 = points[points.length - 1].t || t0 + 1;
  const max = Math.max(...ok.map((p) => p.ms), 10);
  let d = '', prev = null;
  for (const p of points) {
    if (p.ms < 0) { prev = null; continue; }
    const x = ((p.t - t0) / Math.max(1, t1 - t0)) * w, y = h - pad - (p.ms / max) * (h - pad * 2);
    d += `${prev && p.t - prev.t <= 180 ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`;
    prev = p;
  }
  return d;
}

export function paintInternet(root, st) {
  const el = (k) => $(`[data-net="${k}"]`, root);
  if (!el('ms')) return;
  if (!st) return;
  if (!st.enabled) { el('ms').textContent = 'desligado'; el('unit').textContent = ''; el('sub').textContent = 'ligue em Configurações'; return; }
  if (!st.checked) { el('ms').textContent = '—'; el('unit').textContent = ''; el('sub').textContent = 'medindo…'; return; }
  el('ms').textContent = st.up ? fmt(st.ms, st.ms < 10 ? 1 : 0) : 'sem internet';
  el('unit').textContent = st.up ? ' ms' : '';
  root.querySelector('.w-internet')?.classList.toggle('down', !st.up);
  el('sub').textContent = st.uptime != null ? `${fmt(st.uptime, st.uptime === 100 ? 0 : 1)}% no ar · 24 h` : '';
  el('spark').setAttribute('d', sparkPath(st.points.slice(-120), 200, 34));
}

export async function internetDialog() {
  const d = openDialog(`<h2>${svg('globe')}Internet</h2><div class="inet"><div class="inet-load"><div class="f-loading"><i></i><i></i><i></i></div></div></div>`, { width: 640 });
  let st;
  try { st = await api.internet(); } catch (e) { $('.inet', d.el).innerHTML = `<p>${esc(e.message)}</p>`; return; }
  const outs = st.outages || [];
  const now = Date.now() / 1000;
  let range = '24h';
  $('.inet', d.el).innerHTML = `
    <div class="inet-cards">
      <div class="inet-card"><small>Agora</small><b class="${st.up ? '' : 'bad'}">${!st.checked ? '—' : st.up ? `${fmt(st.ms, st.ms < 10 ? 1 : 0)} ms` : 'sem internet'}</b></div>
      <div class="inet-card"><small>Média 24 h</small><b>${st.avg ? `${fmt(st.avg, 1)} ms` : '—'}</b></div>
      <div class="inet-card"><small>No ar · 24 h</small><b>${st.uptime != null ? `${fmt(st.uptime, st.uptime === 100 ? 0 : 2)}%` : '—'}</b></div>
    </div>
    <div class="inet-chart"><div class="inet-lbl"><span>Latência · <span data-inet-span>${PERIOD_LONG[range]}</span> <small>(tempo para conectar a Cloudflare, Google e Quad9; vale o mais rápido; faixas vermelhas: sem internet)</small></span>${periodSeg(range)}</div>
      <div class="inet-cx"></div></div>
    <div class="live-sec">Quedas <small>${outs.length ? `as últimas ${outs.length}` : ''}</small></div>
    <div class="inet-outs">${outs.length ? outs.map((o) => {
      const end = o.end || now;
      return `<div class="inet-out ${o.end ? '' : 'open'}"><span>${svg('alert')}</span><b>${esc(new Date(o.start * 1000).toLocaleString('pt-BR', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' }))}</b>
        <small>${o.end ? `ficou ${esc(fmtDuration(end - o.start))} fora` : 'fora do ar agora'} · ${esc(timeAgo(o.start))}</small></div>`;
    }).join('') : '<div class="empty-note">Nenhuma queda registrada. 🎉</div>'}</div>
    ${st.enabled ? '' : '<p class="ai-note">O monitor de internet está desligado (Configurações → Sistema).</p>'}`;
  // gráfico (0.14.0): Período de 1 h a 30 dias pelos Históricos; quedas como faixas vermelhas
  const chart = mountChart($('.inet-cx', d.el), [], { h: 130, fmt: (v) => `${fmt(v, v > 0 && v < 10 ? 1 : 0)} ms`, min: 0 });
  const load = async () => {
    try {
      const r = await api.history(['net.lat'], range);
      const pts = (r.series['net.lat'] || []).map(([t, v, mx]) => ({ t, v, max: mx }));
      const t0 = pts.length ? pts[0].t : now - 86400;
      const bands = outs.filter((o) => (o.end || now) >= t0).map((o) => ({ t0: o.start, t1: o.end || now }));
      chart.update([{ pts, cls: 'c0' }], { step: r.step, bands, empty: 'Ainda sem medições. O BrunOS mede a cada minuto.' });
    } catch (e) { chart.update([], { empty: e.message }); }
  };
  load();
  d.el.addEventListener('click', (e) => {
    const pe = e.target.closest('[data-period]'); if (!pe) return;
    range = pe.dataset.period;
    d.el.querySelectorAll('[data-period]').forEach((x) => x.classList.toggle('on', x === pe));
    $('[data-inet-span]', d.el).textContent = PERIOD_LONG[range];
    load();
  });
}
