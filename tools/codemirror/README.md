# Pacote do CodeMirror 6

O editor de texto do gerenciador de arquivos (`web/static/js/editor.js`) usa o CodeMirror 6. O
frontend do BrunOS não tem etapa de build, então o CodeMirror fica **pronto, num arquivo só**, em
`web/static/vendor/codemirror/codemirror.mjs` (commitado, como o xterm.js), e só é baixado pelo
navegador quando alguém abre o editor.

Esta pasta é só a receita para refazer esse arquivo (ao atualizar o CodeMirror):

```bash
cd tools/codemirror
npm ci            # versões fixas no package.json / package-lock.json
npm run build     # escreve web/static/vendor/codemirror/{codemirror.mjs,LICENSE,README.txt}
```

`entry.mjs` escolhe as linguagens com cores (markdown, json, yaml, js/ts, python, html, css, xml,
shell, toml, ini/env/conf, Dockerfile, go, nginx, diff) e o tema. Cada linguagem a mais aumenta o
arquivo; o núcleo sozinho tem ~350 KB.
