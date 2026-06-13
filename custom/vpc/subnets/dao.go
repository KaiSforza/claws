package subnets

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	appaws "github.com/clawscli/claws/internal/aws"
	"github.com/clawscli/claws/internal/dao"
	apperrors "github.com/clawscli/claws/internal/errors"
)

// SubnetDAO provides data access for Subnets
type SubnetDAO struct {
	dao.BaseDAO
	client *ec2.Client
}

// NewSubnetDAO creates a new SubnetDAO
func NewSubnetDAO(ctx context.Context) (dao.DAO, error) {
	cfg, err := appaws.NewConfig(ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "new "+ServiceResourcePath+" dao")
	}
	return &SubnetDAO{
		BaseDAO: dao.NewBaseDAO("vpc", "subnets"),
		client:  ec2.NewFromConfig(cfg),
	}, nil
}

func (d *SubnetDAO) List(ctx context.Context) ([]dao.Resource, error) {
	output, err := d.client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{})
	if err != nil {
		return nil, apperrors.Wrap(err, "describe subnets")
	}

	// Get route tables to determine public/private subnets.
	publicIndex := d.getPublicSubnetIndex(ctx)

	var resources []dao.Resource
	for _, subnet := range output.Subnets {
		resources = append(resources, NewSubnetResourceWithPublic(subnet, publicIndex.IsPublic(subnet)))
	}

	return resources, nil
}

func (d *SubnetDAO) Get(ctx context.Context, id string) (dao.Resource, error) {
	output, err := d.client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{id},
	})
	if err != nil {
		return nil, apperrors.Wrapf(err, "describe subnet %s", id)
	}

	if len(output.Subnets) == 0 {
		return nil, fmt.Errorf("subnet not found: %s", id)
	}

	publicIndex := d.getPublicSubnetIndex(ctx)
	return NewSubnetResourceWithPublic(output.Subnets[0], publicIndex.IsPublic(output.Subnets[0])), nil
}

func (d *SubnetDAO) Delete(ctx context.Context, id string) error {
	_, err := d.client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
		SubnetId: &id,
	})
	if err != nil {
		if apperrors.IsNotFound(err) {
			return nil // Already deleted
		}
		if apperrors.IsResourceInUse(err) {
			return apperrors.Wrapf(err, "subnet %s is in use", id)
		}
		return apperrors.Wrapf(err, "delete subnet %s", id)
	}
	return nil
}

type publicSubnetIndex struct {
	publicSubnets               map[string]struct{}
	publicVPCs                  map[string]struct{}
	explicitlyAssociatedSubnets map[string]struct{}
}

func newPublicSubnetIndex() publicSubnetIndex {
	return publicSubnetIndex{
		publicSubnets:               make(map[string]struct{}),
		publicVPCs:                  make(map[string]struct{}),
		explicitlyAssociatedSubnets: make(map[string]struct{}),
	}
}

// IsPublic returns whether a subnet is public based on route table associations.
func (i publicSubnetIndex) IsPublic(subnet types.Subnet) bool {
	if subnet.SubnetId == nil {
		return false
	}

	subnetID := *subnet.SubnetId
	if _, ok := i.publicSubnets[subnetID]; ok {
		return true
	}

	if subnet.VpcId == nil {
		return false
	}
	if _, ok := i.publicVPCs[*subnet.VpcId]; !ok {
		return false
	}

	_, hasExplicitAssociation := i.explicitlyAssociatedSubnets[subnetID]
	return !hasExplicitAssociation
}

// getPublicSubnetIndex returns subnet IDs with IGW routes, VPC IDs whose main RT has an IGW route,
// and subnet IDs that have explicit route table associations.
func (d *SubnetDAO) getPublicSubnetIndex(ctx context.Context) publicSubnetIndex {
	index := newPublicSubnetIndex()

	// Get all route tables
	rtOutput, err := d.client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{})
	if err != nil {
		return index // Return empty on error, fail gracefully
	}

	return buildPublicSubnetIndex(rtOutput.RouteTables)
}

func buildPublicSubnetIndex(routeTables []types.RouteTable) publicSubnetIndex {
	index := newPublicSubnetIndex()

	for _, rt := range routeTables {
		hasIGWRoute := hasInternetGatewayRoute(rt)

		if !hasIGWRoute {
			for _, assoc := range rt.Associations {
				if assoc.SubnetId != nil {
					index.explicitlyAssociatedSubnets[*assoc.SubnetId] = struct{}{}
				}
			}
			continue
		}

		// Mark explicitly associated subnets as public
		for _, assoc := range rt.Associations {
			if assoc.SubnetId != nil {
				index.explicitlyAssociatedSubnets[*assoc.SubnetId] = struct{}{}
				index.publicSubnets[*assoc.SubnetId] = struct{}{}
			}
			// If this is the main route table for a VPC with IGW route
			if assoc.Main != nil && *assoc.Main && rt.VpcId != nil {
				index.publicVPCs[*rt.VpcId] = struct{}{}
			}
		}
	}

	return index
}

func hasInternetGatewayRoute(routeTable types.RouteTable) bool {
	for _, route := range routeTable.Routes {
		if route.GatewayId != nil && strings.HasPrefix(*route.GatewayId, "igw-") {
			return true
		}
	}
	return false
}

// SubnetResource wraps a Subnet
type SubnetResource struct {
	dao.BaseResource
	Item     types.Subnet
	isPublic bool
}

// NewSubnetResource creates a new SubnetResource (for backward compatibility)
func NewSubnetResource(subnet types.Subnet) *SubnetResource {
	return NewSubnetResourceWithPublic(subnet, false)
}

// NewSubnetResourceWithPublic creates a new SubnetResource with public flag
func NewSubnetResourceWithPublic(subnet types.Subnet, isPublic bool) *SubnetResource {
	return &SubnetResource{
		BaseResource: dao.BaseResource{
			ID:   appaws.Str(subnet.SubnetId),
			Name: appaws.EC2NameTag(subnet.Tags),
			Tags: appaws.TagsToMap(subnet.Tags),
			Data: subnet,
		},
		Item:     subnet,
		isPublic: isPublic,
	}
}

// IsPublic returns whether this is a public subnet (has route to IGW)
func (r *SubnetResource) IsPublic() bool {
	return r.isPublic
}

// State returns the subnet state
func (r *SubnetResource) State() string {
	return string(r.Item.State)
}

// VpcId returns the VPC ID
func (r *SubnetResource) VpcId() string {
	if r.Item.VpcId != nil {
		return *r.Item.VpcId
	}
	return ""
}

// CidrBlock returns the CIDR block
func (r *SubnetResource) CidrBlock() string {
	if r.Item.CidrBlock != nil {
		return *r.Item.CidrBlock
	}
	return ""
}

// AvailabilityZone returns the AZ
func (r *SubnetResource) AvailabilityZone() string {
	if r.Item.AvailabilityZone != nil {
		return *r.Item.AvailabilityZone
	}
	return ""
}

// MapPublicIpOnLaunch returns whether public IPs are assigned
func (r *SubnetResource) MapPublicIpOnLaunch() bool {
	if r.Item.MapPublicIpOnLaunch != nil {
		return *r.Item.MapPublicIpOnLaunch
	}
	return false
}

// AvailableIpAddressCount returns the count of available IPs
func (r *SubnetResource) AvailableIpAddressCount() int32 {
	if r.Item.AvailableIpAddressCount != nil {
		return *r.Item.AvailableIpAddressCount
	}
	return 0
}
