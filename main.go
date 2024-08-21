package main

import (
	"github.com/tomekjarosik/fugaci/pkg/imageservice"
	"github.com/tomekjarosik/fugaci/pkg/runtimeservice"
	"google.golang.org/grpc"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"log"
	"net"
	"os"
)

func listenSocket() {

}

func main() {
	const socketPath = "/var/run/fugaci/fugaci.sock"

	// Remove the socket file if it already exists
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("failed to remove existing socket file: %v", err)
	}

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer os.Remove(socketPath)

	file, err := os.OpenFile("fugaci.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	grpcServer := grpc.NewServer()
	imagesvc := imageservice.NewLoggingImageService(imageservice.NewInMemoryImageService())
	runtimeapi.RegisterImageServiceServer(grpcServer, imagesvc)

	runtimesvc := runtimeservice.NewLoggingRuntimeService(runtimeservice.NewInMemoryRuntimeService(imagesvc))
	runtimeapi.RegisterRuntimeServiceServer(grpcServer, runtimesvc)

	log.Printf("Server listening at socket %s\n", socketPath)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
