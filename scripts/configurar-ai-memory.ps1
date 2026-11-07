# Configura o ai-memory (akitaonrails/ai-memory) no Windows para o projeto BrunOS.
#
# O que faz, perguntando antes de cada passo:
#   1. Escolhe o provedor de LLM (Gemini grátis, Codex, LM Studio local ou Anthropic) e guarda a chave
#      numa variável de ambiente do seu usuário (a chave nunca vai para arquivo nem para o git).
#   2. Ajusta o config.toml do ai-memory (provedor, modelo, idioma pt-BR), com backup.
#   3. Reinicia o servidor do ai-memory para ler a configuração nova.
#   4. Testa o provedor (llm-test).
#   5. Liga o MCP e os hooks ao Claude Code (idempotente).
#   6. Semeia a memória com este repositório (ai-memory bootstrap).
#
# Como rodar (PowerShell, dentro da pasta do repositório):
#   powershell -ExecutionPolicy Bypass -File scripts\configurar-ai-memory.ps1

$ErrorActionPreference = 'Stop'
function Titulo($t) { Write-Host ""; Write-Host "== $t ==" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  [ok] $t" -ForegroundColor Green }
function Aviso($t) { Write-Host "  [!] $t" -ForegroundColor Yellow }
function SimNao($pergunta, $padrao = 's') {
    $dica = if ($padrao -eq 's') { 'S/n' } else { 's/N' }
    $r = Read-Host "$pergunta [$dica]"
    if ([string]::IsNullOrWhiteSpace($r)) { $r = $padrao }
    return $r.Trim().ToLower().StartsWith('s')
}

$repo = Split-Path -Parent $PSScriptRoot

# ── 0. ai-memory instalado? ─────────────────────────────────────────
Titulo "Conferindo o ai-memory"
$exe = (Get-Command ai-memory -ErrorAction SilentlyContinue).Source
if (-not $exe) {
    $exe = Join-Path $env:LOCALAPPDATA 'Programs\ai-memory\ai-memory.exe'
    if (-not (Test-Path $exe)) { throw "Não achei o ai-memory. Instale com: irm https://github.com/akitaonrails/ai-memory/releases/latest/download/install.ps1 | iex ; ai-memory setup" }
}
Ok "ai-memory: $exe"

# A entrada de login criada pelo 'ai-memory setup' diz onde ficam os dados e o config.toml
$run = (Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'ai-memory' -ErrorAction SilentlyContinue).'ai-memory'
$dataDir = Join-Path $env:LOCALAPPDATA 'ai-memory'
$config = Join-Path $dataDir 'config.toml'
if ($run) {
    if ($run -match '--data-dir\s+"([^"]+)"') { $dataDir = $Matches[1] }
    if ($run -match '--config\s+"([^"]+)"') { $config = $Matches[1] } else { $config = Join-Path $dataDir 'config.toml' }
    Ok "servidor inicia no login (entrada do 'ai-memory setup')"
} else {
    Aviso "não achei a entrada de login do ai-memory. Rode 'ai-memory setup' primeiro (ele cria o servidor) e depois este script de novo."
    if (-not (SimNao "Continuar mesmo assim?" 'n')) { exit 1 }
}
Ok "dados: $dataDir"
Ok "configuração: $config"
if (-not (Test-Path $config)) { throw "O arquivo $config não existe. Rode 'ai-memory setup' antes." }

# ── 1. provedor de LLM ───────────────────────────────────────────────
Titulo "Provedor de LLM (resume as sessões e alimenta o bootstrap)"
Write-Host "  1) Gemini (Google)      grátis no plano free; chave em https://aistudio.google.com/apikey  [recomendado]"
Write-Host "  2) Codex (OpenAI)       usa o login do Codex CLI (assinatura ChatGPT), sem chave nova"
Write-Host "  3) LM Studio (local)    100% no seu PC, privado; precisa de um modelo carregado e o servidor ligado"
Write-Host "  4) Anthropic (Claude)   pago por uso (Haiku é barato); chave em https://console.anthropic.com"
Write-Host "  5) Não mudar o provedor"
$op = Read-Host "Escolha (1-5)"

$prov = $null; $model = $null; $baseUrl = $null; $keyVar = $null
switch ($op) {
    '1' { $prov = 'gemini'; $model = 'gemini-3.5-flash'; $keyVar = 'GEMINI_API_KEY' }
    '2' {
        $prov = 'codex'; $model = 'gpt-5.6-luna'
        $codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
        if (-not (Test-Path (Join-Path $codexHome 'auth.json'))) { Aviso "não achei $codexHome\auth.json. Abra o Codex CLI e faça login (com armazenamento de credencial 'file')." }
    }
    '3' {
        $prov = 'openai-compat'; $baseUrl = 'http://127.0.0.1:1234/v1'
        try {
            $models = @((Invoke-RestMethod -Uri "$baseUrl/models" -TimeoutSec 5).data.id)
            Write-Host "  Modelos no LM Studio:"; $i = 1; foreach ($m in $models) { Write-Host "    $i) $m"; $i++ }
            $n = Read-Host "  Qual modelo? (número)"; $model = $models[[int]$n - 1]
        } catch {
            Aviso "o servidor do LM Studio não respondeu em $baseUrl. No LM Studio: aba Developer > Start Server, com um modelo carregado."
            $model = Read-Host "  Digite o id do modelo (ex.: qwen3-8b)"
        }
        Aviso "prefira um modelo SEM raciocínio longo (thinking): os modelos que 'pensam' demais estouram o tempo."
    }
    '4' { $prov = 'anthropic'; $model = 'claude-haiku-4-5'; $keyVar = 'ANTHROPIC_API_KEY' }
    default { Aviso "provedor mantido como está" }
}

if ($keyVar) {
    $atual = [Environment]::GetEnvironmentVariable($keyVar, 'User')
    if ($atual -and -not (SimNao "Já existe $keyVar no seu usuário. Trocar a chave?" 'n')) {
        Set-Item -Path "Env:$keyVar" -Value $atual
    } else {
        $sec = Read-Host "Cole a chave ($keyVar) e tecle Enter (não aparece na tela)" -AsSecureString
        $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR([Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
        if ([string]::IsNullOrWhiteSpace($plain)) { throw "Chave vazia." }
        [Environment]::SetEnvironmentVariable($keyVar, $plain.Trim(), 'User')
        Set-Item -Path "Env:$keyVar" -Value $plain.Trim()
        $plain = $null
        Ok "chave guardada na variável de ambiente $keyVar do seu usuário"
    }
}

# ── 2. config.toml ───────────────────────────────────────────────────
Titulo "Ajustando o config.toml"
$backup = "$config.bak-$(Get-Date -Format 'yyyyMMdd-HHmmss')"
Copy-Item $config $backup
Ok "backup: $backup"
$linhas = [System.Collections.Generic.List[string]]::new()
foreach ($l in (Get-Content -Path $config -Encoding UTF8)) { $linhas.Add($l) }

function Definir([string]$chave, $valor) {
    # muda ou cria uma chave do topo do arquivo (antes da primeira [seção])
    $fim = $linhas.Count
    for ($i = 0; $i -lt $linhas.Count; $i++) { if ($linhas[$i] -match '^\s*\[') { $fim = $i; break } }
    $nova = if ($null -eq $valor) { $null } else { "$chave = `"$valor`"" }
    for ($i = 0; $i -lt $fim; $i++) {
        if ($linhas[$i] -match "^\s*$([regex]::Escape($chave))\s*=") {
            if ($nova) { $linhas[$i] = $nova } else { $linhas[$i] = "# $($linhas[$i])" }
            return
        }
    }
    if ($nova) { $linhas.Insert($fim, $nova) }
}

Definir 'language' 'pt-BR'
if ($prov) {
    Definir 'llm_provider' $prov
    Definir 'llm_model' $model
    Definir 'llm_base_url' $baseUrl     # só o LM Studio usa; nos outros a linha antiga é comentada
}
$utf8 = New-Object System.Text.UTF8Encoding($false)
[IO.File]::WriteAllLines($config, $linhas, $utf8)
$resumo = if ($prov) { ", provedor $prov, modelo $model" } else { '' }
Ok "config.toml atualizado (idioma pt-BR$resumo)"

# ── 3. reiniciar o servidor ──────────────────────────────────────────
Titulo "Reiniciando o servidor do ai-memory"
if ($run) {
    $stop = Join-Path $dataDir 'service-host.stop'
    New-Item -ItemType File -Force -Path $stop | Out-Null
    Start-Sleep -Seconds 4
    Remove-Item -Force -ErrorAction SilentlyContinue $stop
    # roda de novo o mesmo comando do login, já com a chave nesta sessão
    if ($run -match '^\s*"([^"]+)"\s*(.*)$') { Start-Process -FilePath $Matches[1] -ArgumentList $Matches[2] -WindowStyle Hidden }
    Start-Sleep -Seconds 4
    & $exe status
    Ok "servidor reiniciado"
} else {
    Aviso "sem entrada de login: reinicie o servidor do jeito que você o iniciou."
}

# ── 4. testar o provedor ─────────────────────────────────────────────
if ($prov) {
    Titulo "Testando o provedor"
    if ($baseUrl) { $env:AI_MEMORY_LLM_BASE_URL = $baseUrl }
    & $exe llm-test --provider $prov --model $model --prompt 'Responda apenas: OK'
    if ($LASTEXITCODE -eq 0) { Ok "o provedor respondeu" } else { Aviso "o teste falhou (veja a mensagem acima)" }
}

# ── 5. MCP e hooks do Claude Code ────────────────────────────────────
Titulo "Ligando o ai-memory ao Claude Code"
if (SimNao "Instalar/atualizar MCP e hooks do Claude Code (é seguro repetir)?") {
    & $exe install-mcp --client claude-code --apply
    & $exe install-hooks --agent claude-code --apply
    Ok "Claude Code ligado (feche e abra o Claude Code/VS Code para valer)"
}
if (SimNao "Ligar também ao app Claude Desktop (chat; precisa do Node.js)?" 'n') {
    & $exe install-mcp --client claude-desktop --apply
    Ok "feche o Claude Desktop pela bandeja (Sair) e abra de novo"
}

# ── 6. bootstrap do projeto ──────────────────────────────────────────
Titulo "Semeando a memória com o projeto BrunOS"
if ($prov -or (SimNao "Rodar o bootstrap com o provedor que já estava configurado?" 'n')) {
    Push-Location $repo
    try {
        & $exe bootstrap --dry-run
        if (SimNao "O resumo acima está ok? Rodar o bootstrap de verdade?") { & $exe bootstrap; Ok "memória semeada" }
    } finally { Pop-Location }
}

Titulo "Pronto"
Write-Host "  Abra o VS Code na pasta do repositório e inicie o Claude Code (ou 'ai-memory run claude')."
Write-Host "  Para ver o que está guardado: ai-memory recent   |   estado do servidor: ai-memory status"
