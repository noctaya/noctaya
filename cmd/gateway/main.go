/*
Copyright 2026 The Noctaya Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command gateway runs the Noctaya data-plane proxy for a single LLMService.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/noctaya/noctaya/internal/gateway"
)

var version = "dev"

const shutdownTimeout = 20 * time.Second

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit.")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg := gateway.ConfigFromEnv()
	if cfg.BackendURL == "" {
		log.Fatalf("%s is required", gateway.EnvBackendURL)
	}

	gw, err := gateway.New(cfg)
	if err != nil {
		log.Fatalf("Failed to build gateway: %v", err)
	}
	defer gw.Close()

	addr := os.Getenv(gateway.EnvListenAddr)
	if addr == "" {
		addr = gateway.DefaultListenAddr
	}

	scalerAddr := os.Getenv(gateway.EnvScalerListenAddr)
	if scalerAddr == "" {
		scalerAddr = gateway.DefaultScalerListenAddr
	}
	log.Printf("Noctaya gateway version %s listening on %s, backend %s", version, addr, cfg.BackendURL)
	log.Printf("Noctaya external scaler listening on %s", scalerAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, gw, addr, scalerAddr); err != nil {
		log.Fatalf("Gateway server failed: %v", err)
	}
}

func serve(ctx context.Context, gw *gateway.Gateway, addr, scalerAddr string) error {
	httpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP: %w", err)
	}
	httpServer := &http.Server{
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	scalerListener, err := net.Listen("tcp", scalerAddr)
	if err != nil {
		_ = httpListener.Close()
		return fmt.Errorf("listen for external scaler gRPC: %w", err)
	}
	grpcServer := grpc.NewServer()
	gateway.RegisterExternalScalerServer(grpcServer, gw)

	serverErrors := make(chan error, 2)
	go func() {
		err := httpServer.Serve(httpListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErrors <- err
	}()
	go func() {
		err := grpcServer.Serve(scalerListener)
		if errors.Is(err, grpc.ErrServerStopped) {
			err = nil
		}
		serverErrors <- err
	}()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverErrors:
	}
	shutdown(httpServer, grpcServer)
	return serveErr
}

func shutdown(httpServer *http.Server, grpcServer *grpc.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	httpDone := make(chan struct{})
	go func() {
		_ = httpServer.Shutdown(ctx)
		close(httpDone)
	}()
	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()

	select {
	case <-grpcDone:
	case <-ctx.Done():
		grpcServer.Stop()
	}
	select {
	case <-httpDone:
	case <-ctx.Done():
		_ = httpServer.Close()
	}
}
