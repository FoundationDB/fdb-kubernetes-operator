# Customization

This document covers some of the options the operator provides for customizing your FoundationDB deployment.

## Running Multiple Storage Servers per Pod

Since FoundationDB is limited to a single core it can make sense to run multiple storage server per disk. You can change the number of storage server per Pod with the `storageServersPerPod` setting. This will start multiple FDB processes inside of a single container, under a single `fdbmonitor` process.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  spec:
    storageServersPerPod: 2
```

A change to the `storageServersPerPod` will replace all of the storage pods. For more information about this feature read the [multiple storage servers per pod](/docs/design/multiple_storage_per_disk.md) design doc.

# Customizing the Volumes

To use a different `StorageClass` than the default you can set your desired `StorageClass` in the [process settings](/docs/cluster_spec.md#processsettings):

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processes:
    general:
      volumeClaimTemplate:
        spec:
          storageClassName: my-storage-class
```

You can use the same field to customize other parts of the volume claim. For instance, this is how you would define the storage size for your volumes:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processes:
    general:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: "256G"
```

A change to the volume claim template will replace all PVC' and the according Pods. You can also use different volume settings for different processes. For instance, you could use a slower but higher-capacity storage class for your storage processes:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processes:
    log:
      volumeClaimTemplate:
        spec:
          storageClassName: fast-storage
    storage:
      volumeClaimTemplate:
        spec:
          storageClassName: slow-storage
```

## Customizing Your Pods

The process settings in the cluster spec also allow specifying a pod template, which allows customizing almost everything about your pods. You can define custom environment variables, add your own containers, add additional volumes, and more. You may want to use these fields to handle things that are specific to your environment, like managing certificates or forwarding logs to a central system.

In this example, we are going to add a volume that mounts certificates from a Kubernetes secret. This is likely not how you would want to manage certificates in a real environment, but it can be helpful as an example.

