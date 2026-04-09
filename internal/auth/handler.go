// internal/auth/handler.go
package auth

import (
	"encoding/json"
	"net/http"

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

// POST /login
// Body: {"username":"alice","password":"alice123"}
// Returns: {"token":"<uuid>","id":1,"username":"alice","subscriptions":[101,102]}
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
		errJSON(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	subs, err := h.store.GetSubscriptions(r.Context(), client.ID)
	if err != nil {
		subs = []int{}
	}

	// create session token
	token, err := h.sessionStore.CreateSession(r.Context(), client.ID)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, "session creation failed")
		return
	}

	// warm cache async — non-blocking
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

// POST /logout
// Requires: Authorization: Bearer <token>
// Deletes the session from Redis.
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

	// token already validated by AuthMiddleware — just delete it
	token := TokenFromContext(r.Context())
	if err := h.sessionStore.DeleteSession(r.Context(), token); err != nil {
		errJSON(w, http.StatusInternalServerError, "logout failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// POST /subscribe
// Requires: Authorization: Bearer <token>
// Body: {"instrument_id":101}
// client_id is derived from the session — NOT from the request body.
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
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.warmer.WarmAsync(clientID, []int{body.InstrumentID})

	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

// POST /unsubscribe
// Requires: Authorization: Bearer <token>
// Body: {"instrument_id":101}
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
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// GET /subscriptions
// Requires: Authorization: Bearer <token>
// client_id derived from session — no query param needed anymore.
func (h *Handler) GetSubscriptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		errJSON(w, http.StatusMethodNotAllowed, "GET only")
		return
	}

	clientID := ClientIDFromContext(r.Context())

	subs, err := h.store.GetSubscriptions(r.Context(), clientID)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if subs == nil {
		subs = []int{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"client_id": clientID, "subscriptions": subs})
}
