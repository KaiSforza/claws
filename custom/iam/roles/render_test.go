package roles

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

func TestRoleRendererStatusOutput(t *testing.T) {
	renderer := &RoleRenderer{}

	tests := []struct {
		name     string
		resource *RoleResource
	}{
		{
			name: "access_denied",
			resource: roleRenderResource(func(r *RoleResource) {
				r.AttachedPoliciesStatus = enrichment.AccessDenied
				r.InlinePoliciesStatus = enrichment.AccessDenied
			}),
		},
		{
			name:     "fetched_empty",
			resource: roleRenderResource(func(*RoleResource) {}),
		},
		{
			name: "fetched_list",
			resource: roleRenderResource(func(r *RoleResource) {
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

func roleRenderResource(mutator func(*RoleResource)) *RoleResource {
	resource := NewRoleResource(types.Role{
		RoleName:           aws.String("my-role"),
		RoleId:             aws.String("AROAEXAMPLE12345"),
		Arn:                aws.String("arn:aws:iam::123456789012:role/my-role"),
		Path:               aws.String("/service-role/"),
		MaxSessionDuration: aws.Int32(3600),
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
