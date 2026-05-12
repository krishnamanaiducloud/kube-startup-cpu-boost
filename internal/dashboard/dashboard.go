// Copyright 2023 Google LLC
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

// Package dashboard reports structured boost events to the optional custom
// dashboard without changing controller behavior when the dashboard is absent.
package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	EventIncrease = "increase"
	EventDecrease = "decrease"
	EventRevert   = EventDecrease

	EventScopeContainer = "container"
	EventScopePod       = "pod"
)

type Event struct {
	EventUID          string    `json:"eventUid,omitempty"`
	BoostName         string    `json:"boostName"`
	Namespace         string    `json:"namespace"`
	Pod               string    `json:"pod"`
	Container         string    `json:"container,omitempty"`
	Containers        []string  `json:"containers,omitempty"`
	ContainerCount    int       `json:"containerCount,omitempty"`
	Scope             string    `json:"scope,omitempty"`
	EventType         string    `json:"eventType"`
	CPURequestsBefore string    `json:"cpuRequestsBefore,omitempty"`
	CPULimitsBefore   string    `json:"cpuLimitsBefore,omitempty"`
	CPURequestsAfter  string    `json:"cpuRequestsAfter,omitempty"`
	CPULimitsAfter    string    `json:"cpuLimitsAfter,omitempty"`
	Source            string    `json:"source,omitempty"`
	PodUID            string    `json:"podUid,omitempty"`
	NodeName          string    `json:"nodeName,omitempty"`
	OwnerKind         string    `json:"ownerKind,omitempty"`
	OwnerName         string    `json:"ownerName,omitempty"`
	PodPhase          string    `json:"podPhase,omitempty"`
	Image             string    `json:"image,omitempty"`
	BoostTimestamp    string    `json:"boostTimestamp,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}

func Report(ctx context.Context, event Event) {
	_ = ctx
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = "kube-startup-cpu-boost"
	}
	if event.Scope == "" {
		event.Scope = EventScopePod
		if event.Container != "" {
			event.Scope = EventScopeContainer
		}
	}
	if event.ContainerCount == 0 && event.Scope == EventScopeContainer {
		event.ContainerCount = 1
	}

	log.Printf(
		"startup_cpu_boost_event scope=%q eventType=%q boost=%q namespace=%q pod=%q podUid=%q container=%q containers=%q containerCount=%d node=%q ownerKind=%q ownerName=%q podPhase=%q image=%q eventUid=%q cpuRequestsBefore=%q cpuLimitsBefore=%q cpuRequestsAfter=%q cpuLimitsAfter=%q reason=%q",
		event.Scope,
		event.EventType,
		event.BoostName,
		event.Namespace,
		event.Pod,
		event.PodUID,
		event.Container,
		strings.Join(event.Containers, ","),
		event.ContainerCount,
		event.NodeName,
		event.OwnerKind,
		event.OwnerName,
		event.PodPhase,
		event.Image,
		event.EventUID,
		event.CPURequestsBefore,
		event.CPULimitsBefore,
		event.CPURequestsAfter,
		event.CPULimitsAfter,
		event.Reason,
	)

	url := strings.TrimSpace(os.Getenv("BOOST_DASHBOARD_EVENT_URL"))
	if url == "" {
		url = strings.TrimSpace(os.Getenv("DASHBOARD_EVENT_URL"))
	}
	if url == "" {
		return
	}

	go postEvent(url, event)
}

func postEvent(url string, event Event) {
	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("failed to marshal boost dashboard event: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("failed to create boost dashboard request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("failed to post boost dashboard event: %v", err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		log.Printf("boost dashboard event rejected: statusCode=%d", res.StatusCode)
	}
}
