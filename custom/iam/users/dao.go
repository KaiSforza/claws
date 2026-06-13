package users

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"

	appaws "github.com/clawscli/claws/internal/aws"
	"github.com/clawscli/claws/internal/dao"
	"github.com/clawscli/claws/internal/enrichment"
	apperrors "github.com/clawscli/claws/internal/errors"
)

// UserDetail contains extended user information from multiple API calls
type UserDetail struct {
	User                   types.User
	AccessKeys             []types.AccessKeyMetadata
	MFADevices             []types.MFADevice
	Groups                 []types.Group
	AttachedPolicies       []types.AttachedPolicy
	InlinePolicies         []string
	AccessKeysStatus       enrichment.Status
	MFADevicesStatus       enrichment.Status
	GroupsStatus           enrichment.Status
	AttachedPoliciesStatus enrichment.Status
	InlinePoliciesStatus   enrichment.Status
}

// UserDAO provides data access for IAM Users
type UserDAO struct {
	dao.BaseDAO
	client *iam.Client
}

// NewUserDAO creates a new UserDAO
func NewUserDAO(ctx context.Context) (dao.DAO, error) {
	cfg, err := appaws.NewConfig(ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "new "+ServiceResourcePath+" dao")
	}
	return &UserDAO{
		BaseDAO: dao.NewBaseDAO("iam", "users"),
		client:  iam.NewFromConfig(cfg),
	}, nil
}

func (d *UserDAO) List(ctx context.Context) ([]dao.Resource, error) {
	// Check for UserName filter
	if userName := dao.GetFilterFromContext(ctx, "UserName"); userName != "" {
		// Direct lookup for specific user
		user, err := d.Get(ctx, userName)
		if err != nil {
			// If not found, return empty list (not an error for filtering)
			if apperrors.IsNotFound(err) {
				return []dao.Resource{}, nil
			}
			return nil, err
		}
		return []dao.Resource{user}, nil
	}

	paginator := iam.NewListUsersPaginator(d.client, &iam.ListUsersInput{})

	var resources []dao.Resource
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, apperrors.Wrap(err, "list users")
		}

		for _, user := range output.Users {
			resources = append(resources, NewUserResource(user))
		}
	}

	return resources, nil
}

func (d *UserDAO) Get(ctx context.Context, id string) (dao.Resource, error) {
	output, err := d.client.GetUser(ctx, &iam.GetUserInput{
		UserName: &id,
	})
	if err != nil {
		return nil, apperrors.Wrapf(err, "get user %s", id)
	}

	detail := UserDetail{User: *output.User}

	var wg sync.WaitGroup
	wg.Add(5)

	var accessKeys []types.AccessKeyMetadata
	var accessKeysStatus enrichment.Status
	go func() {
		defer wg.Done()
		keys, status := enrichment.Fetch(func() (*iam.ListAccessKeysOutput, error) {
			return d.client.ListAccessKeys(ctx, &iam.ListAccessKeysInput{UserName: &id})
		})
		if enrichment.Fetched == status {
			accessKeys = keys.AccessKeyMetadata
		}
		accessKeysStatus = status
	}()

	var mfaDevices []types.MFADevice
	var mfaDevicesStatus enrichment.Status
	go func() {
		defer wg.Done()
		mfa, status := enrichment.Fetch(func() (*iam.ListMFADevicesOutput, error) {
			return d.client.ListMFADevices(ctx, &iam.ListMFADevicesInput{UserName: &id})
		})
		if enrichment.Fetched == status {
			mfaDevices = mfa.MFADevices
		}
		mfaDevicesStatus = status
	}()

	var groups []types.Group
	var groupsStatus enrichment.Status
	go func() {
		defer wg.Done()
		out, status := enrichment.Fetch(func() (*iam.ListGroupsForUserOutput, error) {
			return d.client.ListGroupsForUser(ctx, &iam.ListGroupsForUserInput{UserName: &id})
		})
		if enrichment.Fetched == status {
			groups = out.Groups
		}
		groupsStatus = status
	}()

	var attachedPolicies []types.AttachedPolicy
	var attachedPoliciesStatus enrichment.Status
	go func() {
		defer wg.Done()
		policies, status := enrichment.Fetch(func() (*iam.ListAttachedUserPoliciesOutput, error) {
			return d.client.ListAttachedUserPolicies(ctx, &iam.ListAttachedUserPoliciesInput{UserName: &id})
		})
		if enrichment.Fetched == status {
			attachedPolicies = policies.AttachedPolicies
		}
		attachedPoliciesStatus = status
	}()

	var inlinePolicies []string
	var inlinePoliciesStatus enrichment.Status
	go func() {
		defer wg.Done()
		inline, status := enrichment.Fetch(func() (*iam.ListUserPoliciesOutput, error) {
			return d.client.ListUserPolicies(ctx, &iam.ListUserPoliciesInput{UserName: &id})
		})
		if enrichment.Fetched == status {
			inlinePolicies = inline.PolicyNames
		}
		inlinePoliciesStatus = status
	}()

	wg.Wait()
	detail.AccessKeys = accessKeys
	detail.AccessKeysStatus = accessKeysStatus
	detail.MFADevices = mfaDevices
	detail.MFADevicesStatus = mfaDevicesStatus
	detail.Groups = groups
	detail.GroupsStatus = groupsStatus
	detail.AttachedPolicies = attachedPolicies
	detail.AttachedPoliciesStatus = attachedPoliciesStatus
	detail.InlinePolicies = inlinePolicies
	detail.InlinePoliciesStatus = inlinePoliciesStatus

	return NewUserResourceWithDetail(detail), nil
}

