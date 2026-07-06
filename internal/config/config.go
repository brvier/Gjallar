// Package config loads and validates the Gjallar YAML configuration.
package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so YAML values like "30s" or "720h" parse.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	v, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

type Config struct {
	Listen    string           `yaml:"listen"`
	Database  string           `yaml:"database"`
	Retention Duration         `yaml:"retention"`
	SiteTitle string           `yaml:"site_title"`
	Defaults  Defaults         `yaml:"defaults"`
	Alerts    map[string]Alert `yaml:"alerts"`
	Monitors  []Monitor        `yaml:"monitors"`
}

type Defaults struct {
	Interval         Duration `yaml:"interval"`
	Timeout          Duration `yaml:"timeout"`
	FailureThreshold int      `yaml:"failure_threshold"`
	Realert          Duration `yaml:"realert"` // 0 = no reminder while down
	Alerts           []string `yaml:"alerts"`
}

// Alert is a named notifier. Type "" (default) means a shoutrrr URL;
// type "freemobile" uses the Free Mobile SMS API with User/Pass;
// type "signal" posts to a signal-cli-rest-api /v2/send endpoint.
type Alert struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
	// signal
	Number     string   `yaml:"number"`     // sender number registered on the gateway
	Recipients []string `yaml:"recipients"` // destination numbers
}

type Monitor struct {
	Name             string   `yaml:"name"`
	Group            string   `yaml:"group"` // optional; groups monitors on the status page
	Type             string   `yaml:"type"`  // http | postgres | oracle | ping | prometheus
	Interval         Duration `yaml:"interval"`
	Timeout          Duration `yaml:"timeout"`
	FailureThreshold int      `yaml:"failure_threshold"`
	Realert          Duration `yaml:"realert"` // reminder interval while down; 0 = inherit / disabled
	Alerts           []string `yaml:"alerts"`

	// http
	URL            string            `yaml:"url"` // also used by prometheus
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	ExpectStatus   int               `yaml:"expect_status"` // 0 = any 2xx
	BodyRegex      string            `yaml:"body_regex"`
	CertExpiryWarn Duration          `yaml:"cert_expiry_warn"` // 0 = disabled

	// postgres / oracle
	DSN   string `yaml:"dsn"`
	Query string `yaml:"query"`
	Rule  string `yaml:"rule"` // also used by prometheus

	// ping / redis
	Host       string `yaml:"host"`
	Count      int    `yaml:"count"`
	Privileged bool   `yaml:"privileged"`
	Port       int    `yaml:"port"`     // redis; default 6379
	Password   string `yaml:"password"` // redis; empty = no AUTH

	// prometheus
	Metric string            `yaml:"metric"`
	Labels map[string]string `yaml:"labels"`

	// elasticsearch
	Index          string `yaml:"index"`
	TimestampField string `yaml:"timestamp_field"` // freshness = hours since max(this field)
}

var monitorTypes = map[string]bool{
	"http": true, "postgres": true, "oracle": true, "ping": true, "prometheus": true, "redis": true,
	"elasticsearch": true,
}

