package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/matteogaldi/provision-kit/internal/engine"
	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

type Server struct {
	defs   map[string]*workflow.Definition
	store  store.Store
	engine *engine.Engine
}

func New(defs map[string]*workflow.Definition, st store.Store, eng *engine.Engine) *Server {
	if defs == nil {
		defs = map[string]*workflow.Definition{}
	}
	return &Server{defs: defs, store: st, engine: eng}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/provision/{name}", s.start)
	mux.HandleFunc("GET /v1/operations/{id}", s.get)
	return mux
}

type startRequest struct {
	Inputs map[string]any `json:"inputs"`
}

type startResponse struct {
	OperationID string       `json:"operation_id"`
	Status      store.Status `json:"status"`
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	def, ok := s.defs[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow %q not found", name))
		return
	}

	var req startRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unable to read body")
		return
	}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	inputs, err := stringInputs(req.Inputs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	op, err := s.engine.Start(r.Context(), def, inputs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(startResponse{
		OperationID: op.ID,
		Status:      op.Status,
	})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	op, err := s.store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "operation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(op)
}

func stringInputs(raw map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("inputs.%s must be a string", k)
		}
		out[k] = s
	}
	return out, nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
