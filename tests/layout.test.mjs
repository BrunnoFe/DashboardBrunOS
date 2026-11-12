// Testes das funções de encaixe da Área de widgets (web/static/js/layout.js).
// Rodar: node --test tests/layout.test.mjs   (ferramenta de desenvolvimento; não vai para a imagem)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fits, firstFree, nearestFree, place, dragPlan, compact, quadro, usedRows, overlaps } from '../web/static/js/layout.js';

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
