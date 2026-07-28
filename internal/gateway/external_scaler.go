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

package gateway

import (
	"context"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	externalscaler "github.com/noctaya/noctaya/internal/gateway/externalscaler"
)

const scalerMetricName = "pending"

type demandSource interface {
	Demand() int64
	subscribeDemand() (<-chan int64, func())
	scalerStreamConnected()
	scalerStreamDisconnected()
}

type externalScalerServer struct {
	externalscaler.UnimplementedExternalScalerServer
	source demandSource
}

func RegisterExternalScalerServer(registrar grpc.ServiceRegistrar, source demandSource) {
	externalscaler.RegisterExternalScalerServer(registrar, &externalScalerServer{source: source})
}

func (s *externalScalerServer) IsActive(_ context.Context, ref *externalscaler.ScaledObjectRef) (*externalscaler.IsActiveResponse, error) {
	if _, _, err := parseScalerMetadata(ref); err != nil {
		return nil, err
	}
	return &externalscaler.IsActiveResponse{Result: s.source.Demand() > 0}, nil
}

func (s *externalScalerServer) StreamIsActive(ref *externalscaler.ScaledObjectRef, stream externalscaler.ExternalScaler_StreamIsActiveServer) error {
	if _, _, err := parseScalerMetadata(ref); err != nil {
		return err
	}
	updates, unsubscribe := s.source.subscribeDemand()
	defer unsubscribe()
	s.source.scalerStreamConnected()
	defer s.source.scalerStreamDisconnected()
	var sent bool
	var lastActive bool
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case demand := <-updates:
			active := demand > 0
			if sent && active == lastActive {
				continue
			}
			if err := stream.Send(&externalscaler.IsActiveResponse{Result: active}); err != nil {
				return err
			}
			sent = true
			lastActive = active
		}
	}
}

func (s *externalScalerServer) GetMetricSpec(_ context.Context, ref *externalscaler.ScaledObjectRef) (*externalscaler.GetMetricSpecResponse, error) {
	metricName, target, err := parseScalerMetadata(ref)
	if err != nil {
		return nil, err
	}
	return &externalscaler.GetMetricSpecResponse{MetricSpecs: []*externalscaler.MetricSpec{{
		MetricName:      metricName,
		TargetSizeFloat: target,
	}}}, nil
}

func (s *externalScalerServer) GetMetrics(_ context.Context, req *externalscaler.GetMetricsRequest) (*externalscaler.GetMetricsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "metrics request is required")
	}
	metricName, _, err := parseScalerMetadata(req.GetScaledObjectRef())
	if err != nil {
		return nil, err
	}
	if req.GetMetricName() != metricName {
		return nil, status.Errorf(codes.InvalidArgument, "metricName %q does not match configured metric %q", req.GetMetricName(), metricName)
	}
	return &externalscaler.GetMetricsResponse{MetricValues: []*externalscaler.MetricValue{{
		MetricName:       metricName,
		MetricValueFloat: float64(s.source.Demand()),
	}}}, nil
}

func parseScalerMetadata(ref *externalscaler.ScaledObjectRef) (string, float64, error) {
	if ref == nil {
		return "", 0, status.Error(codes.InvalidArgument, "scaledObjectRef is required")
	}
	metadata := ref.GetScalerMetadata()
	metricName := metadata["metricName"]
	if metricName == "" {
		metricName = scalerMetricName
	}
	if metricName != scalerMetricName {
		return "", 0, status.Errorf(codes.InvalidArgument, "unsupported metricName %q", metricName)
	}
	targetText := metadata["targetValue"]
	if targetText == "" {
		return "", 0, status.Error(codes.InvalidArgument, "targetValue metadata is required")
	}
	target, err := strconv.ParseFloat(targetText, 64)
	if err != nil || target <= 0 {
		return "", 0, status.Errorf(codes.InvalidArgument, "targetValue %q must be a positive number", targetText)
	}
	return metricName, target, nil
}
