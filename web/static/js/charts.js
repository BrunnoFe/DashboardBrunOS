// Gráficos dos widgets em SVG próprio (sem biblioteca): linha/área, barras, rosca e mini-gráfico.
// Funções puras que devolvem texto SVG; o widget só é redesenhado quando chega um dado novo.
// Pontos: [{ t, v }] com v = null quando não há dado (a linha quebra ali).

const f1 = (n) => (Math.round(n * 10) / 10).toString();

// escala com folga: não deixa a linha colada nas bordas e não exagera variações pequenas
function scale(vals, { min, max, zero = false, minSpan = 1 } = {}) {
  const ok = vals.filter((v) => v != null && isFinite(v));
  if (!ok.length) return { lo: 0, hi: 1 };
  let lo = min ?? Math.min(...ok), hi = max ?? Math.max(...ok);
  if (zero) lo = Math.min(0, lo);
  const span = Math.max(hi - lo, minSpan);
  if (min == null) lo = zero ? lo : Math.max(0, lo - span * 0.15);
  if (max == null) hi = hi + span * 0.2;
  if (hi <= lo) hi = lo + 1;
  return { lo, hi };
}

function pathOf(pts, w, h, t0, t1, lo, hi, pad, maxGap) {
  let d = '', prev = null;
  for (const p of pts) {
    if (p.v == null || !isFinite(p.v)) { prev = null; continue; }
    const x = ((p.t - t0) / Math.max(1, t1 - t0)) * w;
    const y = h - pad - ((p.v - lo) / (hi - lo)) * (h - pad * 2);
    d += `${prev && (!maxGap || p.t - prev.t <= maxGap) ? 'L' : 'M'}${f1(x)} ${f1(y)}`;
    prev = p;
  }
  return d;
}

/* Valor ao passar o mouse nos widgets (0.14.0): o SVG leva os horários e os valores já formatados em
   data-tip; um único ouvinte na página (initChartTips) mostra o balão. tip: { fmts: [fn por série],
   names: [nome por série] }. */
function tipAttr(series, tip, kind) {
  if (!tip) return '';
  const ts = series[0].pts.map((p) => p.t);
  const rows = series.map((s, i) => s.pts.map((p) => (p.v == null || !isFinite(p.v) ? null : (tip.fmts?.[i] || tip.fmts?.[0] || f1)(p.v))));
  return ` data-tip="${encodeURIComponent(JSON.stringify({ k: kind, ts, rows, names: tip.names || [] }))}"`;
}
let tipEl = null;
export function initChartTips() {
  if (tipEl) return;
  tipEl = document.createElement('div');
  tipEl.className = 'ch-tip'; tipEl.hidden = true;
  document.body.append(tipEl);
  let cache = { el: null, d: null };
  document.addEventListener('pointermove', (e) => {
    const svg = e.target.closest?.('svg[data-tip]');
    if (!svg) { tipEl.hidden = true; return; }
    if (cache.el !== svg || cache.raw !== svg.dataset.tip) cache = { el: svg, raw: svg.dataset.tip, d: JSON.parse(decodeURIComponent(svg.dataset.tip)) };
    const { k, ts, rows, names } = cache.d, r = svg.getBoundingClientRect(), fx = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width));
    let i;
    if (k === 'bar') i = Math.min(ts.length - 1, Math.floor(fx * ts.length));
    else { const t = ts[0] + fx * (ts[ts.length - 1] - ts[0]); i = 0; for (let j = 1; j < ts.length; j++) if (Math.abs(ts[j] - t) < Math.abs(ts[i] - t)) i = j; }
    const vals = rows.map((row, si) => [names[si], row[i]]).filter(([, v]) => v != null);
    if (!vals.length) { tipEl.hidden = true; return; }
    const span = ts[ts.length - 1] - ts[0];
    tipEl.innerHTML = `<small>${fmtTime(ts[i], span)}</small>${vals.map(([n, v], si) => `<span><i class="c${si}"></i>${n ? `${n} ` : ''}<b>${v}</b></span>`).join('')}`;
    tipEl.hidden = false;
    const tw = tipEl.offsetWidth, th = tipEl.offsetHeight;
    tipEl.style.left = `${Math.min(innerWidth - tw - 8, Math.max(8, e.clientX - tw / 2))}px`;
    tipEl.style.top = `${Math.max(8, e.clientY - th - 14)}px`;
  }, { passive: true });
}

/**
 * Linha (com área opcional) de 1 a 3 séries no mesmo eixo de tempo.
 * series: [{ pts, cls }]; opts: { w, h, area, min, max, maxGap (s), tip, live (Ponta viva, valor formatado) }
 */
