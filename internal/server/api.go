package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/eneat/greenlight/internal/app"
	"github.com/eneat/greenlight/internal/models"
	"github.com/eneat/greenlight/internal/store"
)

// createRequestBody is the JSON accepted by POST /api/requests.
type createRequestBody struct {
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	Source             string          `json:"source"`
	Category           string          `json:"category"`
	Priority           string          `json:"priority"`
	DefaultAction      string          `json:"default_action"`
	TimeoutSeconds     int             `json:"timeout_seconds"`
	ResumeURL          string          `json:"resume_url"`
	ResumePayloadExtra json.RawMessage `json:"resume_payload_extra"`
	Metadata           json.RawMessage `json:"metadata"`
}

// requestView is the JSON representation of a request returned by the API.
type requestView struct {
	ID              string     `json:"id"`
	URL             string     `json:"url"`
	Title           string     `json:"title"`
	Description     string     `json:"description,omitempty"`
	Source          string     `json:"source,omitempty"`
	Category        string     `json:"category,omitempty"`
	Priority        string     `json:"priority"`
	Status          string     `json:"status"`
	DecidedBy       string     `json:"decided_by,omitempty"`
	DecisionComment string     `json:"decision_comment,omitempty"`
	DefaultAction   string     `json:"default_action"`
	TimeoutSeconds  int        `json:"timeout_seconds"`
	Deadline        time.Time  `json:"deadline"`
	CallbackFailed  bool       `json:"callback_failed"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
}

func (s *Server) view(r *models.Request) requestView {
	return requestView{
		ID:              r.ID,
		URL:             s.app.RequestURL(r.ID),
		Title:           r.Title,
		Description:     r.Description,
		Source:          r.Source,
		Category:        r.Category,
		Priority:        string(r.Priority),
		Status:          string(r.Status),
		DecidedBy:       string(r.DecidedBy),
		DecisionComment: r.DecisionComment,
		DefaultAction:   string(r.DefaultAction),
		TimeoutSeconds:  r.TimeoutSeconds,
		Deadline:        r.DeadlineTime(),
		CallbackFailed:  r.CallbackFailed,
		CreatedAt:       r.CreatedAt,
		DecidedAt:       r.DecidedAt,
	}
}

func (s *Server) apiCreateRequest(w http.ResponseWriter, r *http.Request) {
	var body createRequestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)) // 1 MiB cap
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	req, err := s.app.CreateRequest(r.Context(), app.CreateInput{
		Title:              body.Title,
		Description:        body.Description,
		Source:             body.Source,
		Category:           body.Category,
		Priority:           body.Priority,
		DefaultAction:      body.DefaultAction,
		TimeoutSeconds:     body.TimeoutSeconds,
		ResumeURL:          body.ResumeURL,
		ResumePayloadExtra: body.ResumePayloadExtra,
		Metadata:           body.Metadata,
	})
	var verr *app.ValidationError
	if errors.As(err, &verr) {
		writeJSONError(w, http.StatusBadRequest, verr.Msg)
		return
	}
	if err != nil {
		s.app.Log.Error("api create request", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, s.view(req))
}

func (s *Server) apiGetRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := s.app.Store.GetRequest(id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, s.view(req))
}

func (s *Server) apiListRequests(w http.ResponseWriter, r *http.Request) {
	f := store.RequestFilter{
		Status:   r.URL.Query().Get("status"),
		Source:   r.URL.Query().Get("source"),
		Category: r.URL.Query().Get("category"),
		Limit:    200,
	}
	reqs, err := s.app.Store.ListRequests(f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	views := make([]requestView, 0, len(reqs))
	for _, req := range reqs {
		views = append(views, s.view(req))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"requests": views, "count": len(views)})
}

func (s *Server) apiCancelRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := s.app.Cancel(id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "request not found")
		return
	}
	if errors.Is(err, store.ErrAlreadyResolved) {
		writeJSONError(w, http.StatusConflict, "request already resolved")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, s.view(req))
}
