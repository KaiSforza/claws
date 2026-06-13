package buckets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clawscli/claws/internal/enrichment"
	"github.com/clawscli/claws/internal/render"
)

func TestBucketRendererStatusOutput(t *testing.T) {
	renderer := &BucketRenderer{}

	tests := []struct {
		name     string
		resource *BucketResource
	}{
		{
			name: "access_denied",
			resource: bucketRenderResource(func(r *BucketResource) {
				r.VersioningStatus = enrichment.AccessDenied
				r.EncryptionStatus = enrichment.AccessDenied
				r.PublicAccessBlockStatus = enrichment.AccessDenied
				r.ObjectLockStatus = enrichment.AccessDenied
				r.LifecycleStatus = enrichment.AccessDenied
				r.TagsStatus = enrichment.AccessDenied
			}),
		},
		{
			name: "not_configured",
			resource: bucketRenderResource(func(r *BucketResource) {
				r.VersioningStatus = enrichment.NotConfigured
				r.EncryptionStatus = enrichment.NotConfigured
				r.PublicAccessBlockStatus = enrichment.NotConfigured
				r.ObjectLockStatus = enrichment.NotConfigured
				r.LifecycleStatus = enrichment.NotConfigured
				r.TagsStatus = enrichment.NotConfigured
			}),
		},
		{
			name: "fetched_empty",
			resource: bucketRenderResource(func(r *BucketResource) {
				r.VersioningStatus = enrichment.NotConfigured
				r.EncryptionStatus = enrichment.NotConfigured
				r.PublicAccessBlockStatus = enrichment.NotConfigured
			}),
		},
		{
			name: "fetched_list",
			resource: bucketRenderResource(func(r *BucketResource) {
				r.VersioningStatus = enrichment.Configured
				r.Versioning = "Enabled"
				r.MFADelete = "Disabled"
				r.EncryptionEnabled = true
				r.EncryptionStatus = enrichment.Configured
				r.EncryptionAlgorithm = "aws:kms"
				r.EncryptionKMSKeyID = "arn:aws:kms:us-east-1:123456789012:key/example"
				r.BucketKeyEnabled = true
				r.PublicAccessBlockStatus = enrichment.Configured
				r.PublicAccessBlock = &PublicAccessBlockInfo{BlockPublicAcls: true, IgnorePublicAcls: true, BlockPublicPolicy: true, RestrictPublicBuckets: true}
				r.ObjectLockEnabled = true
				r.ObjectLockStatus = enrichment.Configured
				r.ObjectLockMode = "GOVERNANCE"
				r.ObjectLockRetention = "30 days"
				r.LifecycleStatus = enrichment.Configured
				r.LifecycleRulesCount = 2
				r.Tags = map[string]string{"Environment": "prod"}
				r.TagsStatus = enrichment.Configured
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

func bucketRenderResource(mutator func(*BucketResource)) *BucketResource {
	resource := &BucketResource{
		BucketName: "my-bucket",
		Region:     "us-east-1",
	}
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
