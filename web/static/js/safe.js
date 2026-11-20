// Proteções puras do frontend (auditoria de 19/11/2026), sem DOM: testadas em tests/security.test.mjs.

// cssVal: valor de cor/fundo vindo das preferências, só se for um valor CSS inofensivo (cor, número,
// var(), gradiente). Aspas, ";", "<" ou url() fechariam o atributo style ou buscariam algo de fora: vira "".
export const cssVal = (v) => {
  const s = typeof v === 'number' ? String(v) : typeof v === 'string' ? v.trim() : '';
  return s && s.length <= 200 && /^[#a-z0-9(),.%\s+-]+$/i.test(s) && !/url\s*\(|expression/i.test(s) ? s : '';
};

// safeUrl: endereço que dá para abrir (atalhos, apps). Recusa javascript:, data:, vbscript:, blob: e
// file: (mesmo com espaços ou caracteres invisíveis no meio); o resto (http, https, smb, ftp...) passa.
export const safeUrl = (u) => {
  const s = String(u ?? '').trim();
  const scheme = s.replace(/[\s\u0000-\u001f\u007f-\u009f]/g, '').match(/^([a-z][a-z0-9+.-]*):/i)?.[1]?.toLowerCase();
  return scheme && ['javascript', 'data', 'vbscript', 'blob', 'file'].includes(scheme) ? '' : s;
};

// demoAllowed: a demonstração (dados de mentira) só onde não existe servidor de verdade: GitHub Pages,
// arquivo local, servidor de desenvolvimento (python3 -m http.server 8765) ou ?demo no endereço.
export const demoAllowed = (loc) => loc.protocol === 'file:' || /(^|\.)github\.io$/i.test(loc.hostname) ||
  loc.port === '8765' || new URLSearchParams(loc.search).has('demo');

// withKey: acrescenta o código da aba (?k=) a um endereço da API que o navegador busca sozinho
// (imagem, download, EventSource), onde não dá para mandar cabeçalho. Sem código, o endereço fica igual.
export const withKey = (url, key) => (key ? `${url}${url.includes('?') ? '&' : '?'}k=${encodeURIComponent(key)}` : url);
