package subnets

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestPublicSubnetIndex_IsPublic(t *testing.T) {
	t.Parallel()

	index := buildPublicSubnetIndex([]types.RouteTable{
		{
			VpcId: aws.String("vpc-public-main"),
			Routes: []types.Route{
				{GatewayId: aws.String("igw-123")},
			},
			Associations: []types.RouteTableAssociation{
				{Main: aws.Bool(true)},
			},
		},
		{
			VpcId: aws.String("vpc-public-main"),
			Routes: []types.Route{
				{GatewayId: aws.String("nat-123")},
			},
			Associations: []types.RouteTableAssociation{
				{SubnetId: aws.String("subnet-explicit-private")},
			},
		},
		{
			VpcId: aws.String("vpc-private-main"),
			Routes: []types.Route{
				{GatewayId: aws.String("nat-456")},
			},
			Associations: []types.RouteTableAssociation{
				{Main: aws.Bool(true)},
			},
		},
		{
			VpcId: aws.String("vpc-private-main"),
			Routes: []types.Route{
				{GatewayId: aws.String("igw-456")},
			},
			Associations: []types.RouteTableAssociation{
				{SubnetId: aws.String("subnet-explicit-public")},
			},
		},
	})

	tests := []struct {
		name   string
		subnet types.Subnet
		want   bool
	}{
		{
			name: "explicit public route table association",
			subnet: types.Subnet{
				SubnetId: aws.String("subnet-explicit-public"),
				VpcId:    aws.String("vpc-private-main"),
			},
			want: true,
		},
		{
			name: "inherits public main route table without explicit association",
			subnet: types.Subnet{
				SubnetId: aws.String("subnet-public-main"),
				VpcId:    aws.String("vpc-public-main"),
			},
			want: true,
		},
		{
			name: "explicit private route table overrides public main route table",
			subnet: types.Subnet{
				SubnetId: aws.String("subnet-explicit-private"),
				VpcId:    aws.String("vpc-public-main"),
			},
			want: false,
		},
		{
			name: "private main route table without explicit association",
			subnet: types.Subnet{
				SubnetId: aws.String("subnet-private-main"),
				VpcId:    aws.String("vpc-private-main"),
			},
			want: false,
		},
		{
			name: "missing subnet id is private",
			subnet: types.Subnet{
				VpcId: aws.String("vpc-public-main"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := index.IsPublic(tt.subnet)
			if got != tt.want {
				t.Fatalf("IsPublic() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasInternetGatewayRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		routeTable types.RouteTable
		want       bool
	}{
		{
			name: "internet gateway route",
			routeTable: types.RouteTable{
				Routes: []types.Route{{GatewayId: aws.String("igw-123")}},
			},
			want: true,
		},
		{
			name: "nat gateway route",
			routeTable: types.RouteTable{
				Routes: []types.Route{{GatewayId: aws.String("nat-123")}},
			},
			want: false,
		},
		{
			name: "nil gateway id",
			routeTable: types.RouteTable{
				Routes: []types.Route{{GatewayId: nil}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := hasInternetGatewayRoute(tt.routeTable)
			if got != tt.want {
				t.Fatalf("hasInternetGatewayRoute() = %v, want %v", got, tt.want)
			}
		})
	}
}
