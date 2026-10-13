package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/DonovanDiamond/knoxmgr/src/db"
)

func auditEntry(c fiber.Ctx, action, target string, previous, next any) *db.AuditEntry {
	p := principalFrom(c)
	return &db.AuditEntry{ActorUserID: &p.UserID, ActorUsername: p.Username, Action: action, Target: target,
		PreviousValue: jsonPtr(previous), NewValue: jsonPtr(next)}
}

// record writes one audit entry for a mutation that has already happened. A failed write is
// logged, not returned: telling the caller the change failed would be false. Callers must pass
// values that carry no passwords.
func (s *server) record(c fiber.Ctx, action, target string, previous, next any) {
	s.logAuditFailure(s.Audit.Insert(c.Context(), auditEntry(c, action, target, previous, next)), action, target)
}

func (s *server) logAuditFailure(err error, action, target string) {
	if err != nil {
		s.Log.Error().Err(err).Str("action", action).Str("target", target).Msg("audit write failed")
	}
}

func jsonPtr(v any) *string {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}
