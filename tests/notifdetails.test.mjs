// Detalhes dos avisos importantes (0.25.0): cada tipo de aviso de nível Aviso/Importante que o servidor
// emite tem explicação própria; os informativos não abrem.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { canExpand, notifDetail, KNOWN_KINDS } from '../web/static/js/notifdetails.js';

// procura no Go todos os Kind de avisos com nível Aviso ou Importante
function goFiles(dir, out = []) {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f);
    if (f === 'vendor' || f === 'node_modules' || f.startsWith('.')) continue;
    if (statSync(p).isDirectory()) goFiles(p, out);
    else if (f.endsWith('.go') && !f.endsWith('_test.go')) out.push(p);
  }
  return out;
}

test('todo aviso importante do servidor tem explicação própria', () => {
  const kinds = new Set();
  for (const f of goFiles(new URL('..', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1'))) {
    for (const line of readFileSync(f, 'utf8').split('\n')) {
      if (/\.add\(Event\{/.test(line)) continue; // linha do tempo da janela Discos, não é aviso do sino
      const m = line.match(/Kind:\s*"([a-z-]+)",\s*Level:\s*(?:events\.(Aviso|Critico)|"(aviso|critico)")/);
      if (m) kinds.add(m[1]);
    }
  }
  kinds.add('power-manual');
  kinds.add('update-done'); // nível muda para Aviso quando falha (updates.go)
  assert.ok(kinds.size >= 15, `achei só ${kinds.size} tipos: ${[...kinds]}`);
  for (const k of kinds) assert.ok(KNOWN_KINDS.includes(k), `aviso "${k}" sem explicação em notifdetails.js`);
});

test('só Aviso e Importante abrem', () => {
  assert.equal(canExpand({ level: 'info' }), false);
  assert.equal(notifDetail({ level: 'info', kind: 'app-up', time: 1 }), null);
  assert.equal(canExpand({ level: 'aviso' }), true);
  assert.equal(canExpand({ level: 'critico' }), true);
});

test('dados e botão de atalho', () => {
  const app = notifDetail({ level: 'aviso', kind: 'app-down', time: 1_800_000_000, meta: { urn: 'immich:migrated' } });
  assert.deepEqual(app.action, { label: 'Ver detalhes do app', act: 'app' });
  assert.deepEqual(app.facts[0], ['App', 'immich']);
  // sem urn não há como abrir o app
  assert.equal(notifDetail({ level: 'aviso', kind: 'app-down', time: 1 }).action, null);
  // pedido de desligar: Cancelar só enquanto o prazo não passou
  const now = 1_800_000_000;
  const pm = (deadline) => notifDetail({ level: 'critico', kind: 'power-manual', time: now, meta: { deadline, action: 'reiniciar' } }, now);
  assert.equal(pm(now + 30).action.act, 'cancel-power');
  assert.equal(pm(now - 5).action, null);
  // campos internos (prazo, ação) não aparecem na lista
  assert.ok(!pm(now + 30).facts.some(([k]) => /prazo|deadline|action/i.test(k)));
  // tipo desconhecido ainda abre, com texto genérico
  assert.ok(notifDetail({ level: 'critico', kind: 'novo-tipo', time: 1 }).why);
  const login = notifDetail({ level: 'aviso', kind: 'login-new', time: 1, meta: { ip: '192.168.1.20', browser: 'Firefox', os: 'Linux', local: true } });
  assert.deepEqual(login.facts.slice(0, 4), [['Endereço', '192.168.1.20'], ['Navegador', 'Firefox'], ['Sistema', 'Linux'], ['Rede', 'de casa']]);
});
