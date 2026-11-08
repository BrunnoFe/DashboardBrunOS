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

/**
 * Linha (com área opcional) de 1 a 3 séries no mesmo eixo de tempo.
 * series: [{ pts, cls }]; opts: { w, h, area, min, max, maxGap (s) }
 */
export function lineSVG(series, { w = 300, h = 80, area = true, min, max, maxGap, cls = '' } = {}) {
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
  return `<svg class="chart ${cls}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true">${paths}</svg>`;
}

/** Barras de 1 a 3 séries (lado a lado em cada ponto). */
export function barSVG(series, { w = 300, h = 80, min, max, cls = '' } = {}) {
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
  return `<svg class="chart ${cls}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true">${out}</svg>`;
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

/** Mínimo, máximo e média de uma série. */
export function stats(pts) {
  const v = pts.map((p) => p.v).filter((x) => x != null && isFinite(x));
  if (!v.length) return null;
  return { min: Math.min(...v), max: Math.max(...v), avg: v.reduce((s, x) => s + x, 0) / v.length };
}
