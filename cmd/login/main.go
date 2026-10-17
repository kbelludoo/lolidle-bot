package main

import (
	"bytes"
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
		`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		os.Getenv("LOCALAPPDATA") + `\BraveSoftware\Brave-Browser\Application\brave.exe`,
		os.Getenv("LOCALAPPDATA") + `\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func main() {
	fmt.Println("================================================================================")
	fmt.Printf("⚡ LOL IDLE — BRAVE LIVE SESSION WATCHER & CAPTURADOR AUTOMÁTICO\n")
	fmt.Println("================================================================================")

	browserPath := findBrowser()
	if browserPath == "" {
		log.Fatalf("Erro: Brave ou Chromium não encontrado.")
	}

	port := 9222
	userDataDir, err := os.MkdirTemp("", "lolidle_brave_*")
	if err != nil {
		log.Fatalf("Erro ao criar perfil temporário: %v", err)
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--no-first-run",
		"--no-default-browser-check",
		"--window-size=1280,850",
		"https://lolidle.com/jogar",
	}

	log.Printf("[Brave Watcher] Abrindo navegador em: %s", browserPath)
	cmd := exec.Command(browserPath, args...)
	if err := cmd.Start(); err != nil {
		log.Fatalf("Falha ao iniciar processo do Brave: %v", err)
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("👉 A janela do Brave foi aberta no seu monitor.")
	fmt.Println("👉 Faça o login normalmente com seu email/senha ou Google.")
	fmt.Println("👉 NÃO precisa sair ou deslogar! O script captura o token ativo automaticamente,")
	fmt.Println("   injeta na VPS e ativa o bot na hora sem derrubar a sessão!")
	fmt.Println("--------------------------------------------------------------------------------")

	// Aguardar CDP endpoint com URL lolidle.com
	var pageWS string
	for i := 0; i < 40; i++ {
		time.Sleep(400 * time.Millisecond)
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

	log.Printf("[Brave Watcher] Conectado ao depurador da aba via WebSocket!")
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

	reqID := 10
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	evalScript := `
		(function() {
			const tok = localStorage.getItem('lol-idle-token') || localStorage.getItem('darius-idle-token');
			const nome = localStorage.getItem('lol-idle-ultimo') || localStorage.getItem('darius-idle-ultimo');
			if (tok && tok.length > 20) {
				return { status: "logged_in", token: tok, nome: nome || '' };
			}
			return { status: "waiting" };
		})()
	`

	capturedTokens := make(map[string]bool)

	for {
		select {
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
					token, _ := val["token"].(string)
					nome, _ := val["nome"].(string)

					if token != "" && !capturedTokens[token] {
						capturedTokens[token] = true
						handleCapturedToken(token, nome)
					}
				}
			}
		}
	}
}

func handleCapturedToken(token, localNome string) {
	fmt.Println("================================================================================")
	log.Printf("🎉 NOVO TOKEN CAPTURADO DO BRAVE! (%s...)", token[:12])
	if localNome != "" {
		log.Printf("Nome LocalStorage: %s", localNome)
	}

	// 1. Validar via GET /api/eu e pegar dados reais
	realNome, campeao := verifyTokenOnline(token)
	if realNome == "" {
		realNome = localNome
	}
	log.Printf("Personagem Confirmado: %s (Campeão: %s)", realNome, campeao)

	// 2. Salvar em accounts.json
	saveTokenToAccounts(realNome, campeao, token)

	// 3. Sincronizar com VPS
	syncToVPS()

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("✔ SUCESSO! %s está ativo e sincronizado com a VPS!\n", realNome)
	fmt.Println("👉 Se desejar conectar outro personagem da party, basta trocar de conta no Brave!")
	fmt.Println("================================================================================")
}

func verifyTokenOnline(token string) (string, string) {
	req, err := http.NewRequest("GET", "https://lolidle.com/api/eu", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Idioma", "pt")
	req.Header.Set("X-Instancia", "verify_"+fmt.Sprintf("%d", time.Now().UnixNano()))
	req.Header.Set("Origin", "https://lolidle.com")
	req.Header.Set("Referer", "https://lolidle.com/jogar")

	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	var data struct {
		Nome    string `json:"nome"`
		Campeao string `json:"campeao"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&data)
	return data.Nome, data.Campeao
}

func saveTokenToAccounts(charName, campeao, token string) {
	configFile := "accounts.json"
	var configs []game.AccountConfig

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Printf("Erro ao ler accounts.json: %v", err)
		return
	}
	if err := json.Unmarshal(data, &configs); err != nil {
		log.Printf("Erro ao parsear accounts.json: %v", err)
		return
	}

	matched := false
	lowerChar := strings.ToLower(charName)

	for i, c := range configs {
		// Bate pelo nome do personagem ou ID
		if strings.ToLower(c.Name) == lowerChar || strings.ToLower(c.ID) == lowerChar {
			configs[i].Credentials.Token = token
			configs[i].Enabled = true
			matched = true
			log.Printf("[accounts.json] Token atualizado para conta '%s' (%s)", configs[i].Name, configs[i].ID)
			break
		}
	}

	if !matched {
		// Tentar bater pelo campeão se o nome for novo
		for i, c := range configs {
			if strings.EqualFold(campeao, "taric") && strings.Contains(c.ID, "kbelludoo") {
				configs[i].Credentials.Token = token
				configs[i].Name = charName
				configs[i].Enabled = true
				matched = true
				break
			}
			if strings.EqualFold(campeao, "yasuo") && strings.Contains(c.ID, "maumau") {
				configs[i].Credentials.Token = token
				configs[i].Name = charName
				configs[i].Enabled = true
				matched = true
				break
			}
		}
	}

	if !matched {
		log.Printf("[accounts.json] Conta não mapeada, atualizando a primeira vaga livre...")
		for i := range configs {
			if configs[i].Credentials.Token == "" || configs[i].Credentials.Token == token {
				configs[i].Credentials.Token = token
				configs[i].Name = charName
				configs[i].Enabled = true
				matched = true
				break
			}
		}
	}

	out, _ := json.MarshalIndent(configs, "", "  ")
	_ = os.WriteFile(configFile, out, 0644)
}

func syncToVPS() {
	sshKey := `C:\Users\kbell\.ssh\ssh-key-2026-10-06.key`
	if _, err := os.Stat(sshKey); err != nil {
		log.Printf("Aviso: Chave SSH não encontrada em %s", sshKey)
		return
	}

	log.Printf("[VPS Sync] Enviando accounts.json para a Oracle VPS (129.153.157.17)...")
	scpCmd := exec.Command("scp", "-i", sshKey, "-o", "StrictHostKeyChecking=no", "accounts.json", "ubuntu@129.153.157.17:/home/ubuntu/lolidle-bot/accounts.json")
	var scpErr bytes.Buffer
	scpCmd.Stderr = &scpErr
	if err := scpCmd.Run(); err != nil {
		log.Printf("Falha no scp para VPS: %v (detalhe: %s)", err, scpErr.String())
		return
	}

	log.Printf("[VPS Sync] Reiniciando lolidle.service na VPS...")
	sshCmd := exec.Command("ssh", "-i", sshKey, "-o", "StrictHostKeyChecking=no", "ubuntu@129.153.157.17", "sudo systemctl restart lolidle.service")
	if err := sshCmd.Run(); err != nil {
		log.Printf("Falha ao reiniciar lolidle.service: %v", err)
		return
	}

	log.Printf("🚀 lolidle.service reiniciado na VPS com sucesso!")
}
