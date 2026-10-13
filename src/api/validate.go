package api

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"

	"github.com/gofiber/fiber/v3"
)

const (
	maxTextLen   = 100
	maxBatchIDs  = 100
	maxGroupName = 50
)

var (
	knoxIDRe   = regexp.MustCompile(`^[A-Za-z0-9_.@+:-]{1,50}$`)
	orgCodeRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,50}$`)
	groupIDRe  = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	appUserRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.@-]{0,63}$`)
	errBadBody = fiber.NewError(fiber.StatusBadRequest, "body must be valid JSON")
)

func badRequest(msg string) error { return fiber.NewError(fiber.StatusBadRequest, msg) }

func bind(c fiber.Ctx, v any) error {
	if err := c.Bind().JSON(v); err != nil {
		return errBadBody
	}
	return nil
}

func checkPattern(re *regexp.Regexp, field, v string) error {
	if !re.MatchString(v) {
		return badRequest("invalid " + field)
	}
	return nil
}

// checkText bounds length and rejects control characters, which have no place in names or notes.
func checkText(field, v string, max int, required bool) error {
	if v == "" {
		if required {
			return badRequest(field + " is required")
		}
		return nil
	}
	if len(v) > max {
		return badRequest(field + " is too long")
	}
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return badRequest("invalid " + field)
	}
	return nil
}

func checkEmail(v string, required bool) error {
	if err := checkText("email", v, maxTextLen, required); err != nil || v == "" {
		return err
	}
	if a, err := mail.ParseAddress(v); err != nil || a.Address != v {
		return badRequest("invalid email")
	}
	return nil
}

// checkIDList validates a non-empty, bounded, de-duplicated list of Knox user ids.
func checkIDList(ids []string) ([]string, error) { return checkNamedIDs("userId", ids) }

// checkNamedIDs is checkIDList for any Knox id kind; field is the singular JSON name.
func checkNamedIDs(field string, ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > maxBatchIDs {
		return nil, badRequest(field + "s must hold 1 to 100 ids")
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := checkPattern(knoxIDRe, field, id); err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}
