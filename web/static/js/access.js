// Formas de acesso de um app do Runtipi (0.26.0, decisão 65; termos em CONTEXT.md).
// Duas formas: Domínio (https://app.server.lan, pelo Traefik) e Porta (o nome do servidor + a porta do
// app). No Automático o app segue a forma usada para abrir o BrunOS e troca para a outra se a escolhida
// não estiver funcionando. Funções puras (sem DOM), testadas em tests/access.test.mjs.

const isIP = (h) => /^\d{1,3}(\.\d{1,3}){3}$/.test(h) || h.includes(':') || h.startsWith('[');

// Forma usada para abrir o BrunOS: sem porta no endereço e com um nome (brunos.server.lan) = Domínio;
// IP:porta ou server.lan:porta = Porta.
export function brunosForm(loc) {
  const h = loc.hostname || '';
  return !loc.port && h && h !== 'localhost' && !isIP(h) ? 'domain' : 'port';
}

// Nome do servidor na Forma Porta. Configurado à mão ganha; senão, o mesmo nome usado para abrir o
// BrunOS. Aberto pelo Domínio (brunos.server.lan), usa o nome do servidor (server.lan) ou o IP.
export function portHost({ serverHost, loc, localDomain, internalIp }) {
  if (serverHost) return serverHost;
  const h = loc.hostname || '';
  if (brunosForm(loc) === 'port') return h || internalIp || 'localhost';
  if (localDomain && h.endsWith(`.${localDomain}`)) return localDomain;
  return internalIp || h;
}

// Endereço de cada forma ('' quando o app não tem aquela forma).
export function accessUrls(a, host) {
  const domain = a.exposedDomain ? `https://${a.exposedDomain}` : a.localDomain ? `${a.protocol === 'http' ? 'http' : 'https'}://${a.localDomain}` : '';
  return { domain, port: a.port ? `http://${host}:${a.port}` : '' };
}

// Situação de cada forma: 'up' (respondeu no último teste), 'down' (não respondeu) ou '' (sem teste).
// Domínio: teste do Traefik a cada ~10 min; Porta: o vigia de apps, a cada minuto.
export function formStatus(a, form) {
  const v = form === 'domain' ? a.domain : a.health;
  return v === 'up' || v === 'down' ? v : '';
}

// Qual forma usar. mode: 'domain' | 'port' (escolhida, vale se o app tiver) ou 'auto'/'' (Automático:
// a do BrunOS, trocando para a outra se ela não respondeu e a outra sim).
export function pickForm(a, urls, mode, preferred) {
  const other = (f) => (f === 'domain' ? 'port' : 'domain');
  if (mode === 'domain' || mode === 'port') return urls[mode] ? mode : other(mode);
  const first = urls[preferred] ? preferred : other(preferred);
  const second = other(first);
  if (urls[second] && formStatus(a, first) === 'down' && formStatus(a, second) !== 'down') return second;
  return first;
}
