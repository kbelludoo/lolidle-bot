package game

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"bot-template-go/pkg/engine"
	"bot-template-go/pkg/network"
)

// Parâmetros do "andar em grupo" no mundo aberto (Runeterra) via WebSocket.
const (
	arenaBaseX = 125.0 // posição base: perto de Demacia (zona de caça inicial nível 1-15)
	arenaBaseY = 475.0
	arenaRaio  = 3.0 // raio do patrulhamento circular (mantém o bando junto, < raio de 15 do grupo)
	arenaVel   = 0.6 // rad/s do patrulhamento
)

type Bot struct {
	config  AccountConfig
	mu      sync.RWMutex
	status  engine.AccountStatus
	socket  *network.SocketClient
	httpCli *network.HTTPClient

	session       PlayerSession
	startTime     time.Time
	startExp      float64
	instancia     string
	lastSave      time.Time
	ticketChan    chan string
	turnstileChan chan string

	partyMu      sync.Mutex
	inParty      bool
	partyNames   map[string]bool      // nicks atualmente no grupo (minúsculos)
	partyInvited map[string]time.Time // último convite enviado por nick

	arenaX float64 // posição atual em Runeterra (ajustável por {t:"corrigir"})
	arenaY float64

	hunt      *hunter // simulação de caça em Runeterra (abates de monstros)
	expEarned float64 // XP acumulada desde o início (para o EXP/h)

	logMu sync.Mutex
	logs  []string

	recusados int64 // quantos saves o servidor recusou (progresso impossível)
}

func NewBot(cfg AccountConfig) *Bot {
	st := engine.NewStatus(cfg.ID, cfg.Name)
	if cfg.Settings.HuntTarget != "" {
		st.CurrentArea = cfg.Settings.HuntTarget
	} else {
		st.CurrentArea = "rota-inf"
	}
	st.Extra = map[string]any{
		"bindIP": cfg.BindIP,
		"proxy":  cfg.Proxy,
	}

	inst := fmt.Sprintf("%x%x", rand.Int63(), time.Now().UnixNano())
	cli := network.NewHTTPClientWithNetwork("https://lolidle.com", 15*time.Second, cfg.BindIP, cfg.Proxy)
	cli.SetHeader("X-Idioma", "pt")
	cli.SetHeader("X-Instancia", inst)

	if cfg.Credentials.Token != "" {
		cli.SetHeader("Authorization", "Bearer "+cfg.Credentials.Token)
	}

	return &Bot{
		config:        cfg,
		status:        st,
		httpCli:       cli,
		instancia:     inst,
		startTime:     time.Now(),
		ticketChan:    make(chan string, 1),
		turnstileChan: make(chan string, 1),
		partyNames:    make(map[string]bool),
		partyInvited:  make(map[string]time.Time),
	}
}

func (b *Bot) ID() string {
	return b.config.ID
}

func (b *Bot) Name() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.status.Name != "" {
		return b.status.Name
	}
	return b.config.Name
}

func (b *Bot) updateStatus(fn func(s *engine.AccountStatus)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(&b.status)
	b.status.UpdatedAt = time.Now().UnixMilli()
}

func (b *Bot) addLog(msg string) {
	s := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	b.logMu.Lock()
	b.logs = append(b.logs, s)
	if len(b.logs) > 15 {
		b.logs = b.logs[1:]
	}
	b.logMu.Unlock()
}

