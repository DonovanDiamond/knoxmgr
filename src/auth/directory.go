package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	goad "github.com/DonovanDiamond/go-ad-auth/v3"
	"github.com/DonovanDiamond/knoxmgr/src/config"
	"github.com/go-ldap/ldap/v3"
)

var (
	// ErrDirectoryUnreachable is an infrastructure failure, not a bad password.
	ErrDirectoryUnreachable = errors.New("directory unreachable")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	// ErrNoMappedGroup means AD accepted the password but the user is in no configured group.
	ErrNoMappedGroup   = errors.New("user is not a member of any mapped group")
	ErrAccountDisabled = errors.New("account disabled")
)

// DirectoryUser is what a directory reports about a successfully authenticated user.
type DirectoryUser struct {
	DisplayName string
	Groups      []string // the subset of the asked-about groups the user belongs to
}

// Directory is the seam that lets tests replace Active Directory. Authenticate returns
// ErrInvalidCredentials for a wrong username/password and ErrDirectoryUnreachable when the
// server cannot be contacted.
type Directory interface {
	Authenticate(ctx context.Context, username, password string, groups []string) (*DirectoryUser, error)
}

// ADDirectory implements Directory with go-ad-auth.
type ADDirectory struct {
	cfg       *goad.Config
	upnSuffix string
}

// NewADDirectory builds the AD client from a validated config.AD. ldaps:// uses TLS; ldap:// is
// plain (default port 389), so prefer ldaps.
func NewADDirectory(c config.AD) (*ADDirectory, error) {
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, fmt.Errorf("ad.url: %w", err)
	}
	cfg := &goad.Config{Server: u.Hostname(), BaseDN: c.BaseDN}
	port := u.Port()
	switch u.Scheme {
	case "ldaps":
		cfg.Security = goad.SecurityTLS
		cfg.Port = 636
	case "ldap":
		cfg.Security = goad.SecurityNone
		cfg.Port = 389
	default:
		return nil, fmt.Errorf("ad.url scheme %q must be ldap or ldaps", u.Scheme)
	}
	if port != "" {
		if cfg.Port, err = strconv.Atoi(port); err != nil {
			return nil, fmt.Errorf("ad.url port %q: %w", port, err)
		}
	}
	return &ADDirectory{cfg: cfg, upnSuffix: c.UPNSuffix}, nil
}

func (d *ADDirectory) Authenticate(_ context.Context, username, password string, groups []string) (*DirectoryUser, error) {
	// A bare name gets the configured UPN suffix; the library would otherwise derive one from the base DN.
	if d.upnSuffix != "" && !strings.ContainsAny(username, `@\`) {
		username += "@" + d.upnSuffix
	}
	ok, entry, member, err := goad.AuthenticateExtended(d.cfg, username, password, []string{"displayName"}, groups)
	if err != nil {
		if isNetworkErr(err) {
			return nil, fmt.Errorf("%w: %v", ErrDirectoryUnreachable, err)
		}
		return nil, fmt.Errorf("authenticating %q against AD: %w", username, err)
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	return &DirectoryUser{DisplayName: entry.GetAttributeValue("displayName"), Groups: member}, nil
}

func isNetworkErr(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) || ldap.IsErrorWithCode(err, ldap.ErrorNetwork)
}
