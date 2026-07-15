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

package boost

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
)

type selectorSpecificityProvider interface {
	selectorSpecificity() int
}

// SelectCPUBoostForPod returns one deterministic match from the supplied boost
// configurations. Selectors with more requirements take precedence. Ties are
// resolved by namespace and name so API list ordering and map iteration cannot
// change the policy applied to a Pod.
func SelectCPUBoostForPod(pod *corev1.Pod, candidates []StartupCPUBoost) (StartupCPUBoost, bool) {
	matching := make([]StartupCPUBoost, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && candidate.Matches(pod) {
			matching = append(matching, candidate)
		}
	}
	if len(matching) == 0 {
		return nil, false
	}

	sort.SliceStable(matching, func(i, j int) bool {
		iSpecificity := boostSelectorSpecificity(matching[i])
		jSpecificity := boostSelectorSpecificity(matching[j])
		if iSpecificity != jSpecificity {
			return iSpecificity > jSpecificity
		}
		if matching[i].Namespace() != matching[j].Namespace() {
			return matching[i].Namespace() < matching[j].Namespace()
		}
		return matching[i].Name() < matching[j].Name()
	})

	return matching[0], true
}

func boostSelectorSpecificity(candidate StartupCPUBoost) int {
	provider, ok := candidate.(selectorSpecificityProvider)
	if !ok {
		return 0
	}
	return provider.selectorSpecificity()
}
