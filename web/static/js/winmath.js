// Contas das Janelas (0.17.0), sem tocar na página: tamanho inicial, limites da tela, encaixe,
// redimensionar e quem fechar quando passa do limite. Testadas em tests/windows.test.mjs.

export const MIN_W = 360, MIN_H = 240;
const GRIP = 120; // quanto da janela precisa sobrar na tela para dar para pegá-la de volta
const BAR = 44;   // altura da barra (a barra nunca sai por baixo da tela)

const clamp = (v, a, b) => Math.min(b, Math.max(a, v));

// Primeira vez que o app abre: centralizada, ~80 % da largura e ~85 % da altura acima da dock.
export function defaultRect(vw, vh, dockSpace = 94) {
  const free = Math.max(MIN_H, vh - dockSpace);
  const w = clamp(Math.round(vw * 0.8), Math.min(MIN_W, vw), Math.max(MIN_W, vw - 32));
  const h = clamp(Math.round(free * 0.85), Math.min(MIN_H, vh), Math.max(MIN_H, free - 24));
  return { x: Math.round((vw - w) / 2), y: Math.max(12, Math.round((free - h) / 2)), w, h };
}

// Mantém a janela pegável: tamanho entre o mínimo e a tela, barra sempre visível.
export function clampRect(r, vw, vh) {
  const w = clamp(r.w, Math.min(MIN_W, vw), vw), h = clamp(r.h, Math.min(MIN_H, vh), vh);
  return { x: clamp(r.x, GRIP - w, vw - GRIP), y: clamp(r.y, 0, vh - BAR), w, h };
}

// Onde o mouse está ao arrastar: borda de cima maximiza, laterais dividem a tela pela metade.
export function snapZone(px, py, vw) {
  if (py <= 6) return 'max';
  if (px <= 6) return 'left';
  if (px >= vw - 7) return 'right';
  return null;
}

// Redimensionar por uma borda ou canto (dir = n, s, e, w ou a combinação, ex.: "se").
export function resizeRect(start, dir, dx, dy, vw, vh) {
  let { x, y, w, h } = start;
  if (dir.includes('e')) w = start.w + dx;
  if (dir.includes('w')) w = start.w - dx;
  if (dir.includes('s')) h = start.h + dy;
  if (dir.includes('n')) h = start.h - dy;
  w = clamp(w, MIN_W, vw);
  h = clamp(h, MIN_H, vh);
  if (dir.includes('w')) x = start.x + start.w - w;
  if (dir.includes('n')) {
    y = start.y + start.h - h;
    if (y < 0) { h += y; y = 0; }
  }
  return { x, y, w, h };
}

// Passou do limite de apps abertos: a minimizada usada há mais tempo; sem minimizada, a do fundo.
export function pickToClose(list) {
  const mins = list.filter((w) => w.min).sort((a, b) => a.used - b.used);
  if (mins.length) return mins[0].id;
  return [...list].sort((a, b) => a.z - b.z)[0]?.id ?? null;
}

// Clique no ícone da dock de um app aberto.
export function dockAction(w, frontId) {
  if (w.min) return 'restore';
  return w.id === frontId ? 'minimize' : 'focus';
}
