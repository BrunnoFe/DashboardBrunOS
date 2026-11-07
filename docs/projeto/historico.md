# Histórico de versões e problemas resolvidos

## v0 (protótipo, 11/2026)
- Interface estática com tema tirado do papel de parede, inspirada no código e num vídeo do umbrelOS.
- Entregue como zip e como artefato de demonstração.

## 0.1.0 (07/11/2026): primeira versão no Runtipi
- Backend Go (auth, sessões, settings, wallpapers, stats SSE, apps do Runtipi, Docker via proxy,
  gerenciador de arquivos com lixeira), interface completa, CI com teste de fumaça em Docker real,
  imagem no GHCR, app na loja (PR #1).
- O Bruno confirmou: **"Funcionou no Runtipi!"**

Problemas encontrados no caminho:
- Fuga por link simbólico em upload/colar (gravava fora da raiz) → `Root.realDir()` nas escritas.
- CI: lista de discos vazia no runner (raiz aparece como `/dev/root`) → nome do disco pelo `st_dev`.
- Logo parecia uma carinha triste → redesenhado (órbita com lua).
- Widgets claros demais e texto vazando; dock largo demais no celular (tamanho dinâmico); app sem ícone
  virava quadrado branco (agora gradiente da cor de destaque); painel de envios persistente (some
  sozinho); partições minúsculas listadas (filtro de 1 GiB).
- O ambiente da nuvem não cria tags no GitHub → publicação pelo arquivo `VERSION`.

## 0.2.0 (07/11/2026): ajustes pedidos pelo Bruno depois de usar
Pedidos (com prints):
1. Bolinha de status cortada pela metade → inteira, sem anel, sombra leve (o `overflow: hidden` dos
   ícones com logo cortava).
2. "X" de fechar da janela Arquivos desalinhado → mesma altura dos botões da barra (classe `sheet-files`).
3. Pastas de `/mnt` sem disco apareciam → seção **Discos** com "Mapear", recolhível, salva no servidor.
4. **Widget de bateria** (porcentagem, autonomia, Wh, ícone com cor por nível, tomada) + janela com
   histórico, saúde, ciclos.
5. Apps no dock → lista suspensa com caixas de seleção e busca.
6. (Terminal → feito na 0.3.0.)
7. Ícone da **loja de apps do Runtipi** no dock.
8. Descrição da cor de destaque no modo manual quebrava palavra por palavra → linhas de configuração
   quebram os controles para baixo.

## 0.3.0 (07/11/2026): terminal SSH
- Janela Terminal com xterm.js, SSH com senha ou chave, TOFU, sessões com SSE, fechamento automático.
- `golang.org/x/crypto` vendorizado e conferido no CI.
- Testes unitários com servidor SSH em processo e teste de fumaça com OpenSSH real.
- Problemas: a biblioteca preferia a chave ECDSA do servidor (impressão digital diferente da que o
  usuário costuma ver) → ordem do OpenSSH (ED25519 primeiro). Senha errada no OpenSSH real virava erro
  genérico → erro depois da chave aceita = recusa de login.
- Medido no CI: 9,7 MiB de RAM, imagem de 4 MB, 63 checagens da API.

## Próximas
Veja `roadmap.md`.
