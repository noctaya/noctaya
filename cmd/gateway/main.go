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

// Command gateway runs the Noctaya data-plane proxy or demand aggregator.
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

const (
	shutdownTimeout = 20 * time.Second
	modeGateway     = "gateway"
	modeAggregator  = "aggregator"
)

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit.")
	mode := flag.String("mode", modeGateway, "Process mode: gateway or aggregator.")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch *mode {
	case modeGateway:
		err = runGateway(ctx)
	case modeAggregator:
		err = runAggregator(ctx)
	default:
		err = fmt.Errorf("unsupported mode %q", *mode)
	}
	if err != nil {
		log.Fatalf("Gateway process failed: %v", err)
	}
}

func runGateway(ctx context.Context) error {
	cfg := gateway.ConfigFromEnv()
	if cfg.BackendURL == "" {
		return fmt.Errorf("%s is required", gateway.EnvBackendURL)
	}

	gw, err := gateway.New(cfg)
	if err != nil {
		return fmt.Errorf("build gateway: %w", err)
	}
	defer gw.Close()

	addr := os.Getenv(gateway.EnvListenAddr)
	if addr == "" {
		addr = gateway.DefaultListenAddr
	}

	aggregatorURL := os.Getenv(gateway.EnvDemandAggregator)
	if aggregatorURL != "" {
		reporter, err := gateway.NewDemandReporter(gw, gateway.DemandReporterConfig{
			Endpoint:  aggregatorURL,
			GatewayID: processGatewayID(),
		})
		if err != nil {
			return fmt.Errorf("build demand reporter: %w", err)
		}
		reporterCtx, cancelReporter := context.WithCancel(context.Background())
		reporterDone := make(chan struct{})
		go func() {
			reporter.Run(reporterCtx)
			close(reporterDone)
		}()

		log.Printf("Noctaya gateway version %s listening on %s, backend %s", version, addr, cfg.BackendURL)
		log.Printf("Noctaya gateway publishing demand to %s", aggregatorURL)
		err = serve(ctx, gw.Handler(), addr, "", nil)
		cancelReporter()
		<-reporterDone
		return err
	}

	scalerAddr := os.Getenv(gateway.EnvScalerListenAddr)
	if scalerAddr == "" {
		scalerAddr = gateway.DefaultScalerListenAddr
	}
	log.Printf("Noctaya gateway version %s listening on %s, backend %s", version, addr, cfg.BackendURL)
	log.Printf("Noctaya external scaler listening on %s", scalerAddr)

	return serve(ctx, gw.Handler(), addr, scalerAddr, func(server *grpc.Server) {
		gateway.RegisterExternalScalerServer(server, gw)
	})
}

func runAggregator(ctx context.Context) error {
	aggregator := gateway.NewDemandAggregator(gateway.DemandAggregatorConfig{})
	aggregatorCtx, cancelAggregator := context.WithCancel(context.Background())
	aggregatorDone := make(chan struct{})
	go func() {
		aggregator.Run(aggregatorCtx)
		close(aggregatorDone)
	}()
	defer func() {
		cancelAggregator()
		<-aggregatorDone
	}()

	addr := os.Getenv(gateway.EnvAggregatorAddr)
	if addr == "" {
		addr = gateway.DefaultAggregatorAddr
	}
	scalerAddr := os.Getenv(gateway.EnvScalerListenAddr)
	if scalerAddr == "" {
		scalerAddr = gateway.DefaultScalerListenAddr
	}
	log.Printf("Noctaya demand aggregator version %s listening on %s", version, addr)
	log.Printf("Noctaya external scaler listening on %s", scalerAddr)
	return serve(ctx, aggregator.Handler(), addr, scalerAddr, func(server *grpc.Server) {
		gateway.RegisterExternalScalerServer(server, aggregator)
	})
}

func processGatewayID() string {
	if id := os.Getenv(gateway.EnvGatewayID); id != "" {
		return id
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "gateway"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func serve(
	ctx context.Context,
	handler http.Handler,
	addr string,
	scalerAddr string,
	registerScaler func(*grpc.Server),
) error {
	httpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP: %w", err)
	}
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var grpcServer *grpc.Server
	var scalerListener net.Listener
	if scalerAddr != "" {
		if registerScaler == nil {
			_ = httpListener.Close()
			return fmt.Errorf("external scaler registration is required")
		}
		scalerListener, err = net.Listen("tcp", scalerAddr)
		if err != nil {
			_ = httpListener.Close()
			return fmt.Errorf("listen for external scaler gRPC: %w", err)
		}
		grpcServer = grpc.NewServer()
		registerScaler(grpcServer)
	}

	serverCount := 1
	if grpcServer != nil {
		serverCount++
	}
	serverErrors := make(chan error, serverCount)
	go func() {
		err := httpServer.Serve(httpListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErrors <- err
	}()

	if grpcServer != nil {
		go func() {
			err := grpcServer.Serve(scalerListener)
			if errors.Is(err, grpc.ErrServerStopped) {
				err = nil
			}
			serverErrors <- err
		}()
	}

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
	if grpcServer != nil {
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
	}
	select {
	case <-httpDone:
	case <-ctx.Done():
		_ = httpServer.Close()
	}
}
