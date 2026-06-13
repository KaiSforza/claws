package events

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

func TestRenderDetailRedactsCloudTrailRawEvent(t *testing.T) {
	renderer := NewEventRenderer()
	resource := NewEventResource(types.Event{
		EventId:         aws.String("event-1"),
		EventName:       aws.String("CreateAccessKey"),
		AccessKeyId:     aws.String("AKIAIOSFODNN7EXAMPLE"),
		CloudTrailEvent: aws.String(`{"requestParameters":{"password":"super-secret","token":"secret-token"},"responseElements":{"accessKeyId":"AKIAIOSFODNN7EXAMPLE"}}`),
	})

	out := renderer.RenderDetail(resource)
	for _, leaked := range []string{"super-secret", "secret-token", "AKIAIOSFODNN7EXAMPLE"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("RenderDetail leaked %q in:\n%s", leaked, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("RenderDetail did not include redaction marker:\n%s", out)
	}
}
