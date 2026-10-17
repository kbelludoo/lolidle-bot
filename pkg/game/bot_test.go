package game

import (
	"math"
	"testing"
	"time"
)

// O servidor aceita andar até 5,1 tiles/s (arena.js:18: 3,4 × 1,5). O bando nunca pode passar disso.
const velocidadeMaxServidor = 5.1

func testBots(nomes ...string) []*Bot {
	bots := make([]*Bot, 0, len(nomes))
	for _, n := range nomes {
		b := NewBot(AccountConfig{ID: n, Name: n})
		b.arenaX, b.arenaY = arenaBaseX, arenaBaseY
		bots = append(bots, b)
	}
	return bots
}

func TestArenaOffsetEhDeterministicoELimitado(t *testing.T) {
	nomes := []string{"kbelludoo", "kbelludool", "LrN", "maumau"}
	vistos := map[[2]float64]string{}
	for _, n := range nomes {
		dx1, dy1 := arenaOffset(n)
		dx2, dy2 := arenaOffset(n)
		if dx1 != dx2 || dy1 != dy2 {
			t.Fatalf("arenaOffset(%q) não é determinístico", n)
		}
		if math.Abs(dx1) > 2.5 || math.Abs(dy1) > 2.5 {
			t.Fatalf("arenaOffset(%q) = (%.2f,%.2f) fora do limite desejado", n, dx1, dy1)
		}
		chave := [2]float64{dx1, dy1}
		if outro, ok := vistos[chave]; ok {
			t.Fatalf("offsets de %q e %q colidem em (%.2f,%.2f)", outro, n, dx1, dy1)
		}
		vistos[chave] = n
	}
}

func TestPatrolMantemBandoJuntoEDentroDaZona(t *testing.T) {
	bots := testBots("kbelludoo", "kbelludool", "LrN", "maumau")
	// pontos no tempo (deslocamento do startTime simula o tempo passando)
	for _, dt := range []time.Duration{0, 2 * time.Second, 17 * time.Second, 90 * time.Second, 3600 * time.Second} {
		at := time.Now().Add(-dt)
		type pt struct{ x, y float64 }
		pts := make(map[string]pt, len(bots))
		for _, b := range bots {
			b.startTime = at
			x, y := b.patrolPos()
			pts[b.Name()] = pt{x, y}
			if d := math.Hypot(x-arenaBaseX, y-arenaBaseY); d > arenaRaio+4 {
				t.Fatalf("em dt=%s, %s a %.2f tiles da base (perto demais do limite)", dt, b.Name(), d)
			}
		}
		for _, a := range bots {
			for _, c := range bots {
				if a == c {
					continue
				}
				pa, pc := pts[a.Name()], pts[c.Name()]
				if d := math.Hypot(pa.x-pc.x, pa.y-pc.y); d >= 15 {
					t.Fatalf("em dt=%s, %s e %s a %.2f tiles (fora dos 15 do grupo)", dt, a.Name(), c.Name(), d)
				}
			}
		}
	}
}

func TestVelocidadeDoPatrulhamentoDentroDoLimiteDoServidor(t *testing.T) {
	if v := arenaRaio * arenaVel; v > velocidadeMaxServidor {
		t.Fatalf("velocidade do patrulhamento %.2f tiles/s acima do limite %.2f", v, velocidadeMaxServidor)
	}
	b := testBots("kbelludoo")[0]
	at := time.Now()
	b.startTime = at
	x0, y0 := b.patrolPos()
	// 0,1 s depois (o intervalo entre pacotes de posição)
	b.startTime = at.Add(-100 * time.Millisecond)
	x1, y1 := b.patrolPos()
	if v := math.Hypot(x1-x0, y1-y0) / 0.1; v > velocidadeMaxServidor {
		t.Fatalf("variação brusca de %.2f tiles/s acima do limite do servidor", v)
	}
}

func TestPosMessageTemFormatoDoCliente(t *testing.T) {
	b := testBots("kbelludoo")[0]
	m := b.posMessage(12.34)

	if m["t"] != "pos" {
		t.Fatalf("t = %v, queria \"pos\"", m["t"])
	}
	if m["anda"] != true || m["atk"] != false {
		t.Fatalf("anda/atk = %v/%v, queria true/false", m["anda"], m["atk"])
	}
	if mons, ok := m["mons"].([]any); !ok || len(mons) != 0 {
		t.Fatalf("mons = %v, queria []any{}", m["mons"])
	}
	x, ok := m["x"].(float64)
	if !ok {
		t.Fatalf("x não é float64: %T", m["x"])
	}
	if d := math.Hypot(x-arenaBaseX, m["y"].(float64)-arenaBaseY); d > arenaRaio+4 {
		t.Fatalf("x/y fora da zona do bando (d=%.2f)", d)
	}
	if m["tj"] != 12.34 {
		t.Fatalf("tj = %v, queria 12.34", m["tj"])
	}
}

