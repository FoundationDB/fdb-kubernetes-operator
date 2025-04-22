/*
Copyright 2022 FoundationDB project authors.

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

package v1beta2

import (
	"cmp"
	"sort"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FDB v1beta2 API")
}

// sortedMapValues gets the values from a map as a list, sorted according to the ordering of the underlying keys.
func sortedMapValues[K cmp.Ordered, V any](data map[K]V) []V {
	type kv struct {
		key   K
		value V
	}
	keyValues := make([]kv, 0, len(data))
	for key, value := range data {
		keyValues = append(keyValues, kv{key: key, value: value})
	}

	sort.Slice(keyValues, func(i, j int) bool {
		return keyValues[i].key < keyValues[j].key
	})

	values := make([]V, len(keyValues))

	for index, keyValue := range keyValues {
		values[index] = keyValue.value
	}

	return values
}
