// Testes das funções de encaixe da Área de widgets (web/static/js/layout.js).
// Rodar: node --test tests/layout.test.mjs   (ferramenta de desenvolvimento; não vai para a imagem)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  fits, firstFree, nearestFree, place, dragPlan, compact, quadro, usedRows, overlaps, reguaW, iconSizeKey, iconMetrics, appCols, CASA,
  KMIN, KMAX, areaUtil, reguaFor, areaSize, appRowsH, rowsFit, fitStack, fitSide,
} from '../web/static/js/layout.js';
const MOB = { mobile: true };

// ── Ícones pela Régua (0.13.1) ──
test('iconSizeKey aceita as letras e converte o número antigo', () => {
  for (const k of ['p', 'm', 'g', 'e']) assert.equal(iconSizeKey(k), k);
  assert.equal(iconSizeKey(48), 'p');
  assert.equal(iconSizeKey(56), 'm');
  assert.equal(iconSizeKey(64), 'g'); // o padrão antigo vira o Grande
  assert.equal(iconSizeKey(88), 'e');
  assert.equal(iconSizeKey(undefined), 'g');
  assert.equal(iconSizeKey('x'), 'g');
});

test('Grande: 16 colunas com as bordas dos ícones nas bordas da Régua', () => {
  const regua = reguaW(1); // 1580 px
  const { icon, casa } = iconMetrics(regua, 'g');
  const span = 15 * casa + icon;
  assert.ok(span <= regua && regua - span < 25, `15 casas + ícone cabe na Régua e quase a enche (${span} × ${regua})`);
  assert.ok(Math.abs(casa / icon - CASA) < 0.01);
});

test('os Tamanhos seguem a ordem e o piso vale no celular', () => {
  const r = reguaW(1);
  const s = ['p', 'm', 'g', 'e'].map((k) => iconMetrics(r, k).icon);
  assert.deepEqual([...s].sort((a, b) => a - b), s);
  // celular: Régua de 358 px → todos no piso
  assert.deepEqual(['p', 'm', 'g', 'e'].map((k) => iconMetrics(358, k, MOB).icon), [44, 52, 60, 72]);
});

test('computador: os ícones acompanham a escala até 60 % (sem piso fixo)', () => {
  const g1 = iconMetrics(reguaW(1), 'g').icon;
  const g7 = iconMetrics(reguaW(0.7), 'g').icon;
  assert.ok(Math.abs(g7 / g1 - 0.7) < 0.03, 'Régua 70 % → ícone ~70 %');
  const tiny = iconMetrics(300, 'g').icon;
  assert.equal(tiny, Math.round(0.6 * g1), 'nunca menos de 60 % do normal');
});

test('Área útil: ~7 % de margem; Régua não passa dela nem do Quadro máximo', () => {
  assert.deepEqual(areaUtil(2540), { margin: 178, width: 2184 });
  assert.equal(areaUtil(390).margin, 16);
  assert.equal(reguaFor(1240), 1240);
  assert.equal(Math.round(reguaFor(5000)), Math.round(reguaW(KMAX)));
  assert.equal(reguaFor(358, true), 358);
});

test('fitStack: o Quadro enche a largura e os apps cabem embaixo', () => {
  // monitor do Bruno: Área útil 2184 × 1100, widgets 6 × 3, 18 apps em 16 colunas
  const f = fitStack({ auW: 2184, auH: 1100, cols: 6, rows: 3, nApps: 18, appCols: 16, icon: 88, gapH: 30 });
  assert.equal(f.appRows, 2);
  const a = areaSize(6, 3);
  assert.ok(a.h * f.k + 30 + appRowsH(2, 88) <= 1100 + 0.5, 'widgets + 2 fileiras cabem');
  assert.ok(f.k <= KMAX && f.k >= KMIN);
});

