// Package config loads and validates the Gjallar YAML configuration.
package config

import (
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
	Alerts           []string `yaml:"alerts"`
}

// Alert is a named notifier. Type "" (default) means a shoutrrr URL;
// type "freemobile" uses the Free Mobile SMS API with User/Pass.
type Alert struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type Monitor struct {
	Name             string   `yaml:"name"`
	Type             string   `yaml:"type"` // http | postgres | oracle | ping | prometheus
	Interval         Duration `yaml:"interval"`
	Timeout          Duration `yaml:"timeout"`
	FailureThreshold int      `yaml:"failure_threshold"`
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

	// ping
	Host       string `yaml:"host"`
	Count      int    `yaml:"count"`
	Privileged bool   `yaml:"privileged"`

	// prometheus
	Metric string            `yaml:"metric"`
	Labels map[string]string `yaml:"labels"`
}

var monitorTypes = map[string]bool{
	"http": true, "postgres": true, "oracle": true, "ping": true, "prometheus": true,
}

// Load reads, decodes (strict), applies defaults and validates the config.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &Config{}
	dec := yaml.NewDecoder(f)
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
		default:
			return fmt.Errorf("alert %q: unknown type %q (supported: shoutrrr, freemobile)", name, a.Type)
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
		return fmt.Errorf("unknown type %q (supported: http, postgres, oracle, ping, prometheus)", m.Type)
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
	case "ping":
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
