package game

import (
	"testing"
)

func TestHunterGeraGrupoEAbate(t *testing.T) {
	h := newHunter(1)
	z := localDaArena(30)
	// primeiro tick gera um grupo de 2 a 4 monstros
	h.tick(0.1, 30, z, 100, 100, 6)
	ini := h.posMons()
	if len(ini) < 2 || len(ini) > 6 {
		t.Fatalf("grupo inicial = %d monstros, queria entre 2 e 4", len(ini))
	}
	for _, raw := range ini {
		m := raw.(map[string]any)
		if m["i"].(string) == "" {
			t.Fatal("monstro sem id")
		}
		if _, ok := m["n"].(string); !ok {
			t.Fatal("monstro sem nome")
		}
		if v, ok := m["v"].(float64); !ok || v <= 0 || v > 1 {
			t.Fatalf("v inválido: %v", m["v"])
		}
	}
}

func TestHunterAbateAoLongoDoTempo(t *testing.T) {
	h := newHunter(2)
	z := localDaArena(10)
	abates := 0
	ids := map[string]bool{}
	for i := 0; i < 1200; i++ { // 120 s a passos de 0,1 s
		for _, m := range h.tick(0.1, 10, z, 0, 0, 60) {
			abates++
			if ids[m.ID] {
				t.Fatalf("id de abate repetido: %s", m.ID)
			}
			ids[m.ID] = true
			if m.Nivel < z.NM[0] || m.Nivel > z.NM[1] {
				t.Fatalf("nível do monstro %d fora da faixa %v", m.Nivel, z.NM)
			}
		}
	}
	// a 60 abates/min por 120 s, algo entre 40 e 260 é plausível (com respawn e grupos)
	if abates < 40 || abates > 260 {
		t.Fatalf("abates em 120 s = %d, fora do esperado", abates)
	}
}

func TestBotPosMessageIncluiMonstros(t *testing.T) {
	b := testBots("kbelludoo")[0]
	b.hunt = newHunter(1)
	b.hunt.tick(0.1, 30, localDaArena(30), arenaBaseX, arenaBaseY, 6)
	m := b.posMessage(1.0)
	mons, ok := m["mons"].([]any)
	if !ok || len(mons) == 0 {
		t.Fatalf("mons = %v, queria monstros vivos", m["mons"])
	}
	if m["atk"] != true {
		t.Fatalf("atk = %v, queria true com monstros vivos", m["atk"])
	}
}
