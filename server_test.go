package seabird_webhook_receiver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/seabird-chat/seabird-go/pb"
)

// fakeSeabirdCore is a seabird-core stand-in whose event stream ends as soon
// as it is opened, like a stream cut off by a core restart.
type fakeSeabirdCore struct {
	pb.UnimplementedSeabirdServer
}

func (fakeSeabirdCore) StreamEvents(*pb.StreamEventsRequest, pb.Seabird_StreamEventsServer) error {
	return nil
}

func TestRunReturnsWhenSeabirdStreamEnds(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterSeabirdServer(grpcServer, fakeSeabirdCore{})
	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	server, err := NewServer(Config{
		Logger:         zerolog.Nop(),
		SeabirdHost:    "http://" + lis.Addr().String(),
		SeabirdToken:   "token",
		SeabirdChannel: "channel",
	})
	if err != nil {
		t.Fatal(err)
	}
	server.listenAddr = "127.0.0.1:0"

	done := make(chan error, 1)
	go func() { done <- server.Run(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil, want the seabird stream error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the seabird stream ended")
	}
}
