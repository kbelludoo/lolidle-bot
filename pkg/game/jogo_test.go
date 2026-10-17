package game

import (
	"fmt"
	"testing"
)

func TestXpParaNivelCresceEConfere(t *testing.T) {
	anterior := 0.0
	for n := 1; n <= 49; n++ {
		v := xpParaNivel(n)
		if v <= anterior {
			t.Fatalf("xpParaNivel(%d)=%.0f não cresce (anterior %.0f)", n, v, anterior)
		}
		anterior = v
	}
	// valor exato do nível 1 (motor.js: RITMO_XP(1)=43000, ajuste(1).xpUpar=0,04)
	if got := xpParaNivel(1); got != 762 {
		t.Fatalf("xpParaNivel(1) = %.0f, queria 762", got)
	}
}

func TestFatorXpPenalizaMonstroBaixo(t *testing.T) {
	if fatorXp(20, 20) != 1 || fatorXp(20, 16) != 1 {
		t.Fatal("monstro a até 5 níveis abaixo devia render XP cheia")
	}
	if got := fatorXp(20, 10); got != 0.5 {
		t.Fatalf("fatorXp(20,10) = %.2f, queria 0,5", got)
	}
	if got := fatorXp(50, 1); got != 0.1 {
		t.Fatalf("fatorXp(50,1) = %.2f, queria o mínimo 0,1", got)
	}
}

func TestLocalDaArenaEscolheZonaPeloNivel(t *testing.T) {
	casos := map[int]string{1: "rota-inf", 7: "rota-sup", 12: "lobos", 18: "azul", 24: "vermelho", 29: "rio", 30: "dragao", 35: "dragao", 40: "barao", 50: "barao"}
	for nivel, quer := range casos {
		if got := localDaArena(nivel).ID; got != quer {
			t.Fatalf("localDaArena(%d) = %s, queria %s", nivel, got, quer)
		}
	}
}

func TestGanharXpEstadoSobeDeNivel(t *testing.T) {
	st := &PlayerState{Nivel: 1}
	precisa := xpParaNivel(1)
	ganharXpEstado(st, precisa+5)
	if st.Nivel != 2 {
		t.Fatalf("nível = %d, queria 2", st.Nivel)
	}
	if st.Exp != 5 {
		t.Fatalf("exp = %.1f, queria 5", st.Exp)
	}
	if st.Pontos != 1 || st.PontosAtrib != pontosPorNivel {
		t.Fatalf("pontos=%d pontosAtrib=%d", st.Pontos, st.PontosAtrib)
	}
}

func TestGanharXpEstadoRespeitaLimite35(t *testing.T) {
	// Nível 34 prestes a subir: deve parar em 35 com Exp zerada
	st := &PlayerState{Nivel: 34, Exp: 0}
	ganharXpEstado(st, 999_999_999) // XP astronômico
	if st.Nivel != 35 {
		t.Fatalf("nível = %d, queria exatamente 35 (teto de segurança da party)", st.Nivel)
	}
	if st.Exp != 0 {
		t.Fatalf("exp no nível 35 deve zerar como motor.js, veio %.1f", st.Exp)
	}

	// Não ganha mais XP no nível 35
	ganharXpEstado(st, 500_000)
	if st.Nivel != 35 || st.Exp != 0 {
		t.Fatalf("ganhou XP acima do limite 35: nível=%d exp=%.1f", st.Nivel, st.Exp)
	}
}

func TestGanharXpEstadoRespeitaLimiteCustom(t *testing.T) {
	st := &PlayerState{Nivel: 10, Exp: 0}
	ganharXpEstado(st, 999_999_999, 20)
	if st.Nivel != 20 {
		t.Fatalf("nível = %d, queria limite customizado 20", st.Nivel)
	}
}

func TestRelogioMundialEstaNaFaixaDoDia(t *testing.T) {
	r := relogioMundial(1_700_000_000_000)
	if r < 0 || r >= cicloDoDia {
		t.Fatalf("relogioMundial fora de [0,86400): %.0f", r)
	}
}

func TestTabelaProgressao(t *testing.T) {
	taxaHora := 750000.0 // média real medida (~750k exp/h)
	totalXP := 0.0
	t.Logf("\n%-6s %-12s %-12s %-15s %-15s\n", "Nível", "XP do Nível", "XP Total", "Tempo no Nível", "Tempo Acumulado")
	for n := 1; n < nivelMaxJogo; n++ {
		precisa := xpParaNivel(n)
		totalXP += precisa
		horasNivel := precisa / taxaHora
		horasAcum := totalXP / taxaHora
		if n >= 10 && (n <= 25 || n%5 == 0 || n == 49) {
			t.Logf("%-6d %-12.0f %-12.0f %-15s %-15s\n",
				n+1, precisa, totalXP, formatTempo(horasNivel), formatTempo(horasAcum))
		}
	}
}

func formatTempo(horas float64) string {
	minutos := int(horas * 60)
	h := minutos / 60
	m := minutos % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dmin", h, m)
	}
	seg := int(horas*3600) % 60
	if m > 0 {
		return fmt.Sprintf("%dmin %02ds", m, seg)
	}
	return fmt.Sprintf("%ds", seg)
}

