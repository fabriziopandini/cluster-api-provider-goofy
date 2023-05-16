package etcd

import (
	"context"
	"fmt"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"net/http"
)

func NewEtcdServerHandler() http.Handler {
	svr := grpc.NewServer()

	mySvc := &clusterServerService{}
	pb.RegisterClusterServer(svr, mySvc)

	return svr
}

type clusterServerService struct{}

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

	localAddr := ctx.Value(http.LocalAddrContextKey)
	// TODO it is required to figure out how to go back to a cluster/pod
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {

	}
	fmt.Println("Works!", md.Get(":authority"), localAddr)

	return &pb.MemberListResponse{}, nil
}

func (c clusterServerService) MemberPromote(ctx context.Context, request *pb.MemberPromoteRequest) (*pb.MemberPromoteResponse, error) {
	// TODO implement me
	panic("implement me")
}