func TestRound2EToFloat(t *testing.T) {
	if round2(1.239) != 1.24 {
		t.Fatalf("round2(1.239) = %v", round2(1.239))
	}
	if v, ok := toFloat(float64(3.5)); !ok || v != 3.5 {
		t.Fatalf("toFloat float64 falhou: %v %v", v, ok)
	}
	if v, ok := toFloat(int(7)); !ok || v != 7 {
		t.Fatalf("toFloat int falhou: %v %v", v, ok)
	}
	if _, ok := toFloat("x"); ok {
		t.Fatal("toFloat string devia falhar")
	}
}

func TestApplyPartyRegistraBonusEMembros(t *testing.T) {
	b := NewBot(AccountConfig{ID: "kbelludool", Name: "kbelludool"})
	b.applyParty(map[string]any{
		"bonus": 1.30,
		"membros": []any{
			map[string]any{"n": "kbelludoo", "lider": true, "perto": true, "on": true, "v": 1.0},
			map[string]any{"n": "LrN", "perto": false, "on": true, "v": 0.8},
		},
	})
	if !b.inParty {
		t.Fatal("devia estar em grupo")
	}
	if len(b.partyNames) != 2 {
		t.Fatalf("partyNames = %v, queria 2 membros", b.partyNames)
	}
	if got := b.status.Extra["partyBonusXp"]; got != 1.30 {
		t.Fatalf("partyBonusXp = %v, queria 1.30", got)
	}
	b.clearParty()
	if b.inParty || len(b.partyNames) != 0 {
		t.Fatal("clearParty devia limpar o grupo")
	}
}

func TestGroupSendSemSocketNaoPanica(t *testing.T) {
	b := NewBot(AccountConfig{ID: "x", Name: "x"})
	b.groupSend("convidar", "LrN") // socket nil: não deve entrar em pânico
}

func TestIsPartyLeader(t *testing.T) {
	lider := NewBot(AccountConfig{ID: "kbelludoo", Name: "kbelludoo"})
	if !lider.isPartyLeader() {
		t.Fatal("partyLeader vazio devia indicar líder")
	}
	membro := NewBot(AccountConfig{ID: "conta2", Name: "kbelludool", Settings: GameSettings{PartyLeader: "kbelludoo"}})
	if membro.isPartyLeader() {
		t.Fatal("kbelludool não é o líder (partyLeader = kbelludoo)")
	}
}

func TestMaxRankSkillLimitaPorNivel(t *testing.T) {
	// Nível 3: R=0, Q/W/E máx ceil(3/2)=2
	if r := maxRankSkill("R", 3); r != 0 {
		t.Fatalf("R em nível 3 devia ser 0, deu %d", r)
	}
	if q := maxRankSkill("Q", 3); q != 2 {
		t.Fatalf("Q em nível 3 devia ser 2, deu %d", q)
	}
	// Nível 6: R=1, Q/W/E máx ceil(6/2)=3
	if r := maxRankSkill("R", 6); r != 1 {
		t.Fatalf("R em nível 6 devia ser 1, deu %d", r)
	}
	if q := maxRankSkill("Q", 6); q != 3 {
		t.Fatalf("Q em nível 6 devia ser 3, deu %d", q)
	}
	// Nível 11: R=2
	if r := maxRankSkill("R", 11); r != 2 {
		t.Fatalf("R em nível 11 devia ser 2, deu %d", r)
	}
}

func TestAutoUpgradeNaoEstouraOrcamento(t *testing.T) {
	b := NewBot(AccountConfig{ID: "test", Name: "test"})
	b.session.Campeao = "darius"
	b.session.Estado.Nivel = 4
	b.session.Estado.PontosAtrib = 2
	b.session.Estado.Pontos = 1
	b.session.Estado.Atrib = make(map[string]int)
	b.session.Estado.Ranks = make(map[string]int)

	b.autoUpgradeStatsAndSkills()

	if b.session.Estado.PontosAtrib != 0 {
		t.Fatalf("pontosAtrib devia ser 0, deu %d", b.session.Estado.PontosAtrib)
	}
	totalAtrib := 0
	for _, v := range b.session.Estado.Atrib {
		totalAtrib += v
	}
	if totalAtrib != 2 {
		t.Fatalf("gastou mais pontos do que tinha: totalAtrib = %d, queria 2", totalAtrib)
	}

	// Nível 4 não pode ter R
	if b.session.Estado.Ranks["R"] != 0 {
		t.Fatalf("não pode subir R no nível 4! Ranks: %v", b.session.Estado.Ranks)
	}
	if b.session.Estado.Pontos != 0 {
		t.Fatalf("pontos de habilidade devia ser 0, deu %d", b.session.Estado.Pontos)
	}
}
