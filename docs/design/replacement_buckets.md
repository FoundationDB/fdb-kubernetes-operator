# Replacement Buckets For Automatic Replacements

## Metadata

* Authors: @johscheuer
* Created: 2022-10-03
* Updated: 2022-10-03

## Background

The operator supports to replace process groups with different conditions like `MissingProcess`.
This allows the operator to bring the cluster into a better state if some process groups are failing.
A human operator can currently limit the number of automatic replacements, by default the operator will only replace one process group at a time.
The drawback of this approach is that all replacements will be accounted in the same way.
That brings the risk that some replacements are blocked for a longer time by a single (or small) number of replacements.
Replacements for stateless or log process groups are typically fast, but a single replacement of a storage process group could block those replacements for a long time.

## General Design Goals

Allow a human operator to define replacement buckets which allow to set limits on a per process class base.
This should allow the operator to be more flexible during replacements with a lower risk of being blocked by another replacement of a different process class.

## Current Implementation

The current implementation counts the number of the currently ongoing replacements and if this number is higher than the limit the operator won't replace any faulty process groups.

## Proposed Design

The operator should be extended to support replacement buckets that allow limits on a process class level.
The `AutomaticReplacementOptions` struct can be extended to contain a replacement bucket struct like this:


```go
type ReplacementBucket struct {
	// If set the ReplacementBucket will be used. Defaults to false.
	Enabled bool
	// The number of concurrent replacements for storage process groups. Defaults to 1.
	Storage int
	// The number of concurrent replacements for log or transaction process groups. Defaults to 1.
	Log int
	// The number of concurrent replacements for all stateless process groups. Defaults to 1.
	Stateless int
}
```

In order to keep the configuration simple but still flexible we will focus on storage, log and stateless replacements without any further differentiation between specific classes.
The `getMaxReplacements(cluster *fdbv1beta2.FoundationDBCluster, maxReplacements int) int` method would be adjust to return a `map[fdbv1beta2.ProcessClass]int`:

```go
func getMaxReplacements(cluster *fdbv1beta2.FoundationDBCluster, maxReplacements int) map[fdbv1beta2.ProcessClass]int {
	replacementBucketEnabled := cluster.GetReplacementBucketEnabled()
	if replacementBucketEnabled {
		return cluster.getReplacementBucket()
    }
	
	// The maximum number of replacements will be the defined number in the cluster spec
	// minus all currently ongoing replacements e.g. process groups marked for removal but
	// not fully excluded.
	removalCount := 0
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		if processGroupStatus.IsMarkedForRemoval() && !processGroupStatus.IsExcluded() {
			// If we already have a removal in-flight, we should not try
			// replacing more failed process groups.
			removalCount++
		}
	}

	return map[fdbv1beta2.ProcessClass]int{fdbv1beta2.ProcessClassGeneral: maxReplacements - removalCount}
}
```

The `getReplacementBucket()` could be implemented like this:

```go
func (cluster *FoundationDBCluster) getReplacementBucket() map[fdbv1beta2.ProcessClass]int {
	// The maximum number of replacements will be the defined number in the cluster spec
	// minus all currently ongoing replacements e.g. process groups marked for removal but
	// not fully excluded.
	replacementBucket := cluster.getReplacementBucketWithDefaults()
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() && !processGroup.IsExcluded() {
			// If we already have a removal in-flight, we should not try
			// replacing more failed process groups.
			replacementBucket[processGroup.ProcessClass.ReplacementBucket()]--
		}
	}

	return replacementBucket
}
```

The method `getReplacementBucketWithDefaults()` would return the `ReplacementBucket` struct with the user defined values or defaults.
`ReplacementBucket()` will return the process class that should be used for replacements e.g. for `transaction` it would return `log` and for all processes that are not `IsStateful()` we would return `stateless`.
To allow the correct selection of the replacement bucket we need to adjust the `ReplaceFailedProcessGroups` method:

```go
func ReplaceFailedProcessGroups(log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, adminClient fdbadminclient.AdminClient) bool {
    ...
	replacementBucket := getMaxReplacements(cluster, cluster.GetMaxConcurrentAutomaticReplacements())
	replacementBucketEnabled := cluster.GetReplacementBucketEnabled()

	hasReplacement := false
	for _, processGroupStatus := range cluster.Status.ProcessGroups {
		var replacementClass fdbv1beta2.ProcessClass
		if replacementBucketEnabled {
			replacementClass = processGroupStatus.ProcessClass.ReplacementBucket()
		} else {
			replacementClass = fdbv1beta2.ProcessClassGeneral
		}

		...
        if !needsReplacement {
			continue
        }

		if replacementBucket[replacementClass] <= 0 {
			// Add log statement
			continue
        }

		...
        hasReplacement = true
        replacementBucket[replacementClass]--
	}

	return hasReplacement
}
```

This allows to replace different process classes concurrently without blocking each other.

## Related Links

- [Automatic Replacements](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/docs/design/implemented/automatic_replacements.md)
- [Replacements and Deletions](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/docs/manual/replacements_and_deletions.md)