// envRe matches ${VAR} only; a bare $ is left alone so regex rules like
// "~ ^OPEN$" survive expansion.
var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandEnv(data []byte) ([]byte, error) {
	var missing []string
	out := envRe.ReplaceAllFunc(data, func(m []byte) []byte {
		name := string(m[2 : len(m)-1])
		v, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return m
		}
		return []byte(v)
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("undefined environment variables: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// Load reads, expands ${ENV_VAR} references, decodes (strict), applies
// defaults and validates the config.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded, err := expandEnv(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	cfg := &Config{}
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.Database == "" {
		c.Database = "gjallar.db"
	}
	if c.Retention == 0 {
		c.Retention = Duration(720 * time.Hour)
	}
	if c.SiteTitle == "" {
		c.SiteTitle = "Gjallar Status"
	}
	if c.Defaults.Interval == 0 {
		c.Defaults.Interval = Duration(60 * time.Second)
	}
	if c.Defaults.Timeout == 0 {
		c.Defaults.Timeout = Duration(10 * time.Second)
	}
	if c.Defaults.FailureThreshold == 0 {
		c.Defaults.FailureThreshold = 3
	}
	for i := range c.Monitors {
		m := &c.Monitors[i]
		if m.Interval == 0 {
			m.Interval = c.Defaults.Interval
		}
		if m.Timeout == 0 {
			m.Timeout = c.Defaults.Timeout
		}
		if m.FailureThreshold == 0 {
			m.FailureThreshold = c.Defaults.FailureThreshold
		}
		if m.Realert == 0 {
			m.Realert = c.Defaults.Realert
		}
		if m.Alerts == nil {
			m.Alerts = c.Defaults.Alerts
		}
		if m.Type == "ping" && m.Count == 0 {
			m.Count = 3
		}
		if m.Type == "http" && m.Method == "" {
			m.Method = "GET"
		}
	}
}

func (c *Config) Validate() error {
	for name, a := range c.Alerts {
		switch a.Type {
		case "", "shoutrrr":
			if a.URL == "" {
				return fmt.Errorf("alert %q: url is required", name)
			}
		case "freemobile":
			if a.User == "" || a.Pass == "" {
				return fmt.Errorf("alert %q: user and pass are required for type freemobile", name)
			}
		case "signal":
			if a.URL == "" || a.Number == "" || len(a.Recipients) == 0 {
				return fmt.Errorf("alert %q: url, number and recipients are required for type signal", name)
			}
		default:
			return fmt.Errorf("alert %q: unknown type %q (supported: shoutrrr, freemobile, signal)", name, a.Type)
		}
	}
	if len(c.Monitors) == 0 {
		return fmt.Errorf("no monitors defined")
	}
	seen := map[string]bool{}
	for i := range c.Monitors {
		m := &c.Monitors[i]
		if m.Name == "" {
			return fmt.Errorf("monitor #%d: name is required", i+1)
		}
		if seen[m.Name] {
			return fmt.Errorf("monitor %q: duplicate name", m.Name)
		}
		seen[m.Name] = true
		if err := m.validate(); err != nil {
			return fmt.Errorf("monitor %q: %w", m.Name, err)
		}
		for _, a := range m.Alerts {
			if _, ok := c.Alerts[a]; !ok {
				return fmt.Errorf("monitor %q: unknown alert %q (defined alerts: %s)",
					m.Name, a, strings.Join(alertNames(c.Alerts), ", "))
			}
		}
	}
	return nil
}

func (m *Monitor) validate() error {
	if !monitorTypes[m.Type] {
		return fmt.Errorf("unknown type %q (supported: http, postgres, oracle, ping, prometheus, redis, elasticsearch)", m.Type)
	}
	switch m.Type {
	case "http":
		if m.URL == "" {
			return fmt.Errorf("url is required")
		}
		if m.BodyRegex != "" {
			if _, err := regexp.Compile(m.BodyRegex); err != nil {
				return fmt.Errorf("body_regex: %w", err)
			}
		}
	case "postgres", "oracle":
		if m.DSN == "" {
			return fmt.Errorf("dsn is required")
		}
		if m.Query == "" {
			return fmt.Errorf("query is required")
		}
		if m.Rule == "" {
			return fmt.Errorf("rule is required")
		}
	case "ping", "redis":
		if m.Host == "" {
			return fmt.Errorf("host is required")
		}
	case "prometheus":
		if m.URL == "" {
			return fmt.Errorf("url is required")
		}
		if m.Metric == "" {
			return fmt.Errorf("metric is required")
		}
		if m.Rule == "" {
			return fmt.Errorf("rule is required")
		}
	case "elasticsearch":
		if m.URL == "" {
			return fmt.Errorf("url is required")
		}
		if m.Index == "" {
			return fmt.Errorf("index is required")
		}
		if m.TimestampField == "" {
			return fmt.Errorf("timestamp_field is required")
		}
		if m.Rule == "" {
			return fmt.Errorf("rule is required")
		}
	}
	return nil
}

func alertNames(m map[string]Alert) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
