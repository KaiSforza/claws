package users

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/clawscli/claws/internal/enrichment"
	"github.com/clawscli/claws/internal/render"
)

func TestUserRendererStatusOutput(t *testing.T) {
	renderer := &UserRenderer{}

	tests := []struct {
		name     string
		resource *UserResource
	}{
		{
			name: "access_denied",
			resource: userRenderResource(func(r *UserResource) {
				r.AccessKeysStatus = enrichment.AccessDenied
				r.MFADevicesStatus = enrichment.AccessDenied
				r.GroupsStatus = enrichment.AccessDenied
				r.AttachedPoliciesStatus = enrichment.AccessDenied
				r.InlinePoliciesStatus = enrichment.AccessDenied
			}),
		},
		{
			name:     "fetched_empty",
			resource: userRenderResource(func(*UserResource) {}),
		},
		{
			name: "fetched_list",
			resource: userRenderResource(func(r *UserResource) {
				r.AccessKeys = []types.AccessKeyMetadata{{AccessKeyId: aws.String("AKIAEXAMPLE"), Status: types.StatusTypeActive}}
				r.MFADevices = []types.MFADevice{{SerialNumber: aws.String("arn:aws:iam::123456789012:mfa/my-user")}}
				r.Groups = []types.Group{{GroupName: aws.String("admins")}}
				r.AttachedPolicies = []types.AttachedPolicy{{PolicyName: aws.String("ReadOnlyAccess")}}
				r.InlinePolicies = []string{"inline"}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderer.RenderDetail(tt.resource) + "\n---SUMMARY---\n" + renderSummaryFields(renderer.RenderSummary(tt.resource))
			assertGolden(t, tt.name, got)
		})
	}
}

func userRenderResource(mutator func(*UserResource)) *UserResource {
	resource := NewUserResource(types.User{
		UserName: aws.String("my-user"),
		UserId:   aws.String("AIDAEXAMPLE12345"),
		Arn:      aws.String("arn:aws:iam::123456789012:user/my-user"),
		Path:     aws.String("/"),
	})
	mutator(resource)
	return resource
}

func renderSummaryFields(fields []render.SummaryField) string {
	var b strings.Builder
	for _, field := range fields {
		b.WriteString(field.Label)
		b.WriteString("=")
		b.WriteString(field.Value)
		b.WriteString("\n")
	}
	return b.String()
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "render_"+name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("render output mismatch (-want +got)\nwant:\n%s\ngot:\n%s", string(want), got)
	}
}
