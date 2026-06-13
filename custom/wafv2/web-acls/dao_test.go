package webacls

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	appaws "github.com/clawscli/claws/internal/aws"
)

const (
	testWebACLID   = "acl-id"
	testWebACLName = "test-acl"
	testWebACLARN  = "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/test-acl/acl-id"
)

func TestListSkipsCloudFrontScopeOutsideUSEast1(t *testing.T) {
	client := &recordingHTTPClient{}
	d := newTestWebACLDAO(client)

	_, err := d.List(appaws.WithRegionOverride(context.Background(), "us-west-2"))
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}

	if got := client.scopeCalls("REGIONAL"); got != 1 {
		t.Fatalf("REGIONAL calls = %d, want 1; bodies=%v", got, client.bodies)
	}
	if got := client.scopeCalls("CLOUDFRONT"); got != 0 {
		t.Fatalf("CLOUDFRONT calls = %d, want 0; bodies=%v", got, client.bodies)
	}
}

func TestListIncludesCloudFrontScopeInUSEast1(t *testing.T) {
	client := &recordingHTTPClient{}
	d := newTestWebACLDAO(client)

	_, err := d.List(appaws.WithRegionOverride(context.Background(), cloudFrontWebACLRegion))
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}

	if got := client.scopeCalls("REGIONAL"); got != 1 {
		t.Fatalf("REGIONAL calls = %d, want 1; bodies=%v", got, client.bodies)
	}
	if got := client.scopeCalls("CLOUDFRONT"); got != 1 {
		t.Fatalf("CLOUDFRONT calls = %d, want 1; bodies=%v", got, client.bodies)
	}
}

func TestGetWithCompositeIDUsesDirectLookup(t *testing.T) {
	client := &recordingHTTPClient{}
	d := newTestWebACLDAO(client)

	res, err := d.Get(appaws.WithRegionOverride(context.Background(), cloudFrontWebACLRegion), "REGIONAL/"+testWebACLName+"/"+testWebACLID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	acl, ok := res.(*WebACLResource)
	if !ok {
		t.Fatalf("Get() resource type = %T, want *WebACLResource", res)
	}
	if got := acl.GetID(); got != testWebACLID {
		t.Fatalf("Get() ID = %q, want %q", got, testWebACLID)
	}
	if got := client.operationCalls("ListWebACLs"); got != 0 {
		t.Fatalf("ListWebACLs calls = %d, want 0; bodies=%v", got, client.bodies)
	}
	if got := client.operationCalls("GetWebACL"); got != 1 {
		t.Fatalf("GetWebACL calls = %d, want 1; bodies=%v", got, client.bodies)
	}
}

func TestListCreatedIDRoundTripsThroughGetAndDelete(t *testing.T) {
	ctx := appaws.WithRegionOverride(context.Background(), "us-west-2")
	client := &recordingHTTPClient{listResponse: listWebACLsResponse()}
	d := newTestWebACLDAO(client)

	resources, err := d.List(ctx)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("List() resources = %d, want 1", len(resources))
	}
	id := resources[0].GetID()
	if id != testWebACLID {
		t.Fatalf("List-created ID = %q, want raw AWS ID %q", id, testWebACLID)
	}

	res, err := d.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", id, err)
	}
	if got := res.GetID(); got != id {
		t.Fatalf("Get(%q) ID = %q, want unchanged ID", id, got)
	}

	if err := d.Delete(ctx, id); err != nil {
		t.Fatalf("Delete(%q) returned error: %v", id, err)
	}
	if got := client.operationCalls("DeleteWebACL"); got != 1 {
		t.Fatalf("DeleteWebACL calls = %d, want 1; bodies=%v", got, client.bodies)
	}
}

func TestDeleteUsesSingleGetWebACLForLockToken(t *testing.T) {
	client := &recordingHTTPClient{listResponse: listWebACLsResponse()}
	d := newTestWebACLDAO(client)

	err := d.Delete(appaws.WithRegionOverride(context.Background(), "us-west-2"), testWebACLID)
	if err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	if got := client.operationCalls("ListWebACLs"); got != 1 {
		t.Fatalf("ListWebACLs calls = %d, want 1; bodies=%v", got, client.bodies)
	}
	if got := client.operationCalls("GetWebACL"); got != 1 {
		t.Fatalf("GetWebACL calls = %d, want 1; bodies=%v", got, client.bodies)
	}
	if got := client.operationCalls("DeleteWebACL"); got != 1 {
		t.Fatalf("DeleteWebACL calls = %d, want 1; bodies=%v", got, client.bodies)
	}
}

func TestParseWebACLID(t *testing.T) {
	parsed, ok := parseWebACLID("CLOUDFRONT/name/id")
	if !ok {
		t.Fatal("parseWebACLID() ok = false, want true")
	}
	if parsed.scope != types.ScopeCloudfront || parsed.name != "name" || parsed.id != "id" {
		t.Fatalf("parseWebACLID() = %#v, want CLOUDFRONT/name/id", parsed)
	}

	if _, ok := parseWebACLID(testWebACLID); ok {
		t.Fatalf("parseWebACLID(%q) ok = true, want false for current raw ID format", testWebACLID)
	}
}

func newTestWebACLDAO(httpClient aws.HTTPClient) *WebACLDAO {
	return &WebACLDAO{
		client: wafv2.NewFromConfig(aws.Config{
			Region:     cloudFrontWebACLRegion,
			HTTPClient: httpClient,
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", Source: "test"}, nil
			}),
		}),
	}
}

type recordingHTTPClient struct {
	bodies       []string
	targets      []string
	listResponse string
}

func (c *recordingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	c.bodies = append(c.bodies, string(body))
	c.targets = append(c.targets, req.Header.Get("X-Amz-Target"))

	responseBody := c.responseBody(req.Header.Get("X-Amz-Target"))

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}},
		Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
	}, nil
}

func (c *recordingHTTPClient) responseBody(target string) string {
	if strings.Contains(target, "ListWebACLs") {
		if c.listResponse != "" {
			return c.listResponse
		}
		return `{"WebACLs":[]}`
	}
	if strings.Contains(target, "GetWebACL") {
		return getWebACLResponse()
	}
	return `{}`
}

func (c *recordingHTTPClient) scopeCalls(scope string) int {
	needle := `"Scope":"` + scope + `"`
	count := 0
	for _, body := range c.bodies {
		if strings.Contains(body, needle) {
			count++
		}
	}
	return count
}

func (c *recordingHTTPClient) operationCalls(operation string) int {
	count := 0
	for _, target := range c.targets {
		if strings.Contains(target, operation) {
			count++
		}
	}
	return count
}

func listWebACLsResponse() string {
	return `{"WebACLs":[{"Name":"` + testWebACLName + `","Id":"` + testWebACLID + `","ARN":"` + testWebACLARN + `"}]}`
}

func getWebACLResponse() string {
	return `{"WebACL":{"Name":"` + testWebACLName + `","Id":"` + testWebACLID + `","ARN":"` + testWebACLARN + `","DefaultAction":{"Allow":{}},"Rules":[],"VisibilityConfig":{"SampledRequestsEnabled":true,"CloudWatchMetricsEnabled":true,"MetricName":"test-acl"}},"LockToken":"lock-token"}`
}