func (b *Bot) Init(ctx context.Context) error {
	b.updateStatus(func(s *engine.AccountStatus) {
		s.Status = "Autenticando..."
	})

	if b.config.Credentials.Token == "" {
		err := fmt.Errorf("[%s] Token ausente em accounts.json", b.ID())
		b.addLog(err.Error())
		b.updateStatus(func(s *engine.AccountStatus) {
			s.Status = "Token ausente"
		})
		return err
	}

	// 1. Carregar dados reais do personagem via GET /api/eu
	b.addLog("Validando sessão e carregando dados reais do personagem via /api/eu...")
	var sess PlayerSession
	if err := b.httpCli.GetJSON("/api/eu", &sess); err != nil {
		b.updateStatus(func(s *engine.AccountStatus) {
			s.Status = "Erro na sessão: " + err.Error()
		})
		return fmt.Errorf("falha ao carregar /api/eu: %w", err)
	}

	b.mu.Lock()
	b.session = sess
	if sess.Nome != "" {
		b.status.Name = sess.Nome
	}
	b.status.Vocation = sess.Campeao
	b.status.Level = sess.Estado.Nivel
	b.status.HP = int(sess.Estado.Hp)
	b.status.MaxHP = int(sess.Estado.Hp)
	b.status.Mana = int(sess.Estado.Mana)
	b.status.MaxMana = int(sess.Estado.Mana)
	b.status.Gold = int64(sess.Estado.Ouro)
	b.status.Kills = sess.Estado.Totais.Abates
	b.startExp = sess.Estado.Exp
	if sess.Estado.Local != "" {
		b.status.CurrentArea = sess.Estado.Local
	} else if sess.Estado.Area != "" {
		b.status.CurrentArea = sess.Estado.Area
	}
	b.status.Status = "hunting"
	b.mu.Unlock()

	// Mundo aberto (Runeterra): entra no mundo e mantém as contas juntas para o bônus de grupo.
	if b.config.Settings.ArenaWalk {
		b.config.Settings.AutoRoute = false // não deixa o "auto-route" levar a conta para o Rift
		b.config.Settings.AutoHeal = false
		b.initArena()
	}

	netInfo := "IP Padrão"
	if b.config.BindIP != "" {
		netInfo = "Bind IP " + b.config.BindIP
	} else if b.config.Proxy != "" {
		netInfo = "Proxy " + b.config.Proxy
	}

	b.addLog(fmt.Sprintf("✔ Conectado! Personagem: %s (%s, Nv. %d) | Rede: %s | Ouro: %d | Kills: %d | Área: %s",
		sess.Nome, sess.Campeao, sess.Estado.Nivel, netInfo, int64(sess.Estado.Ouro), sess.Estado.Totais.Abates, b.status.CurrentArea))

	return nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.updateStatus(func(s *engine.AccountStatus) {
		s.Status = "hunting"
	})
	b.addLog("Rotinas autônomas de caça, rota e coleta ativadas.")

	// Conectar WebSocket em tempo real para presença no servidor
	go b.startWebSocket(ctx)

	// Resgates iniciais
	go b.claimDailyRewards()

	// Tickers para o ciclo autônomo do bot
	simTicker := time.NewTicker(100 * time.Millisecond)
	defer simTicker.Stop()
	lastSim := time.Now()

	huntTicker := time.NewTicker(5 * time.Second)
	defer huntTicker.Stop()

	routeTicker := time.NewTicker(30 * time.Second)
	defer routeTicker.Stop()

	chestTicker := time.NewTicker(20 * time.Second)
	defer chestTicker.Stop()

	saveTicker := time.NewTicker(15 * time.Second)
	defer saveTicker.Stop()

	presenceTicker := time.NewTicker(10 * time.Second)
	defer presenceTicker.Stop()

	dailyTicker := time.NewTicker(5 * time.Minute)
	defer dailyTicker.Stop()

	partyTicker := time.NewTicker(20 * time.Second)
	defer partyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.salvarSave(true)
			return ctx.Err()

		case <-simTicker.C:
			now := time.Now()
			dt := now.Sub(lastSim).Seconds()
			lastSim = now
			if dt <= 0 {
				dt = 0.1
			}
			if dt > 1.0 {
				dt = 1.0 // previne speed hack
			}
			b.simStep(dt)

		case <-huntTicker.C:
			if !b.config.Settings.ArenaWalk {
				b.stepGame()
			}

		case <-routeTicker.C:
			if b.config.Settings.AutoRoute {
				b.checkOptimalRoute()
			}
			if b.config.Settings.AutoUpgrade {
				b.autoUpgradeStatsAndSkills()
			}

		case <-chestTicker.C:
			if b.config.Settings.AutoChests {
				b.checkWorldChestsAndSacks()
			}

		case <-saveTicker.C:
			b.salvarSave(false)

		case <-presenceTicker.C:
			b.pingPresence()

		case <-partyTicker.C:
			if b.config.Settings.AutoParty {
				b.ensureParty()
			}

		case <-dailyTicker.C:
			if b.config.Settings.AutoDaily {
				b.claimDailyRewards()
			}
		}
	}
}

