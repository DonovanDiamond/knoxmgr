package knox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// UserQuery filters and pages /user/selectUsers. Zero values are not sent.
type UserQuery struct {
	UserID     string
	UserName   string
	UserIDLike string
	OrgCode    string
	Start      int // row index, >= 0
	Limit      int // 1..1000; 0 lets Knox default to 1000
}

func (q UserQuery) values() (url.Values, error) {
	if q.Start < 0 || q.Limit < 0 || q.Limit > 1000 {
		return nil, errors.New("knox: invalid paging")
	}
	f := url.Values{}
	set(f, "userId", q.UserID)
	set(f, "userName", q.UserName)
	set(f, "extraCondition.userIdLike", q.UserIDLike)
	// Not a documented query parameter; it binds to UserSearchCondition.orgCodes.
	set(f, "extraCondition.orgCodes", q.OrgCode)
	f.Set("page.start", strconv.Itoa(q.Start))
	if q.Limit > 0 {
		f.Set("page.limit", strconv.Itoa(q.Limit))
	}
	return f, nil
}

// SelectUsers lists users (GET /user/selectUsers).
func (c *Client) SelectUsers(ctx context.Context, q UserQuery) (UserPage, error) {
	f, err := q.values()
	if err != nil {
		return UserPage{}, err
	}
	type result struct {
		Users struct {
			User  []User `json:"user"`
			Total Count  `json:"total"`
		} `json:"users"`
	}
	r, err := call[result](ctx, c, http.MethodGet, "/user/selectUsers", f)
	return UserPage{Users: r.Users.User, Total: r.Users.Total}, err
}

// SelectUserWithID fetches one user (GET /user/selectUserWithID).
func (c *Client) SelectUserWithID(ctx context.Context, userID string) (User, error) {
	if userID == "" {
		return User{}, errors.New("knox: userId is required")
	}
	type result struct {
		User User `json:"user"`
	}
	r, err := call[result](ctx, c, http.MethodGet, "/user/selectUserWithID", url.Values{"userId": {userID}})
	return r.User, err
}

// UserInput is the writable part of a user. For updates only non-empty fields
// are sent, so a field cannot be cleared through UpdateUser.
type UserInput struct {
	UserID      string // required
	UserName    string // required on create
	Email       string // required on create
	FirstName   string
	LastName    string
	MobilePhone string
	Contact     string
	EmpNo       string
	OrgCode     string
	// UserPassword is optional; Knox defaults it to the userId when empty.
	UserPassword string
	// RequestEnrollment asks Knox to email an enrollment request on create.
	RequestEnrollment bool
}

func (u UserInput) values(create bool) (url.Values, error) {
	if u.UserID == "" || (create && (u.UserName == "" || u.Email == "")) {
		return nil, errors.New("knox: userId, userName and email are required")
	}
	f := url.Values{}
	set(f, "userId", u.UserID)
	set(f, "userName", u.UserName)
	set(f, "email", u.Email)
	set(f, "firstName", u.FirstName)
	set(f, "lastName", u.LastName)
	set(f, "mobilePhone", u.MobilePhone)
	set(f, "contact", u.Contact)
	set(f, "empNo", u.EmpNo)
	set(f, "orgCode", u.OrgCode)
	set(f, "userPassword", u.UserPassword)
	if create && u.RequestEnrollment {
		f.Set("requestEnrollment", "true")
	}
	return f, nil
}

// CreateUser creates a user (POST /user/createUser).
func (c *Client) CreateUser(ctx context.Context, u UserInput) error {
	f, err := u.values(true)
	if err != nil {
		return err
	}
	_, err = call[ignored](ctx, c, http.MethodPost, "/user/createUser", f)
	return err
}

// UpdateUser updates a user (POST /user/updateUser).
func (c *Client) UpdateUser(ctx context.Context, u UserInput) error {
	f, err := u.values(false)
	if err != nil {
		return err
	}
	_, err = call[ignored](ctx, c, http.MethodPost, "/user/updateUser", f)
	return err
}

// PurgeUser deletes a user, or deactivates one that owns an activated device.
// It is irreversible (POST /user/purgeUser).
func (c *Client) PurgeUser(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("knox: userId is required")
	}
	_, err := call[ignored](ctx, c, http.MethodPost, "/user/purgeUser", url.Values{"userId": {userID}})
	return err
}

// EnrollmentResult lists the requested users Knox could not email.
type EnrollmentResult struct {
	NotExistUserIDs     []string `json:"notExistUserId"`
	UserIDsWithoutEmail []string `json:"userIdWithoutEmail"`
}

// RequestEnrollment emails enrollment requests (POST /user/requestEnrollment).
func (c *Client) RequestEnrollment(ctx context.Context, userIDs []string) (EnrollmentResult, error) {
	ids, err := joinIDs(userIDs)
	if err != nil {
		return EnrollmentResult{}, err
	}
	return call[EnrollmentResult](ctx, c, http.MethodPost, "/user/requestEnrollment", url.Values{"userIds": {ids}})
}
