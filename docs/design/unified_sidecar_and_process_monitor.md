# Design on Combining the Main Container and the Sidecar Container on Kubernetes

## Metadata

* Authors: @brownleej
* Created: 2020-12-21
* Updated: 2021-06-23

## Background

We currently make use of three different containers in our Kubernetes deployment: a main container that runs fdbmonitor and fdbserver, an init container that generates the monitor conf and cluster file for the main container, and a sidecar container that provides updates to the monitor conf and cluster file for the main container. Running additional containers creates resource overhead and requires more complex interactions between containers. We would like to find a way to combine the function in the two containers for a simpler set of interactions.

## General Design Goals

* Remove the init container from our deployments
* Remove any use of the sidecar container outside of copying new binaries during upgrades

## Proposed Design

We will build a new process called `fdb-kubernetes-monitor` that is designed to run fdbserver processes itself. It will take a configuration file that specifies the processes to launch, including explicit environment variable substitutions, and will not have any mechanism for fdbmonitor conf file management. It will also have APIs to copy files into another folder, and to check the hashes of files, like the sidecar does. This process will not be a drop-in replacement for either the sidecar or fdbmonitor, and will be designed specifically for use within the Kubernetes operator. It will be available to use for other purposes, such as injecting FDB client libraries into a client application.

fdb-kubernetes-monitor will automatically load new configuration from the local copy of the config map, but will not automatically relaunch processes with new configuration. Instead, it will rely on having the operator do a synchronized kill so that we can instantly bounce all of the processes in a coordinated fashion. When fdb-kubernetes-monitor detects that a child process has been killed, it will launch a new child process with the latest configuration. It will also do appropriate signal handling for SIGKILL and SIGTERM signals in accordance with Docker best practices. The configuration file will be designed such that all of the processes of a single machine class can share a configuration file, with environment variables for command-line arguments that vary between processes. fdb-kubernetes-monitor will have a command-line option for how many fdbserver processes to spawn, and the configuration file will support defining arguments that vary between different processes in the pod based on the process number.

During upgrades, we will have a sidecar container that we upgrade to the new version, with instructions to copy the binary into a shared volume. We will then update the configuration for fdb-kubernetes-monitor in the main container to tell it to use a newer version, which will cause it to use the version-scoped binary in the shared volume. We will include the sidecar container all the time in the initial work for this feature. In the future, we will explore an option to deploy it as an ephemeral container, once that feature is sufficiently stable in Kubernetes.

fdb-kubernetes-monitor will log its activity to standard output, and will have an option to also log to a file.

fdb-kubernetes-monitor will ship in an image called `foundationdb-kubernetes`. This image will be versioned with FoundationDB, and will include multiple client libraries, like the `foundationdb-kubernetes-sidecar` image currently does. The first `foundationdb-kubernetes` image we cut for a release will have the FoundationDB version as its tag. For subsequent updates to the image that do not involve a FoundationDB update, we will append a build number, of the format `-2`. For instance, we would have images in a series like `6.3.0`, `6.3.0-2`, `6.3.0-3`, and so on.

In order to keep the new image lightweight, and allow the development team for the operator to easily manage the development on the monitor, we will write the fdb-kubernetes-monitor process in go.

We will have an option in the cluster spec to use the new launcher, which will be off by default. We will have a minor release of FoundationDB where we publish both the old sidecar image and the new kubernetes image, to aid with the transition. For any patch version of FoundationDB in that minor release, the user will have the option of which launch strategy to use. Upon the next minor release, we will stop publishing the old sidecar image, which means users will need to switch to the new launcher before that release.

When the new launcher is used, both the main container and the sidecar container will run the same image, but the sidecar container will have a different set of arguments that cause it to only copy the fdbserver binary, where necessary. The main container will load its configuration file, launch the configured server processes, and then wait.

The new launcher will retain most of the command-line arguments of the current sidecar, but will not open up a listener. Specifically, it will support the following arguments:

* `--mode` - A choice of three modes to run the launcher in. `launch` mode is the default behavior, and will launch subprocesses as described above. `init` mode will copy files and exist. `sidecar` mode will copy files and wait.
* `--input-dir` - The directory containing the input files from the config map.
* `--output-dir` - The directory into which the launcher should place the files it copies.
* `--copy-file` - A file to copy from the config map to the output directory.
* `--copy-binary` - A binary to copy to the output directory.
* `--copy-library` - A version of the client library to copy to the output directory.
* `--input-monitor-conf` - The name of a monitor conf template in the input files.
* `--main-container-version` - The version of the main foundationdb container in the pod. 
* `--shared-binary-dir` - The directory where the binaries written by the sidecar will be mounted in the main container.
* `--require-not-empty` - A file that must be present and non-empty in the input directory.
* `--additional-env-file` - A file with additional environment variables to load and make available for environment substitution in the process configuration. This file must contain lines of the format `key=value`, where `key` is the name of the environment variable and `value` is the value.
* `--server-count` - The number of fdbserver processes to launch.

This API will have an option to listen on a TLS connection and restrict the incoming connections using the same peer verification options we support in the current sidecar.

