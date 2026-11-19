// Testes das proteções do frontend (auditoria de 19/11/2026): valores de CSS e endereços vindos das
// preferências e o modo demonstração só onde não há servidor de verdade.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cssVal, safeUrl, demoAllowed } from '../web/static/js/safe.js';

test('cssVal deixa passar cor, matiz, var() e gradiente', () => {
  for (const v of ['#2a2a30', 210, '210', 'var(--brand)', 'linear-gradient(160deg, var(--brand-lightest), var(--brand))', 'hsl(210, 80%, 50%)']) {
    assert.equal(cssVal(v), String(v).trim(), String(v));
  }
});

test('cssVal recusa o que fecharia o atributo style ou buscaria algo de fora', () => {
  for (const v of ['red"><iframe src=https://example.org>', 'red;background:url(https://x)', "red' onmouseover='x", 'url(https://example.org/a.png)', 'expression(alert(1))', '<b>', null, undefined, {}]) {
    assert.equal(cssVal(v), '', String(v));
  }
});

test('safeUrl recusa javascript:, data:, vbscript:, blob: e file:', () => {
  for (const u of ['javascript:alert(1)', 'JavaScript://%0Aalert(1)', ' java\tscript:alert(1)', 'data:text/html,<script>', 'vbscript:x', 'blob:https://x/1', 'file:///etc/passwd']) {
    assert.equal(safeUrl(u), '', u);
  }
});

test('safeUrl deixa passar http, https e outros endereços comuns de atalho', () => {
  for (const u of ['http://192.168.1.1', 'https://app.server.lan/x?y=1', 'smb://nas/fotos', 'ftp://nas', '/api/x']) {
    assert.equal(safeUrl(u), u, u);
  }
});

const L = (href) => { const u = new URL(href); return { protocol: u.protocol, hostname: u.hostname, port: u.port, search: u.search }; };

test('demonstração só no Pages, arquivo local, porta de desenvolvimento ou ?demo', () => {
  assert.equal(demoAllowed(L('https://brunnofe.github.io/DashboardBrunOS/')), true);
  assert.equal(demoAllowed(L('file:///C:/x/index.html')), true);
  assert.equal(demoAllowed(L('http://localhost:8765/')), true);
  assert.equal(demoAllowed(L('http://192.168.1.50:8790/?demo')), true);
  assert.equal(demoAllowed(L('http://192.168.1.50:8790/')), false);
  assert.equal(demoAllowed(L('https://brunos.server.lan/')), false);
  assert.equal(demoAllowed(L('https://evilgithub.io/')), false);
});
