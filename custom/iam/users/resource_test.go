package users

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

	"github.com/clawscli/claws/internal/enrichment"
)

func TestUserDAOGetFetchesDetailsInParallel(t *testing.T) {
	detailActions := map[string]bool{
		"ListAccessKeys":           true,
		"ListMFADevices":           true,
		"ListGroupsForUser":        true,
		"ListAttachedUserPolicies": true,
		"ListUserPolicies":         true,
	}
	arrived := make(chan string, len(detailActions))
	release := make(chan struct{})
	client := &userHTTPClient{
		responses: successfulUserResponses(),
		hook: func(req *http.Request, action string) error {
			if !detailActions[action] {
				return nil
			}
			arrived <- action
			select {
			case <-release:
				return nil
			case <-req.Context().Done():
				return req.Context().Err()
			}
		},
	}
	d := newTestUserDAO(client)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		resource, err := d.Get(ctx, "my-user")
		if err != nil {
			result <- err
			return
		}
		user := resource.(*UserResource)
		if len(user.AccessKeys) != 1 || len(user.MFADevices) != 1 || len(user.Groups) != 1 || len(user.AttachedPolicies) != 1 || len(user.InlinePolicies) != 1 {
			result <- io.ErrUnexpectedEOF
			return
		}
		result <- nil
	}()

	seen := map[string]bool{}
	for len(seen) < len(detailActions) {
		select {
		case action := <-arrived:
			seen[action] = true
		case <-time.After(300 * time.Millisecond):
			cancel()
			t.Fatalf("detail calls did not fan out before blocking; saw %v", seen)
		}
	}
	close(release)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Get() returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Get() timed out after releasing detail calls")
	}
}

func TestUserDAOGetDetailFetchFailuresSetStatuses(t *testing.T) {
	responses := successfulUserResponses()
	responses["ListAccessKeys"] = userHTTPResponse{statusCode: http.StatusForbidden, body: iamErrorResponse("AccessDenied", "denied")}
	responses["ListMFADevices"] = userHTTPResponse{statusCode: http.StatusInternalServerError, body: iamErrorResponse("InternalFailure", "boom")}
	responses["ListGroupsForUser"] = userHTTPResponse{statusCode: http.StatusForbidden, body: iamErrorResponse("AccessDenied", "denied")}
	responses["ListAttachedUserPolicies"] = userHTTPResponse{statusCode: http.StatusInternalServerError, body: iamErrorResponse("InternalFailure", "boom")}
	responses["ListUserPolicies"] = userHTTPResponse{statusCode: http.StatusForbidden, body: iamErrorResponse("AccessDenied", "denied")}
	d := newTestUserDAO(&userHTTPClient{responses: responses})

	resource, err := d.Get(context.Background(), "my-user")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	user := resource.(*UserResource)
	if user.AccessKeysStatus != enrichment.AccessDenied {
		t.Fatalf("AccessKeysStatus = %q, want %q", user.AccessKeysStatus, enrichment.AccessDenied)
	}
	if user.MFADevicesStatus != enrichment.FetchFailed {
		t.Fatalf("MFADevicesStatus = %q, want %q", user.MFADevicesStatus, enrichment.FetchFailed)
	}
	if user.GroupsStatus != enrichment.AccessDenied {
		t.Fatalf("GroupsStatus = %q, want %q", user.GroupsStatus, enrichment.AccessDenied)
	}
	if user.AttachedPoliciesStatus != enrichment.FetchFailed {
		t.Fatalf("AttachedPoliciesStatus = %q, want %q", user.AttachedPoliciesStatus, enrichment.FetchFailed)
	}
	if user.InlinePoliciesStatus != enrichment.AccessDenied {
		t.Fatalf("InlinePoliciesStatus = %q, want %q", user.InlinePoliciesStatus, enrichment.AccessDenied)
	}
}

func newTestUserDAO(httpClient aws.HTTPClient) *UserDAO {
	return &UserDAO{client: iam.NewFromConfig(aws.Config{
		Region:     "us-east-1",
		HTTPClient: httpClient,
		Retryer:    func() aws.Retryer { return aws.NopRetryer{} },
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", Source: "test"}, nil
		}),
	})}
}

type userHTTPClient struct {
	mu        sync.Mutex
	responses map[string]userHTTPResponse
	hook      func(*http.Request, string) error
}

type userHTTPResponse struct {
	statusCode int
	body       string
}

func (c *userHTTPClient) Do(req *http.Request) (*http.Response, error) {
	action, err := iamAction(req)
	if err != nil {
		return nil, err
	}
	if c.hook != nil {
		if err := c.hook(req, action); err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	response, ok := c.responses[action]
	c.mu.Unlock()
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return iamResponse(response.statusCode, response.body), nil
}

func successfulUserResponses() map[string]userHTTPResponse {
	return map[string]userHTTPResponse{
		"GetUser":                  {statusCode: http.StatusOK, body: userGetUserResponse},
		"ListAccessKeys":           {statusCode: http.StatusOK, body: listAccessKeysResponse},
		"ListMFADevices":           {statusCode: http.StatusOK, body: listMFADevicesResponse},
		"ListGroupsForUser":        {statusCode: http.StatusOK, body: listGroupsForUserResponse},
		"ListAttachedUserPolicies": {statusCode: http.StatusOK, body: listAttachedUserPoliciesResponse},
		"ListUserPolicies":         {statusCode: http.StatusOK, body: listUserPoliciesResponse},
	}
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

const userGetUserResponse = `<GetUserResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><GetUserResult><User><Path>/</Path><UserName>my-user</UserName><UserId>AIDAEXAMPLE12345</UserId><Arn>arn:aws:iam::123456789012:user/my-user</Arn></User></GetUserResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></GetUserResponse>`
const listAccessKeysResponse = `<ListAccessKeysResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListAccessKeysResult><AccessKeyMetadata><member><UserName>my-user</UserName><AccessKeyId>AKIAEXAMPLE</AccessKeyId><Status>Active</Status></member></AccessKeyMetadata></ListAccessKeysResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListAccessKeysResponse>`
const listMFADevicesResponse = `<ListMFADevicesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListMFADevicesResult><MFADevices><member><UserName>my-user</UserName><SerialNumber>arn:aws:iam::123456789012:mfa/my-user</SerialNumber></member></MFADevices></ListMFADevicesResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListMFADevicesResponse>`
const listGroupsForUserResponse = `<ListGroupsForUserResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListGroupsForUserResult><Groups><member><Path>/</Path><GroupName>admins</GroupName><GroupId>AGPAEXAMPLE12345</GroupId><Arn>arn:aws:iam::123456789012:group/admins</Arn></member></Groups></ListGroupsForUserResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListGroupsForUserResponse>`
const listAttachedUserPoliciesResponse = `<ListAttachedUserPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListAttachedUserPoliciesResult><AttachedPolicies><member><PolicyName>ReadOnlyAccess</PolicyName><PolicyArn>arn:aws:iam::aws:policy/ReadOnlyAccess</PolicyArn></member></AttachedPolicies></ListAttachedUserPoliciesResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListAttachedUserPoliciesResponse>`
const listUserPoliciesResponse = `<ListUserPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListUserPoliciesResult><PolicyNames><member>inline</member></PolicyNames></ListUserPoliciesResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListUserPoliciesResponse>`
