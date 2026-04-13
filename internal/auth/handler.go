// internal/auth/handler.go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/riyansh/chat-backend/internal/history"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

type Handler struct {
	store        *Store
	warmer       *Warmer
	sessionStore *SessionStore
}

func NewHandler(store *Store, histStore *history.Store, lb *chatredis.RedisLoadBalancer, ss *SessionStore) *Handler {
	return &Handler{
		store:        store,
		warmer:       NewWarmer(histStore, lb),
		sessionStore: ss,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// isClientGone returns true when the error is a context cancellation or
// deadline — meaning the HTTP client disconnected before we could respond.
// Only match strings that unambiguously mean "the HTTP client is gone":
// "context canceled" and "context deadline exceeded". Do NOT match
// "conn closed" or "connection reset by peer" — those also appear in pgx
// errors when the Postgres connection drops, which are real server errors
// that should return a 500, not be silently dropped.
func isClientGone(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline exceeded")
}

// POST /login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	client, err := h.store.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		if isClientGone(err) {
			return
		}
		errJSON(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	subs, err := h.store.GetSubscriptions(r.Context(), client.ID)
	if err != nil {
		subs = []int{} // non-fatal — client can still log in
	}

	token, err := h.sessionStore.CreateSession(r.Context(), client.ID)
	if err != nil {
		if isClientGone(err) {
			return
		}
		errJSON(w, http.StatusInternalServerError, "session creation failed")
		return
	}

	if len(subs) > 0 {
		h.warmer.WarmAsync(client.ID, subs)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":         token,
		"id":            client.ID,
		"username":      client.Username,
		"subscriptions": subs,
	})
}

// POST /register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(body.Username) < 3 {
		errJSON(w, http.StatusBadRequest, "username must be at least 3 characters")
		return
	}
	if len(body.Password) < 6 {
		errJSON(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	_, err := h.store.Register(r.Context(), body.Username, body.Password)
	if err != nil {
		if isClientGone(err) {
			return
		}
		if err == ErrUserExists {
			errJSON(w, http.StatusConflict, "username already taken")
			return
		}
		errJSON(w, http.StatusInternalServerError, "registration failed")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

// POST /logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	token := TokenFromContext(r.Context())
	if err := h.sessionStore.DeleteSession(r.Context(), token); err != nil {
		if isClientGone(err) {
			return
		}
		errJSON(w, http.StatusInternalServerError, "logout failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// POST /subscribe
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	clientID := ClientIDFromContext(r.Context())

	var body struct {
		InstrumentID int `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.InstrumentID == 0 {
		errJSON(w, http.StatusBadRequest, "instrument_id required")
		return
	}

	if err := h.store.Subscribe(r.Context(), clientID, body.InstrumentID); err != nil {
		if isClientGone(err) {
			return
		}
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.warmer.WarmAsync(clientID, []int{body.InstrumentID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

// POST /unsubscribe
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	clientID := ClientIDFromContext(r.Context())

	var body struct {
		InstrumentID int `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.InstrumentID == 0 {
		errJSON(w, http.StatusBadRequest, "instrument_id required")
		return
	}

	if err := h.store.Unsubscribe(r.Context(), clientID, body.InstrumentID); err != nil {
		if isClientGone(err) {
			return
		}
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// GET /subscriptions
func (h *Handler) GetSubscriptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		errJSON(w, http.StatusMethodNotAllowed, "GET only")
		return
	}

	clientID := ClientIDFromContext(r.Context())

	subs, err := h.store.GetSubscriptions(r.Context(), clientID)
	if err != nil {
		if isClientGone(err) {
			return // client gone — don't write 500
		}
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if subs == nil {
		subs = []int{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"client_id": clientID, "subscriptions": subs})
}
