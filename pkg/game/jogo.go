package game

import "math"

// Dados e fórmulas extraídos do motor do jogo (motor.js / balanco.js).
// Servem para simular a caça em Runeterra de forma fiel ao cliente, para que o
// servidor aceite o /api/save e calcule o bônus de XP em grupo.

const (
	nivelMaxPadrao = 35 // Limite seguro para a party (evita chamar atenção com top level do jogo ~40)
	nivelMaxJogo   = 50 // Limite absoluto do motor original
	nivelMax       = nivelMaxPadrao
	pontosPorNivel = 3
	arenaBonusXp   = 1.3 // motor.js: ARENA = { bonus: 1.3 } (XP extra por jogar no controle)
	cicloDoDia     = 86400.0
	fusoBrasilia   = -3.0
)

// ---------- Tabela de balanço (balanco.js) ----------
type faixaBalanco struct {
	De, Ate    int
	XpUpar     float64
	Xp, Ouro   float64
	Vida, Dano float64
	Drop       float64
}

var balanco = []faixaBalanco{
	{1, 2, 0.04, 1, 1.5, 0.7, 0.65, 3},
	{3, 4, 0.05, 1, 1.45, 0.72, 0.68, 2.8},
	{5, 6, 0.08, 1, 1.4, 0.75, 0.72, 2.6},
	{7, 8, 0.15, 1, 1.35, 0.79, 0.76, 2.4},
	{9, 10, 0.3, 1, 1.3, 0.83, 0.8, 2.2},
	{11, 12, 0.5, 1, 1.25, 0.86, 0.84, 2},
	{13, 14, 0.6, 1, 1.2, 0.89, 0.88, 1.8},
	{15, 16, 0.7, 1, 1.15, 0.92, 0.91, 1.6},
	{17, 18, 0.8, 1, 1.1, 0.95, 0.94, 1.4},
	{19, 20, 0.9, 1, 1.05, 0.98, 0.97, 1.2},
	{21, 60, 1, 1, 1, 1, 1, 1},
}

func ajusteJogo(n int) faixaBalanco {
	for _, f := range balanco {
		if n >= f.De && n <= f.Ate {
			return f
		}
	}
	return balanco[len(balanco)-1]
}

// ---------- Curvas (motor.js: CURVA) ----------
func curvaXP(n int) float64   { return (30 + 7*float64(n)) * ajusteJogo(n).Xp }
func curvaOuro(n int) float64 { return (8 + 3.5*float64(n)) * ajusteJogo(n).Ouro }
func curvaHP(n int) float64 {
	return (150 + 100*float64(n) + float64(n*n)) * ajusteJogo(n).Vida
}

// fatorXp (motor.js:447): monstro muito abaixo do nível rende menos (−10%/nível, mín. 10%).
func fatorXp(nivelJogador, nivelMonstro int) float64 {
	dif := nivelJogador - nivelMonstro
	if dif > 5 {
		v := 1 - 0.1*float64(dif-5)
		if v < 0.1 {
			v = 0.1
		}
		return v
	}
	return 1
}

// tres arredonda para 3 algarismos significativos (motor.js:454).
func tres(v float64) float64 {
	if v <= 0 {
		return 0
	}
	p := math.Pow(10, math.Max(0, math.Floor(math.Log10(v))-2))
	return math.Round(v/p) * p
}

// xpParaNivel (motor.js:455): XP necessária para sair do nível n.
func xpParaNivel(n int) float64 {
	if n < 1 {
		n = 1
	}
	nm1 := float64(n - 1)
	ritmo := 43000 + 4345*nm1 + 17.9*nm1*nm1
	return tres(ritmo * 0.4433 * math.Pow(1.1715, nm1) * ajusteJogo(n).XpUpar)
}

// relogioMundial devolve o relógio do mundo em segundos (motor.js:1121).
func relogioMundial(ms int64) float64 {
	t := math.Mod(float64(ms)/1000.0+fusoBrasilia*3600, cicloDoDia)
	if t < 0 {
		t += cicloDoDia
	}
	return t
}