test('fitStack Encolher: Medida 2 × 6 encolhe os widgets e, se não der, reduz as fileiras', () => {
  const f = fitStack({ auW: 2184, auH: 1100, cols: 6, rows: 3, nApps: 18, appCols: 2, maxRows: 6, icon: 88, gapH: 30 });
  const a = areaSize(6, 3);
  assert.ok(a.h * f.k + 30 + appRowsH(f.appRows, 88) <= 1100 + 0.5, 'nunca passa da Área útil');
  assert.ok(f.appRows >= 1 && f.appRows < 6, `fileiras: ${f.appRows}`);
  assert.ok(a.h * KMIN + 30 + appRowsH(f.appRows + 1, 88) > 1100, 'uma fileira a mais não caberia nem no mínimo');
});

test('fitStack Widgets grandes: os apps ficam com o que sobrar', () => {
  const enc = fitStack({ auW: 2184, auH: 1100, cols: 6, rows: 3, nApps: 40, appCols: 16, maxRows: 4, icon: 88, gapH: 30 });
  const big = fitStack({ auW: 2184, auH: 1100, cols: 6, rows: 3, nApps: 40, appCols: 16, maxRows: 4, icon: 88, gapH: 30, mode: 'widgets' });
  assert.ok(big.k >= enc.k);
  assert.ok(big.appRows <= enc.appRows);
});

test('fitStack: sem fileira que caiba → apps na tela seguinte; estreito → null (compacta)', () => {
  const f = fitStack({ auW: 1240, auH: 300, cols: 6, rows: 3, nApps: 10, appCols: 10, icon: 50, gapH: 20 });
  assert.equal(f.appRows, 0);
  assert.equal(fitStack({ auW: 600, auH: 900, cols: 6, rows: 3 }), null);
});

test('fitSide: sobra largura para os apps; lado a lado impossível → null', () => {
  const s = fitSide({ auW: 2184, auH: 1100, cols: 6, rows: 3, casa: 139 });
  assert.ok(s.k >= KMIN && s.appsW >= 2184 * 0.3 - 1);
  assert.equal(fitSide({ auW: 880, auH: 600, cols: 6, rows: 3, casa: 90 }), null);
});

test('rowsFit e appRowsH conversam', () => {
  for (const icon of [44, 60, 88]) for (let r = 1; r <= 6; r++) assert.equal(rowsFit(appRowsH(r, icon), icon), r);
});

test('o ícone não muda com a Medida; o que muda são as colunas e a largura', () => {
  const regua = 1500, { icon, casa } = iconMetrics(regua, 'g');
  const a = appCols({ avail: 1700, target: regua, icon, casa, medida: [16, 2] });
  const b = appCols({ avail: 1700, target: regua, icon, casa, medida: [8, 2] });
  assert.equal(a.cols, 16); assert.equal(b.cols, 8);
  assert.equal(a.casa, b.casa); // Fixo: mesma casa
  assert.ok(b.width < a.width);
});

test('Medida que não cabe mostra menos colunas, sem encolher', () => {
  const { icon, casa } = iconMetrics(358, 'g', MOB); // celular
  const r = appCols({ avail: 358, target: 358, icon, casa, medida: [16, 2] });
  assert.equal(r.cols, Math.floor(358 / casa));
  assert.ok(r.width <= 358);
  assert.equal(r.casa, Math.floor(casa * 100) / 100);
});

test('celular de 360 px: 6 / 5 / 4 / 3 colunas', () => {
  const cols = ['p', 'm', 'g', 'e'].map((k) => {
    const { icon, casa } = iconMetrics(358, k, MOB);
    return appCols({ avail: 358, target: 358, icon, casa, medida: [16, 2] }).cols;
  });
  assert.deepEqual(cols, [6, 5, 4, 3]);
});

test('Elástico estica até o limite; Preencher vai de ponta a ponta', () => {
  const regua = 1500, { icon, casa } = iconMetrics(regua, 'g');
  const span = (r) => (r.cols - 1) * r.casa + icon; // da borda do 1º ao último ícone
  const p10 = appCols({ avail: 1700, target: regua, icon, casa, medida: [10, 2], gap: 'preencher' });
  assert.ok(Math.abs(span(p10) - regua) < 1);
  const e11 = appCols({ avail: 1700, target: regua, icon, casa, medida: [11, 2], gap: 'elastico' });
  assert.ok(Math.abs(span(e11) - regua) < 1, 'com 11 colunas o Elástico ainda alcança a Régua');
  const e4 = appCols({ avail: 1700, target: regua, icon, casa, medida: [4, 2], gap: 'elastico' });
  assert.ok(e4.casa <= casa * 1.6 + 0.01 && span(e4) < regua, 'com 4 colunas para no limite e centraliza');
  const p4 = appCols({ avail: 1700, target: regua, icon, casa, medida: [4, 2], gap: 'preencher' });
  assert.ok(Math.abs(span(p4) - regua) < 1);
});

