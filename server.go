package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var httpClient = newHTTPClient()
var cfg Config
var cfgMu sync.RWMutex
var cfgPath string
var cfgModTime time.Time

func loadConfigFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func watchConfig(path string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime() != cfgModTime {
			c, err := loadConfigFromFile(path)
			if err != nil {
				log.Printf("[config] reload failed: %v", err)
				continue
			}
			cfgMu.Lock()
			cfg = c
			cfgModTime = info.ModTime()
			cfgMu.Unlock()
			log.Printf("[config] reloaded: %+v", c)
		}
	}
}

func resolveProvider() (*Provider, error) {
	cfgMu.RLock()
	name := cfg.CurrentProvider
	providers := cfg.Providers
	cfgMu.RUnlock()

	if name == "" {
		return nil, fmt.Errorf("current_provider not configured")
	}
	for i := range providers {
		if providers[i].Name == name {
			return &providers[i], nil
		}
	}
	return nil, fmt.Errorf("provider %q not found", name)
}

func resolveModelOut(model string, p *Provider) string {
	switch p.Mode {
	case "force":
		return p.TargetModel
	case "prefix":
		return strings.TrimPrefix(model, p.ModelPrefix)
	default:
		return model
	}
}

func resolveModelIn(model string, p *Provider) string {
	switch p.Mode {
	case "force":
		return p.TargetModel
	case "prefix":
		if !strings.HasPrefix(model, p.ModelPrefix) {
			return p.ModelPrefix + model
		}
		return model
	default:
		return model
	}
}

func jsonResponse(w http.ResponseWriter, status int, msg, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": errType},
	})
}

