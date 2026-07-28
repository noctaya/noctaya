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

package main

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/noctaya/noctaya/internal/gateway"
)

func TestServeStopsWhenContextIsCanceled(t *testing.T) {
	instance, err := gateway.New(gateway.Config{BackendURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(instance.Close)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, instance.Handler(), "127.0.0.1:0", "", nil)
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not stop after context cancellation")
	}
}

func TestServeStopsScalerWhenContextIsCanceled(t *testing.T) {
	instance, err := gateway.New(gateway.Config{BackendURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(instance.Close)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(
			ctx,
			instance.Handler(),
			"127.0.0.1:0",
			"127.0.0.1:0",
			func(server *grpc.Server) { gateway.RegisterExternalScalerServer(server, instance) },
		)
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve() did not stop the scaler after context cancellation")
	}
}
