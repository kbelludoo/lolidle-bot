package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"bot-template-go/pkg/game"
	"github.com/gorilla/websocket"
)

type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func findBrowser() string {
	candidates := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		os.Getenv("LOCALAPPDATA") + `\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func main() {
	user := "kbelludoo"
	pass := "ECTbb,,0209"

	if len(os.Args) >= 3 {
		user = os.Args[1]
		pass = os.Args[2]
	} else {
		// Tentar ler a primeira conta de accounts.json
		if data, err := os.ReadFile("accounts.json"); err == nil {
			var accs []game.AccountConfig
			if json.Unmarshal(data, &accs) == nil && len(accs) > 0 {
				for _, a := range accs {
					if a.Credentials.Email != "" && a.Credentials.Password != "" && a.Credentials.Token == "" {
						user = a.Credentials.Email
						pass = a.Credentials.Password
						break
					}
				}
			}
		}
	}

	fmt.Println("================================================================================")
	fmt.Printf("⚡ LOL IDLE — AUTENTICADOR MULTI-CONTAS AUTOMÁTICO (CDP BRIDGE)\n")
	fmt.Printf("🔑 Conta alvo: %s\n", user)
	fmt.Println("================================================================================")

	browserPath := findBrowser()
	if browserPath == "" {
		log.Fatalf("Erro: Nenhum navegador Chromium encontrado (Edge, Chrome ou Brave).")
	}

	port := 9222
	userDataDir, err := os.MkdirTemp("", "lolidle_cdp_*")
	if err != nil {
		log.Fatalf("Erro ao criar perfil temporário: %v", err)
	}
	defer os.RemoveAll(userDataDir)

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=msFirstRunDialog,msSyncConfirmationDialog",
		"--window-size=1200,800",
		"https://lolidle.com/jogar",
	}

	log.Printf("[CDP] Iniciando navegador em %s ...", browserPath)
	cmd := exec.Command(browserPath, args...)
	if err := cmd.Start(); err != nil {
		log.Fatalf("Falha ao iniciar processo do navegador: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Aguardar CDP endpoint com URL lolidle.com
	var pageWS string
	for i := 0; i < 40; i++ {
		time.Sleep(300 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var targets []Target
			if json.Unmarshal(body, &targets) == nil {
				for _, t := range targets {
					if t.Type == "page" && strings.Contains(t.URL, "lolidle.com") && t.WebSocketDebuggerURL != "" {
						pageWS = t.WebSocketDebuggerURL
						break
					}
				}
			}
			if pageWS != "" {
				break
			}
		}
	}

	if pageWS == "" {
		log.Fatalf("Timeout aguardando página do LoL Idle no navegador.")
	}

	log.Printf("[CDP] Conectando ao WebSocket de depuração da aba...")
	ws, _, err := websocket.DefaultDialer.Dial(pageWS, nil)
	if err != nil {
		log.Fatalf("Falha ao conectar no CDP WebSocket: %v", err)
	}
	defer ws.Close()

	msgChan := make(chan []byte, 100)
	go func() {
		for {
			_, m, err := ws.ReadMessage()
			if err != nil {
				return
			}
			msgChan <- m
		}
	}()

	sendCDP := func(id int, method string, params map[string]any) {
		msg := map[string]any{
			"id":     id,
			"method": method,
			"params": params,
		}
		_ = ws.WriteJSON(msg)
	}

	sendCDP(1, "Runtime.enable", nil)
	sendCDP(2, "Page.enable", nil)

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("👉 A janela do jogo abriu no seu monitor.")
	fmt.Println("👉 O login e senha já estão sendo digitados automaticamente.")
	fmt.Println("👉 Caso apareça o checkbox do Cloudflare ('Verificar se você é humano'), clique nele.")
	fmt.Println("👉 O bot detecta o login na hora, captura o token e fecha o navegador sozinho!")
	fmt.Println("--------------------------------------------------------------------------------")

	token := ""
	charName := ""
	reqID := 10

	ticker := time.NewTicker(1200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(180 * time.Second)

	evalScript := fmt.Sprintf(`
		(function() {
			const tok = localStorage.getItem('lol-idle-token') || localStorage.getItem('darius-idle-token');
			const nome = localStorage.getItem('lol-idle-ultimo') || localStorage.getItem('darius-idle-ultimo');
			if (tok) {
				return { status: "logged_in", token: tok, nome: nome || '' };
			}

			const email = document.querySelector('#entrada-email');
			const senha = document.querySelector('#entrada-senha');
			if (email && email.value !== %q) {
				email.value = %q;
				email.dispatchEvent(new Event('input', { bubbles: true }));
				email.dispatchEvent(new Event('change', { bubbles: true }));
			}
			if (senha && senha.value !== %q) {
				senha.value = %q;
				senha.dispatchEvent(new Event('input', { bubbles: true }));
				senha.dispatchEvent(new Event('change', { bubbles: true }));
			}

			const turnstileVal = document.querySelector('[name="cf-turnstile-response"]')?.value || '';
			const btn = document.querySelector('#entrada-enviar');
			const erro = document.querySelector('#entrada-erro')?.textContent || '';

			if (btn && turnstileVal.length > 10 && !btn.disabled) {
				btn.click();
				return { status: "submitting", erro: erro };
			}

			return {
				status: "waiting",
				turnstileLen: turnstileVal.length,
				erro: erro
			};
		})()
	`, user, user, pass, pass)

	for {
		select {
		case <-timeout:
			log.Fatalf("Timeout de 3 minutos atingido. Tente novamente executando 'go run ./cmd/login'")

		case <-ticker.C:
			reqID++
			sendCDP(reqID, "Runtime.evaluate", map[string]any{
				"expression":    evalScript,
				"returnByValue": true,
			})

		case m := <-msgChan:
			var res struct {
				ID     int `json:"id"`
				Result struct {
					Result struct {
						Value map[string]any `json:"value"`
					} `json:"result"`
				} `json:"result"`
			}
			if json.Unmarshal(m, &res) == nil && res.Result.Result.Value != nil {
				val := res.Result.Result.Value
				st, _ := val["status"].(string)
				if st == "logged_in" {
					token, _ = val["token"].(string)
					charName, _ = val["nome"].(string)
				} else if st == "submitting" {
					log.Printf("[Autenticador] Turnstile resolvido! Enviando formulário...")
				}
			}
		}

		if token != "" {
			break
		}
	}

	fmt.Println("================================================================================")
	log.Printf("🎉 SUCESSO! Token de sessão obtido para '%s'!", user)
	if charName != "" {
		log.Printf("Personagem detectado: %s", charName)
	}
	fmt.Println("================================================================================")

	saveTokenToAccounts(user, pass, token, charName)

	fmt.Println()
	fmt.Println("🚀 Agora você já pode iniciar o bot multi-contas nativo com:")
	fmt.Println("   go run .")
	fmt.Println("================================================================================")
}

func saveTokenToAccounts(user, pass, token, charName string) {
	configFile := "accounts.json"
	var configs []game.AccountConfig

	if data, err := os.ReadFile(configFile); err == nil {
		_ = json.Unmarshal(data, &configs)
	}

	found := false
	for i, c := range configs {
		if c.Credentials.Email == user || c.ID == user || (charName != "" && c.Name == charName) {
			configs[i].Credentials.Token = token
			configs[i].Credentials.Password = pass
			configs[i].Enabled = true
			if charName != "" {
				configs[i].Name = charName
			}
			found = true
			break
		}
	}

	if !found {
		name := charName
		if name == "" {
			name = user
		}
		configs = append(configs, game.AccountConfig{
			ID:      user,
			Name:    name,
			Enabled: true,
			Credentials: game.GameCredentials{
				Email:    user,
				Password: pass,
				Token:    token,
			},
			Settings: game.GameSettings{
				HuntTarget: "Summoner's Rift",
				AutoHunt:   true,
				AutoSell:   true,
				AutoHeal:   true,
				AutoDaily:  true,
				AutoChests: true,
				AutoSacks:  true,
				Lure:       1,
			},
		})
	}

	out, _ := json.MarshalIndent(configs, "", "  ")
	_ = os.WriteFile(configFile, out, 0644)
	log.Printf("[Autenticador] Token salvo permanentemente em accounts.json!")
}
