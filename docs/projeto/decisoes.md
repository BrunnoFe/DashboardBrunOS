# Decisões técnicas (e por quê)

Formato: decisão, motivo, consequência. Ao mudar alguma, registre aqui com a data.

1. **Painel próprio em vez de Homarr + CSS** (11/2026). O Homarr é pesado e o visual do umbrelOS exige
   controle total de layout/animações. → Projeto próprio, leve, fiel ao umbrelOS.
2. **Go só com stdlib no backend.** Binário estático pequeno, pouca RAM, sem runtime. → Imagem `scratch`
   de ~4 MB, ~10 MiB de RAM. Exceção: `golang.org/x/crypto/ssh` para o terminal (decisão 10).
3. **Frontend sem framework e sem build.** Menos peso, nada a compilar, embutido no binário. →
   Módulos ES, CSS puro, ícones SVG inline. Bibliotecas grandes só sob demanda (xterm.js).
4. **Senhas em PBKDF2-SHA256 (600 mil iterações) do pacote `crypto/pbkdf2`.** Não exige dependência
   externa (bcrypt/argon2 estão no x/crypto). Detalhes em `docs/privado/seguranca.md`.
5. **Docker por socket-proxy (`wollomatic/socket-proxy:1`) com lista de rotas.** O socket do Docker dá
   root no host; o proxy só libera listar containers, stats e reiniciar, e só para o container
   `brunos`. Reiniciar só se `BRUNOS_DOCKER_ACTIONS=true`.
6. **Arquivos com `os.Root`** (Go 1.24) para leituras; escritas validam o caminho real (`realDir`).
   Motivo: bloquear fuga por `..` e links simbólicos. O Go 1.24 não tem `Root.Rename`, por isso a
   validação manual.
7. **Discos mapeados pelo usuário** (0.2.0). O Bruno tem pastas em `/mnt` sem disco montado. → Seção
   Discos com "Mapear"; sem configuração, mostra só pontos de montagem reais.
8. **SSE em vez de WebSocket** (estatísticas e terminal). Stdlib pura, passa pelo Traefik sem
   configuração, reconecta sozinho. Teclas do terminal vão por POST com lote de 8 ms.
9. **Publicação pela versão do arquivo `VERSION`** (0.1.0). O ambiente do Claude na nuvem não consegue
   criar tags no GitHub (o proxy bloqueia). → CI publica quando `VERSION` traz versão que não existe no
   GHCR. Tags `v*` criadas pelo Bruno também publicam.
10. **Terminal por SSH, embutido** (0.3.0; o Bruno escolheu entre: SSH embutido, atalho para o Sshwifty,
    shell root no host). Mantém o container sem privilégios. TOFU da chave do servidor, nada de segredo
    gravado. Usa `golang.org/x/crypto` **vendorizado** (o proxy do Go está bloqueado no ambiente da
    nuvem; o código veio do espelho oficial `github.com/golang/crypto` e o CI compara com o sum.golang.org).
11. **Ações privilegiadas futuras via SSH com chave restrita** (planejado). Desligamento seguro, SMART,
    limite de carga: uma chave com `command="..."` no `authorized_keys` que só executa o necessário.
    Nunca dar `privileged` ao container.
12. **Detalhes de segurança fora do README** (pedido do Bruno, commit `1713d35`). Ficam em
    `docs/privado/seguranca.md`, fora do git.
13. **Modo demonstração no frontend.** Sem backend, `api.js` simula tudo. Serve para testar visual com
    Playwright e para prévias.
14. **Configurações no servidor** (`/api/settings`), não no `localStorage`: iguais em todos os
    dispositivos. Papel de parede "local" (arrastado sem enviar) é a única exceção.
15. **Formato da loja: `docker-compose.json` dinâmico do Runtipi** validado pelo `@runtipi/common` (zod).
    Pegadinhas em `runtipi-e-loja.md`.
