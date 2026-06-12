package rules

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/clawscli/claws/internal/log"
)

func TestNewRuleResourceUsesBusQualifiedID(t *testing.T) {
	rule := types.Rule{
		Name:         aws.String("nightly"),
		EventBusName: aws.String("custom-bus"),
	}

	resource := NewRuleResource(rule)

	if resource.GetID() != "custom-bus/nightly" {
		t.Fatalf("GetID() = %q, want %q", resource.GetID(), "custom-bus/nightly")
	}
	if resource.GetName() != "nightly" {
		t.Fatalf("GetName() = %q, want %q", resource.GetName(), "nightly")
	}
}

func TestParseRuleID(t *testing.T) {
	tests := []struct {
		id       string
		name     string
		eventBus string
	}{
		{id: "nightly", name: "nightly", eventBus: ""},
		{id: "custom-bus/nightly", name: "nightly", eventBus: "custom-bus"},
		{id: "aws.partner/example.com/account/nightly", name: "nightly", eventBus: "aws.partner/example.com/account"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			name, eventBus := parseRuleID(tt.id)
			if name != tt.name || eventBus != tt.eventBus {
				t.Fatalf("parseRuleID(%q) = (%q, %q), want (%q, %q)", tt.id, name, eventBus, tt.name, tt.eventBus)
			}
		})
	}
}

func TestRuleDAODeleteAbortsWhenTargetListingFails(t *testing.T) {
	client := &ruleHTTPClient{
		responses: map[string]ruleHTTPResponse{
			"AWSEvents.ListTargetsByRule": {statusCode: http.StatusInternalServerError, body: `{"message":"target list failed"}`},
		},
	}
	d := newTestRuleDAO(client)

	err := d.Delete(context.Background(), "custom-bus/nightly")
	if err == nil {
		t.Fatal("Delete() error = nil, want target listing error")
	}
	if got := client.calls("AWSEvents.ListTargetsByRule"); got != 1 {
		t.Fatalf("ListTargetsByRule calls = %d, want 1", got)
	}
	if got := client.calls("AWSEvents.DeleteRule"); got != 0 {
		t.Fatalf("DeleteRule calls = %d, want 0", got)
	}
}

func TestRuleDAOGetWarnsAndReturnsResourceWhenTargetListingFails(t *testing.T) {
	var logs bytes.Buffer
	log.Enable(&logs)
	t.Cleanup(log.Disable)

	client := &ruleHTTPClient{
		responses: map[string]ruleHTTPResponse{
			"AWSEvents.DescribeRule": {
				statusCode: http.StatusOK,
				body:       `{"Name":"nightly","Arn":"arn:aws:events:us-east-1:123456789012:rule/custom-bus/nightly","State":"ENABLED","EventBusName":"custom-bus"}`,
			},
			"AWSEvents.ListTargetsByRule": {statusCode: http.StatusInternalServerError, body: `{"message":"target list failed"}`},
		},
	}
	d := newTestRuleDAO(client)

	resource, err := d.Get(context.Background(), "custom-bus/nightly")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	rule, ok := resource.(*RuleResource)
	if !ok {
		t.Fatalf("Get() resource type = %T, want *RuleResource", resource)
	}
	if rule.GetID() != "custom-bus/nightly" {
		t.Fatalf("GetID() = %q, want %q", rule.GetID(), "custom-bus/nightly")
	}
	if len(rule.Targets) != 0 {
		t.Fatalf("Targets len = %d, want 0 after target listing failure", len(rule.Targets))
	}
	if !strings.Contains(logs.String(), "failed to list rule targets") {
		t.Fatalf("log output = %q, want target listing warning", logs.String())
	}
	if got := client.calls("AWSEvents.DeleteRule"); got != 0 {
		t.Fatalf("DeleteRule calls = %d, want 0", got)
	}
}

func newTestRuleDAO(httpClient aws.HTTPClient) *RuleDAO {
	return &RuleDAO{
		client: eventbridge.NewFromConfig(aws.Config{
			Region:     "us-east-1",
			HTTPClient: httpClient,
			Retryer: func() aws.Retryer {
				return aws.NopRetryer{}
			},
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", Source: "test"}, nil
			}),
		}),
	}
}

type ruleHTTPClient struct {
	responses map[string]ruleHTTPResponse
	targets   []string
}

type ruleHTTPResponse struct {
	statusCode int
	body       string
}

func (c *ruleHTTPClient) Do(req *http.Request) (*http.Response, error) {
	target := req.Header.Get("X-Amz-Target")
	c.targets = append(c.targets, target)

	response, ok := c.responses[target]
	if !ok {
		return nil, errors.New("unexpected EventBridge operation: " + target)
	}

	return &http.Response{
		StatusCode: response.statusCode,
		Header:     http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}},
		Body:       io.NopCloser(strings.NewReader(response.body)),
	}, nil
}

func (c *ruleHTTPClient) calls(target string) int {
	count := 0
	for _, called := range c.targets {
		if called == target {
			count++
		}
	}
	return count
}
