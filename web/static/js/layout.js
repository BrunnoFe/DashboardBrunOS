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
