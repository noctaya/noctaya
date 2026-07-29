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
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/noctaya/noctaya/internal/gateway/demand"
	"github.com/noctaya/noctaya/internal/gateway/proxy"
	"github.com/noctaya/noctaya/internal/gateway/scaler"
)

var version = "dev"

const (
	shutdownTimeout       = 20 * time.Second
	aggregatorReadTimeout = 5 * time.Second
	modeGateway           = "gateway"
	modeAggregator        = "aggregator"
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
	cfg := proxy.ConfigFromEnv()
	if cfg.BackendURL == "" {
		return fmt.Errorf("%s is required", proxy.EnvBackendURL)
	}

	gw, err := proxy.New(cfg)
	if err != nil {
		return fmt.Errorf("build gateway: %w", err)
	}
	defer gw.Close()

	addr := os.Getenv(proxy.EnvListenAddr)
	if addr == "" {
		addr = proxy.DefaultListenAddr
	}

	aggregatorURL := os.Getenv(proxy.EnvDemandAggregatorURL)
	if aggregatorURL != "" {
		reporter, err := proxy.NewDemandReporter(gw, proxy.DemandReporterConfig{
			Endpoint:      aggregatorURL,
			GatewayID:     processGatewayID(),
			AuthTokenFile: os.Getenv(demand.EnvAuthTokenFile),
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
		err = serve(ctx, gw.Handler(), addr, 0, "", nil)
		cancelReporter()
		<-reporterDone
		return err
	}

	scalerAddr := os.Getenv(scaler.EnvListenAddr)
	if scalerAddr == "" {
		scalerAddr = scaler.DefaultListenAddr
	}
	log.Printf("Noctaya gateway version %s listening on %s, backend %s", version, addr, cfg.BackendURL)
	log.Printf("Noctaya external scaler listening on %s", scalerAddr)

	return serve(ctx, gw.Handler(), addr, 0, scalerAddr, func(server *grpc.Server) {
		scaler.RegisterExternalScalerServer(server, gw)
	})
}

func runAggregator(ctx context.Context) error {
	authTokenFile := os.Getenv(demand.EnvAuthTokenFile)
	if _, err := demand.ReadAuthToken(authTokenFile); err != nil {
		return err
	}
	maxMembers, err := positiveIntFromEnv(scaler.EnvMaxGatewayMembers)
	if err != nil {
		return err
	}
	maxDemand, err := positiveInt64FromEnv(scaler.EnvMaxGatewayDemand)
	if err != nil {
		return err
	}
	aggregator := scaler.NewDemandAggregator(scaler.DemandAggregatorConfig{
		AuthTokenFile: authTokenFile,
		MaxMembers:    maxMembers,
		MaxDemand:     maxDemand,
	})
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

	addr := os.Getenv(scaler.EnvAggregatorListenAddr)
	if addr == "" {
		addr = scaler.DefaultAggregatorListenAddr
	}
	scalerAddr := os.Getenv(scaler.EnvListenAddr)
	if scalerAddr == "" {
		scalerAddr = scaler.DefaultListenAddr
	}
	log.Printf("Noctaya demand aggregator version %s listening on %s", version, addr)
	log.Printf("Noctaya external scaler listening on %s", scalerAddr)
	return serve(ctx, aggregator.Handler(), addr, aggregatorReadTimeout, scalerAddr, func(server *grpc.Server) {
		scaler.RegisterExternalScalerServer(server, aggregator)
	})
}

func positiveIntFromEnv(name string) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func positiveInt64FromEnv(name string) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func processGatewayID() string {
	if id := os.Getenv(proxy.EnvGatewayID); id != "" {
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
	readTimeout time.Duration,
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
		ReadTimeout:       readTimeout,
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
		serverOptions, err := scaler.ExternalScalerServerOptionsFromEnv()
		if err != nil {
			_ = scalerListener.Close()
			_ = httpListener.Close()
			return fmt.Errorf("configure external scaler gRPC: %w", err)
		}
		grpcServer = grpc.NewServer(serverOptions...)
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