Before you apply this change, you will need to create a secret. You can apply the following YAML to your environment to set up a secret with the self-signed cert that we use for testing the operator:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: fdb-kubernetes-operator-secrets
data:
  tls.crt: |
    LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZ4RENDQTZ3Q0NRQ29Bd2FySFhWeWtEQU5CZ2txaGtpRzl3MEJBUXNGQURDQm96RUxNQWtHQTFVRUJoTUMKVlZNeEN6QUpCZ05WQkFnTUFrTkJNUkl3RUFZRFZRUUhEQWxEZFhCbGNuUnBibTh4RlRBVEJnTlZCQW9NREVadgpkVzVrWVhScGIyNUVRakVWTUJNR0ExVUVDd3dNUm05MWJtUmhkR2x2YmtSQ01Sa3dGd1lEVlFRRERCQm1iM1Z1ClpHRjBhVzl1WkdJdWIzSm5NU293S0FZSktvWklodmNOQVFrQkZodGtiMjV2ZEhKbGNHeDVRR1p2ZFc1a1lYUnAKYjI1a1lpNXZjbWN3SGhjTk1Ua3dOak13TWpNME9URTFXaGNOTWpBd05qSTVNak0wT1RFMVdqQ0JvekVMTUFrRwpBMVVFQmhNQ1ZWTXhDekFKQmdOVkJBZ01Ba05CTVJJd0VBWURWUVFIREFsRGRYQmxjblJwYm04eEZUQVRCZ05WCkJBb01ERVp2ZFc1a1lYUnBiMjVFUWpFVk1CTUdBMVVFQ3d3TVJtOTFibVJoZEdsdmJrUkNNUmt3RndZRFZRUUQKREJCbWIzVnVaR0YwYVc5dVpHSXViM0puTVNvd0tBWUpLb1pJaHZjTkFRa0JGaHRrYjI1dmRISmxjR3g1UUdadgpkVzVrWVhScGIyNWtZaTV2Y21jd2dnSWlNQTBHQ1NxR1NJYjNEUUVCQVFVQUE0SUNEd0F3Z2dJS0FvSUNBUUNmCjRrNDRUTEhvQ2hYQmFCVHVhTWs2WURHQTNyN1VJR1BhOVJWeCsxVFFjNUZnL2Z6VFBtOG80dml5eDBwK0diUUcKWC9TM0hqcTJrSGh4NUJLV3A3NEVFWGE1ODJBTTRKU3hRV1kwVzVVVlV3QWR5dkxXZEY1OGRSOTJGMDQwWVJyZwpPOTFGaW5hRFZndUVUN1NVZlN6eGpnUUc4RXIyZmRTVzJLMy9CZDJBT2FwaVFGbzRWYlBPL3ZqMlhxNU5iSk05CmhtZ254aWZhZ2c0UjRPc25SbzN5YUpjNE5DRDRBamFLQ29kVFR1RmV2cGx3RDJ6QnBrdlo4TEpGUkZscDUzVWYKYjZBZGNQK2VibHU0b3ZoWWFpUWVUZ2htRzRvSy9KVFRwYWgwbHlHbUVmcXJrYTVrblBLWlJoQnBSWVZ1VVJUbwpoVEdvMGVGNzBscGExRjVRZWoza1BzWDBta0JpbmNmbkY0di9YdE9pNmNZK3Q3eG5ldzhQMGlNemw5MWNuNXpHCm9CRjNjamx5amVlNnh6WWxxbUhqbGg5OUs2OEZkQzM3TTl2R29LcGJNT3lNZ1AvQTJsdjh2UGw3eG8xQlBQUnIKZlpOWWpta3UwY212UmszbDVWbDFONzh3N3VrMCtqeHF3RXVFeVN3cVJxdHJ6TlRSbWdUMWxReTJlNzVVSlVxUQoxSDlnMk1CajB5c01tUHJaTWZEejRuUlpWa09XdWorMTFUVEUwTHBZb0lxYTdjNm5wUk12TmxyRjhNQTB1VUlBCjdDY01Qc2FUQjNWTmpHck5ueEZrUTh2UFRpQnRWU2NRQVNBc3V5Q3psNHJ5d210eXVmTEh2ZDJxeU8rWGFrL3YKY0hvT2I4bndsWG9VRS9DbmNyYWpqaFdCbWU5SVI4MWxxSVFNKzNBQ0l3SURBUUFCTUEwR0NTcUdTSWIzRFFFQgpDd1VBQTRJQ0FRQm1CUWxDczNRSmtUSEpsT0g4UjU0WTlPWFEzbWxzVmpsYThQemE3dkpIL2l6bTJGdk5EK1lVCjBDT2V6MDE4MlpXbGtqcllOWTdVblk5aVp1ODNWSVpFc1VjVlc3M05NajN0Zi80dG1aZlRPR1JLYmhoZUtNMUcKRDR0T3dwNjVlWExwbFlRazltSEIvenFDWk9vUmwwMHByNlFWYU5oUUlQdzJ1eTkyL2thVlhNOGhqVTBKOWhHNApwdis4MEpYc3A2bk1aNVQxY3FWdWZta3REektyUmxZNmkzUDBTbHdZNE9OTy9LSzcwNTJUS1RKOG5tZDFuYnpwClJXNG5iRTVnbzlYc2xSWDlQTS9jWW1rSTFwdk5vdFN6T2l3OWJ6UWo1ME45cWVVNTBwSU9WL2JZRkQrVU5rNEcKRDJ3Sk55YW9GRGlWS3U2ajRkaDFxQW93NkNsdEV1dWYydi91S0NUd2QyRzF2UlVFZTRVUDlVREoxeDZlVVVJYQoxMm1sbCtXdUt0NXNUWlRCelczYkhTRXRuQnZyMVVxWlFHdmw4UHlhK1FmV2FWcEtWRVZuYlJpSW5MVkhOa2EzCkwrTm5acGE0WmFWUU9KYlpLbFNQUy9ucDNRL3cvZU1weGdEL2hacTVXMTB2dzM5YVBqZEF6NndIZGpuN1RmWG4KbGN1WnBIWjdNWlZBTERNbDFFdE9aQW5LYjhJajVLNWpWbTFGSGZsMXJWY0MzS2tIdU56TzU0OUNxYkVqelNkdgp5ZEVHbUZBZEpTYllwdnRHUWFtVHA0RnVheEdtbWxOME5mMDNIVmFSVnRUVml1SDB2MUViNDc2MVpKWjczbE55CnVmUkFXMnVDZUlRU1ZmdW1pcVJ5WmhFaEh3MmQ2WnlJMzY1TTQyaGtpZXI0M2E2QlJCVzEvdz09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
  tls.key: |
    LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCk1JSUpRZ0lCQURBTkJna3Foa2lHOXcwQkFRRUZBQVNDQ1N3d2dna29BZ0VBQW9JQ0FRQ2Y0azQ0VExIb0NoWEIKYUJUdWFNazZZREdBM3I3VUlHUGE5UlZ4KzFUUWM1RmcvZnpUUG04bzR2aXl4MHArR2JRR1gvUzNIanEya0hoeAo1QktXcDc0RUVYYTU4MkFNNEpTeFFXWTBXNVVWVXdBZHl2TFdkRjU4ZFI5MkYwNDBZUnJnTzkxRmluYURWZ3VFClQ3U1VmU3p4amdRRzhFcjJmZFNXMkszL0JkMkFPYXBpUUZvNFZiUE8vdmoyWHE1TmJKTTlobWdueGlmYWdnNFIKNE9zblJvM3lhSmM0TkNENEFqYUtDb2RUVHVGZXZwbHdEMnpCcGt2WjhMSkZSRmxwNTNVZmI2QWRjUCtlYmx1NApvdmhZYWlRZVRnaG1HNG9LL0pUVHBhaDBseUdtRWZxcmthNWtuUEtaUmhCcFJZVnVVUlRvaFRHbzBlRjcwbHBhCjFGNVFlajNrUHNYMG1rQmluY2ZuRjR2L1h0T2k2Y1krdDd4bmV3OFAwaU16bDkxY241ekdvQkYzY2pseWplZTYKeHpZbHFtSGpsaDk5SzY4RmRDMzdNOXZHb0twYk1PeU1nUC9BMmx2OHZQbDd4bzFCUFBScmZaTllqbWt1MGNtdgpSazNsNVZsMU43OHc3dWswK2p4cXdFdUV5U3dxUnF0cnpOVFJtZ1QxbFF5MmU3NVVKVXFRMUg5ZzJNQmoweXNNCm1QclpNZkR6NG5SWlZrT1d1aisxMVRURTBMcFlvSXFhN2M2bnBSTXZObHJGOE1BMHVVSUE3Q2NNUHNhVEIzVk4KakdyTm54RmtROHZQVGlCdFZTY1FBU0FzdXlDemw0cnl3bXR5dWZMSHZkMnF5TytYYWsvdmNIb09iOG53bFhvVQpFL0NuY3JhampoV0JtZTlJUjgxbHFJUU0rM0FDSXdJREFRQUJBb0lDQUdQRzlDK1lWVkpNc09VQkVrYnlaOW9oClcrTmpuczE4NVRRb3pOaFVFOHIreEZRMlRVaDdaeDJwLzdCNlJKZkxiSmlwMjJ0SDF6WkZsSlRtMDE3bmtlS3kKRDFqZWRDdTFIN1k2N1JCeHN1a2E0akMxamJTZDdMVlkxbWg1Qk5vVlc1TmlhS1ZVVXIrRnZDdzNIYWVwTXBvUQptWnpHNnRGSEY1dUgzNVlPVC93TWdMTk9HNythWkZzaXJiWDZ3bVlaQXc1YlNiYkFwL0JxUjJPSzdOV1c1MURIClNzL05ZR0hGNThsZjVySHJ3U1BDYUxrUk56cm1qK0dUbjMwd3VXZ3BCT080WXNEYzJ2bEJQOFpMRmhiL0xra24KUTRDTllTbVlGVHk3M2hQY21TZ3RnalQ5OWtwZDA5d3BhR1o1OTFvd0NZOU9SLzVsOUlTMGNxVEtjWTFockNzLwpIWlNJNUNMaFpYMUN3a1BCeGlIYXhnVkJlZ2dRQ0hXTmJtM0lHQjZSYjV0U1pKaVd0OElUL1h3TlJJaEpibnQ0Cm1BdUVnN2dUU1UxNG8weUZjNEk3cXhxZnRTUFRIK0ZEUEx3L2xDbzdLVU0wNzNnRjcydkpqR1R1RUNVYTBqSVoKMmNtajE3Q0tFWk5XUktvSGJyM2x5cjVRRmdLNXBFMjlWbDkwV0d4NkUvV3FGYXpORmJydEZBbDZnL1RTQ1VmaApLY0gyMnY5RlJ6eGJISWc4MGEzQ1hmTS9EbklQaUVRYUYvZTRZSCtobXMvcXp6ZWRvNjJldEh3cUM5am1FS1RnClk3Z3RpZUJ6Q2VSQ3psTzEwMDZnYUJacGZLeEtnY3lCSzRxV3BCVVkvNU5HTDNwVWZJdUQ1MitEQXlLNC9uVHEKdTZDanpSM1pMQUU0UEZsYUdUSjVBb0lCQVFET1Q4cVh4RGVPSXNjNVFhM3NSS3hwSC9YSFprT2FjWFlJUk9iNwpYV3BTN21Jd3JCYVlMa1ZDRWJncHQzeGZ3MEd6MXNKS0FXMzA4V3g0MTQvTE1wT2RZMDZuSHNqSk03QVdpczNTCmFNdnRIL0tzUGlmMllwSHh1eUxIY0NuNE1TNzdneExQRjEwTHJvdEdJU3JNckhIdExsZFcxcUxGVHpDVE90QnUKQXhQZlZCM3B1bVU3WnBCTDdDbEE4Sko2dkJOSEZJNG9yY3BKU1BybnBoQUpjSkFaZWVwaENxZTVISlZKUTVzSwpGbk5rbC9xalJlWDdYd0N1dS9aU003ZndPK2hPVkZCelNLL2x6ZUVNRU1kYWlQTC9JVEQ2ZG0zejhaM0gwNERECmROdUtEcDhaN1VPNXNrNWUyaFVwMHNFQ3BQRzBCb2tmN2ZNQjJ0WU5NVHRZa085dkFvSUJBUURHWkFCeDRsMlcKMjBoRHNieloyQjR2N2h4R2cxWFlYTkhBK3ZES1FZeW1vNXZwV00ydjFiclltc2NxYlE1K0QybldaUGlyaWl3Lwo4SjczczdmajN0S1JDdk9pcjRFb015Z3JpVzNHWHlXWFVJa2cxWjVzYzdDQk1IMTRaL09ObnBIY01VZWJiVDNFCktsUVRxMFhjcS9QaWlFMWZLY0x5aVJncUdSTWZYVjVkYnJMR1BvTzdPc1I0TmdQZXdiMjVZL0t2OVFWM29IMGQKZXVSTXhtTU1YZHAwT2Zqb1YyZnUybUZhSlc4SnNGdlZ6Yi9lbzZGVUp4WWxqMFF5Ry9uVjlTRzN6d2pyM2lTeQpEVVM4UHpINWpUMTI0S0Q5dkpZRGZVTGF4N0txNGxJdjFQbk1WdVZXczNjNHQwV0Y2M2JWVUFIQU4zS0RDNUc4CmladkVYL1cwdFA2TkFvSUJBUUNqV1BldDNCU2tmQkxDMmFiTUI3OSthR2lmM084dnJCL3BBaXpqM3AyZFZkTDIKZUhwWE9XTnFvVDd3QUsvLzNrZjZETks5NTQzWXZ3SEVWK0FvNFQyUkFweTJveUFVZGRFNHQrT29jWUxzbHp2NwpkaWNMNUJWcmtHQkVDaUdndWNoYUtQaE9jVkFoUEt4VzlWRyt4ZFphRlRQZnRJY2hzOFpnKzlNbEYxaTNuUkVtCkNvZTJWVWx3WTJaeVhVZU0xN1pucy9XdWJaTlpIT2hUV3Q4ZHFqcmRnUEs2ck1ZSlFZRk5oYktPZFNJZUJsclMKeFRnSEk3d1ZuUXExSU8vRXpKbnMwc0x6MUJ3NDFoNFdBSDdteHNHbWtQQUhqcGNWNnpxaWlXcE0xd3d2cmMzNApxQ3ZVTGtId3hiaTE2WUVhQitDN1NlVnVHMmNwRTh3Z205ZENFMWNQQW9JQkFHUDVqUWZXN1JiU2xrNFd5WFoyCkpIQSs2OXpVM25QVUFwZmZYV3h2TC9QaHl2WUNuRlNadmpqZGRyUjRsSzhPRVdYTEtFMDVxaWJtbVJWMmFacloKZFA5R3A1UTZJVG9pM1lGakZnQzdmZlFNejYzT09MR3FjeTRIUTVOanZ5YUUzRGc4VlR1TUIyNU5ibVVqRUdldAo5NDhXNVBhcDB1WHFGRlZTb1lKU3lQVUlqZXE5SWlFOThqZ3A4RFZYS01hK0NWU0dneVRQcVgwcnF0VE52S2hFCnU0dUtrMVp5aFp1bVRSemlkRnhMbFZ2ZS9XdXl4ZC9rZXBLZTZkemVvRDRqODhQdS95M3Rta3hueDFXZCt3OHAKRCtwU05JN3BkQ2Q1L2pER0pkRmJqOU11M2xzTkJ6Rno2d2FYeE45QjAzYVhoT3BhaHNobkVpQVNzSDU3WlJTVgppUmtDZ2dFQUtPR1NibXkrUEl3dTA5WHI0a2RGWlZqc3F2S0NrZHBFTEVXWmhJT2pQanVVV1BvRXllL0xxR3NCCmFqd3pTdlFLQmU2U2xLeXZ4SVNsdmJmWktseG5JUW1SRHpoZ0x0SGo1dG1ZRDV4dHJmM1BtYUVmUlhMSy93bG8KNk1aRnczK21qaTNORmtPN0F5M3NoVzMwYnlKcDRyeHhrWUgvOHpnMjN3dnZNY2RXOHNBVXJlWW5BWkpSM1lxdQpoeS91aDlJajNDdXBSc1Q3Nyttb3o4YlZReEN0aGxFK2s0NG54ZWNqMUw0SzZuWEpnOHUxV3BPN0FNc1l3aXFKClJxVEJXeXV3UXR3QkxMUWxiQVBTTmlxaXZtb2pyTGdNZmdXam90SWFqYTRFV0RzcmlkaTRkcXVTb2Irc0tXRjIKYU1HYXRWRzQvbjNsdmphTDNNbGRuaENOWXB4TmdBPT0KLS0tLS1FTkQgUFJJVkFURSBLRVktLS0tLQo=
