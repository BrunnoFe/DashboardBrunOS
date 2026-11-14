// Testes das contas das Janelas (web/static/js/winmath.js, 0.17.0).
// Rodar: node --test tests/windows.test.mjs   (ferramenta de desenvolvimento; não vai para a imagem)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultRect, clampRect, snapZone, resizeRect, pickToClose, dockAction, MIN_W, MIN_H } from '../web/static/js/winmath.js';

test('defaultRect: centralizada, ~80 % da largura, acima da dock', () => {
  const r = defaultRect(1600, 900, 94);
  assert.equal(r.w, 1280);
  assert.equal(r.x, 160);
  assert.ok(r.y >= 12 && r.y + r.h <= 900 - 94, JSON.stringify(r));
});

test('defaultRect: tela pequena não passa da tela', () => {
  const r = defaultRect(400, 500, 80);
  assert.ok(r.w <= 400 && r.x >= 0, JSON.stringify(r));
  assert.ok(r.h >= MIN_H && r.h <= 500, JSON.stringify(r));
});

test('clampRect: barra sempre pegável e tamanho mínimo', () => {
  assert.deepEqual(clampRect({ x: -5000, y: -40, w: 800, h: 600 }, 1600, 900), { x: 120 - 800, y: 0, w: 800, h: 600 });
  assert.deepEqual(clampRect({ x: 5000, y: 5000, w: 800, h: 600 }, 1600, 900), { x: 1600 - 120, y: 900 - 44, w: 800, h: 600 });
  assert.deepEqual(clampRect({ x: 10, y: 10, w: 50, h: 50 }, 1600, 900), { x: 10, y: 10, w: MIN_W, h: MIN_H });
  assert.deepEqual(clampRect({ x: 0, y: 0, w: 4000, h: 4000 }, 1600, 900), { x: 0, y: 0, w: 1600, h: 900 });
});

test('snapZone: topo maximiza, laterais encaixam, meio não faz nada', () => {
  assert.equal(snapZone(800, 0, 1600), 'max');
  assert.equal(snapZone(0, 400, 1600), 'left');
  assert.equal(snapZone(1599, 400, 1600), 'right');
  assert.equal(snapZone(800, 400, 1600), null);
});

test('resizeRect: cada borda mexe no lado certo e respeita o mínimo', () => {
  const s = { x: 100, y: 100, w: 800, h: 600 };
  assert.deepEqual(resizeRect(s, 'se', 50, 30, 1600, 900), { x: 100, y: 100, w: 850, h: 630 });
  assert.deepEqual(resizeRect(s, 'w', 100, 0, 1600, 900), { x: 200, y: 100, w: 700, h: 600 });
  assert.deepEqual(resizeRect(s, 'n', 0, -50, 1600, 900), { x: 100, y: 50, w: 800, h: 650 });
  assert.deepEqual(resizeRect(s, 'nw', 2000, 2000, 1600, 900), { x: 100 + 800 - MIN_W, y: 100 + 600 - MIN_H, w: MIN_W, h: MIN_H });
  assert.deepEqual(resizeRect(s, 'n', 0, -500, 1600, 900), { x: 100, y: 0, w: 800, h: 700 }); // não passa do topo
});

test('pickToClose: minimizada usada há mais tempo; sem minimizada, a do fundo', () => {
  const l = [
    { id: 'a', min: false, z: 3, used: 10 },
    { id: 'b', min: true, z: 1, used: 50 },
    { id: 'c', min: true, z: 2, used: 20 },
  ];
  assert.equal(pickToClose(l), 'c');
  assert.equal(pickToClose(l.map((w) => ({ ...w, min: false }))), 'b');
  assert.equal(pickToClose([]), null);
});

test('dockAction: volta, vem para a frente ou minimiza', () => {
  assert.equal(dockAction({ id: 'a', min: true }, 'b'), 'restore');
  assert.equal(dockAction({ id: 'a', min: false }, 'a'), 'minimize');
  assert.equal(dockAction({ id: 'a', min: false }, 'b'), 'focus');
});
