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

// Command vllm-stub is a CPU-only fake of the vLLM surfaces used in tests.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	completionID              = "stub-cmpl"
	chatCompletionObject      = "chat.completion"
	chatCompletionChunkObject = "chat.completion.chunk"
	textCompletionObject      = "text_completion"
)

type Config struct {
	StartupDelay time.Duration
	TokenCount   int
	TokenDelay   time.Duration
}

type Server struct {
	cfg       Config
	startedAt time.Time
	now       func() time.Time

	mu      sync.Mutex
	metrics vllmMetrics
}

// vllmMetrics mirrors gauges exposed by vLLM for optional observability tests.
type vllmMetrics struct {
	Waiting float64 `json:"waiting"`
	Running float64 `json:"running"`
	KVCache float64 `json:"kv_cache"`
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg, now: time.Now, startedAt: time.Now()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		s.handleCompletions(w, r, true)
	})
	mux.HandleFunc("/v1/completions", func(w http.ResponseWriter, r *http.Request) {
		s.handleCompletions(w, r, false)
	})
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/control", s.handleControl)
	return mux
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.mu.Lock()
	m := s.metrics
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for _, g := range []struct {
		name string
		val  float64
	}{
		{"vllm:num_requests_waiting", m.Waiting},
		{"vllm:num_requests_running", m.Running},
		{"vllm:kv_cache_usage_perc", m.KVCache},
	} {
		_, _ = fmt.Fprintf(w, "# TYPE %s gauge\n%s %g\n", g.name, g.name, g.val)
	}
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var in vllmMetrics
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Waiting < 0 || in.Running < 0 || in.KVCache < 0 || in.KVCache > 1 {
		http.Error(w, "metrics must be non-negative and kv_cache must be between 0 and 1", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.metrics = in
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// ready reports whether the configured startup delay has elapsed since boot, mimicking
// vLLM's /health returning 200 only once the engine has finished loading.
func (s *Server) ready() bool {
	return s.now().Sub(s.startedAt) >= s.cfg.StartupDelay
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type completionRequest struct {
	Stream bool `json:"stream"`
}

type completionPayload struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Choices []completionChoice `json:"choices"`
}

type completionChoice struct {
	Index        int                `json:"index"`
	Delta        *completionContent `json:"delta,omitempty"`
	Message      *completionMessage `json:"message,omitempty"`
	Text         string             `json:"text,omitempty"`
	FinishReason string             `json:"finish_reason,omitempty"`
}

type completionContent struct {
	Content string `json:"content"`
}

type completionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Server) tokens(r *http.Request) (int, error) {
	value := r.URL.Query().Get("tokens")
	if value != "" {
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 {
			return 0, fmt.Errorf("tokens must be a positive integer")
		}
		return count, nil
	}
	if s.cfg.TokenCount > 0 {
		return s.cfg.TokenCount, nil
	}
	return 1, nil
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request, chat bool) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req completionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON request", http.StatusBadRequest)
		return
	}
	n, err := s.tokens(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Stream {
		s.streamTokens(w, r, n, chat)
		return
	}
	s.writeJSON(w, n, chat)
}

func (s *Server) streamTokens(w http.ResponseWriter, r *http.Request, n int, chat bool) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for i := range n {
		choice := completionChoice{Index: 0}
		object := textCompletionObject
		if chat {
			object = chatCompletionChunkObject
			choice.Delta = &completionContent{Content: fmt.Sprintf("tok%d ", i)}
		} else {
			choice.Text = fmt.Sprintf("tok%d ", i)
		}
		chunk := completionPayload{ID: completionID, Object: object, Choices: []completionChoice{choice}}
		b, _ := json.Marshal(chunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if s.cfg.TokenDelay <= 0 {
			continue
		}
		timer := time.NewTimer(s.cfg.TokenDelay)
		select {
		case <-r.Context().Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, n int, chat bool) {
	var content strings.Builder
	for i := range n {
		fmt.Fprintf(&content, "tok%d ", i)
	}
	w.Header().Set("Content-Type", "application/json")
	choice := completionChoice{Index: 0, FinishReason: "stop"}
	object := textCompletionObject
	if chat {
		object = chatCompletionObject
		choice.Message = &completionMessage{Role: "assistant", Content: content.String()}
	} else {
		choice.Text = content.String()
	}
	_ = json.NewEncoder(w).Encode(completionPayload{
		ID: completionID, Object: object, Choices: []completionChoice{choice},
	})
}
