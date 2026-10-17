// Comando de teste de limite: sobe os abates/min de 1 em 1 e para na primeira
// recusa do servidor, mostrando a última taxa aceita.
//
// Uso (na VPS, onde existem os bindIP 10.0.0.x):
//
//	ACCOUNT=kbelludoo RATE_START=1 RATE_END=40 RATE_STEP=1 SEGUNDOS=45 go run ./cmd/limite
//
// Variáveis: ACCOUNT (id da conta; padrão = primeira habilitada), RATE_START,
// RATE_END, RATE_STEP, SEGUNDOS (por taxa).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"bot-template-go/pkg/game"
)

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func main() {
	content, err := os.ReadFile("accounts.json")
	if err != nil {
		log.Fatalf("accounts.json: %v", err)
	}
	var cfgs []game.AccountConfig
	if err := json.Unmarshal(content, &cfgs); err != nil {
		log.Fatalf("accounts.json inválido: %v", err)
	}

	alvo := os.Getenv("ACCOUNT")
	var cfg game.AccountConfig
	achou := false
	for _, c := range cfgs {
		if !c.Enabled {
			continue
		}
		if alvo != "" && c.ID != alvo && c.Name != alvo {
			continue
		}
		cfg = c
		achou = true
		break
	}
	if !achou {
		log.Fatalf("nenhuma conta habilitada (ACCOUNT=%q)", alvo)
	}

	inicio := envInt("RATE_START", 1)
	fim := envInt("RATE_END", 40)
	passo := envInt("RATE_STEP", 1)
	seg := envInt("SEGUNDOS", 45)
	if passo <= 0 {
		passo = 1
	}
	if fim < inicio {
		fim = inicio
	}

	cfg.Settings.KillRate = float64(inicio)
	cfg.Settings.AutoRoute = false // fica em Runeterra; não viaja para o Rift
	cfg.Settings.AutoHeal = false

	fmt.Println("== Teste de limite de abates (Runeterra) ==")
	fmt.Printf("conta=%s  bindIP=%s  rate %d..%d passo %d  %ds por taxa\n\n", cfg.Name, cfg.BindIP, inicio, fim, passo, seg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := game.NewBot(cfg)
	if err := b.Init(ctx); err != nil {
		log.Fatalf("Init falhou: %v", err)
	}
	go func() { _ = b.Run(ctx) }()

	fmt.Printf("%-6s %-10s %-9s %-10s %-10s %-7s\n", "rate", "abatesΔ", "ab/min", "exp/h", "ouroΔ", "recus")
	time.Sleep(time.Duration(seg) * time.Second)

	prevKills := b.GetStatus().Kills
	prevGold := b.GetStatus().Gold
	baseRecus := b.SaveRecusados()
	rate := inicio
	ultimoBom := inicio - passo

	for {
		st := b.GetStatus()
		rec := b.SaveRecusados() - baseRecus
		abates := st.Kills - prevKills
		ouro := st.Gold - prevGold
		abMin := float64(abates) * 60.0 / float64(seg)
		fmt.Printf("%-6d %-10d %-9.1f %-10.0f %-10d %-7d\n", rate, abates, abMin, st.ExpPerHour, ouro, rec)

		if rec > 0 {
			fmt.Printf("\n>> LIMITE DO SERVIDOR: taxa %d RECUSADA. Última taxa aceita: %d\n", rate, ultimoBom)
			cancel()
			return
		}
		ultimoBom = rate
		if rate+passo > fim {
			fmt.Printf("\n>> Fim do intervalo (%d) sem recusa. Aumente RATE_END e continue.\n", fim)
			cancel()
			return
		}
		prevKills, prevGold = st.Kills, st.Gold
		rate += passo
		b.SetKillRate(float64(rate))
		time.Sleep(time.Duration(seg) * time.Second)
	}
}
