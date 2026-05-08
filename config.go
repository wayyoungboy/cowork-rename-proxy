package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host            string   `yaml:"host"`
	Port            int      `yaml:"port"`
	UpstreamBaseURL string   `yaml:"upstream_base_url"`
	ModelPrefix     string   `yaml:"model_prefix"`
	TLS             bool     `yaml:"tls"`
	TLSCert         string   `yaml:"tls_cert"`
	TLSKey          string   `yaml:"tls_key"`
	TargetModel     string   `yaml:"target_model"`
	Mode            string   `yaml:"mode"` // "force" or "prefix"; defaults based on target_model presence
	MockModels      []string `yaml:"mock_models"`
}

func loadConfig() Config {
	cfgFlag := flag.String("config", "config.yaml", "path to config file")
	host := flag.String("host", "", "proxy listen address (overrides config)")
	port := flag.Int("port", 0, "proxy listen port (overrides config)")
	upstream := flag.String("upstream", "", "upstream base URL (overrides config)")
	prefix := flag.String("prefix", "", "model prefix to strip (overrides config)")
	tlsCert := flag.String("tls_cert", "", "TLS cert file path (overrides config)")
	tlsKey := flag.String("tls_key", "", "TLS key file path (overrides config)")
	targetModel := flag.String("target_model", "", "force all requests to use this model (overrides config)")
	showVer := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVer {
		fmt.Println("anthropic-model-rewrite-proxy v1.0.0")
		os.Exit(0)
	}

	cfgPath = *cfgFlag

	var cfg Config
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("failed to read config file %s: %v", cfgPath, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("failed to parse config file: %v", err)
	}

	// Record initial mod time
	if info, err := os.Stat(cfgPath); err == nil {
		cfgModTime = info.ModTime()
	}

	// CLI args override config file
	if *host != "" {
		cfg.Host = *host
	}
	if *port > 0 {
		cfg.Port = *port
	}
	if *upstream != "" {
		cfg.UpstreamBaseURL = *upstream
	}
	if *prefix != "" {
		cfg.ModelPrefix = *prefix
	}
	if *tlsCert != "" {
		cfg.TLSCert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKey = *tlsKey
	}
	if *targetModel != "" {
		cfg.TargetModel = *targetModel
	}
	cfgPath = *cfgFlag

	return cfg
}
