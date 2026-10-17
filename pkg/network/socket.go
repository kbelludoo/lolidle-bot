package network

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type MessageHandler func(data []byte)
type DisconnectHandler func(err error)

// SocketClient gerencia conexão WebSocket com escrita thread-safe e keepalive
type SocketClient struct {
	url          string
	headers      http.Header
	bindIP       string
	proxyURL     string
	conn         *websocket.Conn
	writeMu      sync.Mutex
	mu           sync.RWMutex
	isConnected  bool
	onMessage    MessageHandler
	onDisconnect DisconnectHandler
	closeChan    chan struct{}
}

func NewSocketClient(url string, headers http.Header, onMsg MessageHandler, onDisc DisconnectHandler) *SocketClient {
	return NewSocketClientWithNetwork(url, headers, "", "", onMsg, onDisc)
}

func NewSocketClientWithNetwork(url string, headers http.Header, bindIP string, proxyURL string, onMsg MessageHandler, onDisc DisconnectHandler) *SocketClient {
	if headers == nil {
		headers = make(http.Header)
	}
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	}

	return &SocketClient{
		url:          url,
		headers:      headers,
		bindIP:       bindIP,
		proxyURL:     proxyURL,
		onMessage:    onMsg,
		onDisconnect: onDisc,
		closeChan:    make(chan struct{}),
	}
}

func (s *SocketClient) Connect() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	if s.proxyURL != "" {
		if parsedURL, err := url.Parse(s.proxyURL); err == nil {
			dialer.Proxy = http.ProxyURL(parsedURL)
		}
	}

	if s.bindIP != "" {
		if ip := net.ParseIP(s.bindIP); ip != nil {
			netDialer := &net.Dialer{
				LocalAddr: &net.TCPAddr{IP: ip},
				Timeout:   15 * time.Second,
			}
			dialer.NetDial = netDialer.Dial
			dialer.NetDialContext = netDialer.DialContext
		}
	}

	conn, _, err := dialer.Dial(s.url, s.headers)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.isConnected = true
	s.closeChan = make(chan struct{})
	s.mu.Unlock()

	// Iniciar loop de leitura
	go s.readLoop()

	return nil
}

func (s *SocketClient) readLoop() {
	defer func() {
		s.Close()
	}()

	s.conn.SetReadLimit(10 * 1024 * 1024) // 10MB max packet

	// Configurar pong handler
	s.conn.SetPongHandler(func(string) error {
		_ = s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, message, err := s.conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			wasConnected := s.isConnected
			s.isConnected = false
			s.mu.Unlock()

			if wasConnected && s.onDisconnect != nil {
				s.onDisconnect(err)
			}
			return
		}

		if s.onMessage != nil {
			s.onMessage(message)
		}
	}
}

func (s *SocketClient) SendJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.RLock()
	conn := s.conn
	connected := s.isConnected
	s.mu.RUnlock()

	if !connected || conn == nil {
		return fmt.Errorf("websocket não está conectado")
	}

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (s *SocketClient) SendText(text string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.RLock()
	conn := s.conn
	connected := s.isConnected
	s.mu.RUnlock()

	if !connected || conn == nil {
		return fmt.Errorf("websocket não está conectado")
	}

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, []byte(text))
}

func (s *SocketClient) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isConnected {
		return
	}

	s.isConnected = false
	if s.conn != nil {
		_ = s.conn.Close()
	}

	select {
	case <-s.closeChan:
	default:
		close(s.closeChan)
	}
}

func (s *SocketClient) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isConnected
}
