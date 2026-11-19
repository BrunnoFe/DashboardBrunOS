// Formas de acesso (0.26.0): o Automático segue o endereço usado para abrir o BrunOS.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { brunosForm, portHost, accessUrls, formStatus, pickForm } from '../web/static/js/access.js';

const L = (hostname, port = '') => ({ hostname, port });
const app = (extra = {}) => ({ port: 8384, localDomain: 'syncthing.server.lan', protocol: 'https', ...extra });

test('forma usada para abrir o BrunOS', () => {
  assert.equal(brunosForm(L('brunos.server.lan')), 'domain');
  assert.equal(brunosForm(L('brunos.server.lan', '443')), 'port'); // porta escrita no endereço
  assert.equal(brunosForm(L('server.lan', '8090')), 'port');
  assert.equal(brunosForm(L('192.168.1.50', '8090')), 'port');
  assert.equal(brunosForm(L('192.168.1.50')), 'port'); // IP sem porta também é Porta
  assert.equal(brunosForm(L('localhost')), 'port');
});

test('nome do servidor na forma Porta', () => {
  const base = { localDomain: 'server.lan', internalIp: '192.168.1.50' };
  assert.equal(portHost({ ...base, loc: L('server.lan', '8090') }), 'server.lan');
  assert.equal(portHost({ ...base, loc: L('192.168.1.50', '8090') }), '192.168.1.50');
  assert.equal(portHost({ ...base, loc: L('brunos.server.lan') }), 'server.lan');
  assert.equal(portHost({ ...base, loc: L('painel.outro.net') }), '192.168.1.50');
  assert.equal(portHost({ ...base, serverHost: '10.0.0.2', loc: L('brunos.server.lan') }), '10.0.0.2');
});

test('endereços de cada forma', () => {
  assert.deepEqual(accessUrls(app(), 'server.lan'), { domain: 'https://syncthing.server.lan', port: 'http://server.lan:8384' });
  assert.deepEqual(accessUrls(app({ exposedDomain: 'sync.meudominio.com' }), 'x'), { domain: 'https://sync.meudominio.com', port: 'http://x:8384' });
  assert.deepEqual(accessUrls(app({ localDomain: '', port: 0 }), 'x'), { domain: '', port: '' });
});

test('situação de cada forma', () => {
  assert.equal(formStatus(app({ domain: 'up', health: 'down' }), 'domain'), 'up');
  assert.equal(formStatus(app({ domain: 'up', health: 'down' }), 'port'), 'down');
  assert.equal(formStatus(app({ health: 'unknown' }), 'port'), '');
});

test('Automático segue a forma do BrunOS e troca quando ela não responde', () => {
  const urls = { domain: 'd', port: 'p' };
  assert.equal(pickForm(app(), urls, 'auto', 'domain'), 'domain');
  assert.equal(pickForm(app(), urls, 'auto', 'port'), 'port');
  assert.equal(pickForm(app({ domain: 'down', health: 'up' }), urls, 'auto', 'domain'), 'port');
  assert.equal(pickForm(app({ domain: 'down' }), urls, 'auto', 'domain'), 'port'); // porta sem teste: tenta
  assert.equal(pickForm(app({ health: 'down', domain: 'up' }), urls, 'auto', 'port'), 'domain');
  assert.equal(pickForm(app({ health: 'down', domain: 'down' }), urls, 'auto', 'port'), 'port'); // as duas caídas: fica
  assert.equal(pickForm(app(), { domain: '', port: 'p' }, 'auto', 'domain'), 'port'); // sem domínio
  assert.equal(pickForm(app(), { domain: 'd', port: '' }, 'auto', 'port'), 'domain'); // sem porta
});

test('forma escolhida à mão vale (se o app tiver)', () => {
  const urls = { domain: 'd', port: 'p' };
  assert.equal(pickForm(app({ domain: 'down' }), urls, 'domain', 'port'), 'domain');
  assert.equal(pickForm(app(), urls, 'port', 'domain'), 'port');
  assert.equal(pickForm(app(), { domain: '', port: 'p' }, 'domain', 'domain'), 'port');
});
