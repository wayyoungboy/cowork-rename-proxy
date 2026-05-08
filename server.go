package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const basePath = "/apps/anthropic"

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

func rewriteModelOut(model string) string {
	cfgMu.RLock()
	prefix := cfg.ModelPrefix
	target := cfg.TargetModel
	mode := cfg.Mode
	cfgMu.RUnlock()
	switch mode {
	case "force":
		return target
	case "prefix":
		return strings.TrimPrefix(model, prefix)
	default:
		return model
	}
}

func rewriteModelIn(model string) string {
	cfgMu.RLock()
	prefix := cfg.ModelPrefix
	target := cfg.TargetModel
	mode := cfg.Mode
	cfgMu.RUnlock()
	switch mode {
	case "force":
		return target
	case "prefix":
		if !strings.HasPrefix(model, prefix) {
			return prefix + model
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

// handleModelsRequest forwards /v1/models to upstream, appends mock_models, and returns.
func handleModelsRequest(w http.ResponseWriter, r *http.Request, mockModels []string) {
	if cfg.UpstreamBaseURL == "" {
		// No upstream configured, return mock models only
		returnMockModels(w, mockModels)
		return
	}

	upstreamModelsURL := cfg.UpstreamBaseURL + "/v1/models"
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamModelsURL, nil)
	if err != nil {
		returnMockModels(w, mockModels)
		return
	}
	upstreamResp, err := httpClient.Do(upReq)
	if err != nil {
		returnMockModels(w, mockModels)
		return
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode == http.StatusOK {
		var body map[string]any
		if json.NewDecoder(upstreamResp.Body).Decode(&body) == nil {
			if data, ok := body["data"].([]any); ok && len(mockModels) > 0 {
				for _, name := range mockModels {
					data = append(data, map[string]any{
						"id": name, "object": "model", "name": name,
					})
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(body)
			return
		}
	}
	returnMockModels(w, mockModels)
}

func returnMockModels(w http.ResponseWriter, names []string) {
	models := []map[string]any{}
	for _, name := range names {
		models = append(models, map[string]any{
			"id": name, "object": "model", "name": name,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": models})
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, basePath) {
		jsonResponse(w, 404, fmt.Sprintf("Route not found: %s", r.URL.Path), "not_found_error")
		return
	}

	// Intercept /v1/models: forward to upstream and append mock_models
	modelsPath := basePath + "/v1/models"
	if r.URL.Path == modelsPath && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		cfgMu.RLock()
		mockModels := cfg.MockModels
		mode := cfg.Mode
		cfgMu.RUnlock()

		// transparent mode: pass through to upstream without interception
		if mode == "" {
			goto forwardToUpstream
		}

		handleModelsRequest(w, r, mockModels)
		return
	}
forwardToUpstream:

	// Build upstream URL: use upstream's own path as the base, not basePath.
	// This avoids double-prefix when cowork base URL includes /v1/messages.
	if cfg.UpstreamBaseURL == "" {
		jsonResponse(w, 500, "upstream_base_url not configured", "configuration_error")
		return
	}
	upstreamParsed, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil {
		jsonResponse(w, 500, "Invalid upstream URL", "api_error")
		return
	}

	// Determine what prefix to strip from the incoming request path.
	// We look for the upstream path in the request to avoid double-prefixing.
	upstreamPath := upstreamParsed.Path
	remainder := strings.TrimPrefix(r.URL.Path, basePath)

	// If remainder starts with the upstream path, strip it to avoid duplication
	if strings.HasPrefix(remainder, upstreamPath) {
		remainder = strings.TrimPrefix(remainder, upstreamPath)
	}
	if remainder == "" {
		remainder = "/"
	}

	// Build path-only URL first, then attach query separately
	upstreamURL, err := url.JoinPath(cfg.UpstreamBaseURL, remainder)
	if err != nil {
		jsonResponse(w, 500, "Invalid upstream URL", "api_error")
		return
	}
	upstreamParsed, err = url.Parse(upstreamURL)
	if err != nil {
		jsonResponse(w, 500, "Invalid upstream URL", "api_error")
		return
	}
	if r.URL.RawQuery != "" {
		upstreamParsed.RawQuery = r.URL.RawQuery
	}
	upstreamURL = upstreamParsed.String()

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
				// Force to target model if configured, otherwise strip prefix
				if cfg.TargetModel != "" {
					body["model"] = cfg.TargetModel
				} else {
					body["model"] = rewriteModelOut(model)
				}
				reqModel = body["model"]
			}
			bodyBytes, _ = json.Marshal(body)
		}
	}

	log.Printf("[%s] %s %s → %s (model: %v)", time.Now().Format(time.RFC3339), r.Method, r.URL.Path, upstreamURL, reqModel)

	// Create upstream request
	upstreamReq, err := http.NewRequest(r.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		jsonResponse(w, 502, fmt.Sprintf("Upstream error: %s", err.Error()), "api_error")
		return
	}

	// Forward all headers except hop-by-hop and content-length
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
						j["model"] = rewriteModelIn(model)
					}
					if msg, ok := j["message"].(map[string]any); ok {
						if model, ok := msg["model"].(string); ok {
							msg["model"] = rewriteModelIn(model)
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
				j["model"] = rewriteModelIn(model)
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

	// Start config file watcher for hot-reload
	go watchConfig(cfgPath)

	http.HandleFunc("/", handleProxy)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	log.Printf("anthropic-model-rewrite-proxy listening on %s", addr)
	log.Printf("  upstream:    %s", cfg.UpstreamBaseURL)
	log.Printf("  model strip: %q prefix", cfg.ModelPrefix)
	log.Printf("  endpoint:    %s://%s%s/v1/messages", scheme, addr, basePath)

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
