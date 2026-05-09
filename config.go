package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Provider struct {
	Name        string   `yaml:"name"`
	BaseURL     string   `yaml:"base_url"`
	APIKey      string   `yaml:"api_key"`
	Models      []string `yaml:"models"`
	Mode        string   `yaml:"mode"`
	TargetModel string   `yaml:"target_model"`
	ModelPrefix string   `yaml:"model_prefix"`
}

type Config struct {
	Host            string     `yaml:"host"`
	Port            int        `yaml:"port"`
	TLS             bool       `yaml:"tls"`
	TLSCert         string     `yaml:"tls_cert"`
	TLSKey          string     `yaml:"tls_key"`
	Providers       []Provider `yaml:"providers"`
	CurrentProvider string     `yaml:"current_provider"`
	MockModels      []string   `yaml:"mock_models"`
}

func loadConfig() Config {
	cfgFlag := flag.String("config", "config.yaml", "path to config file")
	host := flag.String("host", "", "proxy listen address (overrides config)")
	port := flag.Int("port", 0, "proxy listen port (overrides config)")
	tlsCert := flag.String("tls_cert", "", "TLS cert file path (overrides config)")
	tlsKey := flag.String("tls_key", "", "TLS key file path (overrides config)")
	currentProvider := flag.String("provider", "", "current provider name (overrides config)")
	showVer := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVer {
		fmt.Println("cowork-rename-proxy v2.0.0")
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
	if *tlsCert != "" {
		cfg.TLSCert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKey = *tlsKey
	}
	if *currentProvider != "" {
		cfg.CurrentProvider = *currentProvider
	}
	cfgPath = *cfgFlag

	return cfg
}
