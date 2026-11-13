package web

import (
	"log"
	"net/http"

	"temporal-schedules-ui/internal/temporal"
)

// GET / — full page rendering the schedule list.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := ctxFrom(r)
	defer cancel()

	schedules, err := s.svc.List(ctx)
	if err != nil {
		log.Printf("list schedules: %v", err)
		schedules = nil // render empty list with a banner instead of crashing
	}

	s.renderPage(w, "index.html", map[string]any{
		"Schedules": schedules,
		"Error":     err,
	})
}

// GET /schedules — htmx partial: just the table body, used for polling.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()

	schedules, err := s.svc.List(ctx)
	if err != nil {
		log.Printf("list schedules: %v", err)
		s.renderErr(w, http.StatusBadGateway, "Failed to list schedules: "+err.Error())
		return
	}
	s.render(w, "rows.html", map[string]any{"Schedules": schedules})
}

// GET /schedules/new — create form partial (loaded into a modal/section).
func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, "create.html", nil)
}

// POST /schedules — create a schedule, then return the refreshed table.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()

	in := temporal.CreateInput{
		ID:         r.FormValue("id"),
		Workflow:   r.FormValue("workflow"),
		WorkflowID: r.FormValue("workflow_id"),
		TaskQueue:  r.FormValue("task_queue"),
		Cron:       r.FormValue("cron"),
		Interval:   r.FormValue("interval"),
		Note:       r.FormValue("note"),
		Paused:     r.FormValue("paused") == "on",
		Args:       r.FormValue("args"),
	}

	if err := s.svc.Create(ctx, in); err != nil {
		log.Printf("create schedule: %v", err)
		s.renderErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Refresh the full table so the new row shows up.
	schedules, err := s.svc.List(ctx)
	if err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.render(w, "rows.html", map[string]any{"Schedules": schedules})
}

// GET /schedules/{id} — detail view (full page or partial depending on HX-Request).
func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()

	id := r.PathValue("id")
	d, err := s.svc.Describe(ctx, id)
	if err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}

	// htmx request → render just the detail panel; otherwise full page.
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "detail.html", map[string]any{"D": d})
		return
	}
	s.renderPage(w, "detail_page.html", map[string]any{"D": d})
}

// POST /schedules/{id}/pause
func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()
	id := r.PathValue("id")
	if err := s.svc.Pause(ctx, id, r.FormValue("note")); err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.renderRowByID(w, r, id)
}

// POST /schedules/{id}/unpause
func (s *Server) handleUnpause(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()
	id := r.PathValue("id")
	if err := s.svc.Unpause(ctx, id, r.FormValue("note")); err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.renderRowByID(w, r, id)
}

// POST /schedules/{id}/trigger
func (s *Server) handleTrigger(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()
	id := r.PathValue("id")
	if err := s.svc.Trigger(ctx, id); err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.renderRowByID(w, r, id)
}

// DELETE /schedules/{id}
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFrom(r)
	defer cancel()
	id := r.PathValue("id")
	if err := s.svc.Delete(ctx, id); err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// htmx out-of-band: remove the row.
	w.Header().Set("HX-Reswap", "delete")
	w.WriteHeader(http.StatusOK)
}

// renderRowByID re-fetches a single schedule's summary and renders its row.
func (s *Server) renderRowByID(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := ctxFrom(r)
	defer cancel()

	all, err := s.svc.List(ctx)
	if err != nil {
		s.renderErr(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, sc := range all {
		if sc.ID == id {
			s.render(w, "row.html", map[string]any{"S": sc})
			return
		}
	}
	// Not found → tell htmx to remove the row.
	w.Header().Set("HX-Reswap", "delete")
	w.WriteHeader(http.StatusOK)
}
