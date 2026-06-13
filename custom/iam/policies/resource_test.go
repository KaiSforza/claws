package policies

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/clawscli/claws/internal/enrichment"
)

func TestPolicyDAOGetDetailFetchFailuresSetStatuses(t *testing.T) {
	client := &policyHTTPClient{responses: map[string]policyHTTPResponse{
		"GetPolicy":             {statusCode: http.StatusOK, body: policyGetPolicyResponse},
		"GetPolicyVersion":      {statusCode: http.StatusForbidden, body: iamErrorResponse("AccessDenied", "denied")},
		"ListEntitiesForPolicy": {statusCode: http.StatusInternalServerError, body: iamErrorResponse("InternalFailure", "boom")},
	}}
	d := newTestPolicyDAO(client)

	resource, err := d.Get(context.Background(), "arn:aws:iam::123456789012:policy/my-policy")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	policy, ok := resource.(*PolicyResource)
	if !ok {
		t.Fatalf("Get() resource type = %T, want *PolicyResource", resource)
	}
	if policy.PolicyDocumentStatus != enrichment.AccessDenied {
		t.Fatalf("PolicyDocumentStatus = %q, want %q", policy.PolicyDocumentStatus, enrichment.AccessDenied)
	}
	if policy.AttachedEntitiesStatus != enrichment.FetchFailed {
		t.Fatalf("AttachedEntitiesStatus = %q, want %q", policy.AttachedEntitiesStatus, enrichment.FetchFailed)
	}
}

func newTestPolicyDAO(httpClient aws.HTTPClient) *PolicyDAO {
	return &PolicyDAO{client: iam.NewFromConfig(aws.Config{
		Region:     "us-east-1",
		HTTPClient: httpClient,
		Retryer:    func() aws.Retryer { return aws.NopRetryer{} },
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", Source: "test"}, nil
		}),
	})}
}

type policyHTTPClient struct {
	mu        sync.Mutex
	responses map[string]policyHTTPResponse
}

type policyHTTPResponse struct {
	statusCode int
	body       string
}

func (c *policyHTTPClient) Do(req *http.Request) (*http.Response, error) {
	action, err := iamAction(req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
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

const policyGetPolicyResponse = `<GetPolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><GetPolicyResult><Policy><PolicyName>my-policy</PolicyName><PolicyId>ANPAEXAMPLE12345</PolicyId><Arn>arn:aws:iam::123456789012:policy/my-policy</Arn><Path>/</Path><DefaultVersionId>v1</DefaultVersionId></Policy></GetPolicyResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></GetPolicyResponse>`
