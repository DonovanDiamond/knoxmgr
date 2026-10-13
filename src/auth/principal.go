package auth

import (
	"errors"

	"github.com/DonovanDiamond/knoxmgr/src/config"
)

var ErrForbidden = errors.New("forbidden")

// Principal is the authenticated caller, as carried in the access token.
type Principal struct {
	UserID   int64
	Username string
	Role     string
	OrgCode  string // empty for admins
	Access   string // "read" or "write"; empty for admins
}

func (p *Principal) IsAdmin() bool { return p != nil && p.Role == config.RoleAdmin }

func RequireAdmin(p *Principal) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	return nil
}

// RequireWrite allows admins and users whose access is write.
func RequireWrite(p *Principal) error {
	if p == nil {
		return ErrForbidden
	}
	if p.IsAdmin() || (p.Role == config.RoleUser && p.Access == config.AccessWrite) {
		return nil
	}
	return ErrForbidden
}
