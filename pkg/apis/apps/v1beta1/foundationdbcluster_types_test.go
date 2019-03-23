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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

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

func TestParsingClusterStatus(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	statusFile, err := os.OpenFile(filepath.Join("testdata", "fdb_status_6_0.json"), os.O_RDONLY, os.ModePerm)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer statusFile.Close()
	statusDecoder := json.NewDecoder(statusFile)
	status := FoundationDBStatus{}
	err = statusDecoder.Decode(&status)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(status).To(gomega.Equal(FoundationDBStatus{
		Cluster: FoundationDBStatusClusterInfo{
			Processes: map[string]FoundationDBStatusProcessInfo{
				"c9eb35e25a364910fd77fdeec5c3a1f6": {
					Address:     "172.17.0.6:4500",
					CommandLine: "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-4 --locality_zoneid=foundationdbcluster-sample-4 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.6:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				},
				"d532d8cb1c23d002c4b97742f5195fdb": {
					Address:     "172.17.0.7:4500",
					CommandLine: "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-3 --locality_zoneid=foundationdbcluster-sample-3 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.7:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				},
				"f7058e8bed0618a0533f6188e9e35cdb": {
					Address:     "172.17.0.9:4500",
					CommandLine: "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-2 --locality_zoneid=foundationdbcluster-sample-2 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.9:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				},
				"6a5d5735fc8a58add63cceba1da46421": {
					Address:     "172.17.0.8:4500",
					CommandLine: "/var/dynamic-conf/bin/6.0.18/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_machineid=foundationdbcluster-sample-1 --locality_zoneid=foundationdbcluster-sample-1 --logdir=/var/log/fdb-trace-logs --loggroup=foundationdbcluster-sample --public_address=172.17.0.8:4500 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
				},
			},
		},
	}))
}
