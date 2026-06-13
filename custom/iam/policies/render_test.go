package policies

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

func TestPolicyRendererStatusOutput(t *testing.T) {
	renderer := &PolicyRenderer{}

	tests := []struct {
		name     string
		resource *PolicyResource
	}{
		{
			name: "access_denied",
			resource: policyRenderResource(func(r *PolicyResource) {
				r.AttachedEntitiesStatus = enrichment.AccessDenied
				r.PolicyDocumentStatus = enrichment.AccessDenied
			}),
		},
		{
			name:     "fetched_empty",
			resource: policyRenderResource(func(*PolicyResource) {}),
		},
		{
			name: "fetched_list",
			resource: policyRenderResource(func(r *PolicyResource) {
				r.AttachedUsers = []types.PolicyUser{{UserName: aws.String("my-user")}}
				r.AttachedRoles = []types.PolicyRole{{RoleName: aws.String("my-role")}}
				r.AttachedGroups = []types.PolicyGroup{{GroupName: aws.String("admins")}}
				r.PolicyDocument = `%7B%22Version%22%3A%222012-10-17%22%2C%22Statement%22%3A%5B%5D%7D`
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

func policyRenderResource(mutator func(*PolicyResource)) *PolicyResource {
	resource := NewPolicyResource(types.Policy{
		PolicyName:       aws.String("my-policy"),
		PolicyId:         aws.String("ANPAEXAMPLE12345"),
		Arn:              aws.String("arn:aws:iam::123456789012:policy/my-policy"),
		Path:             aws.String("/"),
		IsAttachable:     true,
		AttachmentCount:  aws.Int32(0),
		DefaultVersionId: aws.String("v1"),
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