```

You can make the values from this secret available through a custom volume mount:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  processes:
    general:
      podTemplate:
        spec:
          volumes:
            - name: fdb-certs
              secret:
                secretName: fdb-kubernetes-operator-secrets
          containers:
            - name: foundationdb
              env:
                - name: FDB_TLS_CERTIFICATE_FILE
                  value: /tmp/fdb-certs/tls.crt
                - name: FDB_TLS_CA_FILE
                  value: /tmp/fdb-certs/tls.crt
                - name: FDB_TLS_KEY_FILE
                  value: /tmp/fdb-certs/tls.key
              volumeMounts:
                - name: fdb-certs
                  mountPath: /tmp/fdb-certs
```

This will delete the pods in the cluster and recreate them with the new environment variables and volumes.

You can customize the same kind of fields on the sidecar container by adding them under the `sidecarContainer` section of the spec.

Note: The example above adds certificates to the environment, but it does not enable TLS for the cluster. We do not currently have a way to enable TLS once a cluster is running. If you set the `enableTls` flag on the container when you create the cluster, it will be created with TLS enabled.

## Pod Update Strategy

When you need to update your pods in a way that requires recreating them, there are two strategies you can use.

The default strategy is to do a rolling bounce, where at most one fault domain is bounced at a time. While a pod is being recreated, it is unavailable, so this will degrade the fault tolerance for the cluster. The operator will ensure that pods are not deleted unless the cluster is at full fault tolerance, so if all goes well this will not create an availability loss for clients.

