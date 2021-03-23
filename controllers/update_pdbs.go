/*
 * update_pdbs.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	ctx "context"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// UpdatePDBs provides a reconciliation step for adding services to a cluster.
type UpdatePDBs struct{}

// Reconcile runs the reconciler's work.
func (u UpdatePDBs) Reconcile(r *FoundationDBClusterReconciler, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	newPDBs, err := GetPodDisruptionBudgets(cluster)
	if err != nil {
		return false, err
	}

	newPDBMap := make(map[string]policyv1beta1.PodDisruptionBudget, len(newPDBs))
	for _, pdb := range newPDBs {
		newPDBMap[pdb.Name] = pdb
	}

	pdbs := &policyv1beta1.PodDisruptionBudgetList{}
	err = r.Client.List(context, pdbs, getPodListOptions(cluster, "", "")...)
	if err != nil {
		return false, err
	}

	for _, pdb := range pdbs.Items {
		newPDB, pdbDesired := newPDBMap[pdb.Name]
		if pdbDesired {
			if !equality.Semantic.DeepEqual(pdb.Spec, newPDB.Spec) {
				pdb.Spec = newPDB.Spec
				log.Info("Updating PodDisruptionBudget", "cluster", cluster.Name, "namespace", cluster.Name, "pdb", pdb.Name)

				err = r.Update(context, &pdb)
				if err != nil {
					return false, err
				}
			}
			delete(newPDBMap, pdb.Name)
		} else {
			err = r.Delete(context, &pdb)
			if err != nil {
				return false, err
			}
		}
	}

	for _, pdb := range newPDBMap {
		log.Info("Creating PodDisruptionBudget", "cluster", cluster.Name, "namespace", cluster.Name, "pdb", pdb.Name)
		err = r.Create(context, &pdb)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// RequeueAfter returns the delay before we should run the reconciliation
// again.
func (u UpdatePDBs) RequeueAfter() time.Duration {
	return 0
}
