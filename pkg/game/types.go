package game

import "encoding/json"

// AccountConfig modelo de configuração lido do accounts.json
type AccountConfig struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Enabled     bool            `json:"enabled"`
	BindIP      string          `json:"bindIP,omitempty"` // IP local da interface (ex: "10.0.0.X" na VPS)
	Proxy       string          `json:"proxy,omitempty"`  // Proxy dedicado (ex: "http://user:pass@ip:port" ou "socks5://ip:port")
	Credentials GameCredentials `json:"credentials"`
	Settings    GameSettings    `json:"settings"`
}

type GameCredentials struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
	SessionCookie string `json:"sessionCookie"`
	Token         string `json:"token"`
}

type GameSettings struct {
	HuntTarget  string `json:"huntTarget"`  // ex: "auto", "rota-inf", "rota-sup", "lobos", "azul", "vermelho", "rio", "dragao", "barao"
	AutoHunt    bool   `json:"autoHunt"`    // Caça contínua
	AutoRoute   bool   `json:"autoRoute"`   // Troca de mapa inteligente por nível
	AutoSell    bool   `json:"autoSell"`    // Vende espólios e lixo na Fonte
	AutoHeal    bool   `json:"autoHeal"`    // Retorna à Fonte se HP estiver crítico (< 30%)
	AutoUpgrade bool   `json:"autoUpgrade"` // Distribui atributos e habilidades
	AutoDaily   bool   `json:"autoDaily"`   // Reivindica missões e login diário
	AutoChests  bool   `json:"autoChests"`  // Abre baús e pega sacos de loot
	AutoSacks   bool   `json:"autoSacks"`   // Recolhe sacos de loot caídos no chão
	AutoParty   bool   `json:"autoParty"`   // Gerencia grupo automático (+15% a +45% XP)
	PartyLeader string `json:"partyLeader"` // ID ou Nome do líder do grupo (vazio = esta conta é o líder)
	Lure        int    `json:"lure"`
	// ArenaWalk mantém a conta no mundo aberto (Runeterra), entrando no grupo e transmitindo a
	// posição via WebSocket para ficar junta das outras contas (bônus de XP em grupo).
	ArenaWalk bool    `json:"arenaWalk"`
	ArenaX    float64 `json:"arenaX,omitempty"` // Posição base em Runeterra (0 = padrão)
	ArenaY    float64 `json:"arenaY,omitempty"` // Posição base em Runeterra (0 = padrão)
	// KillRate é quantos monstros por minuto o bot simula abater em Runeterra (0 = padrão).
	KillRate float64 `json:"killRate,omitempty"`
	// MaxLevel define o nível máximo fixo do personagem (0 = dinâmico: MaxLevelOffset abaixo do topo).
	MaxLevel int `json:"maxLevel,omitempty"`
	// MaxLevelOffset é a distância de níveis abaixo do Top 1 do servidor (padrão: 5).
	MaxLevelOffset int `json:"maxLevelOffset,omitempty"`
	// StealthLevel é o nível a partir do qual o XP/h é reduzido para discrição (padrão: 30).
	StealthLevel int `json:"stealthLevel,omitempty"`
	// StealthKillRate é a taxa reduzida de abates/min no modo stealth (padrão: 50% do KillRate).
	StealthKillRate float64 `json:"stealthKillRate,omitempty"`
	// PartyMembers são os nicks EM JOGO que o líder convida repetidamente para o grupo.
	// Só é usado pela conta líder (partyLeader vazio ou igual ao próprio nick).
	PartyMembers []string `json:"partyMembers,omitempty"`
}

// PlayerSession representa o retorno de GET /api/eu
type PlayerSession struct {
	Nome      string         `json:"nome"`
	Campeao   string         `json:"campeao"`
	Email     string         `json:"email"`
	Convidado bool           `json:"convidado"`
	Diamantes int64          `json:"diamantes"`
	VipAte    int64          `json:"vipAte"`
	Estado    PlayerState    `json:"estado"`
	RawEstado map[string]any `json:"-"`
}

func (s *PlayerSession) UnmarshalJSON(data []byte) error {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}
	if v, ok := rawMap["nome"]; ok {
		_ = json.Unmarshal(v, &s.Nome)
	}
	if v, ok := rawMap["campeao"]; ok {
		_ = json.Unmarshal(v, &s.Campeao)
	}
	if v, ok := rawMap["email"]; ok {
		_ = json.Unmarshal(v, &s.Email)
	}
	if v, ok := rawMap["convidado"]; ok {
		_ = json.Unmarshal(v, &s.Convidado)
	}
	if v, ok := rawMap["diamantes"]; ok {
		_ = json.Unmarshal(v, &s.Diamantes)
	}
	if v, ok := rawMap["vipAte"]; ok {
		_ = json.Unmarshal(v, &s.VipAte)
	}
	if v, ok := rawMap["estado"]; ok {
		_ = json.Unmarshal(v, &s.Estado)
		_ = json.Unmarshal(v, &s.RawEstado)
	}
	return nil
}

type PlayerState struct {
	Nivel       int            `json:"nivel"`
	Exp         float64        `json:"xp"`
	Pontos      int            `json:"pontos"`
	PontosAtrib int            `json:"pontosAtrib"`
	Ranks       map[string]int `json:"ranks"`
	Atrib       map[string]int `json:"atrib"`
	Ouro        float64        `json:"ouro"`
	Hp          float64        `json:"vida"`
	Mana        float64        `json:"mana"`
	Local       string         `json:"local"`
	Area        string         `json:"area,omitempty"`
	Mochila     []any          `json:"mochila,omitempty"`
	Itens       []any          `json:"itens,omitempty"`
	Espolios    map[string]int `json:"espolios"`
	Totais      PlayerTotais   `json:"totais"`
	Rev         int64          `json:"rev"`
	SalvoEm     int64          `json:"salvoEm"`
	TempoJogo   float64        `json:"tempoJogo"`
	Relogio     float64        `json:"relogio"`
}

type PlayerTotais struct {
	Abates     int            `json:"abates"`
	Ouro       float64        `json:"ouro"`
	Drops      int            `json:"drops"`
	Mortes     int            `json:"mortes"`
	Chefes     int            `json:"chefes"`
	Furiosos   int            `json:"furiosos"`
	PorMonstro map[string]int `json:"porMonstro,omitempty"`
}

// WorldChest representa baús no mapa (GET /api/baus)
type WorldChest struct {
	ID         string `json:"id"`
	Nome       string `json:"nome"`
	Local      string `json:"local"`
	Disponivel bool   `json:"disponivel"`
}

// WorldSack representa sacos com itens caídos no chão (GET /api/sacos)
type WorldSack struct {
	ID    string `json:"id"`
	Local string `json:"local"`
	Dono  string `json:"dono"`
	Itens []any  `json:"itens"`
}

type WSMessage struct {
	Type  string `json:"t"`
	Token string `json:"token,omitempty"`
	Msg   string `json:"msg,omitempty"`
	Data  any    `json:"data,omitempty"`
}