func (d *UserDAO) Delete(ctx context.Context, id string) error {
	_, err := d.client.DeleteUser(ctx, &iam.DeleteUserInput{
		UserName: &id,
	})
	if err != nil {
		return apperrors.Wrapf(err, "delete user %s", id)
	}
	return nil
}

// UserResource wraps an IAM User
type UserResource struct {
	dao.BaseResource
	Item                   types.User
	AccessKeys             []types.AccessKeyMetadata
	MFADevices             []types.MFADevice
	Groups                 []types.Group
	AttachedPolicies       []types.AttachedPolicy
	InlinePolicies         []string
	AccessKeysStatus       enrichment.Status
	MFADevicesStatus       enrichment.Status
	GroupsStatus           enrichment.Status
	AttachedPoliciesStatus enrichment.Status
	InlinePoliciesStatus   enrichment.Status
}

// NewUserResource creates a new UserResource
func NewUserResource(user types.User) *UserResource {
	name := appaws.Str(user.UserName)

	return &UserResource{
		BaseResource: dao.BaseResource{
			ID:   name,
			Name: name,
			ARN:  appaws.Str(user.Arn),
			Tags: appaws.TagsToMap(user.Tags),
			Data: user,
		},
		Item: user,
	}
}

// NewUserResourceWithDetail creates a new UserResource with extended details
func NewUserResourceWithDetail(detail UserDetail) *UserResource {
	name := appaws.Str(detail.User.UserName)

	return &UserResource{
		BaseResource: dao.BaseResource{
			ID:   name,
			Name: name,
			ARN:  appaws.Str(detail.User.Arn),
			Tags: appaws.TagsToMap(detail.User.Tags),
			Data: detail.User,
		},
		Item:                   detail.User,
		AccessKeys:             detail.AccessKeys,
		MFADevices:             detail.MFADevices,
		Groups:                 detail.Groups,
		AttachedPolicies:       detail.AttachedPolicies,
		InlinePolicies:         detail.InlinePolicies,
		AccessKeysStatus:       detail.AccessKeysStatus,
		MFADevicesStatus:       detail.MFADevicesStatus,
		GroupsStatus:           detail.GroupsStatus,
		AttachedPoliciesStatus: detail.AttachedPoliciesStatus,
		InlinePoliciesStatus:   detail.InlinePoliciesStatus,
	}
}

// Path returns the user path
func (r *UserResource) Path() string {
	if r.Item.Path != nil {
		return *r.Item.Path
	}
	return ""
}

// Arn returns the user ARN
func (r *UserResource) Arn() string {
	if r.Item.Arn != nil {
		return *r.Item.Arn
	}
	return ""
}

// UserId returns the user ID
func (r *UserResource) UserId() string {
	if r.Item.UserId != nil {
		return *r.Item.UserId
	}
	return ""
}
