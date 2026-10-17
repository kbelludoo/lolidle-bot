package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/gorilla/websocket"
)

type Account struct {
	ID          string `json:"id"`
	Credentials struct {
		Token string `json:"token"`
	} `json:"credentials"`
}

func main() {
	rawAccs, err := os.ReadFile("accounts.json")
	if err != nil {
		fmt.Printf("Error reading accounts.json: %v\n", err)
		return
	}
	var accs []Account
	if err := json.Unmarshal(rawAccs, &accs); err != nil || len(accs) == 0 {
		fmt.Printf("Error parsing accounts: %v\n", err)
		return
	}
	token := accs[0].Credentials.Token
	fmt.Printf("Using token for %s\n", accs[0].ID)

	edgePath := `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
	tempDir, _ := os.MkdirTemp("", "edge_capture_*")
	defer os.RemoveAll(tempDir)

	port := 9223
	cmd := exec.Command(edgePath,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", tempDir),
		"--no-first-run",
		"--no-default-browser-check",
		"--headless=new",
		"--disable-gpu",
	)

	if err := cmd.Start(); err != nil {
		fmt.Printf("Failed to start Edge: %v\n", err)
		return
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	time.Sleep(1500 * time.Millisecond)

	// Get target list
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json", port))
	if err != nil {
		fmt.Printf("Failed to get targets: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var targets []map[string]any
	json.Unmarshal(body, &targets)
	if len(targets) == 0 {
		fmt.Println("No targets found")
		return
	}

	wsURL, _ := targets[0]["webSocketDebuggerUrl"].(string)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Printf("WS dial error: %v\n", err)
		return
	}
	defer conn.Close()

	sendCDP := func(id int, method string, params any) {
		msg := map[string]any{
			"id":     id,
			"method": method,
			"params": params,
		}
		b, _ := json.Marshal(msg)
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}

	sendCDP(1, "Network.enable", map[string]any{})
	sendCDP(2, "Page.enable", map[string]any{})

	// Add script to set token before load
	initScript := fmt.Sprintf(`localStorage.setItem("lol-idle-token", %q);`, token)
	sendCDP(3, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source": initScript,
	})

	// Navigate
	sendCDP(4, "Page.navigate", map[string]any{
		"url": "https://lolidle.com/jogar",
	})

	fmt.Println("Navigated to https://lolidle.com/jogar. Listening for network events for 20 seconds...")

	reqBodies := make(map[string]string)
	deadline := time.Now().Add(25 * time.Second)

	for time.Now().Before(deadline) {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("WS read ended or error: %v\n", err)
			break
		}
		var ev map[string]any
		json.Unmarshal(msg, &ev)
		method, _ := ev["method"].(string)
		params, _ := ev["params"].(map[string]any)

		if method == "Network.requestWillBeSent" {
			req, _ := params["request"].(map[string]any)
			url, _ := req["url"].(string)
			reqID, _ := params["requestId"].(string)
			postData, _ := req["postData"].(string)
			if postData != "" {
				reqBodies[reqID] = postData
			}
			if url == "https://lolidle.com/api/save" {
				fmt.Printf("\n>>> Network.requestWillBeSent: %s\n", url)
				headers, _ := json.MarshalIndent(req["headers"], "", "  ")
				fmt.Printf("Headers:\n%s\n", string(headers))
				fmt.Printf("PostData:\n%s\n", postData)
			}
		}

		if method == "Network.responseReceived" {
			resp, _ := params["response"].(map[string]any)
			url, _ := resp["url"].(string)
			status, _ := resp["status"].(float64)
			reqID, _ := params["requestId"].(string)
			if url == "https://lolidle.com/api/save" {
				fmt.Printf("\n<<< Network.responseReceived: %s -> Status: %.0f\n", url, status)
				// Request response body via CDP
				sendCDP(100, "Network.getResponseBody", map[string]any{"requestId": reqID})
			}
		}

		// Check getResponseBody result
		if id, ok := ev["id"].(float64); ok && id == 100 {
			result, _ := ev["result"].(map[string]any)
			bodyStr, _ := result["body"].(string)
			fmt.Printf("Save Response Body:\n%s\n", bodyStr)
			if bodyStr != "" {
				break
			}
		}
	}

	fmt.Println("\nCapture session finished.")
}