export function lineSVG(series, { w = 300, h = 80, area = true, min, max, maxGap, cls = '', tip, live } = {}) {
  const all = series.flatMap((s) => s.pts);
  const ts = all.map((p) => p.t);
  if (all.filter((p) => p.v != null).length < 2) return '';
  const t0 = Math.min(...ts), t1 = Math.max(...ts);
  const { lo, hi } = scale(all.map((p) => p.v), { min, max });
  const paths = series.map((s, i) => {
    const d = pathOf(s.pts, w, h, t0, t1, lo, hi, 3, maxGap);
    if (!d) return '';
    // a área só faz sentido sob a primeira série, e só se a linha não quebra
    const fill = area && i === 0 && !d.slice(1).includes('M') ? `<path class="ch-area" d="${d}L${w} ${h}L0 ${h}Z"/>` : '';
    return `${fill}<path class="ch-line ${s.cls || `c${i}`}" d="${d}" vector-effect="non-scaling-stroke"/>`;
  }).join('');
  const out = `<svg class="chart ${cls}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true"${tipAttr(series, tip, 'line')}>${paths}</svg>`;
  const last = live ? [...series[0].pts].reverse().find((p) => p.v != null && isFinite(p.v)) : null;
  if (!last) return out;
  // Ponta viva: bolinha e valor em HTML por cima (o SVG estica e deformaria o círculo)
  const x = ((last.t - t0) / Math.max(1, t1 - t0)) * 100, y = ((h - 3 - ((last.v - lo) / (hi - lo)) * (h - 6)) / h) * 100;
  return `<span class="ch-wrap">${out}<span class="cx-live ${y < 25 ? 'low' : ''}" style="left:${x.toFixed(2)}%;top:${y.toFixed(2)}%"><i></i><b>${live}</b></span></span>`;
}

/** Barras de 1 a 3 séries (lado a lado em cada ponto). */
export function barSVG(series, { w = 300, h = 80, min, max, cls = '', tip } = {}) {
  const n = Math.max(...series.map((s) => s.pts.length), 0);
  if (!n) return '';
  const all = series.flatMap((s) => s.pts.map((p) => p.v));
  if (!all.some((v) => v != null)) return '';
  const { hi } = scale(all, { min: min ?? 0, max, zero: true });
  const lo = min ?? 0;
  const slot = w / n, gap = Math.min(2, slot * 0.25), bw = (slot - gap) / series.length;
  let out = '';
  series.forEach((s, si) => s.pts.forEach((p, i) => {
    if (p.v == null || !isFinite(p.v)) return;
    const bh = Math.max(p.v > lo ? 1 : 0, ((p.v - lo) / (hi - lo)) * (h - 2));
    out += `<rect class="ch-bar ${s.cls || `c${si}`}" x="${f1(i * slot + gap / 2 + si * bw)}" y="${f1(h - bh)}" width="${f1(Math.max(0.5, bw))}" height="${f1(bh)}" rx="${bw > 4 ? 1.5 : 0}"/>`;
  }));
  return `<svg class="chart ${cls}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true"${tipAttr(series, tip, 'bar')}>${out}</svg>`;
}

/**
 * Rosca: parts = [{ v, cls }]. Com uma parte só e total, desenha o anel até v/total.
 */
export function donutSVG(parts, { total, size = 100, thick = 12, cls = '' } = {}) {
  const r = (size - thick) / 2, c = 2 * Math.PI * r, mid = size / 2;
  const sum = total ?? parts.reduce((s, p) => s + Math.max(0, p.v || 0), 0);
  let off = 0, arcs = '';
  if (sum > 0) {
    for (const [i, p] of parts.entries()) {
      const frac = Math.max(0, Math.min(1, (p.v || 0) / sum));
      if (frac <= 0) continue;
      const len = frac * c;
      arcs += `<circle class="ch-arc ${p.cls || `c${i}`}" cx="${mid}" cy="${mid}" r="${f1(r)}" stroke-width="${thick}" stroke-dasharray="${f1(len)} ${f1(c - len)}" stroke-dashoffset="${f1(-off)}" transform="rotate(-90 ${mid} ${mid})"/>`;
      off += len;
    }
  }
  return `<svg class="donut ${cls}" viewBox="0 0 ${size} ${size}" aria-hidden="true"><circle class="ch-ring" cx="${mid}" cy="${mid}" r="${f1(r)}" stroke-width="${thick}"/>${arcs}</svg>`;
}

