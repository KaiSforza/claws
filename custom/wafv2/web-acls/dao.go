package webacls

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	"github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	appaws "github.com/clawscli/claws/internal/aws"
	appconfig "github.com/clawscli/claws/internal/config"
	"github.com/clawscli/claws/internal/dao"
	apperrors "github.com/clawscli/claws/internal/errors"
)

const (
	cloudFrontWebACLRegion = "us-east-1"
	webACLIDParts          = 3
)

type parsedWebACLID struct {
	scope types.Scope
	name  string
	id    string
}

type webACLDetailResult struct {
	resource  *WebACLResource
	lockToken *string
}

// WebACLDAO provides data access for WAFv2 Web ACLs
type WebACLDAO struct {
	dao.BaseDAO
	client *wafv2.Client
}

// NewWebACLDAO creates a new WebACLDAO
func NewWebACLDAO(ctx context.Context) (dao.DAO, error) {
	cfg, err := appaws.NewConfig(ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "new "+ServiceResourcePath+" dao")
	}
	return &WebACLDAO{
		BaseDAO: dao.NewBaseDAO("wafv2", "web-acls"),
		client:  wafv2.NewFromConfig(cfg),
	}, nil
}

// List returns all WAFv2 Web ACLs (both REGIONAL and CLOUDFRONT scopes)
func (d *WebACLDAO) List(ctx context.Context) ([]dao.Resource, error) {
	var resources []dao.Resource

	// List REGIONAL Web ACLs
	regionalResources, err := d.listByScope(ctx, types.ScopeRegional)
	if err != nil {
		return nil, apperrors.Wrap(err, "list regional web acls")
	}
	resources = append(resources, regionalResources...)

	// List CLOUDFRONT Web ACLs only from us-east-1, where WAFv2 exposes CloudFront scope.
	if currentRegion(ctx) != cloudFrontWebACLRegion {
		return resources, nil
	}
	cloudfrontResources, err := d.listByScope(ctx, types.ScopeCloudfront)
	if err != nil {
		return resources, apperrors.Wrap(err, "list cloudfront web acls")
	}
	resources = append(resources, cloudfrontResources...)

	return resources, nil
}

func currentRegion(ctx context.Context) string {
	if region := appaws.GetRegionFromContext(ctx); region != "" {
		return region
	}
	return appconfig.Global().Region()
}

func (d *WebACLDAO) listByScope(ctx context.Context, scope types.Scope) ([]dao.Resource, error) {
	acls, err := appaws.Paginate(ctx, func(token *string) ([]types.WebACLSummary, *string, error) {
		output, err := d.client.ListWebACLs(ctx, &wafv2.ListWebACLsInput{
			Scope:      scope,
			NextMarker: token,
		})
		if err != nil {
			return nil, nil, err
		}
		return output.WebACLs, output.NextMarker, nil
	})
	if err != nil {
		return nil, err
	}

	resources := make([]dao.Resource, len(acls))
	for i, acl := range acls {
		resources[i] = NewWebACLResourceFromSummary(acl, scope)
	}

	return resources, nil
}

// Get returns a specific WAFv2 Web ACL by ID.
// List-created resource IDs are the raw AWS Web ACL ID. Explicit scope/name/id
// IDs can be resolved directly without listing.
func (d *WebACLDAO) Get(ctx context.Context, id string) (dao.Resource, error) {
	detail, err := d.findWebACLDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	return detail.resource, nil
}

func parseWebACLID(id string) (parsedWebACLID, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != webACLIDParts || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return parsedWebACLID{}, false
	}

	scope := types.Scope(parts[0])
	if scope != types.ScopeRegional && scope != types.ScopeCloudfront {
		return parsedWebACLID{}, false
	}

	return parsedWebACLID{scope: scope, name: parts[1], id: parts[2]}, true
}

func (d *WebACLDAO) findWebACLDetail(ctx context.Context, id string) (webACLDetailResult, error) {
	if parsed, ok := parseWebACLID(id); ok {
		return d.getWebACL(ctx, parsed.scope, parsed.name, parsed.id)
	}

	var scopeErrs []error
	for _, scope := range d.scopes(ctx) {
		resources, err := d.listByScope(ctx, scope)
		if err != nil {
			scopeErrs = append(scopeErrs, apperrors.Wrapf(err, "list %s web acls", scope))
			continue
		}

		for _, res := range resources {
			if acl, ok := res.(*WebACLResource); ok {
				if acl.GetID() == id || acl.WebACLId() == id {
					return d.getWebACLDetail(ctx, acl)
				}
			}
		}
	}

	if len(scopeErrs) > 0 {
		return webACLDetailResult{}, errors.Join(append([]error{errors.New("web acl " + id + " not found")}, scopeErrs...)...)
	}
	return webACLDetailResult{}, errors.New("web acl " + id + " not found")
}

func (d *WebACLDAO) scopes(ctx context.Context) []types.Scope {
	if currentRegion(ctx) == cloudFrontWebACLRegion {
		return []types.Scope{types.ScopeRegional, types.ScopeCloudfront}
	}
	return []types.Scope{types.ScopeRegional}
}

