// Package web serves the status page: templates + HTMX fragments, all
// embedded in the binary.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gjallar/internal/config"
	"gjallar/internal/store"
)

//go:embed all:templates static
var content embed.FS

const (
	tickCount     = 50  // results per uptime bar
	historyCount  = 100 // rows on the detail page
	incidentCount = 20
)

type Server struct {
	cfg       *config.Config
	st        *store.Store
	mux       *http.ServeMux
	indexTmpl *template.Template
	monTmpl   *template.Template
}

func New(cfg *config.Config, st *store.Store) (*Server, error) {
	s := &Server{cfg: cfg, st: st, mux: http.NewServeMux()}

	funcs := template.FuncMap{
		"sparkline":  sparkline,
		"fmtLatency": fmtLatency,
	}
	var err error
	s.indexTmpl, err = template.New("").Funcs(funcs).ParseFS(content,
		"templates/layout.tmpl", "templates/index.tmpl", "templates/_monitors.tmpl")
	if err != nil {
		return nil, err
	}
	s.monTmpl, err = template.New("").Funcs(funcs).ParseFS(content,
		"templates/layout.tmpl", "templates/monitor.tmpl", "templates/_detail.tmpl")
	if err != nil {
		return nil, err
	}

	s.mux.HandleFunc("GET /{$}", s.index)
	s.mux.HandleFunc("GET /fragments/monitors", s.monitorsFragment)
	s.mux.HandleFunc("GET /monitor/{name}", s.monitorPage)
	s.mux.HandleFunc("GET /fragments/monitor/{name}", s.monitorFragment)
	s.mux.Handle("GET /static/", http.FileServerFS(content))
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

// --- view models ---

type Tick struct {
	Known bool // false renders an empty placeholder
	OK    bool
	Title string
}

type MonitorView struct {
	Name      string
	Type      string
	Status    string // up | down | pending
	Latency   string
	Uptime24h string
	Ticks     []Tick
}

type IncidentView struct {
	Monitor  string
	Reason   string
	Started  string
	Duration string
	Open     bool
}

type GroupView struct {
	Name     string // empty = ungrouped
	Up       int
	Total    int // enabled monitors only
	Disabled int
	Monitors []MonitorView
}

type overviewData struct {
	Title     string
	Groups    []GroupView
	Incidents []IncidentView
}

type detailData struct {
	Title     string
	Name      string
	Type      string
	Status    string
	Uptime24h string
	Uptime30d string
	Sparkline template.HTML
	Rows      []store.ResultRow
	Incidents []IncidentView
}

// --- handlers ---

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.render(w, s.indexTmpl, "layout", s.overview())
}

func (s *Server) monitorsFragment(w http.ResponseWriter, r *http.Request) {
	s.render(w, s.indexTmpl, "monitors_fragment", s.overview())
}

func (s *Server) monitorPage(w http.ResponseWriter, r *http.Request) {
	d, ok := s.detail(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, s.monTmpl, "layout", d)
}

func (s *Server) monitorFragment(w http.ResponseWriter, r *http.Request) {
	d, ok := s.detail(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, s.monTmpl, "detail_fragment", d)
}

func (s *Server) render(w http.ResponseWriter, t *template.Template, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("rendering template", "template", name, "error", err)
	}
}

// --- data assembly ---

func (s *Server) overview() overviewData {
	d := overviewData{Title: s.cfg.SiteTitle}
	idx := map[string]int{} // group name -> position, in order of first appearance
	for _, m := range s.cfg.Monitors {
		v := s.monitorView(m)
		gi, ok := idx[m.Group]
		if !ok {
			gi = len(d.Groups)
			idx[m.Group] = gi
			d.Groups = append(d.Groups, GroupView{Name: m.Group})
		}
		g := &d.Groups[gi]
		g.Monitors = append(g.Monitors, v)
		if v.Status == "disabled" {
			g.Disabled++
			continue // disabled monitors don't count toward up/total health
		}
		g.Total++
		if v.Status == "up" {
			g.Up++
		}
	}
	incs, err := s.st.Incidents(incidentCount)
	if err != nil {
		slog.Error("loading incidents", "error", err)
	}
	for _, inc := range incs {
		d.Incidents = append(d.Incidents, incidentView(inc))
	}
	return d
}