func (b *Bot) startWebSocket(ctx context.Context) {
	wsURL := "wss://lolidle.com/ws"
	backoff := 2 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		headers := make(http.Header)
		headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

		client := network.NewSocketClientWithNetwork(wsURL, headers, b.config.BindIP, b.config.Proxy, b.onWSMessage, nil)

		if err := client.Connect(); err != nil {
			b.addLog(fmt.Sprintf("WS falha ao conectar: %v (nova tentativa em %s)", err, backoff))
			if !b.sleepCtx(ctx, backoff) {
				return
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		b.mu.Lock()
		b.socket = client
		b.mu.Unlock()

		_ = client.SendJSON(map[string]any{"t": "oi", "token": b.config.Credentials.Token})
		b.addLog("🟢 WebSocket conectado: presença online no servidor.")
		backoff = 2 * time.Second

		if b.config.Settings.ArenaWalk {
			go b.posLoop(ctx, client)
		}

		ticker := time.NewTicker(1 * time.Second)
		for client.IsConnected() {
			select {
			case <-ctx.Done():
				ticker.Stop()
				client.Close()
				return
			case <-ticker.C:
			}
		}
		ticker.Stop()
		client.Close()

		b.mu.Lock()
		if b.socket == client {
			b.socket = nil
		}
		b.mu.Unlock()
		b.clearParty()

		b.addLog("🔌 WebSocket desconectado. Reconectando...")
		if !b.sleepCtx(ctx, backoff) {
			return
		}
	}
}

func (b *Bot) sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// initArena coloca o personagem em Runeterra (local "arena") e define a posição base do bando.
func (b *Bot) initArena() {
	x, y := b.config.Settings.ArenaX, b.config.Settings.ArenaY
	if x == 0 && y == 0 {
		x, y = arenaBaseX, arenaBaseY
	}
	b.mu.Lock()
	b.arenaX, b.arenaY = x, y
	b.session.Estado.Local = "arena"
	b.status.CurrentArea = "arena"
	if b.hunt == nil {
		b.hunt = newHunter(int64(hashNome(b.status.Name, 0x9e3779b1)))
	}
	b.mu.Unlock()

	b.salvarSave(true)
	b.addLog(fmt.Sprintf("🗺️ Runeterra: presença em (%.0f, %.0f) — seguindo o líder para o bônus de grupo.", x, y))
}

// patrolPos devolve a posição atual do bando: todos seguem o mesmo círculo (mesma base e tempo),
// com um pequeno deslocamento por conta, mantendo-se sempre dentro dos 15 tiles do grupo.
func (b *Bot) patrolPos() (float64, float64) {
	b.mu.RLock()
	baseX, baseY := b.arenaX, b.arenaY
	name := b.status.Name
	b.mu.RUnlock()
	if baseX == 0 && baseY == 0 {
		baseX, baseY = arenaBaseX, arenaBaseY
	}

	ox, oy := arenaOffset(name)
	ang := arenaVel * time.Since(b.startTime).Seconds()
	x := baseX + arenaRaio*math.Cos(ang) + ox
	y := baseY + arenaRaio*math.Sin(ang) + oy
	return round2(x), round2(y)
}

// arenaOffset gera um deslocamento pequeno e estável a partir do nick (evita empilhar as contas no mesmo pixel).
func arenaOffset(name string) (float64, float64) {
	dx := (float64(hashNome(name, 2166136261)%7) - 3) * 0.8
	dy := (float64(hashNome(name, 2166136262)%7) - 3) * 0.8
	return dx, dy
}

// hashNome é um FNV-1a curto com semente, só para espalhar os offsets das contas.
func hashNome(s string, seed uint32) uint32 {
	h := seed
	for _, r := range s {
		h ^= uint32(r)
		h *= 16777619
		h ^= h >> 13
	}
	return h
}

// posLoop transmite a posição ~10x/s (igual ao cliente), mantendo as contas do grupo juntas em Runeterra.
func (b *Bot) posLoop(ctx context.Context, c *network.SocketClient) {
	time.Sleep(400 * time.Millisecond) // dá tempo do handshake {t:"oi"} ser aceito
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !c.IsConnected() {
				return
			}
			b.mu.RLock()
			tj := b.session.Estado.TempoJogo
			b.mu.RUnlock()
			_ = c.SendJSON(b.posMessage(tj))
		}
	}
}

// posMessage monta o pacote {t:"pos"} igual ao do cliente (jogo.js:1273).
func (b *Bot) posMessage(tj float64) map[string]any {
	x, y := b.patrolPos()
	mons := b.posMons()
	b.mu.RLock()
	vida := float64(b.status.MaxHP)
	b.mu.RUnlock()
	return map[string]any{
		"t": "pos", "tj": round2(tj),
		"x": x, "y": y, "dx": 1, "dy": 0, "anda": true, "atk": len(mons) > 0,
		"vida": vida, "skin": 0, "pvp": false, "parado": false, "morto": false, "mt": "",
		"mons": mons,
	}
}

// posMons devolve os monstros vivos que a conta está caçando (campo mons do {t:"pos"}).
func (b *Bot) posMons() []any {
	b.mu.RLock()
	h := b.hunt
	b.mu.RUnlock()
	if h == nil {
		return []any{}
	}
	return h.posMons()
}

// killRate é quantos abates por minuto o bot simula em Runeterra.
func (b *Bot) killRate() float64 {
	b.mu.RLock()
	r := b.config.Settings.KillRate
	b.mu.RUnlock()
	if r <= 0 {
		r = 32 // taxa ideal calibrada (~50 ab/min em grupo sem recusa)
	}
	return r
}

// SetKillRate ajusta os abates/min em tempo real (usado pelo teste de limite do servidor).
func (b *Bot) SetKillRate(v float64) {
	b.mu.Lock()
	b.config.Settings.KillRate = v
	b.mu.Unlock()
}

// SaveRecusados devolve quantos saves o servidor recusou desde o início.
func (b *Bot) SaveRecusados() int64 {
	b.logMu.Lock()
	defer b.logMu.Unlock()
	return b.recusados
}

