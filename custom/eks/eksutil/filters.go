package eksutil

import (
	"context"
	"errors"

	"github.com/clawscli/claws/internal/dao"
)

const ClusterNameFilter = "ClusterName"

func RequireClusterName(ctx context.Context) (string, error) {
	clusterName := dao.GetFilterFromContext(ctx, ClusterNameFilter)
	if clusterName == "" {
		return "", errors.New("ClusterName filter required")
	}
	return clusterName, nil
}
