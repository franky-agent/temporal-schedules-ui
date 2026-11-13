package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime"
	"time"

	"temporal-schedules-ui/internal/temporal"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Server is the HTTP server. It holds a parsed template set and a
// ScheduleService backed by Temporal.
type Server struct {
	svc     *temporal.ScheduleService
	pages   map[string]*template.Template // full-page templates (base + content)
	partial *template.Template            // shared fragments: row, rows, create, detail
	mux     *http.ServeMux
	rc      *temporal.ReconnectingClient
}

// New parses the embedded templates and wires up routes. It takes a
// ReconnectingClient so the UI can start before Temporal is reachable.
func New(rc *temporal.ReconnectingClient) (*Server, error) {
	funcMap := template.FuncMap{
		"ago":        humanAgo,
		"formatTime": formatTime,
		"formatDate": formatDate,
		"truncate":   truncate,
		"statusCls":  statusClass,
		"goVersion":  runtime.Version,
	}

	// Shared partials rendered as htmx fragments (no base layout).
	partial, err := template.New("").Funcs(funcMap).ParseFS(assets,
		"templates/row.html",
		"templates/rows.html",
		"templates/create.html",
		"templates/detail.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse partials: %w", err)
	 }

	// Full-page templates: each is base.html + a page that defines "content".
	// We clone base per page so the "content" block doesn't collide.
	pageFiles := map[string]string{
		"index.html":        "templates/index.html",
		"detail_page.html": "templates/detail_page.html",
	}
	pages := make(map[string]*template.Template, len(pageFiles))
	for name, file := range pageFiles {
		base, err := template.New("").Funcs(funcMap).ParseFS(assets, "templates/base.html")
		if err != nil {
			return nil, fmt.Errorf("parse base: %w", err)
		}
		// Include shared fragments so pages can invoke them via {{template "rows.html"}}.
		base, err = base.ParseFS(assets, "templates/row.html", "templates/rows.html", "templates/detail.html")
		if err != nil {
			return nil, fmt.Errorf("parse fragments for %s: %w", file, err)
		}
		page, err := base.ParseFS(assets, file)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		pages[name] = page
	}

	s := &Server{
		svc:     temporal.NewScheduleService(rc),
		pages:   pages,
		partial: partial,
		rc:      rc,
	}
	s.mux = http.NewServeMux()
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	s.mux.Handle("GET /static/", http.FileServerFS(assets))
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /schedules", s.handleList)        // htmx partial only
	s.mux.HandleFunc("GET /schedules/new", s.handleNewForm) // create form partial
	s.mux.HandleFunc("POST /schedules", s.handleCreate)
	s.mux.HandleFunc("GET /schedules/{id}", s.handleDetail)
	s.mux.HandleFunc("POST /schedules/{id}/trigger", s.handleTrigger)
}

// ListenAndServe starts the HTTP server on addr (default ":7099").
func (s *Server) ListenAndServe(addr string) error {
	if addr == "" {
		addr = ":7099"
	}
	log.Printf("schedules UI listening on http://localhost%s", addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           logger(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}

// renderPage renders a full-page template (base + content block).
func (s *Server) renderPage(w http.ResponseWriter, name string, data any) {
	t := s.pages[name]
	if t == nil {
		http.Error(w, "unknown page template: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base.html", data); err != nil {
		log.Printf("render page %s: %v", name, err)
	}
}

// render renders a partial fragment via the shared partial template set.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.partial.ExecuteTemplate(w, name, data); err != nil {
		// Template execution already started writing to w, so we can't change
		// the status code. Log the error; the client gets a partial fragment.
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) renderErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<div class="alert alert-error">%s</div>`, msg)
}

// --- template helpers ---

func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("15:04:05")
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func statusClass(paused bool) string {
	if paused {
		return "badge badge-paused"
	}
	return "badge badge-active"
}

// logger wraps an http.Handler with a tiny access logger.
func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// ensure context isn't held forever.
func ctxFrom(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 15*time.Second)
}
