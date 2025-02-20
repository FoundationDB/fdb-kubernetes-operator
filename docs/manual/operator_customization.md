# Operator Customization

This document provides more information on how to customize the way the operator runs based on the needs of your infrastructure.

## Details on FoundationDB Version Compatibility

Out of the box, the operator only supports a single minor version of FoundationDB, matching the oldest FoundationDB version listed for that operator version in the [compatibility guide](/docs/compatibility.md).
This constraint comes from the FoundationDB client library, which must match the protocol version of the servers that it connects to. 
To connect to newer versions of FoundationDB, you must install a [multi-version client library](https://apple.github.io/foundationdb/api-general.html#multi-version-client-api).
The operator supports using init containers to provide additional client libraries independently of the libraries shipped in the basic operator docker image. We have an example of this configuration in our [example deployment configuration](../../config/samples/deployment.yaml#L176).
This example is updated regularly to include binaries and client libraries for all supported versions of FoundationDB.

If you need to customize this, to support pre-releases or custom builds, you can use this example as a baseline and define your own init containers. The configuration for these init containers works as follows:

* The init container runs the FoundationDB sidecar image for the desired minor version, with arguments for copying binaries.
* It takes a `--copy-library` argument providing the version you want to use, which will typically match the minor version of the image in this pattern.
* It takes a `--copy-binary` argument for each binary that you want the operator to use/
* It takes an `--output-dir` argument provided a directory with a fully-qualified version.
* It takes an `--init-mode` argument telling the sidecar to copy these files and exit.
* The parent directory of the directory used in the `--output-dir` argument is mapped to a volume mount. That volume mount is also mounted in the manager container for the operator, at the path `/usr/bin/fdb`.

At start time, the operator scans this directory for version-specific binaries, and remaps the files to match the locations where the operator needs them.

## Customizing the Primary Client Library

By default, the primary client library used by the operator is the oldest supported version, as discussed above.
If you want to use a newer version of the client library as your primary client, you can control that through additional init containers.

```yaml
# This provides partial configuration for the deployment to show what needs to change in order to
# use a special client library.
apiVersion: apps/v1
 kind: Deployment
 metadata:
   name: controller-manager
 spec:
   template:
     spec:
       initContainers:
         # Install this library in a special location to force the operator to
         # use it as the primary library.
         - name: foundationdb-kubernetes-init-7-3-primary
           image: foundationdb/fdb-kubernetes-monitor:7.3.59
           args:
             # Note that we are only copying a library, rather than copying any binaries. 
             - --copy-library
             - "7.3"
             - --copy-primary-library
             - "7.3"
             - --output-dir
             - /var/output-files/primary # Note that we use `primary` as the subdirectory rather than specifying the FoundationDB version like we did in the other examples.
             - --mode
             - init
           volumeMounts:
             - name: fdb-binaries
               mountPath: /var/output-files
         # Install binaries alone, to a different directory than the primary client library.
         - name: foundationdb-kubernetes-init-7-3
           image: foundationdb/fdb-kubernetes-monitor:7.3.59
           args:
             - --copy-binary
             - fdbcli
             - --copy-binary
             - fdbbackup
             - --copy-binary
             - fdbrestore
             - --output-dir
             - /var/output-files"
             - --mode
             - init
           volumeMounts:
             - name: fdb-binaries
               mountPath: /var/output-files
       containers:
         - name: manager
           env:
             # Set the LD_LIBRARY_PATH environment variable to tell FoundationDB to load its primary client library from this directory instead of the directory provided by the image.
             - name: LD_LIBRARY_PATH
               value: /usr/bin/fdb/primary/lib
```

## Next

You can continue on to the [next section](replacements_and_deletions.md) or go back to the [table of contents](index.md).
