// Encaixe dos widgets na Área de widgets (0.13.0, decisão 48). Funções puras, sem DOM: a Área é uma
// lista de { id, x, y, w, h } (em Quadros, x/y a partir de 0) numa grade de cols × rows (a Medida).
// Testadas com `node --test tests/layout.test.mjs`.

/** Os dois retângulos se sobrepõem? */
export const overlaps = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** O retângulo (x, y, w, h) cabe na grade sem encostar em ninguém (menos os ids em `skip`)? */
export function fits(list, cols, rows, x, y, w, h, skip = []) {
  if (x < 0 || y < 0 || w < 1 || h < 1 || x + w > cols || y + h > rows) return false;
  const r = { x, y, w, h };
  return !list.some((o) => !skip.includes(o.id) && overlaps(o, r));
}

/** Primeiro lugar livre na ordem de leitura (linha por linha, da esquerda para a direita). */
export function firstFree(list, cols, rows, w, h, skip = []) {
  for (let y = 0; y + h <= rows; y++) {
    for (let x = 0; x + w <= cols; x++) if (fits(list, cols, rows, x, y, w, h, skip)) return { x, y };
  }
  return null;
}

/** Lugar livre mais perto de (px, py); empate fica com o que vem antes na leitura. */
export function nearestFree(list, cols, rows, w, h, px, py, skip = []) {
  let best = null, bd = Infinity;
  for (let y = 0; y + h <= rows; y++) {
    for (let x = 0; x + w <= cols; x++) {
      if (!fits(list, cols, rows, x, y, w, h, skip)) continue;
      const d = (x - px) ** 2 + (y - py) ** 2;
      if (d < bd) { bd = d; best = { x, y }; }
    }
  }
  return best;
}

/** Quantas linhas estão em uso (linhas vazias no fim não ocupam espaço na tela). */
export const usedRows = (list) => list.reduce((m, o) => Math.max(m, o.y + o.h), 0);

/** Ordem de leitura: de cima para baixo, da esquerda para a direita. */
export const readingOrder = (list) => [...list].sort((a, b) => a.y - b.y || a.x - b.x);

/**
 * Encaixa itens novos ({ id, w, h }, na ordem dada) no primeiro lugar livre. O que não cabe volta em
 * `overflow` (a migração e o "não coube" mandam para uma Página nova). Largura maior que a grade vira
 * a largura da grade.
 */
export function place(list, cols, rows, items) {
  const out = [...list], overflow = [];
  for (const it of items) {
    const w = Math.min(it.w, cols), h = Math.min(it.h, rows);
    const p = firstFree(out, cols, rows, w, h);
    if (p) out.push({ ...it, w, h, ...p });
    else overflow.push(it);
  }
  return { list: out, overflow };
}

/**
 * Arrastar (ou mudar o tamanho de) `id` para (x, y) com w × h. Devolve o arranjo novo, ou null quando
 * não dá (a sombra fica vermelha). Lugar livre: só ele muda. Ocupado: quem estava lá vai para o lugar
 * que ele deixou (troca), se couber; senão para o lugar livre mais perto. Os outros não se mexem.
 */
export function dragPlan(list, cols, rows, id, x, y, w, h) {
  const me = list.find((o) => o.id === id);
  if (!me || x < 0 || y < 0 || x + w > cols || y + h > rows) return null;
  const moved = { ...me, x, y, w, h };
  const hit = list.filter((o) => o.id !== id && overlaps(o, moved));
  if (!hit.length) return list.map((o) => (o.id === id ? moved : o));
  // os que ficam parados (nem o que arrasta, nem os atingidos), mais o que arrasta no lugar novo
  const out = [...list.filter((o) => o.id !== id && !hit.includes(o)), moved];
  for (const o of readingOrder(hit)) {
    // troca: o atingido vai para onde estava o que arrasta (alinhado pelo canto de cima à esquerda)
    let p = fits(out, cols, rows, me.x, me.y, o.w, o.h) ? { x: me.x, y: me.y } : null;
    // senão, o lugar livre mais perto de onde ele estava
    if (!p) p = nearestFree(out, cols, rows, o.w, o.h, o.x, o.y);
    if (!p) return null;
    out.push({ ...o, ...p });
  }
  // devolve na mesma ordem da lista original (a ordem guardada não muda à toa)
  const by = new Map(out.map((o) => [o.id, o]));
  return list.map((o) => by.get(o.id));
}

/**
 * Área compacta (tela estreita): os widgets em ordem de leitura, em `cols` colunas, sem limite de
 * linhas; largura maior que `cols` vira `cols`. Não muda o que está guardado.
 */
export function compact(list, cols) {
  const out = [];
  for (const o of readingOrder(list)) {
    const w = Math.min(o.w, cols);
    const p = firstFree(out, cols, 1e4, w, o.h);
    out.push({ ...o, w, ...p });
  }
  return out;
}

// Medidas do Quadro (o mesmo desenho de sempre: 240 × 148, rótulo de 26 embaixo).
export const Q = { w: 240, h: 148, label: 26, gapX: 28, gapY: 18 };

