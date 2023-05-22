package etcd

import (
	"context"
	"fmt"
	cmanager "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/manager"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"net/http"
	"strings"
)

// ResourceGroupResolver defines a func that can identify which workloadCluster/resourceGroup a
// request targets to.
type ResourceGroupResolver func(host string) (string, error)

// NewEtcdServerHandler returns an http.Handler for fake etcd members.
func NewEtcdServerHandler(manager cmanager.Manager, log logr.Logger, resolver ResourceGroupResolver) http.Handler {
	svr := grpc.NewServer()

	mySvc := &clusterServerService{
		manager:               manager,
		log:                   log,
		resourceGroupResolver: resolver,
	}
	pb.RegisterClusterServer(svr, mySvc)

	return svr
}

// clusterServerService implements the ClusterServer grpc server.
type clusterServerService struct {
	manager               cmanager.Manager
	log                   logr.Logger
	resourceGroupResolver ResourceGroupResolver
}

func (c clusterServerService) MemberAdd(ctx context.Context, request *pb.MemberAddRequest) (*pb.MemberAddResponse, error) {
	// TODO implement me

	panic("implement me")
}

func (c clusterServerService) MemberRemove(ctx context.Context, request *pb.MemberRemoveRequest) (*pb.MemberRemoveResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (c clusterServerService) MemberUpdate(ctx context.Context, request *pb.MemberUpdateRequest) (*pb.MemberUpdateResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (c clusterServerService) MemberList(ctx context.Context, request *pb.MemberListRequest) (*pb.MemberListResponse, error) {
	resourceGroup, etcdMember, err := c.getResourceGroupAndMember(ctx)
	if err != nil {
		return nil, err
	}

	c.log.Info("Etcd Works!", "resourceGroup", resourceGroup, "etcdMember", etcdMember)

	return &pb.MemberListResponse{}, nil
}

func (c clusterServerService) MemberPromote(ctx context.Context, request *pb.MemberPromoteRequest) (*pb.MemberPromoteResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (c clusterServerService) getResourceGroupAndMember(ctx context.Context) (resourceGroup string, etcdMember string, err error) {
	localAddr := ctx.Value(http.LocalAddrContextKey)
	resourceGroup, err = c.resourceGroupResolver(fmt.Sprintf("%s", localAddr))
	if err != nil {
		return
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return resourceGroup, "", errors.Errorf("failed to get metadata when processing request to etcd in resourceGroup %s", resourceGroup)
	}
	etcdMember = strings.Join(md.Get(":authority"), ",")
	return
}