// huntStep avança a caça em Runeterra e reporta cada abate com {t:"abateGrupo"},
// para o servidor distribuir o XP de grupo aos companheiros próximos (jogo.js:702).
func (b *Bot) huntStep(dt float64) {
	b.mu.RLock()
	h := b.hunt
	level := b.status.Level
	inParty := b.inParty
	sock := b.socket
	b.mu.RUnlock()
	if h == nil {
		return
	}

	z := localDaArena(level)
	px, py := b.patrolPos()
	mortos := h.tick(dt, level, z, px, py, b.killRate())
	for _, m := range mortos {
		if inParty && sock != nil && sock.IsConnected() {
			_ = sock.SendJSON(map[string]any{"t": "abateGrupo", "id": m.ID})
		}
		b.creditarAbate(m)
	}
}

// creditarAbate soma XP e ouro do abate no estado local, como processarAbates do jogo
// (motor.js:1789), para o /api/save bater com o que o servidor espera.
func (b *Bot) creditarAbate(m *inimigoArena) {
	b.mu.Lock()
	st := &b.session.Estado
	bonus := 1.0
	if v, ok := b.status.Extra["partyBonusXp"].(float64); ok && v > 0 {
		bonus = v
	}
	fx := fatorXp(st.Nivel, m.Nivel)
	xp := curvaXP(m.Nivel) * m.XpMult * fx * arenaBonusXp * bonus
	ouro := curvaOuro(m.Nivel) * m.OuroMult * (0.5 + rand.Float64()*0.5) * fx

	st.Ouro += ouro
	st.Totais.Ouro += ouro
	st.Totais.Abates++
	if st.Totais.PorMonstro == nil {
		st.Totais.PorMonstro = make(map[string]int)
	}
	st.Totais.PorMonstro[m.Nome]++
	ganharXpEstado(st, xp)
	b.expEarned += xp

	b.status.Level = st.Nivel
	b.status.Gold = int64(st.Ouro)
	b.status.Kills = st.Totais.Abates
	abates := st.Totais.Abates
	b.mu.Unlock()

	if abates%20 == 0 {
		b.addLog(fmt.Sprintf("⚔️ %d abates — caçando em Runeterra (nível %d).", abates, b.status.Level))
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case int:
		return float64(n), true
	}
	return 0, false
}

// onWSMessage trata os eventos em tempo real (inclui o grupo/party de Runeterra).
func (b *Bot) onWSMessage(data []byte) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	typ, _ := m["t"].(string)
	switch typ {
	case "estado":
		if grupo, ok := m["grupo"].(map[string]any); ok && len(grupo) > 0 {
			b.applyParty(grupo)
		} else {
			b.clearParty()
		}

	case "grupoConvite":
		de, _ := m["de"].(string)
		if de == "" {
			de = "alguém"
		}
		b.addLog(fmt.Sprintf("📨 Convite de grupo recebido de %s.", de))
		if b.config.Settings.AutoParty {
			b.groupSend("aceitar", "")
			b.addLog(fmt.Sprintf("✅ Entrei no grupo de %s.", de))
		}

	case "grupoAviso":
		if pt, _ := m["pt"].(string); pt != "" {
			b.addLog("👥 Grupo: " + pt)
		}

	case "corrigir":
		// O servidor corrigiu a posição (anti-cheat): passa a usar a posição validada por ele.
		x, okX := toFloat(m["x"])
		y, okY := toFloat(m["y"])
		if okX && okY {
			b.mu.Lock()
			b.arenaX, b.arenaY = x, y
			b.mu.Unlock()
		}

	case "xpGrupo":
		// Um companheiro de grupo, perto, abateu um monstro dele: o servidor manda a sua parte (jogo.js:1025).
		xp, ok := toFloat(m["xp"])
		if !ok || xp <= 0 {
			return
		}
		b.mu.Lock()
		if b.session.Estado.Local == "arena" {
			ganharXpEstado(&b.session.Estado, xp)
			b.expEarned += xp
			b.status.Level = b.session.Estado.Nivel
		}
		b.mu.Unlock()
	}
}

// groupSend envia uma ação do grupo via WebSocket ({t:"grupo", acao, ...}).
func (b *Bot) groupSend(acao, nome string) {
	b.mu.RLock()
	sock := b.socket
	b.mu.RUnlock()
	if sock == nil || !sock.IsConnected() {
		return
	}
	msg := map[string]any{"t": "grupo", "acao": acao}
	if nome != "" {
		msg["nome"] = nome
	}
	if err := sock.SendJSON(msg); err != nil {
		b.addLog("Falha ao enviar ação de grupo: " + err.Error())
	}
}

