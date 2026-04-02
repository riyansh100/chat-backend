// internal/auth/handler.go
package auth

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/riyansh/chat-backend/internal/history"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

type Handler struct {
	store  *Store
	warmer *Warmer
}

func NewHandler(store *Store, histStore *history.Store, lb *chatredis.RedisLoadBalancer) *Handler {
	return &Handler{
		store:  store,
		warmer: NewWarmer(histStore, lb),
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
// Returns: {"id":1,"username":"alice","subscriptions":[101,102]}
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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

	// warm cache async — non-blocking
	if len(subs) > 0 {
		h.warmer.WarmAsync(client.ID, subs)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            client.ID,
		"username":      client.Username,
		"subscriptions": subs,
	})
}

// POST /subscribe
// Body: {"client_id":1,"instrument_id":101}
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		ClientID     int `json:"client_id"`
		InstrumentID int `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.ClientID == 0 || body.InstrumentID == 0 {
		errJSON(w, http.StatusBadRequest, "client_id and instrument_id required")
		return
	}

	if err := h.store.Subscribe(r.Context(), body.ClientID, body.InstrumentID); err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	// warm cache for this single instrument
	h.warmer.WarmAsync(body.ClientID, []int{body.InstrumentID})

	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

// POST /unsubscribe
// Body: {"client_id":1,"instrument_id":101}
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		errJSON(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	var body struct {
		ClientID     int `json:"client_id"`
		InstrumentID int `json:"instrument_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.ClientID == 0 || body.InstrumentID == 0 {
		errJSON(w, http.StatusBadRequest, "client_id and instrument_id required")
		return
	}

	if err := h.store.Unsubscribe(r.Context(), body.ClientID, body.InstrumentID); err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// GET /subscriptions?client_id=1
func (h *Handler) GetSubscriptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != http.MethodGet {
		errJSON(w, http.StatusMethodNotAllowed, "GET only")
		return
	}

	clientIDStr := r.URL.Query().Get("client_id")
	clientID, err := strconv.Atoi(clientIDStr)
	if err != nil || clientID == 0 {
		errJSON(w, http.StatusBadRequest, "valid client_id required")
		return
	}

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
