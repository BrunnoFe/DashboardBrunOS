// Gera ../../web/static/vendor/codemirror/codemirror.mjs a partir de entry.mjs.
// Uso (uma vez, só quando for atualizar o CodeMirror): npm ci && npm run build
import { build } from 'esbuild';
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';

const out = new URL('../../web/static/vendor/codemirror/', import.meta.url);
mkdirSync(out, { recursive: true });
await build({
  entryPoints: ['entry.mjs'],
  bundle: true,
  format: 'esm',
  minify: true,
  target: 'es2020',
  legalComments: 'none',
  outfile: new URL('codemirror.mjs', out).pathname.replace(/^\/([A-Za-z]:)/, '$1'),
});
copyFileSync('node_modules/@codemirror/view/LICENSE', new URL('LICENSE', out));
const pkg = JSON.parse(readFileSync('package.json', 'utf8'));
const list = Object.entries(pkg.dependencies).filter(([n]) => n !== 'esbuild').map(([n, v]) => `${n} ${v}`).join('\n');
writeFileSync(new URL('README.txt', out), `CodeMirror 6 (licença MIT, ver LICENSE), empacotado num arquivo só por tools/codemirror (npm ci && npm run build).\nhttps://codemirror.net\n\n${list}\n`);
console.log('ok');