// applyParty registra o estado do grupo recebido do servidor (mensagens "estado").
func (b *Bot) applyParty(grupo map[string]any) {
	nomes := make([]string, 0, 4)
	set := make(map[string]bool)
	bonus := 1.0
	if v, ok := grupo["bonus"].(float64); ok {
		bonus = v
	}
	if membros, ok := grupo["membros"].([]any); ok {
		for _, raw := range membros {
			mm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			n, _ := mm["n"].(string)
			if n == "" {
				continue
			}
			set[strings.ToLower(n)] = true
			rot := n
			if lider, _ := mm["lider"].(bool); lider {
				rot += " (líder)"
			}
			nomes = append(nomes, rot)
		}
	}

	b.partyMu.Lock()
	b.partyNames = set
	b.partyMu.Unlock()

	b.mu.Lock()
	wasIn := b.inParty
	b.inParty = true
	b.status.Extra["party"] = nomes
	b.status.Extra["partyBonusXp"] = bonus
	b.mu.Unlock()

	if !wasIn {
		b.addLog(fmt.Sprintf("👥 Entrei no grupo (%d membro(s), +%d%% XP quando juntos).", len(nomes), int((bonus-1)*100)))
	}
}

func (b *Bot) clearParty() {
	b.partyMu.Lock()
	b.partyNames = make(map[string]bool)
	b.partyInvited = make(map[string]time.Time)
	b.partyMu.Unlock()

	b.mu.Lock()
	wasIn := b.inParty
	b.inParty = false
	delete(b.status.Extra, "party")
	delete(b.status.Extra, "partyBonusXp")
	b.mu.Unlock()

	if wasIn {
		b.addLog("👥 Saí do grupo.")
	}
}

// ensureParty mantém a conta no grupo. O líder convida os membros configurados.
func (b *Bot) ensureParty() {
	if !b.isPartyLeader() {
		return
	}
	self := b.selfName()
	for _, nome := range b.config.Settings.PartyMembers {
		nome = strings.TrimSpace(nome)
		if nome == "" || strings.EqualFold(nome, self) {
			continue
		}
		chave := strings.ToLower(nome)
		b.partyMu.Lock()
		jáEstá := b.partyNames[chave]
		ultimo := b.partyInvited[chave]
		b.partyMu.Unlock()
		if jáEstá || time.Since(ultimo) < 90*time.Second {
			continue
		}
		b.partyMu.Lock()
		b.partyInvited[chave] = time.Now()
		b.partyMu.Unlock()
		b.groupSend("convidar", nome)
		b.addLog(fmt.Sprintf("📨 Convite de grupo enviado para %s.", nome))
	}
}

// isPartyLeader decide se esta conta é quem cria/convida o grupo.
func (b *Bot) isPartyLeader() bool {
	lead := strings.TrimSpace(b.config.Settings.PartyLeader)
	if lead == "" {
		return true
	}
	self := b.selfName()
	return strings.EqualFold(lead, self) ||
		strings.EqualFold(lead, b.config.ID) ||
		strings.EqualFold(lead, b.config.Name)
}

func (b *Bot) selfName() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.status.Name != "" {
		return b.status.Name
	}
	return b.config.Name
}

// simStep avança a simulação local rigorosamente 1:1 com o relógio real (motor.js:1374),
// garantindo que delta(tempoJogo) seja idêntico ao delta do relógio do servidor (evita speed hack e recusa).
func (b *Bot) simStep(dt float64) {
	b.mu.Lock()
	b.session.Estado.TempoJogo += dt
	b.status.DurationSec = int(time.Since(b.startTime).Seconds())
	if h := time.Since(b.startTime).Hours(); h > 0.001 {
		b.status.ExpPerHour = b.expEarned / h
	}
	b.status.UpdatedAt = time.Now().UnixMilli()
	b.mu.Unlock()

	if b.config.Settings.ArenaWalk {
		b.huntStep(dt)
	}
}

func (b *Bot) stepGame() {
	// Em Runeterra o estado é autoritativo localmente (igual ao cliente): não deixa
	// o GET /api/eu sobrescrever a XP/ouro que estamos ganhando antes do save.
	if b.config.Settings.ArenaWalk {
		b.mu.Lock()
		b.status.DurationSec = int(time.Since(b.startTime).Seconds())
		if h := time.Since(b.startTime).Hours(); h > 0.001 {
			b.status.ExpPerHour = b.expEarned / h
		}
		b.status.UpdatedAt = time.Now().UnixMilli()
		b.mu.Unlock()
		return
	}

	var sess PlayerSession
	if err := b.httpCli.GetJSON("/api/eu", &sess); err == nil {
		b.mu.Lock()
		b.session = sess
		b.status.Level = sess.Estado.Nivel
		b.status.HP = int(sess.Estado.Hp)
		b.status.Mana = int(sess.Estado.Mana)
		b.status.Gold = int64(sess.Estado.Ouro)
		b.status.Kills = sess.Estado.Totais.Abates
		if sess.Estado.Local != "" {
			b.status.CurrentArea = sess.Estado.Local
		}

		elapsedHours := time.Since(b.startTime).Hours()
		if elapsedHours > 0.001 {
			diffExp := sess.Estado.Exp - b.startExp
			if diffExp > 0 {
				b.status.ExpPerHour = diffExp / elapsedHours
			}
		}

		b.status.DurationSec = int(time.Since(b.startTime).Seconds())
		b.status.UpdatedAt = time.Now().UnixMilli()
		b.mu.Unlock()

		// Auto-Heal: se vida < 30%, vai à Fonte se curar e vender
		if b.config.Settings.AutoHeal && !b.config.Settings.ArenaWalk && sess.Estado.Hp > 0 && sess.Estado.Hp < (float64(b.status.MaxHP)*0.30) {
			b.healAtFountain()
		}
	}
}

