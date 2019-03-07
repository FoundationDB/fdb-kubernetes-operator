/*
Copyright 2019 FoundationDB project authors.

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

package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterDesiredProcessCounts(t *testing.T) {
	cluster := &FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: FoundationDBClusterSpec{
			ProcessCounts:   map[string]int{"storage": 5},
			ReplicationMode: "double",
		},
	}
	count := cluster.DesiredProcessCount("storage")
	if count != 5 {
		t.Errorf("Incorrect process count for storage. Expected=%d, actual=%d", 5, count)
	}

	delete(cluster.Spec.ProcessCounts, "storage")
	count = cluster.DesiredProcessCount("storage")
	if count != 3 {
		t.Errorf("Incorrect process count for storage. Expected=%d, actual=%d", 3, count)
	}

	cluster.Spec.ReplicationMode = "single"
	count = cluster.DesiredProcessCount("storage")
	if count != 1 {
		t.Errorf("Incorrect process count for storage. Expected=%d, actual=%d", 1, count)
	}

	cluster.Spec.ReplicationMode = "double"
	count = cluster.DesiredProcessCount("log")
	if count != 0 {
		t.Errorf("Incorrect process count for log. Expected=%d, actual=%d", 0, count)
	}
}

func TestClusterDesiredCoordinatorCounts(t *testing.T) {
	cluster := &FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: FoundationDBClusterSpec{
			ReplicationMode: "double",
		},
	}

	count := cluster.DesiredCoordinatorCount()
	if count != 3 {
		t.Errorf("Incorrect coordinator count. Expected=%d, actual=%d", 3, count)
	}

	cluster.Spec.ReplicationMode = "single"
	count = cluster.DesiredCoordinatorCount()
	if count != 1 {
		t.Errorf("Incorrect coordinator count. Expected=%d, actual=%d", 1, count)
	}
}