Deleting a pod may cause it to come back with a different IP address. If the process was serving as a coordinator, the coordinator will still be considered unavailable after the replaced pod starts. The operator will detect this condition, and will change the coordinators automatically to ensure that we regain fault tolerance.

The other strategy you can use is to do a migration, where we replace all of the instances in the cluster. If you want to opt in to this strategy, you can set the field `updatePodsByReplacement` in the cluster spec to `true`. This strategy will temporarily use more resources, and requires moving all of the data to a new set of pods, but it will not degrade fault tolerance, and will require fewer recoveries and coordinator changes.

There are some changes that require a migration regardless of the value for the `updatePodsByReplacement` section. For instance, changing the volume size or any other part of the volume spec is always done through a migration.

## Choosing Your Public IP Source

The default behavior of the operator is to use the IP assigned to the pod as the public IP for FoundationDB. This is not the right choice for some environments, so you may need to consider an alternative approach.

### Pod IPs

You can choose this option by setting `spec.services.publicIPSource=pod`. This is currently the default selection.

In this mode, we use the pod's IP as both the listen address and the public address. We will not create any services for the pods.

Using pod IPs can present several challenges:

* Deleting and recreating a pod will lead to the IP changing. If the process is a coordinator, this can only be recovered by changing coordinators, which requires that a majority of the old coordinators still be functioning on their original IP.
* Pod IPs may not be routable from outside the Kubernetes cluster.
* Pods that are failing to schedule will not have IP addresses. This prevents us from excluding them, requiring manual safety checks before removing the pod.