test('Automática: quantas couberem na Régua, no máximo 20', () => {
  const regua = 1500, { icon, casa } = iconMetrics(regua, 'g');
  const r = appCols({ avail: 1700, target: regua, icon, casa, medida: 'auto' });
  assert.ok((r.cols - 1) * casa + icon <= regua + 0.5);
  assert.ok(r.cols * casa + icon > regua);
  const big = appCols({ avail: 5000, target: 5000, icon: 44, casa: 70, medida: 'auto' });
  assert.equal(big.cols, 20);
});

const A = (id, x, y, w = 1, h = 1) => ({ id, x, y, w, h });
const at = (list, id) => { const o = list.find((x) => x.id === id); return [o.x, o.y, o.w, o.h]; };
const noOverlap = (list) => list.every((a, i) => list.every((b, j) => i === j || !overlaps(a, b)));

test('fits respeita a grade e os vizinhos', () => {
  const l = [A('a', 0, 0, 2, 1)];
  assert.equal(fits(l, 6, 3, 2, 0, 1, 1), true);
  assert.equal(fits(l, 6, 3, 1, 0, 1, 1), false); // em cima do a
  assert.equal(fits(l, 6, 3, 1, 0, 1, 1, ['a']), true); // ignorando o a
  assert.equal(fits(l, 6, 3, 5, 0, 2, 1), false); // passa da borda
  assert.equal(fits(l, 6, 3, 0, 2, 1, 2), false); // passa da última linha
});

test('firstFree segue a ordem de leitura e acha buraco', () => {
  const l = [A('a', 0, 0), A('b', 2, 0)];
  assert.deepEqual(firstFree(l, 3, 2, 1, 1), { x: 1, y: 0 });
  assert.deepEqual(firstFree(l, 3, 2, 2, 1), { x: 0, y: 1 });
  assert.equal(firstFree([A('a', 0, 0, 3, 2)], 3, 2, 1, 1), null);
});

test('o exemplo do Bruno: widget 2×1 no centro da última linha de uma grade 6×3', () => {
  const l = [A('a', 0, 0, 2, 2), A('b', 2, 0, 1, 2), A('c', 3, 0, 2, 1), A('d', 5, 0), A('e', 3, 1, 3, 1), A('f', 0, 2, 2, 1)];
  const r = dragPlan(l, 6, 3, 'f', 2, 2, 2, 1);
  assert.deepEqual(at(r, 'f'), [2, 2, 2, 1]);
  assert.ok(noOverlap(r));
});

test('soltar no fim de uma linha (antes não dava)', () => {
  const l = [A('a', 0, 0), A('b', 1, 0), A('c', 2, 0)];
  const r = dragPlan(l, 4, 1, 'a', 3, 0, 1, 1);
  assert.deepEqual(at(r, 'a'), [3, 0, 1, 1]);
  assert.deepEqual(at(r, 'b'), [1, 0, 1, 1]); // os outros não se mexem
});

test('lugar ocupado: troca com quem estava lá', () => {
  const l = [A('a', 0, 0), A('b', 1, 0), A('c', 2, 0)];
  const r = dragPlan(l, 3, 1, 'a', 2, 0, 1, 1);
  assert.deepEqual(at(r, 'a'), [2, 0, 1, 1]);
  assert.deepEqual(at(r, 'c'), [0, 0, 1, 1]);
  assert.deepEqual(at(r, 'b'), [1, 0, 1, 1]);
});

