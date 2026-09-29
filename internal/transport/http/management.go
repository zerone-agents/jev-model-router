package httptransport

import (
	"encoding/json"
	"github.com/zerone-agents/jev-model-router/contracts"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"io"
	"net/http"
	"strings"
	"time"
)

func writeResult(w http.ResponseWriter, r management.Result) {
	w.Header().Set("Content-Type", "application/json")
	if !r.OK && r.Error != nil {
		w.WriteHeader(contracts.ErrorMapping(r.Error.Code).HTTP)
	}
	json.NewEncoder(w).Encode(r)
}
func NewManagementHandler(s *management.Service, authenticate func(string) (management.Principal, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, e := authenticate(r.Header.Get("Authorization"))
		if e != nil {
			writeResult(w, management.Failure(e))
			return
		}
		if p.Role != "settings" {
			writeResult(w, management.Failure(routing.Fail("forbidden", "settings credential required")))
			return
		}
		path := r.URL.Path
		if r.Method == "GET" && path == "/admin/v1/schema" {
			writeResult(w, management.Success(s.Catalog(), time.Now()))
			return
		}
		if r.Method == "GET" && strings.HasPrefix(path, "/admin/v1/schema/") {
			id := strings.TrimPrefix(path, "/admin/v1/schema/")
			for _, c := range s.Catalog() {
				if c.ID == id {
					writeResult(w, management.Success(c, time.Now()))
					return
				}
			}
			writeResult(w, management.Failure(routing.Fail("not_found", "capability unavailable")))
			return
		}
		if r.Method == "POST" && strings.HasPrefix(path, "/admin/v1/call/") {
			r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			var c management.Call
			if dec.Decode(&c) != nil || dec.Decode(new(any)) != io.EOF {
				writeResult(w, management.Failure(routing.Fail("invalid_request", "invalid management call")))
				return
			}
			id := strings.TrimPrefix(path, "/admin/v1/call/")
			if c.CapabilityID != "" && c.CapabilityID != id {
				writeResult(w, management.Failure(routing.Fail("invalid_request", "capability mismatch")))
				return
			}
			c.CapabilityID = id
			result, e := s.Execute(r.Context(), p.ID, c)
			if e != nil {
				result = management.Failure(e)
			}
			writeResult(w, result)
			return
		}
		writeResult(w, management.Failure(routing.Fail("not_found", "endpoint unavailable")))
	})
}