func (d *WebACLDAO) getWebACLDetail(ctx context.Context, summary *WebACLResource) (webACLDetailResult, error) {
	return d.getWebACL(ctx, summary.Scope, appaws.Str(summary.Summary.Name), appaws.Str(summary.Summary.Id))
}

func (d *WebACLDAO) getWebACL(ctx context.Context, scope types.Scope, name string, id string) (webACLDetailResult, error) {
	input := &wafv2.GetWebACLInput{
		Name:  &name,
		Id:    &id,
		Scope: scope,
	}

	output, err := d.client.GetWebACL(ctx, input)
	if err != nil {
		return webACLDetailResult{}, apperrors.Wrap(err, "get web acl")
	}

	return webACLDetailResult{
		resource:  NewWebACLResourceFromDetail(output.WebACL, scope),
		lockToken: output.LockToken,
	}, nil
}

// Delete deletes a WAFv2 Web ACL
func (d *WebACLDAO) Delete(ctx context.Context, id string) error {
	detail, err := d.findWebACLDetail(ctx, id)
	if err != nil {
		return err
	}

	acl := detail.resource
	if acl == nil || acl.Detail == nil {
		return errors.New("invalid resource type")
	}

	// Delete the Web ACL
	deleteInput := &wafv2.DeleteWebACLInput{
		Name:      acl.Detail.Name,
		Id:        acl.Detail.Id,
		Scope:     acl.Scope,
		LockToken: detail.lockToken,
	}

	_, err = d.client.DeleteWebACL(ctx, deleteInput)
	if err != nil {
		return apperrors.Wrap(err, "delete web acl")
	}

	return nil
}

// WebACLResource represents a WAFv2 Web ACL
type WebACLResource struct {
	dao.BaseResource
	Summary *types.WebACLSummary
	Detail  *types.WebACL
	Scope   types.Scope
}

// NewWebACLResourceFromSummary creates a new WebACLResource from summary
func NewWebACLResourceFromSummary(summary types.WebACLSummary, scope types.Scope) *WebACLResource {
	id := appaws.Str(summary.Id)
	name := appaws.Str(summary.Name)
	arn := appaws.Str(summary.ARN)

	return &WebACLResource{
		BaseResource: dao.BaseResource{
			ID:   id,
			Name: name,
			ARN:  arn,
			Tags: make(map[string]string),
			Data: summary,
		},
		Summary: &summary,
		Scope:   scope,
	}
}

// NewWebACLResourceFromDetail creates a new WebACLResource from detail
func NewWebACLResourceFromDetail(detail *types.WebACL, scope types.Scope) *WebACLResource {
	id := appaws.Str(detail.Id)
	name := appaws.Str(detail.Name)
	arn := appaws.Str(detail.ARN)

	return &WebACLResource{
		BaseResource: dao.BaseResource{
			ID:   id,
			Name: name,
			ARN:  arn,
			Tags: make(map[string]string),
			Data: detail,
		},
		Detail: detail,
		Scope:  scope,
	}
}

// WebACLName returns the web ACL name
func (r *WebACLResource) WebACLName() string {
	if r.Summary != nil {
		return appaws.Str(r.Summary.Name)
	}
	if r.Detail != nil {
		return appaws.Str(r.Detail.Name)
	}
	return ""
}

// WebACLId returns the web ACL ID
func (r *WebACLResource) WebACLId() string {
	if r.Summary != nil {
		return appaws.Str(r.Summary.Id)
	}
	if r.Detail != nil {
		return appaws.Str(r.Detail.Id)
	}
	return ""
}

// ScopeString returns the scope as string
func (r *WebACLResource) ScopeString() string {
	return string(r.Scope)
}

// RuleCount returns the number of rules
func (r *WebACLResource) RuleCount() int {
	if r.Detail != nil {
		return len(r.Detail.Rules)
	}
	return 0
}

// Rules returns the rules
func (r *WebACLResource) Rules() []types.Rule {
	if r.Detail != nil {
		return r.Detail.Rules
	}
	return nil
}

// DefaultAction returns the default action
func (r *WebACLResource) DefaultAction() string {
	if r.Detail != nil && r.Detail.DefaultAction != nil {
		if r.Detail.DefaultAction.Allow != nil {
			return "ALLOW"
		}
		if r.Detail.DefaultAction.Block != nil {
			return "BLOCK"
		}
	}
	return ""
}

// Description returns the description
func (r *WebACLResource) Description() string {
	if r.Summary != nil {
		return appaws.Str(r.Summary.Description)
	}
	if r.Detail != nil {
		return appaws.Str(r.Detail.Description)
	}
	return ""
}

// ManagedByFirewallManager returns whether managed by Firewall Manager
func (r *WebACLResource) ManagedByFirewallManager() bool {
	if r.Detail != nil {
		return r.Detail.ManagedByFirewallManager
	}
	return false
}

// Capacity returns the WCU capacity
func (r *WebACLResource) Capacity() int64 {
	if r.Detail != nil {
		return r.Detail.Capacity
	}
	return 0
}

// LabelNamespace returns the label namespace
func (r *WebACLResource) LabelNamespace() string {
	if r.Detail != nil {
		return appaws.Str(r.Detail.LabelNamespace)
	}
	return ""
}