### Service IPs

You can choose this option by setting `spec.services.publicIPSource=service`. This feature is new, and still experimental, but we plan to make it the default in the future.

In this mode, we create one service for each pod, and use that service's IP as the public IP for the pod. The pod IP will still be used as the listen address. This ensures that IPs stay fixed even when pods get rescheduled, which reduces the need for changing coordinators and protects against some unrecoverable failure modes.

Using service IPs presents its own challenges:

* In some networking configurations, pods may not be able to access service IPs that route to the pod. See the section on hairpin mode in the [Kubernetes Docs](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-service/#a-pod-fails-to-reach-itself-via-the-service-ip) for more information.
* Creating one service for each pod may cause performance problems for the Kubernetes cluster
* We currently only support services with the ClusterIP type. These IPs may not be routable from outside the Kubernetes cluster.
* The Service IP space is often more limited than the pod IP space, which could cause you to run out of service IPs.

## Using Multiple Namespaces

Our [sample deployment](https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml) configures the operator to run in single-namespace mode, where it only manages resources in the namespace where the operator itself is running. If you want a single deployment of the operator to manage your FDB clusters across all of your namespaces, you will need to run it in global mode. Which mode is appropriate will depend on the constraints of your environment.

### Single-Namespace Mode

To use single-namespace mode, set the `WATCH_NAMESPACE` environment variable for the controller to be the namespace where your FDB clusters will run. It does not have to be the same namespace where the operator is running, though this is generally the simplest way to configure it. When you are running in single-namespace mode, the controller will ignore any clusters you try to create in namespaces other than the one you give it.

The advantage of single-namespace mode is that it allows owners of different namespaces to run the operator themselves without needing access to other namespaces that may be managed by other tenants. The only cluster-level configuration it requires is the installation of the CRD. The disadvantage of single-namespace mode is that if you are running multiple namespaces for a single team, each namespace will need its own installation of the controller, which can make it more operationally challenging.


To run the controller in single-namespace mode, you will need to configure the following things:

* A service account for the controller
* The serviceAccountName field in the controller's pod spec
* A `WATCH_NAMESPACE` environment variable defined in the controller's pod spec
* A Role that grants access to the necessary permissions to all of the resources that the controller manages. See the [sample role](https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/samples/deployment/rbac_role.yaml) for the list of those permissions.
* A RoleBinding that binds that role to the service account for the controller

The sample deployment provides all of this configuration.

### Global Mode

To use global mode, omit the `WATCH_NAMESPACE` environment variable for the controller. When you are running in global mode, the controller will watch for changes to FDB clusters in all namespaces, and will manage them all through a single instance of the controller.

The advantage of global mode is that you can easily add new namespaces without needing to run a new instance of the controller, which limits the per-namespace operational load. The disadvantage of global mode is that it requires the controller to have extensive access to all namespaces in the Kubernetes cluster. In a multi-tenant environment, this means the controller would have to be managed by the team that is adminstering your Kubernetes environment, which may create its own operational concerns.

To run the controller in global mode, you will need to configure the following things:

* A service account for the controller
* The serviceAccountName field in the controller's pod spec
* A ClusterRole that grants access to the necessary permissions to all of the resources that the controller manages. See the [sample role](https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/samples/deployment/rbac_role.yaml) for the list of those permissions.
* A ClusterRoleBinding that binds that role to the service account for the controller

You can build this kind of configuration easily from the sample deployment by changing the following things:

* Delete the configuration for the `WATCH_NAMESPACE` variable
* Change the Roles to ClusterRoles
* Change the RoleBindings to ClusterRoleBindings