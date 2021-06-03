# Running with TLS

The operator supports running clusters with mutual TLS to secure connections and control access to the cluster. Running with TLS requires additional configuration that will vary based on your environment. This document will cover the elements you need to configure. We also recommend reading the [main FoundationDB docs on TLS](https://apple.github.io/foundationdb/tls.html).

Running FDB with mutual TLS means that all server processes must be configured with a certificate, which they will use as both a server certificate and a client certificate. All client processes must also have a client certificate. In order to control access, you must define peer verification rules that specify what certificates a process will accept from the other end of its connections.

## Example Cluster with TLS

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: fdb-certs
stringData:
  cert.pem: |
    # Put your certificate here.
  key.pem: |
    # Put your key here.
---
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  trustedCAs:
    - |
      # Put the root CAs you trust here.
    - |
      # All certificates you use for this cluster must be signed by a chain leading to a CA in this list.
  mainContainer:
    enableTls: true
    peerVerificationRules: "S.CN=sample-cluster.foundationdb.example|S.CN=sample-cluster-client.foundationdb.example|S.CN=fdb-kubernetes-operator.foundationdb.example"
  sidecarContainer:
    enableTls: true
    peerVerificationRules: "S.CN=fdb-kubernetes-operator.foundationdb.example"
  processes:
    general:
      podTemplate:
        spec:
          containers:
          - name: foundationdb
            env:
            - name: FDB_TLS_CERTIFICATE_FILE
              value: /var/fdb-certs/cert.pem
            - name: FDB_TLS_KEY_FILE
              value: /var/fdb-certs/key.pem
            volumeMounts:
            - name: fdb-certs
              mountPath: /var/fdb-certs
          - name: foundationdb-kubernetes-sidecar
            env:
            - name: FDB_TLS_CERTIFICATE_FILE
              value: /var/fdb-certs/cert.pem
            - name: FDB_TLS_KEY_FILE
              value: /var/fdb-certs/key.pem
            volumeMounts:
            - name: fdb-certs
              mountPath: /var/fdb-certs
          volumes:
          - name: fdb-certs
            secret:
              secretName: fdb-certs
```

We'll use this example as a reference as we break down the different customization points.

## Defining Certificates

The example above uses a secret called `fdb-certs` to hold the certificate and key. You could provide this certificate through Kubernetes' [built-in certificate management](https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/), or something like [cert-manager](https://cert-manager.io). You could also use a totally different way of providing certificates, like an init container that generates certificates at startup. The only requirement is that the certificate and key are put in files that are available to the fdbserver process.

In this example, we're using the same certificates for connections to the main FDB process and connections to the Kubernetes sidecar. If you want to use TLS for both processes, you'll need to set the environment variables in both containers.

## Defining a CA File

In order for the fdbserver processes to know which certificates they can trust, you must provide them with a CA file containing the trusted root certificate authorities. The operator can automatically generate this file based on a list of root certificates provided to the `trustedCAs` field. This field results in the following configuration being defined:

1. A `ca.pem` file is added to the main config map for the cluster.
2. This file is exposed to all of the built-in containers.
3. The built-in containers get an environment variable set with the name `FDB_TLS_CA_FILE`, set to the path to this `ca.pem` file.

If you don't want to list the CAs in the cluster spec, you can provide the CA file to the containers through a custom config map or some other mechanism for injecting the files. You can set the `FDB_TLS_CA_FILE` environment to a custom value, and the operator will not override it.

## Peer Verification Rules

You can define custom peer verification rules to restrict what certificates processes accept. This rules are applied for both inbound and outbound connections. In the example above, we specified `S.CN=sample-cluster.foundationdb.example|S.CN=sample-cluster-client.foundationdb.example|S.CN=fdb-kubernetes-operator.foundationdb.example` for the foundationdb container. This means it will accept certificaters with a common name of `sample-cluster.foundationdb.example`, or a common name of `sample-cluster-client.foundationdb.example`, or a common name of `fdb-kubernetes-operator.foundationdb.example`. You can find more details on the syntax of the peer verification rules in FDB's TLS documentation.

You can specify different peer verification rules for the main container and the sidecar container, to support limiting access to each container based on what each one is doing.

You must always ensure that the peer verification rules allow access from the cluster's own certificates, from the operator's certificates, and from any clients that you want to allow to access the cluster.

## Configuring the Operator

If you want to run any clusters with TLS, you must configure the operator to support FDB's mutual TLS. This requires setting the same environment variables that you set on the fdbserver processes: `FDB_TLS_CERTIFICATE_FILE`, `FDB_TLS_KEY_FILE`, and `FDB_TLS_CA_FILE`. You will probably want to use the same certificate mechanism that you use for the FDB certs for the operator certs as well. This certificate configuration will only be used for connections to FoundationDB and to the sidecar process.

Connections to FDB will use the peer verification logic provided by the FDB client, which can be configured with peer verification rules in the same way as we support for the server. However, there is no mechanism to set these rules on a per-cluster basis, so it may not be beneficial to define them on the operator's side of the connection.

Connections to the sidecar will use the peer verification logic provided by go's tls library. This means that the sidecar's certificate must be valid for the pod's IP. You can disable verification for the connections to the sidecar by setting the environment variable `DISABLE_SIDECAR_TLS_CHECK=1` on the operator, but this will also disable the validation of the certificate chain, so it is not recommended to use this in real environments.

## Next

You can continue on to the [next section](backup.md) or go back to the [table of contents](index.md).