/** Mini-gráfico (sparkline) de uma série. */
export function sparkSVG(pts, { w = 120, h = 28, maxGap, cls = '' } = {}) {
  if (!pts || pts.filter((p) => p.v != null).length < 2) return '';
  const t0 = pts[0].t, t1 = pts[pts.length - 1].t;
  const { lo, hi } = scale(pts.map((p) => p.v));
  const d = pathOf(pts, w, h, t0, t1, lo, hi, 2, maxGap);
  return `<svg class="spark-mini ${cls}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true"><path d="${d}" vector-effect="non-scaling-stroke"/></svg>`;
}

/* ── Gráfico interativo das janelas (0.14.0) ──────────────────────────────────────────────────────
   Eixo Y com 3 valores redondos, horário do começo e do fim, valor ao passar o mouse (ou arrastar o
   dedo) e Ponta viva no Período ao vivo. O SVG estica (preserveAspectRatio="none"); bolinhas, textos e
   o balão são HTML por cima, posicionados em %, para não deformar. */

/** Seletor de Período igual em todas as janelas (live = "5 min" ao vivo, quando a janela tem). */
export const PERIOD_LONG = { live: 'últimos 5 min', '5m': 'últimos 5 min', '1h': 'última hora', '24h': 'últimas 24 h', '48h': 'últimas 48 h', '7d': 'últimos 7 dias', '30d': 'últimos 30 dias' };
export function periodSeg(cur, live = false) {
  const list = [...(live ? [['live', '5 min']] : []), ['1h', '1 h'], ['24h', '24 h'], ['48h', '48 h'], ['7d', '7 dias'], ['30d', '30 dias']];
  return `<div class="seg seg-sm seg-period" role="group" aria-label="Período">${list.map(([k, l]) => `<button type="button" class="${cur === k ? 'on' : ''}" data-period="${k}">${l}</button>`).join('')}</div>`;
}

/** 3 (ou n) marcas redondas entre lo e hi: passos 1, 2, 2,5 ou 5 × 10^k. */
export function niceTicks(lo, hi, n = 3) {
  if (!(hi > lo)) hi = lo + 1;
  const raw = (hi - lo) / (n - 1), p = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * p).find((s) => s >= raw) || 10 * p;
  const start = Math.floor(lo / step) * step;
  const ticks = [];
  for (let v = start; ticks.length < n || v < hi - 1e-9; v += step) ticks.push(+v.toFixed(10));
  return ticks;
}

