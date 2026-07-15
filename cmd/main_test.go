// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"net/http"
	"testing"
)

type staticCacheSyncer bool

func (s staticCacheSyncer) WaitForCacheSync(context.Context) bool {
	return bool(s)
}

func TestReplicaReadyCheckAllowsNonLeaderAfterRegistrationAndCacheSync(t *testing.T) {
	componentsReady := make(chan struct{})
	close(componentsReady)
	check := replicaReadyCheck(staticCacheSyncer(true), componentsReady)

	if err := check((&http.Request{}).WithContext(context.Background())); err != nil {
		t.Fatalf("ready follower returned error: %v", err)
	}
}

func TestReplicaReadyCheckWaitsForRegistration(t *testing.T) {
	check := replicaReadyCheck(staticCacheSyncer(true), make(chan struct{}))

	if err := check((&http.Request{}).WithContext(context.Background())); err == nil {
		t.Fatal("expected registration readiness error")
	}
}

func TestReplicaReadyCheckWaitsForCacheSync(t *testing.T) {
	componentsReady := make(chan struct{})
	close(componentsReady)
	check := replicaReadyCheck(staticCacheSyncer(false), componentsReady)

	if err := check((&http.Request{}).WithContext(context.Background())); err == nil {
		t.Fatal("expected cache synchronization readiness error")
	}
}
