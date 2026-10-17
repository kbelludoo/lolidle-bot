package game

import (
	"fmt"
	"math/rand"
	"sync"
)

// inimigoArena é um monstro que a conta está caçando em Runeterra.
// HP/HPMax são o "tempo até morrer" (em segundos): o campo v do pacote {t:"pos"}
// (jogo.js:1277) é hp/hpMax, que decresce até o abate.
type inimigoArena struct {
	ID       string
	Nome     string
	Nivel    int
	XpMult   float64
	OuroMult float64
	X, Y     float64
	HP       float64
	HPMax    float64
}

// hunter simula a caça em Runeterra: gera grupos de monstros perto do campeão,
// derruba a vida ao longo do tempo e devolve os abates para reportar em {t:"abateGrupo"}.
type hunter struct {
	mu      sync.Mutex
	rng     *rand.Rand
	ini     []*inimigoArena
	proxID  int
	spawnCd float64
}

func newHunter(seed int64) *hunter {
	return &hunter{rng: rand.New(rand.NewSource(seed)), proxID: 1}
}

// spawn gera um grupo de 2 a 4 monstros da zona, com níveis na faixa dela.
func (h *hunter) spawn(nivel int, z zonaCaca, px, py, killsPerMin float64) {
	totalPeso := 0
	for _, s := range z.Spawns {
		totalPeso += s.Peso
	}
	qtd := 2 + h.rng.Intn(3)
	intervalo := 60.0 / killsPerMin
	for i := 0; i < qtd; i++ {
		nome := sortearSpawn(h.rng, z.Spawns, totalPeso)
		def := monstros[nome]
		nv := clampInt(nivel-2+h.rng.Intn(4), z.NM[0], z.NM[1])
		tdie := intervalo * (0.7 + h.rng.Float64()*0.6)
		h.ini = append(h.ini, &inimigoArena{
			ID:       fmt.Sprintf("m%d", h.proxID),
			Nome:     nome,
			Nivel:    nv,
			XpMult:   def.XP,
			OuroMult: def.Ouro,
			X:        round2(px + (h.rng.Float64()-0.5)*6),
			Y:        round2(py + (h.rng.Float64()-0.5)*6),
			HP:       tdie,
			HPMax:    tdie,
		})
		h.proxID++
	}
}

// tick avança a simulação por dt segundos e devolve os monstros abatidos neste passo.
func (h *hunter) tick(dt float64, nivel int, z zonaCaca, px, py, killsPerMin float64) []*inimigoArena {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.spawnCd > 0 {
		h.spawnCd -= dt
	}

	vivos := 0
	for _, i := range h.ini {
		if i.HP > 0 {
			vivos++
		}
	}
	if vivos == 0 && h.spawnCd <= 0 {
		h.ini = h.ini[:0]
		h.spawn(nivel, z, px, py, killsPerMin)
	}

	var mortos []*inimigoArena
	for _, i := range h.ini {
		if i.HP <= 0 {
			continue
		}
		i.HP -= dt
		if i.HP <= 0 {
			i.HP = 0
			mortos = append(mortos, i)
		}
	}
	if len(mortos) > 0 {
		todosMortos := true
		for _, i := range h.ini {
			if i.HP > 0 {
				todosMortos = false
				break
			}
		}
		if todosMortos {
			h.spawnCd = 1.5
		}
	}
	return mortos
}

// posMons monta o campo mons do pacote {t:"pos"} (até 6 monstros vivos), no
// formato do cliente: {i,n,nv,x,y,v}.
func (h *hunter) posMons() []any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]any, 0, 6)
	for _, i := range h.ini {
		if i.HP <= 0 {
			continue
		}
		v := 1.0
		if i.HPMax > 0 {
			v = i.HP / i.HPMax
		}
		out = append(out, map[string]any{
			"i": i.ID, "n": i.Nome, "nv": i.Nivel,
			"x": i.X, "y": i.Y, "v": round2(v),
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func sortearSpawn(rng *rand.Rand, spawns []spawnDef, total int) string {
	if total <= 0 {
		return spawns[0].Nome
	}
	x := rng.Intn(total)
	for _, s := range spawns {
		x -= s.Peso
		if x < 0 {
			return s.Nome
		}
	}
	return spawns[0].Nome
}