const pad2 = (n) => String(n).padStart(2, '0');
const MESES = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];
/** Horário curto: "14:20" (até 48 h) ou "12/nov 14:20" / "12/nov" (períodos longos). */
export function fmtTime(t, span = 0, { day = false } = {}) {
  const d = new Date(t * 1000), hm = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`, dm = `${d.getDate()}/${MESES[d.getMonth()]}`;
  if (day) return dm;
  return span > 2 * 86400 ? `${dm} ${hm}` : hm;
}

/**
 * Monta um gráfico interativo em `el`. Devolve { update(series, opts?), destroy() }.
 * series: [{ pts: [{ t, v, max? }], name, cls: 'c0' | 'c1', area? }]
 * opts: { h = 120, kind: 'line' | 'bar', fmt: (v) => texto, min, max (fixos, ex.: 0–100 %), step (s:
 *   quebra a linha em buracos maiores que 2,5 × step), live (Ponta viva), bands: [{ t0, t1, cls }],
 *   tLabel: (t) => texto do balão/eixo (barras de dias, por exemplo), empty: texto sem dados }
 */
export function mountChart(el, series = [], opts = {}) {
  let S = series, O = { h: 120, kind: 'line', fmt: (v) => f1(v), ...opts };
  el.classList.add('cx');
  el.innerHTML = `<div class="cx-body"><div class="cx-y"></div><div class="cx-plot"><svg class="cx-svg" preserveAspectRatio="none" aria-hidden="true"></svg>
    <i class="cx-cursor" hidden></i><div class="cx-dots"></div><div class="cx-live" hidden><i></i><b></b></div><div class="cx-tip" hidden></div><div class="cx-empty" hidden></div></div></div>
    <div class="cx-x"><span></span><span></span></div>`;
  const plot = el.querySelector('.cx-plot'), svg = el.querySelector('.cx-svg'), yAx = el.querySelector('.cx-y');
  const cursor = el.querySelector('.cx-cursor'), dots = el.querySelector('.cx-dots'), tip = el.querySelector('.cx-tip');
  const live = el.querySelector('.cx-live'), empty = el.querySelector('.cx-empty'), [x0, x1] = el.querySelectorAll('.cx-x span');
  const W = 1000;
  let geo = null; // { t0, t1, lo, hi, H }
  const ok = (p) => p && p.v != null && isFinite(p.v);

  function draw() {
    const H = O.h;
    plot.style.height = H + 'px';
    svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
    const all = S.flatMap((s) => s.pts.filter(ok));
    const has = O.kind === 'bar' ? all.length > 0 : S.some((s) => s.pts.filter(ok).length >= 2);
    empty.hidden = has; empty.textContent = O.empty || 'ainda sem dados';
    if (!has) { svg.innerHTML = ''; yAx.innerHTML = ''; x0.textContent = x1.textContent = ''; live.hidden = true; dots.innerHTML = ''; geo = null; return; }
    const vals = all.flatMap((p) => (p.max != null && isFinite(p.max) ? [p.v, p.max] : [p.v]));
    let lo = O.min ?? Math.min(...vals), hi = O.max ?? Math.max(...vals);
    if (O.kind === 'bar') lo = Math.min(0, lo);
    if (O.min == null && O.kind !== 'bar') lo = Math.max(0, lo - (hi - lo || 1) * 0.1);
    if (O.max == null) hi = hi + (hi - lo || 1) * 0.08;
    const ticks = niceTicks(lo, hi, 3);
    lo = O.min ?? ticks[0]; hi = O.max ?? ticks[ticks.length - 1];
    if (hi <= lo) hi = lo + 1;
    const ts = all.map((p) => p.t);
    let t0 = Math.min(...ts), t1 = Math.max(...ts);
    if (O.kind === 'bar') { const st = O.step || (S[0].pts.length > 1 ? S[0].pts[1].t - S[0].pts[0].t : 1); t1 += st; }
    if (t1 <= t0) t1 = t0 + 1;
    geo = { t0, t1, lo, hi, H };
    const X = (t) => ((t - t0) / (t1 - t0)) * W, Y = (v) => H - 2 - ((v - lo) / (hi - lo)) * (H - 4);
    // eixo Y e linhas-guia
    const tk = O.min != null || O.max != null ? niceTicks(lo, hi, 3).filter((v) => v >= lo - 1e-9 && v <= hi + 1e-9) : ticks;
    yAx.innerHTML = tk.map((v) => `<span style="top:${((Y(v) / H) * 100).toFixed(2)}%">${O.fmt(v)}</span>`).join('');
    let out = tk.map((v) => `<line class="cx-grid" x1="0" x2="${W}" y1="${f1(Y(v))}" y2="${f1(Y(v))}" vector-effect="non-scaling-stroke"/>`).join('');
    for (const b of O.bands || []) out += `<rect class="cx-band ${b.cls || ''}" x="${f1(X(Math.max(b.t0, t0)))}" y="0" width="${f1(Math.max(2, X(Math.min(b.t1 || t1, t1)) - X(Math.max(b.t0, t0))))}" height="${H}"/>`;
    if (O.kind === 'bar') {
      const n = Math.max(...S.map((s) => s.pts.length)), slot = W / Math.max(1, n), gap = Math.min(6, slot * 0.2), bw = (slot - gap) / S.length;
      S.forEach((s, si) => s.pts.forEach((p, i) => {
        if (!ok(p)) return;
        const y = Y(p.v), base = Y(Math.max(lo, 0));
        out += `<rect class="ch-bar ${s.cls || `c${si}`}" x="${f1(i * slot + gap / 2 + si * bw)}" y="${f1(Math.min(y, base))}" width="${f1(Math.max(1, bw))}" height="${f1(Math.max(1, Math.abs(base - y)))}" rx="2"/>`;
      }));
    } else {
      const maxGap = O.step ? O.step * 2.5 : 0;
      S.forEach((s, si) => {
        let d = '', prev = null, broke = false;
        for (const p of s.pts) {
          if (!ok(p)) { prev = null; broke = true; continue; }
          const jump = prev && maxGap && p.t - prev.t > maxGap;
          if (jump) broke = true;
          d += `${prev && !jump ? 'L' : 'M'}${f1(X(p.t))} ${f1(Y(p.v))}`;
          prev = p;
        }
        if (!d) return;
        if ((s.area ?? si === 0) && !broke) out += `<path class="ch-area" d="${d}L${f1(X(prev.t))} ${H}L${f1(X(s.pts.find(ok).t))} ${H}Z"/>`;
        out += `<path class="ch-line ${s.cls || `c${si}`}" d="${d}" vector-effect="non-scaling-stroke"/>`;
      });
    }
    svg.innerHTML = out;
    const lab = (t) => (O.xLabel ? O.xLabel(t) : O.tLabel ? O.tLabel(t) : fmtTime(t, t1 - t0));
    x0.textContent = lab(t0);
    x1.textContent = O.kind === 'bar' ? lab(S[0].pts[S[0].pts.length - 1].t) : lab(Math.max(...ts));
    // Ponta viva: último ponto da primeira série
    const last = [...S[0].pts].reverse().find(ok);
    live.hidden = !(O.live && last && O.kind !== 'bar');
    if (!live.hidden) {
      live.style.left = `${(X(last.t) / W) * 100}%`; live.style.top = `${(Y(last.v) / H) * 100}%`;
      live.querySelector('b').textContent = O.fmt(last.v);
      live.classList.toggle('low', Y(last.v) < 22);
    }
    if (hoverX != null) hover(hoverX);
  }

  // valor ao passar o mouse / arrastar o dedo
  let hoverX = null;
  function nearest(pts, t) {
    let a = 0, b = pts.length - 1;
    while (b - a > 1) { const m = (a + b) >> 1; if (pts[m].t < t) a = m; else b = m; }
    return Math.abs(pts[a]?.t - t) <= Math.abs(pts[b]?.t - t) ? pts[a] : pts[b];
  }
  function hover(px) {
    if (!geo) return;
    const r = plot.getBoundingClientRect(), fx = Math.min(1, Math.max(0, (px - r.left) / r.width));
    const { t0, t1, lo, hi, H } = geo;
    let t = t0 + fx * (t1 - t0), rows = [], at = null;
    if (O.kind === 'bar') {
      const n = Math.max(...S.map((s) => s.pts.length)), i = Math.min(n - 1, Math.floor(fx * n));
      at = S[0].pts[i]?.t;
      rows = S.map((s) => ({ s, p: s.pts[i] })).filter((x) => ok(x.p));
      t = at;
      cursor.style.left = `${((i + 0.5) / n) * 100}%`;
    } else {
      const p0 = nearest(S[0].pts.filter(ok), t);
      at = p0?.t; t = at;
      rows = S.map((s) => ({ s, p: nearest(s.pts.filter(ok), at) })).filter((x) => ok(x.p) && Math.abs(x.p.t - at) <= (O.step || 60) * 1.5);
      cursor.style.left = `${((at - t0) / (t1 - t0)) * 100}%`;
    }
    if (at == null || !rows.length) { leave(); return; }
    cursor.hidden = false;
    dots.innerHTML = O.kind === 'bar' ? '' : rows.map(({ s, p }, i) => `<i class="${s.cls || `c${i}`}" style="left:${((p.t - t0) / (t1 - t0)) * 100}%;top:${((H - 2 - ((p.v - lo) / (hi - lo)) * (H - 4)) / H) * 100}%"></i>`).join('');
    const lab = O.tLabel ? O.tLabel(at) : fmtTime(at, t1 - t0);
    tip.innerHTML = `<small>${lab}</small>${rows.map(({ s, p }, i) => `<span><i class="${s.cls || `c${i}`}"></i>${s.name ? `${s.name} ` : ''}<b>${O.fmt(p.v)}</b>${p.max != null && isFinite(p.max) && Math.abs(p.max - p.v) > 1e-6 ? ` <em>pico ${O.fmt(p.max)}</em>` : ''}</span>`).join('')}`;
    tip.hidden = false;
    const tw = tip.offsetWidth, cx = parseFloat(cursor.style.left) / 100 * r.width;
    tip.style.left = `${Math.max(0, Math.min(r.width - tw, cx - tw / 2))}px`;
  }
  function leave() { hoverX = null; cursor.hidden = true; tip.hidden = true; dots.innerHTML = ''; }
  const onMove = (e) => { hoverX = e.clientX; hover(e.clientX); };
  plot.addEventListener('pointermove', onMove);
  plot.addEventListener('pointerdown', onMove);
  plot.addEventListener('pointerleave', leave);
  plot.addEventListener('pointercancel', leave);
  draw();
  return {
    update(series2, opts2) { if (series2) S = series2; if (opts2) O = { ...O, ...opts2 }; draw(); },
    destroy() { plot.removeEventListener('pointermove', onMove); plot.removeEventListener('pointerdown', onMove); el.innerHTML = ''; },
  };
}

/** Mínimo, máximo e média de uma série. */
export function stats(pts) {
  const v = pts.map((p) => p.v).filter((x) => x != null && isFinite(x));
  if (!v.length) return null;
  return { min: Math.min(...v), max: Math.max(...v), avg: v.reduce((s, x) => s + x, 0) / v.length };
}
