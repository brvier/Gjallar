package check

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gjallar/internal/config"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// promCheck fetches a Prometheus /metrics endpoint and evaluates the rule
// against every series of the named metric matching the label selectors.
type promCheck struct {
	url    string
	metric string
	labels map[string]string
	rule   *Rule
	client *http.Client
}

func newPromCheck(m config.Monitor) (*promCheck, error) {
	rule, err := ParseRule(m.Rule)
	if err != nil {
		return nil, err
	}
	if rule.TargetsRows() {
		return nil, fmt.Errorf("rule %q: row count rules do not apply to prometheus metrics", m.Rule)
	}
	return &promCheck{
		url:    m.URL,
		metric: m.Metric,
		labels: m.Labels,
		rule:   rule,
		client: &http.Client{},
	}, nil
}

func (c *promCheck) Check(ctx context.Context) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Sprintf("status %d fetching metrics", resp.StatusCode)
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return false, fmt.Sprintf("parsing metrics: %v", err)
	}
	family, ok := families[c.metric]
	if !ok {
		return false, fmt.Sprintf("metric %q not found", c.metric)
	}

	matched := 0
	for _, m := range family.GetMetric() {
		if !labelsMatch(m, c.labels) {
			continue
		}
		matched++
		value, ok := metricValue(family.GetType(), m)
		if !ok {
			return false, fmt.Sprintf("metric %q: unsupported type %s", c.metric, family.GetType())
		}
		if err := c.rule.EvalValue(strconv.FormatFloat(value, 'g', -1, 64)); err != nil {
			return false, fmt.Sprintf("%s{%s}: %v", c.metric, labelString(m), err)
		}
	}
	if matched == 0 {
		return false, fmt.Sprintf("no series of %q matches labels %v", c.metric, c.labels)
	}
	return true, ""
}

func labelsMatch(m *dto.Metric, want map[string]string) bool {
	have := map[string]string{}
	for _, lp := range m.GetLabel() {
		have[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func metricValue(t dto.MetricType, m *dto.Metric) (float64, bool) {
	switch t {
	case dto.MetricType_GAUGE:
		return m.GetGauge().GetValue(), true
	case dto.MetricType_COUNTER:
		return m.GetCounter().GetValue(), true
	case dto.MetricType_UNTYPED:
		return m.GetUntyped().GetValue(), true
	}
	return 0, false
}

func labelString(m *dto.Metric) string {
	parts := make([]string, 0, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		parts = append(parts, fmt.Sprintf("%s=%q", lp.GetName(), lp.GetValue()))
	}
	return strings.Join(parts, ",")
}
