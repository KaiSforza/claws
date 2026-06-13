package roles

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/clawscli/claws/internal/enrichment"
)

func TestNewRoleResource(t *testing.T) {
	createDate := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	role := types.Role{
		RoleName:           aws.String("my-role"),
		RoleId:             aws.String("AROAEXAMPLE12345"),
		Arn:                aws.String("arn:aws:iam::123456789012:role/my-role"),
		Path:               aws.String("/service-role/"),
		Description:        aws.String("Test role"),
		MaxSessionDuration: aws.Int32(7200),
		CreateDate:         &createDate,
		Tags: []types.Tag{
			{Key: aws.String("Environment"), Value: aws.String("prod")},
		},
	}

	resource := NewRoleResource(role)

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"GetID", resource.GetID(), "my-role"},
		{"GetName", resource.GetName(), "my-role"},
		{"Path", resource.Path(), "/service-role/"},
		{"Arn", resource.Arn(), "arn:aws:iam::123456789012:role/my-role"},
		{"Description", resource.Description(), "Test role"},
		{"MaxSessionDuration", resource.MaxSessionDuration(), int32(7200)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}

	// Test tags
	tags := resource.GetTags()
	if tags["Environment"] != "prod" {
		t.Errorf("GetTags()[Environment] = %q, want %q", tags["Environment"], "prod")
	}
}

func TestRoleRendererShowsUnknownForFailedPolicyEnrichment(t *testing.T) {
	role := types.Role{RoleName: aws.String("my-role")}
	resource := NewRoleResource(role)
	resource.AttachedPoliciesStatus = enrichment.FetchFailed
	resource.InlinePoliciesStatus = enrichment.AccessDenied

	detail := (&RoleRenderer{}).RenderDetail(resource)

	for _, want := range []string{"Unknown (fetch failed)", "Unknown (access denied)"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("expected %q in detail, got %q", want, detail)
		}
	}
	if strings.Contains(detail, "Policies: Empty") {
		t.Fatalf("fetch failures must not render as empty policies: %q", detail)
	}
}

func TestRoleDAOGetPolicyFetchFailuresSetStatuses(t *testing.T) {
	client := &roleHTTPClient{responses: map[string]roleHTTPResponse{
		"GetRole":                  {statusCode: http.StatusOK, body: roleGetRoleResponse},
		"ListAttachedRolePolicies": {statusCode: http.StatusForbidden, body: iamErrorResponse("AccessDenied", "denied")},
		"ListRolePolicies":         {statusCode: http.StatusInternalServerError, body: iamErrorResponse("InternalFailure", "boom")},
	}}
	d := newTestRoleDAO(client)

	resource, err := d.Get(context.Background(), "my-role")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	role, ok := resource.(*RoleResource)
	if !ok {
		t.Fatalf("Get() resource type = %T, want *RoleResource", resource)
	}
	if role.AttachedPoliciesStatus != enrichment.AccessDenied {
		t.Fatalf("AttachedPoliciesStatus = %q, want %q", role.AttachedPoliciesStatus, enrichment.AccessDenied)
	}
	if role.InlinePoliciesStatus != enrichment.FetchFailed {
		t.Fatalf("InlinePoliciesStatus = %q, want %q", role.InlinePoliciesStatus, enrichment.FetchFailed)
	}
}

func TestRoleResource_MinimalRole(t *testing.T) {
	role := types.Role{
		RoleName: aws.String("minimal-role"),
	}

	resource := NewRoleResource(role)

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"GetID", resource.GetID(), "minimal-role"},
		{"Path", resource.Path(), ""},
		{"Arn", resource.Arn(), ""},
		{"Description", resource.Description(), ""},
		{"MaxSessionDuration", resource.MaxSessionDuration(), int32(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestRoleResource_PathVariations(t *testing.T) {
	paths := []struct {
		path     *string
		expected string
	}{
		{aws.String("/"), "/"},
		{aws.String("/service-role/"), "/service-role/"},
		{aws.String("/application/admin/"), "/application/admin/"},
		{nil, ""},
	}

	for _, tc := range paths {
		name := "nil"
		if tc.path != nil {
			name = *tc.path
		}
		t.Run(name, func(t *testing.T) {
			role := types.Role{
				RoleName: aws.String("test"),
				Path:     tc.path,
			}
			resource := NewRoleResource(role)
			if got := resource.Path(); got != tc.expected {
				t.Errorf("Path() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func newTestRoleDAO(httpClient aws.HTTPClient) *RoleDAO {
	return &RoleDAO{client: iam.NewFromConfig(aws.Config{
		Region:     "us-east-1",
		HTTPClient: httpClient,
		Retryer:    func() aws.Retryer { return aws.NopRetryer{} },
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", Source: "test"}, nil
		}),
	})}
}

type roleHTTPClient struct {
	mu        sync.Mutex
	responses map[string]roleHTTPResponse
	calls     []string
}

type roleHTTPResponse struct {
	statusCode int
	body       string
}

func (c *roleHTTPClient) Do(req *http.Request) (*http.Response, error) {
	action, err := iamAction(req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.calls = append(c.calls, action)
	response, ok := c.responses[action]
	c.mu.Unlock()
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return iamResponse(response.statusCode, response.body), nil
}

func iamAction(req *http.Request) (string, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return "", err
	}
	return values.Get("Action"), nil
}

func iamResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"text/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{},
	}
}

func iamErrorResponse(code, message string) string {
	return `<ErrorResponse><Error><Type>Sender</Type><Code>` + code + `</Code><Message>` + message + `</Message></Error><RequestId>test</RequestId></ErrorResponse>`
}

const roleGetRoleResponse = `<GetRoleResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><GetRoleResult><Role><Path>/</Path><RoleName>my-role</RoleName><RoleId>AROAEXAMPLE12345</RoleId><Arn>arn:aws:iam::123456789012:role/my-role</Arn></Role></GetRoleResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></GetRoleResponse>`
