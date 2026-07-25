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
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/noctaya/noctaya/internal/gateway"
)

var version = "dev"

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
	log.Printf("Noctaya gateway version %s listening on %s, backend %s", version, addr, cfg.BackendURL)
	if scalerAddr != "" {
		log.Printf("Noctaya external scaler listening on %s", scalerAddr)
	}
	if err := serve(gw, addr, scalerAddr); err != nil {
		log.Fatalf("Gateway server failed: %v", err)
	}
}

func serve(gw *gateway.Gateway, addr, scalerAddr string) error {
	httpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP: %w", err)
	}
	httpServer := &http.Server{
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if scalerAddr == "" {
		return httpServer.Serve(httpListener)
	}

	scalerListener, err := net.Listen("tcp", scalerAddr)
	if err != nil {
		_ = httpListener.Close()
		return fmt.Errorf("listen for external scaler gRPC: %w", err)
	}
	grpcServer := grpc.NewServer()
	gateway.RegisterExternalScalerServer(grpcServer, gw)

	errors := make(chan error, 2)
	go func() { errors <- httpServer.Serve(httpListener) }()
	go func() { errors <- grpcServer.Serve(scalerListener) }()
	err = <-errors
	grpcServer.Stop()
	_ = httpServer.Close()
	return err
}
