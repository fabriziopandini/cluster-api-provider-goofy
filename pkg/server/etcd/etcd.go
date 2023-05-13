package etcd

import (
	"context"
	"github.com/fabriziopandini/cluster-api-provider-goofy/resources/pki"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"log"
	"net/http"
)

func NewEtcdServer() http.Handler {
	creds, err := credentials.NewServerTLSFromFile(string(pki.APIServerCertificateData()), string(pki.APIServerKeyData()))
	if err != nil {
		log.Fatalf("Failed to setup TLS: %v", err)
	}

	svr := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterClusterServer(svr, &clusterServerService{})

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
	// TODO implement me
	/*
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {

		}
		 fmt.Println("Works!", md.Get(":authority"))
	*/
	return &pb.MemberListResponse{}, nil
}

func (c clusterServerService) MemberPromote(ctx context.Context, request *pb.MemberPromoteRequest) (*pb.MemberPromoteResponse, error) {
	// TODO implement me
	panic("implement me")
}