/**
 * Escala do Quadro para a grade caber em espaçoW × espaçoH (px). Entre min e max de largura (px).
 * Devolve { k, w } (k = fator de zoom sobre 240) ou null quando nem no mínimo cabe na largura (aí a
 * Área fica compacta). Sem espaçoH (Infinity), só a largura conta. Altura apertada não deixa o Quadro
 * menor que o mínimo (a tela rola).
 */
export function quadro(spaceW, spaceH, cols, rows, { min = 150, max = 260, gapX = Q.gapX, gapY = Q.gapY } = {}) {
  cols = Math.max(1, cols); rows = Math.max(1, rows);
  // em largura: cols quadros + gaps (os gaps também escalam junto com o zoom)
  const byW = spaceW / (cols + (cols - 1) * (gapX / Q.w));
  if (byW < min) return null;
  const rowH = Q.h + Q.label, byH = spaceH / (rows * (rowH / Q.w) + (rows - 1) * (gapY / Q.w));
  const w = Math.max(min, Math.min(byW, byH, max));
  return { k: w / Q.w, w };
}

/* ── Ícones da Área de apps (0.13.1, decisão 52) ─────────────────────────────────────────────────
   Régua = largura da Área de widgets com 6 colunas naquela tela. O ícone Grande é o maior em que 16
   colunas cabem nela com as bordas dos ícones nas bordas dos widgets: 15 casas + 1 ícone = Régua. Os
   outros Tamanhos são proporções do Grande, com piso em px (no celular quem manda é o piso). A casa
   (largura de cada coluna, com o nome embaixo) é o ícone × CASA. */
export const ICON_SIZES = {
  p: { k: 0.75, floor: 44, name: 'Pequeno' },
  m: { k: 0.88, floor: 52, name: 'Médio' },
  g: { k: 1, floor: 60, name: 'Grande' },
  e: { k: 1.2, floor: 72, name: 'Enorme' },
};
export const CASA = 1.58;
export const ICON_GAPS = { fixo: 'Fixo', elastico: 'Elástico', preencher: 'Preencher' };
const ELASTIC_MAX = 1.6; // o Elástico estica a casa até 1,6 × a mínima

/** Largura da Régua para o zoom k do Quadro. */
export const reguaW = (k) => (6 * Q.w + 5 * Q.gapX) * k;

/** Tamanho do ícone guardado → 'p' | 'm' | 'g' | 'e'. Aceita o número antigo (px do controle 48–88). */
export function iconSizeKey(v) {
  if (typeof v === 'string' && ICON_SIZES[v]) return v;
  const n = Number(v);
  if (!Number.isFinite(n) || n <= 0) return 'g';
  return n < 52 ? 'p' : n < 60 ? 'm' : n < 72 ? 'g' : 'e';
}

export const CASA_CELULAR = 1.3; // no celular a casa é mais justa (≈ 6 / 5 / 4 / 3 colunas em 360 px)

/** Ícone e casa (px) de um Tamanho do ícone para uma Régua. `ratio`: casa ÷ ícone. */
export function iconMetrics(regua, size, ratio = CASA) {
  const t = ICON_SIZES[iconSizeKey(size)];
  const big = regua / (15 * CASA + 1);
  const icon = Math.round(Math.max(t.floor, t.k * big));
  return { icon, casa: Math.round(icon * ratio * 100) / 100 };
}

/**
 * Colunas e largura da Área de apps numa tela.
 * avail: largura disponível (px); target: até onde o Elástico e o Preencher esticam (a Régua, ou a
 * sobra ao lado dos widgets); medida: 'auto' ou [colunas, linhas]; gap: 'fixo' | 'elastico' | 'preencher'.
 * Devolve { cols, casa, extra, width }: casa = distância entre os centros de duas colunas; cada coluna
 * tem a casa mínima de largura e `extra` px de espaço entre elas (o nome embaixo fica sempre da mesma
 * largura); width = (cols − 1) × casa + casa mínima. Quando a Medida não cabe, menos colunas (Área
 * compacta); o ícone nunca encolhe.
 */
export function appCols({ avail, target, icon, casa, medida, gap = 'fixo' }) {
  target = Math.min(target, avail);
  const maxCols = Math.max(1, Math.floor((avail + 0.5) / casa));
  const want = Array.isArray(medida)
    ? Math.max(1, medida[0])
    : Math.min(16, Math.max(1, Math.floor((target - icon + 0.5) / casa) + 1));
  const cols = Math.min(want, maxCols);
  // casa que deixa a borda do primeiro e do último ícone nas bordas do alvo
  const fill = cols > 1 ? (target - icon) / (cols - 1) : casa;
  let c = casa;
  if (gap === 'preencher') c = Math.max(casa, fill);
  else if (gap === 'elastico') c = Math.max(casa, Math.min(fill, casa * ELASTIC_MAX));
  if (cols > 1) c = Math.max(casa, Math.min(c, (avail - casa) / (cols - 1))); // a caixa nunca passa do disponível
  c = Math.floor(c * 100) / 100;
  const extra = Math.round((c - casa) * 100) / 100;
  return { cols, casa: c, extra, width: Math.round(((cols - 1) * c + casa) * 100) / 100 };
}