When in `launch` mode, the launcher will watch the input configuration file for changes, and upon detecting changes it will set annotations on the pod to indicate the latest configuration that it has loaded. It will also perform basic safety checks on this configuration, and if the safety checks fail it will create an event describing the error with its configuration. This flow will require that the launcher has a service account token that allows it to communicate with the Kubernetes API. The operator will require that `automountServiceAccountToken` is either unset or set to `true` on the pod spec. We will also be able to set an annotation to tell the launcher to reload configuration, in case it misses an event about changes to the file, but we will generally want the launcher to proactively reload the configuration so that the operator doesn't need to do multiple runs to get a pod to converge on new configuration. The launcher will also create an event on the pod when it loads new configuration.

### Process Configuration

The process configuration will be represented in a JSON file that contains a ProcessConfiguration object. The spec for this file is describe below.

```
ProcessConfiguration {
	version: string - The version of FoundationDB the process should run. This will determine the path to the fdbserver process.
	arguments: []Argument - The arguments to the fdbserver process
}

Argument {
	type: ArgumentType - Determines how the value is generated. The default is Literal
	value: string - For Literal arguments, the value for the argument
	values: []Argument - For Concatenate arguments, a list of smaller arguments that are combined to form a single argument
	source: string - For Environment arguments, the name of an environment variable to use as the source of the argument.
	multiplier: integer - For ProcessNumber arguments, a multiplier to apply to the process number
	offset: integer - For ProcessNumber arguments, an offset to apply to the process number
}

ArgumentType enum {
	Literal - A value provided in the argument
	Concatenante - A combination of other arguments
	Environment - A value from an environment variable
	ProcessNumber - A value calculated from the process number. The processes are numbered starting from 1.
}
```

Example configuration, in JSON form:

```json
{
	"version": "6.3.0",
	"arguments": [
		{"value": "--public_address"},
		{"type": "Concatenate", "values": [
			{"type": "Environment", "source": "FDB_PUBLIC_IP"},
			{"value": ":"},
			{"type": "ProcessNumber", "offset": 4498, "multiplier": 2},
			{"value": ":tls"}
		]},
		{"value": "--listen_address"},
		{"type": "Concatenate", "values": [
			{"type": "Environment", "source": "FDB_POD_IP"},
			{"value": ":"},
			{"type": "ProcessNumber", "offset": 4498, "multiplier": 2},
			{"value": ":tls"}
		]},
		{"value": "--datadir"},
		{"type": "Concatenate", "values": [
			{"value": "/var/fdb/data/"},
			{"type": "ProcessNumber"}
		]},
		{"value": "--class"},
		{"value": "storage"},
		{"value": "--locality_zoneid"},
		{"type": "Environment", "source": "FDB_ZONE_ID"},
		{"value": "--locality_instance-id"},
		{"type": "Environment", "source": "FDB_INSTANCE_ID"},
		{"value": "--locality_process-id"},
		{"type": "Concatenate", "values": [
			{"type": "Environment", "source": "FDB_INSTANCE_ID"},
			{"value": "-"},
			{"type": "ProcessNumber"}
		]}
	]
}
```

Let's assume this is run with a process count of 2, that 6.3.0 is the version of the main container, and that the following environment variables are set:

```sh
FDB_PUBLIC_IP=10.0.0.1
FDB_POD_IP=192.168.0.1
FDB_ZONE_ID=zone1
FDB_INSTANCE_ID=storage-1
```

This would spawn two processes, with the following configuration:

```sh
/usr/bin/fdbserver --public_address 10.0.0.1:4500:tls --listen_address 192.168.0.1:4500:tls --datadir /var/fdb/data/1 --class storage --locality_zoneid zone1 --locality_instance_id storage-1 --locality_process_id storage-1-1

/usr/bin/fdbserver --public_address 10.0.0.1:4502:tls --listen_address 192.168.0.1:4502:tls --datadir /var/fdb/data/2 --class storage --locality_zoneid zone1 --locality_instance_id storage-1 --locality_process_id storage-1-2
```

### Annotations 

* `foundationdb.org/launcher-current-configuration`: A JSON document with the process configuration that the launcher has currently loaded. This will have the same structure as the input configuration.
* `foundationdb.org/launcher-environment`: A JSON document with the keys and values for the environment variables that the launcher is using in its process configuration. This will include variables loaded from the `--additional-env-file` parameter.
* `foundationdb.org/outdated-config-map-seen`: This annotation will be set by the operator to tell the pod to update its config map. The value for this annotation will be a timestamp. Upon detecting a change to this annotation, the launcher will check the latest configuration.

### Version Management

In `launch` mode and `sidecar` mode, we will require two parameters called `--main-container-version` that specifies the version of the main container. When in `launch` mode, the launcher will check if this version is equal to the version in its input configuration. If the two are equal, the launcher will use `/usr/bin/fdbserver` as the path to the fdbserver binary. If the two are not equal, the launcher will use `$dynamic/bin/$version/fdbserver` as the path to the fdbserver binary. In the latter form, `$dynamic` refers to the path set in the `--shared-binary-dir` directory, and `version` refers to the version specified in the `version` field in the process configuration.

### Safety Checks

Before enabling newly loaded configuration, the launcher will preform safety checks on the configuration to ensure that it is safe to use.

* The fdbserver binary path must point to a file that exists, is non-empty, and is executable.
* All environment variables that are referenced in the process configuration must have non-empty values.

If one of these conditions is not met, the launcher will create an error event describing the problem, and will not enable the new configuration.