func (s *Server) monitorView(m config.Monitor) MonitorView {
	v := MonitorView{Name: m.Name, Type: m.Type, Status: "pending", Latency: "—", Uptime24h: "—"}
	if !m.IsEnabled() {
		v.Status = "disabled"
		return v // no checks run, nothing to read from the store
	}

	results, err := s.st.RecentResults(m.Name, tickCount)
	if err != nil {
		slog.Error("loading results", "monitor", m.Name, "error", err)
	}
	if len(results) > 0 {
		last := results[0]
		if last.OK {
			v.Status = "up"
		} else {
			v.Status = "down"
		}
		v.Latency = fmtLatency(last.Latency)
	}
	if up, count, err := s.st.UptimeSince(m.Name, time.Now().Add(-24*time.Hour)); err == nil && count > 0 {
		v.Uptime24h = fmt.Sprintf("%.2f%%", up*100)
	}

	// Oldest→newest ticks, left-padded with placeholders to a fixed width.
	v.Ticks = make([]Tick, tickCount)
	for i, r := range results {
		title := r.Time.Format("2006-01-02 15:04:05") + " · " + fmtLatency(r.Latency)
		if r.Message != "" {
			title += " · " + r.Message
		}
		v.Ticks[tickCount-1-i] = Tick{Known: true, OK: r.OK, Title: title}
	}
	return v
}

func (s *Server) detail(name string) (detailData, bool) {
	var mon *config.Monitor
	for i := range s.cfg.Monitors {
		if s.cfg.Monitors[i].Name == name {
			mon = &s.cfg.Monitors[i]
			break
		}
	}
	if mon == nil {
		return detailData{}, false
	}

	d := detailData{
		Title:  mon.Name + " — " + s.cfg.SiteTitle,
		Name:   mon.Name,
		Type:   mon.Type,
		Status: "pending", Uptime24h: "—", Uptime30d: "—",
	}
	if !mon.IsEnabled() {
		d.Status = "disabled"
	}
	rows, err := s.st.RecentResults(name, historyCount)
	if err != nil {
		slog.Error("loading results", "monitor", name, "error", err)
	}
	d.Rows = rows
	if mon.IsEnabled() && len(rows) > 0 {
		if rows[0].OK {
			d.Status = "up"
		} else {
			d.Status = "down"
		}
	}
	if up, count, err := s.st.UptimeSince(name, time.Now().Add(-24*time.Hour)); err == nil && count > 0 {
		d.Uptime24h = fmt.Sprintf("%.2f%%", up*100)
	}
	if up, count, err := s.st.UptimeSince(name, time.Now().Add(-30*24*time.Hour)); err == nil && count > 0 {
		d.Uptime30d = fmt.Sprintf("%.2f%%", up*100)
	}

	latencies := make([]int64, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- { // oldest → newest
		latencies = append(latencies, rows[i].Latency.Milliseconds())
	}
	d.Sparkline = sparkline(latencies, 600, 80)

	incs, err := s.st.MonitorIncidents(name, incidentCount)
	if err != nil {
		slog.Error("loading incidents", "monitor", name, "error", err)
	}
	for _, inc := range incs {
		d.Incidents = append(d.Incidents, incidentView(inc))
	}
	return d, true
}

func incidentView(inc store.Incident) IncidentView {
	return IncidentView{
		Monitor:  inc.Monitor,
		Reason:   inc.Reason,
		Started:  inc.StartedAt.Format("2006-01-02 15:04"),
		Duration: inc.Duration().Round(time.Second).String(),
		Open:     inc.Open(),
	}
}

func fmtLatency(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// sparkline renders latency points as an inline SVG polyline.
func sparkline(points []int64, w, h int) template.HTML {
	if len(points) < 2 {
		return ""
	}
	var maxVal int64 = 1
	for _, p := range points {
		if p > maxVal {
			maxVal = p
		}
	}
	pad := 4.0
	var b strings.Builder
	for i, p := range points {
		x := pad + float64(i)*(float64(w)-2*pad)/float64(len(points)-1)
		y := float64(h) - pad - float64(p)/float64(maxVal)*(float64(h)-2*pad)
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}
	svg := fmt.Sprintf(`<svg viewBox="0 0 %d %d" width="100%%" height="%d" preserveAspectRatio="none" role="img" aria-label="latency history">`+
		`<polyline points="%s" fill="none" stroke="#e5383b" stroke-width="1.5"/>`+
		`<text x="%d" y="12" fill="#a0a0a8" font-size="10" text-anchor="end">max %dms</text></svg>`,
		w, h, h, b.String(), w-6, maxVal)
	return template.HTML(svg)
}