func (b *Bot) checkOptimalRoute() {
	b.mu.RLock()
	lvl := b.status.Level
	currentZone := b.session.Estado.Local
	b.mu.RUnlock()

	targetZone := b.config.Settings.HuntTarget
	if targetZone == "" || targetZone == "auto" {
		targetZone = getBestZone(lvl)
	}

	if currentZone != targetZone {
		b.addLog(fmt.Sprintf("🗺️ Nível %d: Viajando para melhor rota de caça -> %s", lvl, targetZone))
		b.travelTo(targetZone)
	}
}

func getBestZone(level int) string {
	switch {
	case level < 6:
		return "rota-inf"
	case level < 11:
		return "rota-sup"
	case level < 16:
		return "lobos"
	case level < 21:
		return "azul"
	case level < 26:
		return "vermelho"
	case level < 33:
		return "rio"
	case level < 41:
		return "dragao"
	default:
		return "barao"
	}
}

func (b *Bot) travelTo(zone string) {
	b.mu.Lock()
	b.session.Estado.Local = zone
	b.status.CurrentArea = zone
	b.mu.Unlock()
	b.salvarSave(false)
}

func (b *Bot) healAtFountain() {
	b.addLog("🚑 Vida crítica! Retornando à Fonte para cura da Soraka e venda de espólios...")
	b.travelTo("fonte")

	b.mu.Lock()
	b.session.Estado.Hp = float64(b.status.MaxHP)
	b.session.Estado.Mana = float64(b.status.MaxMana)

	// Vender espólios acumulados
	if len(b.session.Estado.Espolios) > 0 {
		var totalEspolios int64 = 0
		for item, qtd := range b.session.Estado.Espolios {
			totalEspolios += int64(qtd * 15)
			_ = item
		}
		b.session.Estado.Ouro += float64(totalEspolios)
		b.status.Gold += totalEspolios
		b.session.Estado.Espolios = make(map[string]int)
		b.addLog(fmt.Sprintf("💰 Espólios vendidos na Fonte: +%d ouro!", totalEspolios))
	}
	b.mu.Unlock()

	// Visitar o Ornn na Fonte para forjar ou aprimorar equipamentos
	b.tentarOrnnNaFonte()

	b.salvarSave(true)

	// Retornar à melhor rota
	time.Sleep(1 * time.Second)
	b.checkOptimalRoute()
}

func (b *Bot) tentarOrnnNaFonte() {
	b.mu.Lock()
	ouro := b.session.Estado.Ouro
	itens := make([]any, len(b.session.Estado.Itens))
	copy(itens, b.session.Estado.Itens)
	b.mu.Unlock()

	// 1. Tentar forjar itens equipados se tiver ouro de sobra (> 10.000)
	if ouro > 10000 && len(itens) > 0 {
		for idx, it := range itens {
			itStr, ok := it.(string)
			if !ok {
				continue
			}
			p := ParseItem(itStr)
			if p.Refino < 3 { // Tenta refino seguro até +3
				var resp map[string]any
				reqData := map[string]any{
					"estado": b.session.Estado,
					"onde":   "itens",
					"k":      idx,
					"vezes":  1,
				}
				if err := b.httpCli.PostJSON("/api/ornn/forjar", reqData, &resp); err == nil {
					if res, ok := resp["resultado"].(map[string]any); ok {
						if suc, _ := res["sucesso"].(bool); suc {
							novoItem, _ := res["item"].(string)
							b.mu.Lock()
							if idx < len(b.session.Estado.Itens) {
								b.session.Estado.Itens[idx] = novoItem
							}
							if g, ok := toFloat(res["gasto"]); ok {
								b.session.Estado.Ouro -= g
							}
							b.mu.Unlock()
							b.addLog(fmt.Sprintf("🔨 Ornn forjou com sucesso: %s (+%d)!", NomeExibicaoItem(novoItem), p.Refino+1))
						}
					}
				}
			}
		}
	}
}

func maxRankSkill(sk string, level int) int {
	switch sk {
	case "R":
		count := 0
		for _, req := range []int{6, 11, 16, 25, 35} {
			if level >= req {
				count++
			}
		}
		return count
	case "Q", "W", "E":
		max := (level + 1) / 2
		if max > 10 {
			max = 10
		}
		return max
	default:
		return 10
	}
}

