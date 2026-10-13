package settings

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/DonovanDiamond/knoxmgr/src/config"
)

const (
	RegionCustom = "custom"

	maxText     = 100
	maxURL      = 2048
	maxSecret   = 1024
	maxMappings = 200
	maxGroup    = 256
)

// presetRegions are the server ids in docs/knox-api/km-api.yml.
var presetRegions = []string{"ap01", "ap02", "ap03", "ap04", "ap05", "eu01", "eu02", "eu03", "eu04", "eu05", "eu06", "eu07", "eu08", "us01", "us02", "us03"}

var (
	knoxIDRe   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,100}$`) // no '@': the OAuth client_id is clientId@tenantId
	orgCodeRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,50}$`)
	upnSuffixR = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
)

// InvalidError is a rejected input; its message is safe to show to the admin.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &InvalidError{fmt.Sprintf(format, a...)} }

// Region is a preset Knox region.
type Region struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	BaseURL string `json:"baseURL"`
}

func Regions() []Region {
	out := make([]Region, len(presetRegions))
	for i, id := range presetRegions {
		out[i] = Region{ID: id, Label: strings.ToUpper(id), BaseURL: presetBaseURL(id)}
	}
	return out
}

func presetBaseURL(id string) string { return "https://" + id + ".manage.samsungknox.com" }

func isPreset(id string) bool {
	for _, p := range presetRegions {
		if p == id {
			return true
		}
	}
	return false
}

func checkText(field, v string, max int) error {
	if len(v) > max {
		return invalid("%s is too long", field)
	}
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return invalid("%s has invalid characters", field)
	}
	return nil
}

// KnoxInput is the PUT /knox body. ClientSecret is empty to keep the stored secret.
type KnoxInput struct {
	Region            string `json:"region"`
	BaseURL           string `json:"baseURL"`
	ClientID          string `json:"clientId"`
	TenantID          string `json:"tenantId"`
	ClientSecret      string `json:"clientSecret"`
	AllowInsecureHTTP bool   `json:"allowInsecureHTTP"`
}

// normalise validates in and fixes the derived fields: a preset region decides the base URL.
func (in *KnoxInput) normalise() error {
	in.ClientID, in.TenantID = strings.TrimSpace(in.ClientID), strings.TrimSpace(in.TenantID)
	switch {
	case in.Region == RegionCustom:
		in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if err := checkText("baseURL", in.BaseURL, maxURL); err != nil {
			return err
		}
		u, err := url.Parse(in.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return invalid("baseURL must be an http(s) URL such as https://eu01.manage.samsungknox.com")
		}
		if u.Path != "" {
			// The client appends /emm/oapi itself.
			return invalid("baseURL must be the host only, with no path (no /emm/oapi)")
		}
		if u.Scheme == "http" && !in.AllowInsecureHTTP {
			return invalid("baseURL uses plain http: tick \"allow insecure HTTP\" to permit it (the mock only)")
		}
	case isPreset(in.Region):
		in.BaseURL, in.AllowInsecureHTTP = presetBaseURL(in.Region), false
	default:
		return invalid("region must be one of the presets or %q", RegionCustom)
	}
	if !knoxIDRe.MatchString(in.ClientID) {
		return invalid("clientId must be 1 to 100 letters, digits or _.:-")
	}
	if !knoxIDRe.MatchString(in.TenantID) {
		return invalid("tenantId must be 1 to 100 letters, digits or _.:-")
	}
	return checkText("clientSecret", in.ClientSecret, maxSecret)
}

// ADInput is the PUT /ad body. BindPassword is empty to keep the stored password.
type ADInput struct {
	Enabled      bool        `json:"enabled"`
	URL          string      `json:"url"`
	BaseDN       string      `json:"baseDN"`
	UPNSuffix    string      `json:"upnSuffix"`
	BindUser     string      `json:"bindUser"`
	BindPassword string      `json:"bindPassword"`
	Mappings     []ADMapping `json:"mappings"`
}

type ADMapping struct {
	Group   string `json:"group"`
	Role    string `json:"role"`
	OrgCode string `json:"orgCode"`
	Access  string `json:"access"`
}

func (in *ADInput) normalise() error {
	in.URL, in.BaseDN, in.UPNSuffix, in.BindUser = strings.TrimSpace(in.URL), strings.TrimSpace(in.BaseDN), strings.TrimSpace(in.UPNSuffix), strings.TrimSpace(in.BindUser)
	for field, v := range map[string]string{"url": in.URL, "baseDN": in.BaseDN, "bindUser": in.BindUser} {
		if err := checkText(field, v, maxURL); err != nil {
			return err
		}
	}
	if err := checkText("bindPassword", in.BindPassword, maxSecret); err != nil {
		return err
	}
	if in.URL != "" || in.Enabled {
		if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Hostname() == "" {
			return invalid("url must be ldap://host[:port] or ldaps://host[:port]")
		} else if port := u.Port(); port != "" {
			if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
				return invalid("url port must be 1 to 65535")
			}
		}
	}
	if in.Enabled && in.BaseDN == "" {
		return invalid("baseDN is required")
	}
	if in.UPNSuffix != "" && !upnSuffixR.MatchString(in.UPNSuffix) {
		return invalid("upnSuffix must be a domain such as example.com (no @)")
	}
	if len(in.Mappings) > maxMappings {
		return invalid("too many group mappings")
	}
	if in.Enabled && len(in.Mappings) == 0 {
		return invalid("at least one group mapping is needed when AD is enabled")
	}
	seen := map[string]bool{}
	for i := range in.Mappings {
		m := &in.Mappings[i]
		m.Group, m.OrgCode = strings.TrimSpace(m.Group), strings.TrimSpace(m.OrgCode)
		if m.Group == "" {
			return invalid("mappings[%d]: group is required", i)
		}
		if err := checkText("group", m.Group, maxGroup); err != nil {
			return err
		}
		if seen[m.Group] {
			return invalid("mappings[%d] (%q): group is mapped twice", i, m.Group)
		}
		seen[m.Group] = true
		switch m.Role {
		case config.RoleAdmin:
			if m.OrgCode != "" || m.Access != "" {
				return invalid("mappings[%d] (%q): admin mappings must not set orgCode or access", i, m.Group)
			}
		case config.RoleUser:
			if !orgCodeRe.MatchString(m.OrgCode) {
				return invalid("mappings[%d] (%q): a valid orgCode is required for role user", i, m.Group)
			}
			if m.Access != config.AccessRead && m.Access != config.AccessWrite {
				return invalid("mappings[%d] (%q): access must be %q or %q for role user", i, m.Group, config.AccessRead, config.AccessWrite)
			}
		default:
			return invalid("mappings[%d] (%q): role must be %q or %q", i, m.Group, config.RoleAdmin, config.RoleUser)
		}
	}
	return nil
}

func (in *GeneralInput) normalise() error {
	in.InstallationName = strings.TrimSpace(in.InstallationName)
	if in.InstallationName == "" {
		return invalid("installationName is required")
	}
	if err := checkText("installationName", in.InstallationName, maxText); err != nil {
		return err
	}
	if in.SyncIntervalMinutes < 1 || in.SyncIntervalMinutes > 1440 {
		return invalid("syncIntervalMinutes must be 1 to 1440")
	}
	return nil
}

// GeneralInput is the PUT /general body.
type GeneralInput struct {
	InstallationName    string `json:"installationName"`
	SyncIntervalMinutes int    `json:"syncIntervalMinutes"`
	CheckUpdates        *bool  `json:"checkUpdates"` // nil keeps the current value
	ShowMap             *bool  `json:"showMap"`      // nil keeps the current value
}
