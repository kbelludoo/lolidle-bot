package game

import (
	"testing"
)

func TestParseItem(t *testing.T) {
	cases := []struct {
		input  string
		base   string
		refino int
		qual   string
	}{
		{"1029~C", "1029", 0, "C"},
		{"3071+2~S", "3071", 2, "S"},
		{"3006+5~SS", "3006", 5, "SS"},
		{"1001", "1001", 0, "B"},
		{"3068~?", "3068", 0, "?"},
		{"3031+1", "3031", 1, "B"},
	}

	for _, c := range cases {
		p := ParseItem(c.input)
		if p.BaseID != c.base || p.Refino != c.refino || p.Qualidade != c.qual {
			t.Errorf("ParseItem(%q) = %+v; want base=%s, refino=%d, qual=%s",
				c.input, p, c.base, c.refino, c.qual)
		}
	}
}

func TestScoreItemAffinities(t *testing.T) {
	// Lux deve preferir Rabadon (3089) sobre Gume do Infinito (3031)
	scLuxRabadon := ScoreItem("lux", "3089~B")
	scLuxGume := ScoreItem("lux", "3031~B")
	if scLuxRabadon <= scLuxGume {
		t.Errorf("Lux deveria preferir Rabadon sobre Gume: %.1f <= %.1f", scLuxRabadon, scLuxGume)
	}

	// Yasuo deve preferir Gume (3031) sobre Rabadon (3089)
	scYasuoGume := ScoreItem("yasuo", "3031~B")
	scYasuoRabadon := ScoreItem("yasuo", "3089~B")
	if scYasuoGume <= scYasuoRabadon {
		t.Errorf("Yasuo deveria preferir Gume sobre Rabadon: %.1f <= %.1f", scYasuoGume, scYasuoRabadon)
	}

	// Taric deve preferir Fogo Solar (3068) sobre Gume ou Rabadon
	scTaricSunfire := ScoreItem("taric", "3068~B")
	scTaricRabadon := ScoreItem("taric", "3089~B")
	if scTaricSunfire <= scTaricRabadon {
		t.Errorf("Taric deveria preferir Fogo Solar sobre Rabadon: %.1f <= %.1f", scTaricSunfire, scTaricRabadon)
	}
}

func TestAutoEquipBoots(t *testing.T) {
	st := &PlayerState{
		Itens:   []any{},
		Mochila: []any{"1001~C", "3006~S"},
	}

	logs := AutoEquip(st, "yasuo")
	if len(logs) == 0 {
		t.Fatalf("esperava logs de equipamento")
	}

	// Deve ter equipado as Grevas do Berserker (3006~S)
	tem3006 := false
	for _, it := range st.Itens {
		if it == "3006~S" {
			tem3006 = true
		}
	}
	if !tem3006 {
		t.Errorf("esperava 3006~S equipado, encontrou: %v", st.Itens)
	}
}

func TestAutoEquipRegularSlotsMax4(t *testing.T) {
	st := &PlayerState{
		Itens: []any{"3006~B"}, // 1 bota
		Mochila: []any{
			"1029~C",
			"1028~B",
			"1036~A",
			"1042~S",
			"3031~S", // Lendário melhor
		},
	}

	AutoEquip(st, "yasuo")

	// Deve ter exatamente 1 bota + 4 regulares = 5 itens
	if len(st.Itens) != 5 {
		t.Fatalf("esperava 5 itens equipados, obteve: %d (%v)", len(st.Itens), st.Itens)
	}

	// Gume do Infinito (3031~S) deve estar obrigatoriamente equipado
	tem3031 := false
	for _, it := range st.Itens {
		if it == "3031~S" {
			tem3031 = true
		}
	}
	if !tem3031 {
		t.Errorf("3031~S não foi equipado no lugar do pior item! Itens: %v", st.Itens)
	}
}

func TestAutoEquipNaoDuplicaLendario(t *testing.T) {
	st := &PlayerState{
		Itens:   []any{"3031~B"},
		Mochila: []any{"3031~C"},
	}

	AutoEquip(st, "yasuo")

	// Não deve duplicar 3031
	count3031 := 0
	for _, it := range st.Itens {
		if ParseItem(it.(string)).BaseID == "3031" {
			count3031++
		}
	}
	if count3031 > 1 {
		t.Errorf("duplicou lendário 3031: %v", st.Itens)
	}
}