func (b *Bot) autoUpgradeStatsAndSkills() {
	b.mu.Lock()

	st := &b.session.Estado
	upgraded := false

	// 1. Distribuir atributos ponto a ponto (sem estourar o orçamento do nível)
	maxOrcamento := 3 * (st.Nivel - 1)
	gastos := 0
	for _, v := range st.Atrib {
		if v > 0 {
			gastos += v
		}
	}
	if gastos > maxOrcamento {
		st.Atrib = make(map[string]int)
		gastos = 0
		st.PontosAtrib = maxOrcamento
	} else if gastos+st.PontosAtrib > maxOrcamento {
		st.PontosAtrib = maxOrcamento - gastos
	}
	if st.PontosAtrib < 0 {
		st.PontosAtrib = 0
	}

	if st.PontosAtrib > 0 {
		if st.Atrib == nil {
			st.Atrib = make(map[string]int)
		}
		champ := b.session.Campeao
		var seq []string
		switch champ {
		case "darius", "zac", "taric":
			seq = []string{"vitalidade", "vitalidade", "forca", "resistencia"}
		case "lux":
			seq = []string{"foco", "foco", "agilidade", "resistencia"}
		case "yasuo", "ezreal":
			seq = []string{"forca", "forca", "agilidade", "vitalidade"}
		default:
			seq = []string{"forca", "vitalidade", "resistencia"}
		}
		idx := 0
		for st.PontosAtrib > 0 {
			attr := seq[idx%len(seq)]
			st.Atrib[attr]++
			st.PontosAtrib--
			idx++
			upgraded = true
		}
		b.addLog(fmt.Sprintf("✨ Pontos de atributos distribuídos para classe %s!", champ))
	}

	// 2. Subir habilidades respeitando requisitos de nível (R > Q > W > E)
	if st.Pontos > 0 {
		if st.Ranks == nil {
			st.Ranks = make(map[string]int)
		}
		skills := []string{"R", "Q", "W", "E"}
		deu := true
		for deu && st.Pontos > 0 {
			deu = false
			for _, sk := range skills {
				max := maxRankSkill(sk, st.Nivel)
				if st.Ranks[sk] < max && st.Pontos > 0 {
					st.Ranks[sk]++
					st.Pontos--
					deu = true
					upgraded = true
					break
				}
			}
		}
		if upgraded {
			b.addLog("⚡ Habilidades elevadas de nível com limites válidos!")
		}
	}
	b.mu.Unlock()

	if upgraded {
		b.salvarSave(false)
	}
}

func (b *Bot) pingPresence() {
	var res any
	_ = b.httpCli.PostJSON("/api/presenca", map[string]any{
		"naFonte": false,
	}, &res)
}

func (b *Bot) salvarSave(force bool) {
	b.mu.Lock()
	now := time.Now().UnixMilli()
	b.session.Estado.SalvoEm = now
	b.session.Estado.Relogio = relogioMundial(now)

	// AutoEquip: avalia a mochila e equipa os melhores itens para o campeão
	equipLogs := AutoEquip(&b.session.Estado, b.session.Campeao)

	raw := b.session.RawEstado
	if raw == nil {
		raw = make(map[string]any)
	}
	raw["nivel"] = b.session.Estado.Nivel
	raw["xp"] = b.session.Estado.Exp
	raw["pontos"] = b.session.Estado.Pontos
	raw["pontosAtrib"] = b.session.Estado.PontosAtrib
	if b.session.Estado.Ranks != nil {
		raw["ranks"] = b.session.Estado.Ranks
	}
	if b.session.Estado.Atrib != nil {
		raw["atrib"] = b.session.Estado.Atrib
	}
	raw["ouro"] = b.session.Estado.Ouro
	raw["vida"] = b.session.Estado.Hp
	raw["mana"] = b.session.Estado.Mana
	if b.session.Estado.Local != "" {
		raw["local"] = b.session.Estado.Local
	}
	if b.session.Estado.Mochila != nil {
		raw["mochila"] = b.session.Estado.Mochila
	} else {
		raw["mochila"] = []any{}
	}
	if b.session.Estado.Itens != nil {
		raw["itens"] = b.session.Estado.Itens
	} else {
		raw["itens"] = []any{}
	}
	if len(b.session.Estado.Espolios) > 0 {
		raw["espolios"] = b.session.Estado.Espolios
	}
	raw["totais"] = b.session.Estado.Totais
	raw["tempoJogo"] = b.session.Estado.TempoJogo
	raw["relogio"] = b.session.Estado.Relogio
	raw["salvoEm"] = now
	raw["autoHab"] = false

	if b.status.Extra != nil {
		b.status.Extra["itens"] = b.session.Estado.Itens
		b.status.Extra["mochila"] = b.session.Estado.Mochila
	}

	body := map[string]any{
		"estado": raw,
	}
	b.mu.Unlock()

	for _, l := range equipLogs {
		b.addLog("🛡️ " + l)
	}

	for tent := 0; tent < 2; tent++ {
		var resp map[string]any
		err := b.httpCli.PutJSON("/api/save", body, &resp)
		b.lastSave = time.Now()

		if outra, _ := resp["outraAba"].(bool); outra {
			b.addLog("↩️ outraAba no save: retomando personagem via GET /api/eu...")
			var sess PlayerSession
			if errEu := b.httpCli.GetJSON("/api/eu", &sess); errEu == nil {
				b.mu.Lock()
				b.session = sess
				b.mu.Unlock()
			}
			continue
		}

		if rec, _ := resp["recusado"].(bool); rec {
			b.logMu.Lock()
			b.recusados++
			b.logMu.Unlock()
			b.addLog("⛔ SAVE RECUSADO pelo servidor: " + fmt.Sprint(resp) + " — sincronizando com /api/eu...")
			var sess PlayerSession
			if errEu := b.httpCli.GetJSON("/api/eu", &sess); errEu == nil {
				b.mu.Lock()
				b.session = sess
				b.mu.Unlock()
			}
			return
		}

		if err != nil {
			b.addLog("⚠️ Falha ao salvar: " + err.Error())
			return
		}

		b.addLog(fmt.Sprintf("💾 Save confirmado com sucesso (Nv. %d | Ouro: %d | Kills: %d)",
			b.session.Estado.Nivel, int64(b.session.Estado.Ouro), b.session.Estado.Totais.Abates))
		return
	}
}

