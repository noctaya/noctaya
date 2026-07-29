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

// Package demand publishes local demand changes to scaler consumers.
package demand

import "sync"

type Subscriptions struct {
	mu             sync.Mutex
	subscribers    map[uint64]chan int64
	nextSubscriber uint64
	lastDemand     int64
}

func NewSubscriptions() Subscriptions {
	return Subscriptions{subscribers: make(map[uint64]chan int64)}
}

func (s *Subscriptions) Publish(demand int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if demand == s.lastDemand {
		return false
	}
	activated := s.lastDemand == 0 && demand > 0
	s.lastDemand = demand
	for _, ch := range s.subscribers {
		select {
		case ch <- demand:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- demand
		}
	}
	return activated
}

func (s *Subscriptions) Subscribe(current int64) (<-chan int64, func()) {
	ch := make(chan int64, 1)
	s.mu.Lock()
	ch <- current
	id := s.nextSubscriber
	s.nextSubscriber++
	s.subscribers[id] = ch
	s.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subscribers, id)
			s.mu.Unlock()
		})
	}
}

func (s *Subscriptions) Current() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDemand
}
