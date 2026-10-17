# ⚔️ LoL Idle Multi-Bot & Real-Time Dashboard (Go Native Engine)

Bot autônomo multi-contas de altíssima performance para **LoL Idle (Runeterra)** desenvolvido 100% nativo em **Go (Golang)**, operando em modo **Party de 4 Jogadores** no mundo aberto com compartilhamento contínuo de experiência (+45% EXP bônus), salvamento infalível via HTTP 200 e dashboard web em tempo real.

🌐 **Dashboard Individual (GitHub Pages):** [https://kbelludoo.github.io/lolidle-bot/](https://kbelludoo.github.io/lolidle-bot/)  
🌐 **VPS Command Center Central:** [https://kbelludoo.github.io/bot-template-go/](https://kbelludoo.github.io/bot-template-go/)

---

## 🌟 Recursos Principais

- 👥 **Party Cooperativa de 4 Jogadores:** Conecta automaticamente Taric (Tanque/Suporte), Lux (Mago/Escudo), Ezreal (Atirador/DPS) e Yasuo (Lutador/Crítico) via WebSocket no mapa de Runeterra, garantindo o bônus máximo de **+45% de EXP de grupo**.
- 🗺️ **Multiplayer em Runeterra (Mundo Aberto):** Transmite pacotes de posição `{t: "pos"}` e abates compartilhados `{t: "abateGrupo"}` ~10x por segundo com patrulhamento circular síncrono.
- 💾 **Salvamento Confiável (HTTP 200 OK):** Preserva rigorosamente todas as 31 chaves de esquema do servidor, contadores de abates e orçamentos de atributos (`PONTOS_POR_NIVEL = 3`), eliminando qualquer recusa ou rollback.
- 🪶 **Consumo Cirúrgico (< 2.5 MB de RAM):** Zero navegadores headless (sem Chromium, Puppeteer ou Playwright). Executável nativo compilado em Go.
- 📊 **Telemetria Contínua no GitHub Pages:** Atualização automática das estatísticas de EXP/h, abates, ouro, vida/mana e logs de combate.

---

## 📁 Estrutura do Projeto

```text
lolidle-bot/
├── accounts.example.json       # Modelo de configuração das contas e party
├── accounts.json               # Credenciais reais (ignorado pelo git)
├── main.go                     # Entrypoint do bot e dashboard HTTP local
├── go.mod / go.sum             # Módulo Go nativo
│
├── docs/                       # 🌐 GITHUB PAGES (DASHBOARD AO VIVO)
│   ├── index.html              # Interface Hextech/Cyberpunk em tempo real
│   └── status.json             # Telemetria sincronizada 24/7 da VPS
│
├── pkg/
│   ├── game/                   # 🎮 Lógica do LoL Idle: combate, party, rotas e save
│   │   ├── bot.go              # Loop principal, party WebSocket e HTTP save
│   │   ├── hunter.go           # Simulação fiel de caça em Runeterra
│   │   ├── jogo.go             # Fórmulas de balanço, curvas de EXP e monstros
│   │   └── types.go            # Tipos e preservação do esquema RawEstado
│   ├── engine/                 # Motor concorrente com supervisor contra panic
│   ├── network/                # Conexão WebSocket resiliente e cliente HTTP
│   └── server/                 # Servidor HTTP local na porta 3011
│
└── cmd/
    └── build/                  # Script para compilação cruzada Linux/Windows
```

---

## 🚀 Como Executar

### 1. Configurar Contas
Copie o arquivo de exemplo e insira os tokens dos personagens:
```bash
cp accounts.example.json accounts.json
```

### 2. Rodar Localmente
```bash
go run .
```
Acesse o dashboard local em: `http://localhost:3011`

### 3. Deploy na VPS Linux
Compile para Linux x86_64:
```bash
cmd /c "set GOOS=linux&& set GOARCH=amd64&& go build -ldflags=\"-s -w\" -o bot-lolidle-linux ."
```

Copie para a VPS e execute via systemd (`lolidle.service`):
```bash
sudo systemctl start lolidle.service
```
