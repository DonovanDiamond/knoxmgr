// Package knoxmock is an in-memory fake of the Knox Manage REST API (spec: docs/knox-api/km-api.yml),
// covering only the operations knoxmgr uses. It exists for tests and local development.
package knoxmock

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	tokenTTL     = time.Hour
	maxBody      = 1 << 20
	tenantID     = "mock.local"
	timeLayout   = "2006-01-02T15:04:05.000Z"
	codeOK       = "0"
	codeBadParam = "1001"
	codeNotFound = "1002"
	codeConflict = "1003"
)

// Server is a fake Knox Manage server. Safe for concurrent use.
type Server struct {
	clientID, clientSecret string
	log                    zerolog.Logger
	mux                    *http.ServeMux

	mu      sync.Mutex
	tokens  map[string]time.Time
	orgs    map[string]*org
	users   map[string]*user
	groups  map[string]*group
	devices map[string]*device
}

// New returns a Server seeded with a small org tree, users and USER groups.
func New(clientID, clientSecret string, log zerolog.Logger) *Server {
	s := &Server{
		clientID: clientID, clientSecret: clientSecret, log: log,
		tokens: map[string]time.Time{},
		orgs:   map[string]*org{}, users: map[string]*user{}, groups: map[string]*group{}, devices: map[string]*device{},
	}
	s.seed()

	api := http.NewServeMux()
	api.HandleFunc("GET /emm/oapi/user/selectUsers", s.selectUsers)
	api.HandleFunc("GET /emm/oapi/user/selectUserWithID", s.selectUserWithID)
	api.HandleFunc("POST /emm/oapi/user/createUser", s.createUser)
	api.HandleFunc("POST /emm/oapi/user/updateUser", s.updateUser)
	api.HandleFunc("POST /emm/oapi/user/purgeUser", s.purgeUser)
	api.HandleFunc("POST /emm/oapi/user/requestEnrollment", s.requestEnrollment)
	api.HandleFunc("GET /emm/oapi/group/selectGroups", s.selectGroups)
	api.HandleFunc("POST /emm/oapi/group/insertGroup", s.insertGroup)
	api.HandleFunc("POST /emm/oapi/group/deleteGroup", s.deleteGroup)
	api.HandleFunc("POST /emm/oapi/group/selectGroupUsers", s.selectGroupUsers)
	api.HandleFunc("POST /emm/oapi/group/insertGroupUnits", s.insertGroupUnits)
	api.HandleFunc("POST /emm/oapi/group/deleteGroupUnits", s.deleteGroupUnits)
	api.HandleFunc("POST /emm/oapi/device/selectDeviceList", s.selectDeviceList)
	api.HandleFunc("POST /emm/oapi/device/selectDeviceInfo", s.selectDeviceInfo)
	api.HandleFunc("POST /emm/oapi/device/selectDeviceAppList", s.selectDeviceAppList)
	api.HandleFunc("POST /emm/oapi/device/selectDeviceLocation", s.selectDeviceLocation)
	api.HandleFunc("POST /emm/oapi/device/selectDeviceLocationHistory", s.selectDeviceLocationHistory)
	api.HandleFunc("POST /emm/oapi/device/selectDevicesByUser", s.selectDevicesByUser)
	api.HandleFunc("GET /emm/oapi/org/selectList", s.orgSelectList)
	api.HandleFunc("GET /emm/oapi/org/select", s.orgSelect)
	api.HandleFunc("GET /emm/oapi/org/selectUsers", s.orgSelectUsers)

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /emm/oauth/token", s.token)
	s.mux.Handle("/emm/oapi/", s.requireBearer(api))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ok writes a success envelope; value may be nil.
func ok(w http.ResponseWriter, value any) {
	resp := map[string]any{"resultCode": codeOK, "resultMessage": "No Error"}
	if value != nil {
		resp["resultValue"] = value
	}
	writeJSON(w, http.StatusOK, resp)
}

// fail writes an application error. Knox reports these inside a 200 response via resultCode.
func fail(w http.ResponseWriter, code, msg string) {
	writeJSON(w, http.StatusOK, map[string]any{"resultCode": code, "resultMessage": msg})
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	}
	if r.PostFormValue("grant_type") != "client_credentials" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	// The real client_id is "{clientId}@{tenantId}"; accept it as sent or without the tenant.
	bare, _, _ := strings.Cut(id, "@")
	if id == "" || !(eq(id, s.clientID) || eq(bare, s.clientID)) || !eq(secret, s.clientSecret) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	tok := randHex(16)
	s.mu.Lock()
	now := time.Now()
	for t, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, t)
		}
	}
	s.tokens[tok] = now.Add(tokenTTL)
	s.mu.Unlock()
	s.log.Info().Str("client_id", id).Msg("token issued")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": tok, "token_type": "bearer", "expires_in": int(tokenTTL.Seconds()), "scope": "read",
	})
}

func (s *Server) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, tok, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		s.mu.Lock()
		exp, found := s.tokens[tok]
		s.mu.Unlock()
		if !strings.EqualFold(scheme, "bearer") || !found || time.Now().After(exp) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "invalid_token", "error_description": "Invalid access token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
