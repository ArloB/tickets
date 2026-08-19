package httpapi

import (
	"net/http"

	"github.com/ArloB/tickets/internal/service"
)

// Server holds the handler dependencies. Server itself is unexported
// so nothing outside this package constructs handlers directly (ADR
// 0005: httpapi is a thin translation layer, never a second place
// business logic lives); NewHandler is what cmd/tickets wires up.
type Server struct {
	svc *service.Service
}

// NewHandler builds the /api/v1 router plus /healthz and /readyz.
func NewHandler(svc *service.Service) http.Handler {
	s := &Server{svc: svc}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	mux.HandleFunc("GET /api/v1/projects/{key}", s.getProject)
	mux.HandleFunc("POST /api/v1/projects/{key}/tickets", s.createTicket)
	mux.HandleFunc("GET /api/v1/tickets/{ref}", s.getTicket)
	mux.HandleFunc("PATCH /api/v1/tickets/{ref}", s.updateTicketStatus)

	return mux
}
