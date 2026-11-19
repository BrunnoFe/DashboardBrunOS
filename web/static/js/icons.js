// Ícones dos apps (0.24.0): o que o campo "Catálogo" aceita e o endereço reserva no CDN.
// Sem DOM: os testes (tests/icons.test.mjs) importam este arquivo direto.

// Tipos aceitos no "Enviar arquivo" (o servidor confere o conteúdo de novo).
export const ICON_ACCEPT = '.svg,.png,.ico,.jpg,.jpeg,.webp,.gif';
export const ICON_MAX = 1 << 20; // 1 MB, igual ao servidor

const SLUG = /^[a-z0-9](?:[a-z0-9-]{0,78}[a-z0-9])?$/;

// Endereço do ícone direto no jsDelivr: reserva para quando o servidor não consegue baixar
// (e o que o modo demonstração usa).
export const cdnIcon = (slug) => `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/${encodeURIComponent(slug)}.webp`;

// Lê o que a pessoa digitou ou colou: o nome ("router", "Home Assistant") ou um link do
// dashboard-icons (jsDelivr, GitHub, dashboardicons.com). Devolve { slug } ou { error } em pt-BR.
export function parseIconInput(text) {
  const t = String(text || '').trim().toLowerCase();
  if (!t) return { error: 'Digite o nome do ícone ou cole o link.' };
  // jsDelivr (…/dashboard-icons/svg/router.svg, com ou sem @versão) e GitHub (…/dashboard-icons/…/png/router.png)
  let m = t.match(/dashboard-icons(?:@[^/]+)?\/(?:.+\/)?(?:svg|png|webp)\/([a-z0-9-]+)\.(?:svg|png|webp)(?:[?#].*)?$/);
  if (m) return { slug: m[1] };
  // site: dashboardicons.com/icons/router ou ?icon=router
  m = t.match(/dashboardicons\.com\/(?:icons?\/([a-z0-9-]+)|.*[?&](?:icon|q|search)=([a-z0-9-]+))/);
  if (m) return { slug: m[1] || m[2] };
  if (/^[a-z][a-z0-9+.-]*:\/\/|^www\.|\//.test(t)) {
    return { error: 'Esse link não é do dashboardicons.com. Para outra imagem, baixe o arquivo e use "Enviar".' };
  }
  const slug = t.replace(/\.(svg|png|webp)$/, '').replace(/[\s_]+/g, '-');
  if (!SLUG.test(slug)) return { error: 'Use só letras, números e hífen (ex.: home-assistant).' };
  return { slug };
}