func (b *Bot) checkWorldChestsAndSacks() {
	var baus []WorldChest
	if err := b.httpCli.GetJSON("/api/baus", &baus); err == nil {
		for _, bau := range baus {
			if bau.Disponivel {
				var res any
				if err := b.httpCli.PostJSON("/api/bau", map[string]any{"id": bau.ID}, &res); err == nil {
					b.addLog(fmt.Sprintf("🎁 Baú aberto: %s (%s)!", bau.Nome, bau.Local))
				}
			}
		}
	}

	var sacos []WorldSack
	if err := b.httpCli.GetJSON("/api/sacos", &sacos); err == nil {
		for _, saco := range sacos {
			var res any
			if err := b.httpCli.PostJSON("/api/saco/pegar", map[string]any{"id": saco.ID}, &res); err == nil {
				b.addLog(fmt.Sprintf("💰 Saco de loot recolhido em %s!", saco.Local))
			}
		}
	}

	// Baú da Vilemaw
	var vmRes any
	if err := b.httpCli.PostJSON("/api/vilemaw/bau", map[string]any{}, &vmRes); err == nil {
		b.addLog("🕷️ Baú da Vilemaw resgatado com sucesso!")
	}
}

func (b *Bot) claimDailyRewards() {
	var loginRes any
	if err := b.httpCli.PostJSON("/api/login-diario", map[string]any{}, &loginRes); err == nil {
		b.addLog("🎁 Prêmio de login diário resgatado com sucesso!")
	}

	// Missão diária do Rift
	var diariaRes any
	if err := b.httpCli.PostJSON("/api/diaria", map[string]any{}, &diariaRes); err == nil {
		b.addLog("📜 Missão diária do Rift resgatada!")
	}
}

func (b *Bot) GetStatus() engine.AccountStatus {
	b.mu.RLock()
	st := b.status
	b.mu.RUnlock()
	b.logMu.Lock()
	st.Logs = append([]string(nil), b.logs...)
	b.logMu.Unlock()
	return st
}

func (b *Bot) ExecuteAction(action string, payload map[string]any) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch action {
	case "autoHunt":
		b.config.Settings.AutoHunt = !b.config.Settings.AutoHunt
		if b.config.Settings.AutoHunt {
			b.status.Status = "hunting"
			b.addLog("Auto-hunt ativado.")
		} else {
			b.status.Status = "idle"
			b.addLog("Auto-hunt pausado.")
		}
		return map[string]any{"autoHunt": b.config.Settings.AutoHunt}, nil

	case "autoRoute":
		b.config.Settings.AutoRoute = !b.config.Settings.AutoRoute
		return map[string]any{"autoRoute": b.config.Settings.AutoRoute}, nil

	case "healNow":
		go b.healAtFountain()
		return "Retornando à Fonte para curar!", nil

	case "upgradeNow":
		go b.autoUpgradeStatsAndSkills()
		return "Atributos e habilidades aprimorados!", nil

	case "claimDaily":
		go b.claimDailyRewards()
		return "Resgate diário solicitado!", nil

	default:
		return nil, fmt.Errorf("ação desconhecida: %s", action)
	}
}

func (b *Bot) SupplyTicket(ticket, wsURL string) error {
	select {
	case b.ticketChan <- ticket:
	default:
	}
	return nil
}

func (b *Bot) SupplyTurnstileToken(token string) error {
	select {
	case b.turnstileChan <- token:
	default:
	}
	return nil
}