test('troca impossível: vai para o lugar livre mais perto', () => {
  // a (1×1) vai para cima do b (2×1); o b não cabe no lugar do a (borda), vai para a linha de baixo
  const l = [A('a', 0, 0), A('b', 1, 0, 2, 1)];
  const r = dragPlan(l, 3, 2, 'a', 1, 0, 1, 1);
  assert.deepEqual(at(r, 'a'), [1, 0, 1, 1]);
  assert.ok(noOverlap(r));
  assert.equal(at(r, 'b')[1], 1);
});

test('sem espaço para ninguém: null (sombra vermelha)', () => {
  const l = [A('a', 0, 0), A('b', 1, 0, 2, 1)];
  assert.equal(dragPlan(l, 3, 1, 'a', 1, 0, 1, 1), null);
  assert.equal(dragPlan(l, 3, 1, 'a', 3, 0, 1, 1), null); // fora da grade
});

test('mudar o tamanho empurra o vizinho para um lugar livre', () => {
  const l = [A('a', 0, 0), A('b', 1, 0), A('c', 2, 0)];
  const r = dragPlan(l, 3, 2, 'a', 0, 0, 2, 1); // a vira Largo em cima do b
  assert.deepEqual(at(r, 'a'), [0, 0, 2, 1]);
  assert.ok(noOverlap(r));
  assert.equal(r.length, 3);
});

test('a ordem guardada não muda à toa', () => {
  const l = [A('a', 0, 0), A('b', 1, 0), A('c', 2, 0)];
  assert.deepEqual(dragPlan(l, 4, 1, 'b', 3, 0, 1, 1).map((o) => o.id), ['a', 'b', 'c']);
});

test('place: migração na ordem de hoje e o que não cabe vai para o overflow', () => {
  const items = [{ id: 'a', w: 2, h: 2 }, { id: 'b', w: 1, h: 1 }, { id: 'c', w: 2, h: 1 }, { id: 'd', w: 9, h: 1 }];
  const { list, overflow } = place([], 3, 2, items);
  assert.deepEqual(at(list, 'a'), [0, 0, 2, 2]);
  assert.deepEqual(at(list, 'b'), [2, 0, 1, 1]);
  assert.deepEqual(overflow.map((o) => o.id), ['c', 'd']); // não sobrou 2×1 nem 3×1 livre
  const big = place([], 3, 3, [{ id: 'f', w: 6, h: 1 }]); // Faixa antiga: largura da grade
  assert.deepEqual(at(big.list, 'f'), [0, 0, 3, 1]);
});

test('compact: ordem de leitura em 2 colunas, largura limitada', () => {
  const l = [A('c', 3, 0, 3, 1), A('a', 0, 0, 2, 1), A('b', 2, 0), A('d', 0, 1, 1, 2)];
  const r = compact(l, 2);
  assert.deepEqual(r.map((o) => o.id), ['a', 'b', 'c', 'd']);
  assert.deepEqual(at(r, 'a'), [0, 0, 2, 1]);
  assert.deepEqual(at(r, 'b'), [0, 1, 1, 1]);
  assert.deepEqual(at(r, 'c'), [0, 2, 2, 1]);
  assert.ok(noOverlap(r));
});

test('usedRows ignora as linhas vazias do fim', () => {
  assert.equal(usedRows([A('a', 0, 0), A('b', 0, 1, 1, 2)]), 3);
  assert.equal(usedRows([]), 0);
});

test('nearestFree escolhe o mais perto', () => {
  const l = [A('a', 0, 0), A('b', 1, 0)];
  assert.deepEqual(nearestFree(l, 3, 2, 1, 1, 0, 0), { x: 0, y: 1 });
});

test('quadro: escala dentro do mínimo e do máximo, null quando não cabe', () => {
  const big = quadro(4000, Infinity, 3, 1);
  assert.equal(big.w, 260); // não passa do máximo
  const mid = quadro(1000, Infinity, 4, 1);
  assert.ok(mid.w > 150 && mid.w < 260);
  assert.equal(quadro(500, Infinity, 6, 1), null); // celular: fica compacta
  assert.ok(Math.abs(quadro(4000, 400, 3, 3).w - 172) < 1); // a altura manda: 3 linhas em 400 px
  assert.equal(quadro(4000, 300, 3, 3).w, 150); // altura apertada não passa do mínimo (a tela rola)
});
