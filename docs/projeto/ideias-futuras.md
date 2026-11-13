# Ideias para o futuro

Tudo o que o Bruno aprovou ou sugeriu mas **não foi feito**. O que já foi entregue está em
`historico.md`; o que está combinado para as próximas versões, em `roadmap.md`.
Ao retomar uma ideia daqui, pergunte as decisões em aberto com opções antes de implementar.

## Anotadas na etapa 11 (não aprovadas)

- Formatar uma partição só e editor de partições (criar, apagar, redimensionar).
- "Dormir após X minutos parado" (`hdparm -S`): ganho pequeno e alguns adaptadores USB ignoram.
- Integração com o Beszel como Fonte de Métricas em widgets (não como lugar dos Históricos).

Regra geral das ideias que mexem no servidor: nada de privilégios no container; o que precisa de root vai
por SSH com chave restrita (ver `docs/privado/seguranca.md`).

## Segurança

| Ideia | Por que ficou para depois | Notas técnicas |
|---|---|---|
| **H1 Verificação em dois passos (TOTP)** | Etapa 3 adiada em 08/11/2026: "Gostaria de deixar a etapa 3 para o futuro" | RFC 6238 com a biblioteca padrão (HMAC-SHA1); QR code gerado em JS ou Go sem dependência; códigos de recuperação. |
| **H2 Sessões ativas com "encerrar"** | Mesma etapa 3 | `sessions.json` já guarda IP, navegador e criação; falta listar e revogar. |
| **F3 Link público de compartilhamento** | Vai junto com a etapa 3: abre arquivos para fora de casa | Desligado por padrão; token longo e aleatório, `/s/<token>` só leitura, expiração, contador, revogar. |
| **H4 Contas de convidado (família)** | 10/11/2026: "Vamos deixar H4 e I2 para o futuro" | Papéis (admin/convidado) e permissões por recurso (arquivos, terminal, configurações, apps visíveis). Muda a autenticação e todas as rotas. Decisão em aberto: quais permissões. |
| **H5 Revisão de segurança dos acessos e do script do servidor** | Pedido em 11/11/2026: "No futuro vamos fazer um checagem de segurança antes e podemos rever esses detalhes de acesso e excução de scripts." | Revisar cada pedido aceito pelo script do servidor (desligar, limite, leituras, atualizar Linux/apps, atualizar o próprio script) e quem pode pedir. Fazer antes de abrir o BrunOS para convidados ou para fora de casa. |

## Rede e casa

| Ideia | Por que ficou para depois | Notas técnicas |
|---|---|---|
| **I2 Wake-on-LAN** | 10/11/2026, junto com H4 | Pacote mágico UDP broadcast; da rede bridge do Docker o broadcast pode não chegar à LAN (testar); alternativa por SSH (`wakeonlan` no servidor). |
| **Vaultwarden como integração** | "Vamos sem integração com Vaultwarden nesse momento. Deixe para o futuro." (08/11/2026) | Sem token só dá no ar, versão e cadastro aberto; o que interessa (usuários) exige o `ADMIN_TOKEN`, que administra o cofre de senhas: decidir antes. |

## Celular

O Bruno adiou em 07/11/2026: "Vamos deixar para outro momento no futuro qualquer implementação que
envolva o celular." Em 11/11/2026 acrescentou: "Futuramente o acesso ao dashboard no celular será muito
importante" (por isso a interface já é testada em tamanho de celular).

- **A3 PWA** (instalar no celular): `manifest.webmanifest`, ícones, service worker mínimo (só a casca; a API
  nunca vai para o cache). Base para as duas abaixo.
- **C2 Avisos no celular**: ntfy (tem app no Runtipi), Telegram (bot) ou Web Push (exige VAPID, dá para
  fazer só com a biblioteca padrão: ECDSA P-256 + JWT). Regras de quais eventos vão para onde.
- **F6 Enviar do celular pelo menu Compartilhar**: depende da PWA (Web Share Target) e de uma pasta
  configurável.

## Ideias anotadas na etapa 11 (não aprovadas)

- Formatar só uma partição e editor de partições (criar, apagar, redimensionar) ("quem sabe no futuro").
- "Dormir após X minutos parado" (`hdparm -S`): ganho pequeno e alguns adaptadores USB ignoram.
- Beszel como Fonte de Métricas em widgets (não como lugar dos Históricos).
- Um BrunOS de teste no servidor, com os discos só para leitura (opção B do acesso de diagnóstico),
  deixado para depois da 0.13.0.

## Limpezas e conferências pendentes

- Tirar a conversão das preferências antigas de widgets (`widgets2` v1/v2 → v3, em `app.js`) quando o Bruno
  confirmar a tela inicial nova no servidor.
- Tomada Tuya: ainda sem confirmação de uso diário; apagar no servidor os arquivos temporários do tinytuya
  usados para pegar a chave local.

## Recusado (não propor de novo)

- **Tema claro**: "não vejo necessidade" (07/11/2026).
- Misturar ícones e widgets numa grade só (como no iPhone): descartado na decisão 48.
- Prometheus/InfluxDB para os históricos: pesados demais (decisão 53).
