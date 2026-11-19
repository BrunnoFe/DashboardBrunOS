// Campo "Catálogo" do seletor de ícone (0.24.0): nome ou link viram o nome do ícone.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseIconInput, cdnIcon } from '../web/static/js/icons.js';

test('aceita o nome do ícone', () => {
  assert.deepEqual(parseIconInput('router'), { slug: 'router' });
  assert.deepEqual(parseIconInput('  Home Assistant '), { slug: 'home-assistant' });
  assert.deepEqual(parseIconInput('home_assistant'), { slug: 'home-assistant' });
  assert.deepEqual(parseIconInput('router.svg'), { slug: 'router' });
});

test('aceita links do dashboard-icons (o caso do print do Bruno)', () => {
  const ok = {
    'https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/router.svg': 'router',
    'https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@main/png/home-assistant.png': 'home-assistant',
    'cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/jellyfin.webp?v=2': 'jellyfin',
    'https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main/svg/plex.svg': 'plex',
    'https://github.com/homarr-labs/dashboard-icons/blob/main/png/syncthing.png': 'syncthing',
    'https://dashboardicons.com/icons/router': 'router',
    'https://dashboardicons.com/?q=immich': 'immich',
  };
  for (const [url, slug] of Object.entries(ok)) assert.deepEqual(parseIconInput(url), { slug }, url);
});

test('recusa o que não dá para usar, com mensagem', () => {
  for (const bad of ['', '   ', 'https://exemplo.com/logo.png', 'www.site.com/x.svg', 'a/b', '-x', 'ícone!']) {
    const r = parseIconInput(bad);
    assert.ok(r.error && !r.slug, `${JSON.stringify(bad)} deveria dar erro`);
  }
  assert.match(parseIconInput('https://exemplo.com/logo.png').error, /"Enviar"/);
});

test('endereço reserva no CDN', () => {
  assert.equal(cdnIcon('router'), 'https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/router.webp');
});