// ---------- Monstros (motor.js:302) ----------
type monDef struct {
	HP       float64
	XP, Ouro float64
	Chefe    bool
}

var monstros = map[string]monDef{
	"Tropa Corpo a Corpo":     {1, 1, 1, false},
	"Tropa Mago":              {0.7, 0.9, 0.9, false},
	"Tropa de Cerco":          {1.8, 1.6, 1.8, false},
	"Lobo Gigante":            {1.1, 1.1, 1, false},
	"Krug Anciã":              {1.5, 1.2, 1.2, false},
	"Sentinela Azul":          {2.2, 2, 2.2, false},
	"Brambleback Vermelho":    {2.2, 2, 2.2, false},
	"Arraia do Rio":           {1.5, 1.2, 1.1, false},
	"Larva do Vazio":          {1.1, 1.1, 1, false},
	"Dragão Ancião (filhote)": {1.6, 1.5, 1.4, false},
	"Criatura do Vazio":       {1.3, 1.4, 1.3, false},
	"Arauto do Vale":          {10, 12, 12, true},
	"Dragão Infernal":         {12, 14, 14, true},
	"Barão Nashor":            {16, 20, 20, true},
}

// ---------- Zonas de caça do Rift (motor.js: LOCAIS) ----------
type spawnDef struct {
	Nome string
	Peso int
}

type zonaCaca struct {
	ID     string
	Faixa  [2]int
	NM     [2]int
	Spawns []spawnDef
}

var zonasCaca = []zonaCaca{
	{"rota-inf", [2]int{1, 10}, [2]int{1, 6}, []spawnDef{{"Tropa Corpo a Corpo", 3}, {"Tropa Mago", 2}}},
	{"rota-sup", [2]int{5, 15}, [2]int{5, 13}, []spawnDef{{"Tropa Corpo a Corpo", 3}, {"Tropa Mago", 2}, {"Tropa de Cerco", 1}}},
	{"lobos", [2]int{10, 20}, [2]int{10, 18}, []spawnDef{{"Lobo Gigante", 1}}},
	{"azul", [2]int{15, 25}, [2]int{15, 23}, []spawnDef{{"Krug Anciã", 2}, {"Sentinela Azul", 1}}},
	{"vermelho", [2]int{20, 30}, [2]int{20, 28}, []spawnDef{{"Brambleback Vermelho", 1}, {"Lobo Gigante", 1}, {"Krug Anciã", 1}}},
	{"rio", [2]int{25, 35}, [2]int{25, 33}, []spawnDef{{"Arraia do Rio", 2}, {"Larva do Vazio", 2}}},
	{"dragao", [2]int{30, 40}, [2]int{30, 38}, []spawnDef{{"Dragão Ancião (filhote)", 2}, {"Larva do Vazio", 1}}},
	{"barao", [2]int{40, 50}, [2]int{40, 48}, []spawnDef{{"Criatura do Vazio", 2}, {"Dragão Ancião (filhote)", 1}}},
}

// localDaArena devolve a zona de caça de Runeterra para o nível (motor.js:440):
// a mais alta cuja faixa ele já alcançou.
func localDaArena(nivel int) zonaCaca {
	melhor := zonasCaca[0]
	for _, z := range zonasCaca {
		if nivel >= z.Faixa[0] {
			melhor = z
		}
	}
	return melhor
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ganharXpEstado aplica XP e sobe de nível (motor.js:2834), respeitando o teto de nível (maxLvl opcional).
func ganharXpEstado(st *PlayerState, xp float64, maxLvl ...int) {
	limite := nivelMaxPadrao
	if len(maxLvl) > 0 && maxLvl[0] > 0 {
		limite = maxLvl[0]
	}
	if st.Nivel >= limite || xp <= 0 {
		return
	}
	st.Exp += xp
	for st.Nivel < limite && st.Exp >= xpParaNivel(st.Nivel) {
		st.Exp -= xpParaNivel(st.Nivel)
		st.Nivel++
		st.Pontos++
		st.PontosAtrib += pontosPorNivel
	}
	if st.Nivel >= limite {
		st.Exp = 0
	}
}
