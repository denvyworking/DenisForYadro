package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yadrotask/internal/archive"
	"yadrotask/internal/config"
	"yadrotask/internal/logger"
	"yadrotask/internal/parser"
	"yadrotask/internal/store"
)

type Server struct {
	cfg    config.Config
	store  *store.Store
	logger *logger.Logger
	mux    *http.ServeMux
}

func New(cfg config.Config, st *store.Store, log *logger.Logger) *Server {
	s := &Server{cfg: cfg, store: st, logger: log, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.requestLoggingMiddleware(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /api/v1/parse/", s.handleParse)
	s.mux.HandleFunc("GET /api/v1/topology/", s.handleTopology)
	s.mux.HandleFunc("GET /api/v1/node/", s.handleNode)
	s.mux.HandleFunc("GET /api/v1/port/", s.handlePorts)
	s.mux.HandleFunc("GET /api/v1/log/", s.handleLog)
}

type apiError struct {
	Error string `json:"error"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		s.logger.Info("parse request finished", logger.Field{"duration_ms": time.Since(start).Milliseconds()})
	}()

	var request struct {
		Path string `json:"path"`
	}
	if err := decodeAnyBody(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	resolved, err := s.resolveDataPath(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	source, err := archive.Load(resolved)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.StartupWait)
	defer cancel()

	logID, err := s.store.CreatePendingLog(ctx, source.Path, source.Size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	parsed, parseErr := parser.Parse(source.Path, source.Data)
	if parseErr != nil {
		_ = s.store.MarkLogRejected(ctx, logID, parseErr.Error())
		s.logger.Error("parse failed", logger.Field{"log_id": logID, "error": parseErr.Error(), "path": source.Path, "duration_ms": time.Since(start).Milliseconds()})
		writeError(w, http.StatusUnprocessableEntity, parseErr.Error())
		return
	}

	if err := s.store.InsertParsedLog(ctx, logID, parsed); err != nil {
		_ = s.store.MarkLogRejected(ctx, logID, err.Error())
		s.logger.Error("store failed", logger.Field{"log_id": logID, "error": err.Error(), "path": source.Path, "duration_ms": time.Since(start).Milliseconds()})
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.MarkLogParsed(ctx, logID, len(parsed.Nodes), len(parsed.Ports)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"log_id": logID})
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	logID, err := parseTailInt64(r.URL.Path, "/api/v1/topology/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid log_id")
		return
	}
	result, err := s.store.GetTopology(r.Context(), logID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parseTailInt64(r.URL.Path, "/api/v1/node/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node_id")
		return
	}
	result, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parseTailInt64(r.URL.Path, "/api/v1/port/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid node_id")
		return
	}
	ports, err := s.store.GetPortsByNode(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_id": nodeID, "ports": ports})
}

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	logID, err := parseTailInt64(r.URL.Path, "/api/v1/log/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid log_id")
		return
	}
	result, err := s.store.GetLog(r.Context(), logID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("http request", logger.Field{
			"method":         r.Method,
			"path":           r.URL.Path,
			"status_code":    recorder.status,
			"duration_ms":    time.Since(start).Milliseconds(),
			"remote_addr":    r.RemoteAddr,
			"content_type":   r.Header.Get("Content-Type"),
			"content_length": r.ContentLength,
		})
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message})
}

func decodeAnyBody(body io.ReadCloser, dst any) error {
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return errors.New("empty body")
	}
	if trimmed[0] == '{' {
		if err := json.Unmarshal([]byte(trimmed), dst); err != nil {
			return fmt.Errorf("decode json body: %w", err)
		}
		return nil
	}
	return json.Unmarshal([]byte(fmt.Sprintf(`{"path":%q}`, trimmed)), dst)
}

func parseTailInt64(path, prefix string) (int64, error) {
	value := strings.TrimPrefix(path, prefix)
	value = strings.Trim(value, "/")
	if value == "" {
		return 0, errors.New("empty")
	}
	return strconv.ParseInt(value, 10, 64)
}

func (s *Server) resolveDataPath(requestPath string) (string, error) {
	base := s.cfg.DataDir
	if base == "" {
		base = "data"
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	candidateAbs, err := filepath.Abs(requestPath)
	if err != nil {
		return "", err
	}
	if candidateAbs != baseAbs && !strings.HasPrefix(candidateAbs, baseAbs+string(filepath.Separator)) {
		return "", errors.New("path must point inside data/ directory")
	}
	return candidateAbs, nil
}