// handleModelsRequest aggregates models from all providers + mock_models
func handleModelsRequest(w http.ResponseWriter, r *http.Request) {
	cfgMu.RLock()
	providers := cfg.Providers
	mockModels := cfg.MockModels
	cfgMu.RUnlock()

	// Collect unique model names from all providers
	modelSet := make(map[string]bool)
	for _, p := range providers {
		for _, m := range p.Models {
			modelSet[m] = true
		}
	}
	for _, m := range mockModels {
		modelSet[m] = true
	}

	models := []map[string]any{}
	for name := range modelSet {
		models = append(models, map[string]any{
			"id": name, "object": "model", "name": name,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	p, err := resolveProvider()
	if err != nil {
		jsonResponse(w, 500, err.Error(), "configuration_error")
		return
	}

	// Determine upstream URL and path
	var upstreamURL string

	switch {
	case r.URL.Path == "/v1/models" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		handleModelsRequest(w, r)
		return

	case r.URL.Path == "/v1/messages" || r.URL.Path == "/v1/chat/completions":
		upstreamURL = p.BaseURL + r.URL.Path
		if r.URL.RawQuery != "" {
			upstreamURL += "?" + r.URL.RawQuery
		}

	default:
		jsonResponse(w, 404, fmt.Sprintf("Route not found: %s", r.URL.Path), "not_found_error")
		return
	}

	var bodyBytes []byte
	var reqModel any

	// For POST requests with a body, parse and rewrite model
	if r.Method == http.MethodPost && r.Body != nil {
		bodyBytes, err = io.ReadAll(io.LimitReader(r.Body, 50*1024*1024))
		if err != nil {
			jsonResponse(w, 400, "Failed to read body", "invalid_request_error")
			return
		}

		var body map[string]any
		if err := json.Unmarshal(bodyBytes, &body); err == nil {
			if model, ok := body["model"].(string); ok {
				// Check model allowance
				if len(p.Models) > 0 {
					allowed := false
					for _, m := range p.Models {
						if m == model {
							allowed = true
							break
						}
					}
					if !allowed {
						jsonResponse(w, 400, fmt.Sprintf("model %q is not supported by provider %q", model, p.Name), "invalid_request_error")
						return
					}
				}

				body["model"] = resolveModelOut(model, p)
				reqModel = body["model"]
				bodyBytes, _ = json.Marshal(body)
			}
		}
	}

	log.Printf("[%s] %s %s → %s (model: %v)", time.Now().Format(time.RFC3339), r.Method, r.URL.Path, upstreamURL, reqModel)

	// Create upstream request
	upstreamReq, err := http.NewRequest(r.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		jsonResponse(w, 502, fmt.Sprintf("Upstream error: %s", err.Error()), "api_error")
		return
	}

	// Inject provider API key if configured
	if p.APIKey != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+p.APIKey)
		upstreamReq.Header.Set("x-api-key", p.APIKey)
	} else {
		// Forward client headers
		skipHeaders := map[string]bool{
			"host": true, "connection": true, "content-length": true,
		}
		for k, v := range r.Header {
			if !skipHeaders[strings.ToLower(k)] {
				for _, hv := range v {
					upstreamReq.Header.Add(k, hv)
				}
			}
		}
	}

	// Forward to upstream
	upstreamRes, err := httpClient.Do(upstreamReq)
	if err != nil {
		jsonResponse(w, 502, fmt.Sprintf("Upstream error: %s", err.Error()), "api_error")
		return
	}
	defer upstreamRes.Body.Close()

	// Copy response headers (skip hop-by-hop and Content-Length)
	hopHeaders := map[string]bool{
		"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
		"Proxy-Authorization": true, "Te": true, "Trailers": true,
		"Transfer-Encoding": true, "Upgrade": true,
		"Content-Length": true,
	}
	for k, vv := range upstreamRes.Header {
		if !hopHeaders[k] {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	w.WriteHeader(upstreamRes.StatusCode)

	ct := upstreamRes.Header.Get("Content-Type")

	if strings.Contains(ct, "text/event-stream") {
		// SSE streaming: process line by line, rewrite model in JSON payloads
		flusher, ok := w.(http.Flusher)
		if !ok {
			io.Copy(w, upstreamRes.Body)
			return
		}
		prefixBytes := []byte("data:")
		scanner := newLineScanner(upstreamRes.Body)
		for {
			line, err := scanner.ReadLine()
			if err != nil {
				break
			}
			if bytes.HasPrefix(line, prefixBytes) {
				jsonPart := bytes.TrimPrefix(line, prefixBytes)
				var j map[string]any
				if json.Unmarshal(jsonPart, &j) == nil {
					if model, ok := j["model"].(string); ok {
						j["model"] = resolveModelIn(model, p)
					}
					if msg, ok := j["message"].(map[string]any); ok {
						if model, ok := msg["model"].(string); ok {
							msg["model"] = resolveModelIn(model, p)
						}
					}
					out, _ := json.Marshal(j)
					fmt.Fprintf(w, "data: %s\n", out)
					flusher.Flush()
					continue
				}
			}
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		}
	} else {
		// Non-streaming: collect full body, rewrite model in response, send
		respBytes, err := io.ReadAll(upstreamRes.Body)
		if err != nil {
			return
		}
		var j map[string]any
		if json.Unmarshal(respBytes, &j) == nil {
			if model, ok := j["model"].(string); ok {
				j["model"] = resolveModelIn(model, p)
				out, _ := json.Marshal(j)
				w.Write(out)
				return
			}
		}
		w.Write(respBytes)
	}
}

// lineScanner reads from an io.Reader and returns complete lines.
type lineScanner struct {
	r    io.Reader
	buf  []byte
	left []byte
}

func newLineScanner(r io.Reader) *lineScanner {
	return &lineScanner{r: r, buf: make([]byte, 4096)}
}

func (s *lineScanner) ReadLine() ([]byte, error) {
	for {
		if idx := bytes.IndexByte(s.left, '\n'); idx >= 0 {
			line := s.left[:idx]
			s.left = s.left[idx+1:]
			return line, nil
		}
		n, err := s.r.Read(s.buf)
		if n > 0 {
			s.left = append(s.left, s.buf[:n]...)
			continue
		}
		if len(s.left) > 0 {
			line := s.left
			s.left = nil
			return line, err
		}
		return nil, err
	}
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}

func main() {
	cfg = loadConfig()

	// Validate config
	if len(cfg.Providers) == 0 {
		log.Fatal("no providers configured")
	}
	if cfg.CurrentProvider == "" {
		log.Fatal("current_provider not set")
	}

	// Start config file watcher for hot-reload
	go watchConfig(cfgPath)

	http.HandleFunc("/", handleProxy)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	log.Printf("cowork-rename-proxy listening on %s", addr)
	log.Printf("  current provider: %s", cfg.CurrentProvider)
	log.Printf("  providers: %d configured", len(cfg.Providers))
	log.Printf("  endpoints: %s://%s/v1/messages, /v1/chat/completions, /v1/models", scheme, addr)

	if cfg.TLS {
		certFile := cfg.TLSCert
		if certFile == "" {
			certFile = "cert.pem"
		}
		keyFile := cfg.TLSKey
		if keyFile == "" {
			keyFile = "key.pem"
		}
		srv := &http.Server{
			Addr:      addr,
			Handler:   nil,
			TLSConfig: tlsConfig(certFile, keyFile, cfg.Host),
		}
		if err := srv.ListenAndServeTLS("", ""); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatal(err)
		}
	}
}
