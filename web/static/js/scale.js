// Escala da interface (0.33.0, decisão 72): duas, por aparelho, guardadas no navegador.
// "Tela inicial" (saudação, widgets, ícones, busca) e "Sistema" (janelas, dock, botões do canto, avisos,
// menus e diálogos). Viram as variáveis --hs e --ss do CSS (zoom). No celular o mínimo é 85 %, para o dedo.
const KEY = 'brunos.scale';
export const SCALE = { min: 70, max: 110, step: 5, minMobile: 85 };
const MOBILE_W = 640;
let cur = { home: 100, sys: 100 };
try { const v = JSON.parse(localStorage.getItem(KEY) || 'null'); if (v) cur = { home: norm(v.home), sys: norm(v.sys) }; } catch { /* sem armazenamento: 100 % */ }

function norm(n) {
  n = Math.round(Number(n) / SCALE.step) * SCALE.step;
  return Number.isFinite(n) ? Math.min(SCALE.max, Math.max(SCALE.min, n)) : 100;
}
/** Mínimo para esta tela (celular: 85 %). */
export const scaleMin = () => (innerWidth < MOBILE_W ? SCALE.minMobile : SCALE.min);
/** Valor guardado (em %), sem o limite do celular. */
export const scaleSaved = (part) => cur[part] ?? 100;
/** Valor em uso (em %), já com o limite do celular. */
export const scalePct = (part) => Math.max(scaleMin(), scaleSaved(part));
/** Fator em uso (1 = 100 %). */
export const scaleOf = (part) => scalePct(part) / 100;

/** Muda uma escala (part: 'home' | 'sys') e aplica. Devolve o valor em uso. */
export function setScale(part, pct) {
  cur[part] = norm(Math.max(scaleMin(), pct));
  try { localStorage.setItem(KEY, JSON.stringify(cur)); } catch { /* fica só nesta aba */ }
  applyScale();
  return scalePct(part);
}

/** Põe as variáveis no :root (chamar ao abrir e quando a largura da tela muda). */
export function applyScale() {
  const r = document.documentElement.style;
  r.setProperty('--hs', String(scaleOf('home')));
  r.setProperty('--ss', String(scaleOf('sys')));
}
