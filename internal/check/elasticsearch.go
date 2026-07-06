package check

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gjallar/internal/config"
)

// esCheck measures the freshness of an Elasticsearch index: the number of hours
// between now and max(timestamp_field), evaluated against the rule (e.g. "< 3").
// This is the end-of-chain freshness signal — when an indexing worker stalls,
// max(timestamp_field) stops advancing and the lag grows without bound.
type esCheck struct {
	baseURL string
	index   string
	tsField string
	rule    *Rule
	client  *http.Client
}

func newESCheck(m config.Monitor) (*esCheck, error) {
	rule, err := ParseRule(m.Rule)
	if err != nil {
		return nil, err
	}
	if rule.TargetsRows() {
		return nil, fmt.Errorf("rule %q: row count rules do not apply to elasticsearch", m.Rule)
	}
	return &esCheck{
		baseURL: strings.TrimRight(m.URL, "/"),
		index:   m.Index,
		tsField: m.TimestampField,
		rule:    rule,
		client:  &http.Client{},
	}, nil
}

func (c *esCheck) Check(ctx context.Context) (bool, string) {
	body := fmt.Sprintf(`{"size":0,"aggs":{"max_ts":{"max":{"field":%q}}}}`, c.tsField)
	url := c.baseURL + "/" + c.index + "/_search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Sprintf("status %d querying %s", resp.StatusCode, c.index)
	}

	var parsed struct {
		Aggregations struct {
			MaxTS struct {
				Value float64 `json:"value"` // epoch millis
			} `json:"max_ts"`
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return false, fmt.Sprintf("parsing response: %v", err)
	}
	ms := parsed.Aggregations.MaxTS.Value
	if ms <= 0 {
		return false, fmt.Sprintf("no %q value (empty index?)", c.tsField)
	}

	last := time.UnixMilli(int64(ms))
	lagHours := time.Since(last).Hours()
	if err := c.rule.EvalValue(strconv.FormatFloat(lagHours, 'f', 2, 64)); err != nil {
		return false, fmt.Sprintf("freshness lag %.1fh (last %s): %v",
			lagHours, last.UTC().Format("2006-01-02 15:04 MST"), err)
	}
	return true, ""
}
