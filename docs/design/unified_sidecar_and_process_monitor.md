# Design on Combining the Main Container and the Sidecar Container on Kubernetes

## Metadata

* Authors: @brownleej
* Created: 2020-12-21
* Updated: 2020-12-21

## Background

We currently make use of three different containers in our Kubernetes deployment: a main container that runs fdbmonitor and fdbserver, an init container that generates the monitor conf and cluster file for the main container, and a sidecar container that provides updates to the monitor conf and cluster file for the main container. Running additional containers creates resource overhead and requires more complex interactions between containers. We would like to find a way to combine the function in the two containers for a simpler set of interactions.

## General Design Goals

* Remove the init container from our deployments
* Remove any use of the sidecar container outside of copying new binaries during upgrades

## Proposed Design

We will build a new process called `fdb-kubernetes-monitor` that is designed to run fdbserver processes itself. It will take a configuration file that specifies the processes to launch, including explicit environment variable substitutions, and will not have any mechanism for fdbmonitor conf file management. It will also have APIs to copy files into another folder, and to check the hashes of files, like the sidecar does. This process will not be a drop-in replacement for either the sidecar or fdbmonitor, and will be designed specifically for use within the Kubernetes operator. It will be available to use for other purposes, such as injecting FDB client libraries into a client application.

fdb-kubernetes-monitor will have APIs for loading new configuration from the local copy of the config map, but will not automatically relaunch processes with new configuration. Instead, it will rely on having the operator do a synchronized kill so that we can instantly bounce all of the processes in a coordinated fashion. When fdb-kubernetes-monitor detects that a child process has been killed, it will launch a new child process with the latest configuration. It will also do appropriate signal handling for SIGKILL and SIGTERM signals in accordance with Docker best practices. The configuration file will be designed such that all of the processes of a single machine class can share a configuration file, with environment variables for command-line arguments that vary between processes. fdb-kubernetes-monitor will have a command-line option for how many fdbserver processes to spawn, and the configuration file will support defining arguments that vary between different processes in the pod based on the process number.

During upgrades, we will have a sidecar container that we upgrade to the new version, with instructions to copy the binary into a shared volume. We will then update the configuration for fdb-kubernetes-monitor in the main container to tell it to use a newer version, which will cause it to use the version-scoped binary in the shared volume. We will include the sidecar container all the time in the initial work for this feature. In the future, we will explore an option to deploy it as an ephemeral container, once that feature is sufficiently stable in Kubernetes.

fdb-kubernetes-monitor will log its activity to standard output, and will have an option to also log to a file.

fdb-kubernetes-monitor will ship in an image called `foundationdb-kubernetes`. This image will be versioned with FoundationDB, and will include multiple client libraries, like the `foundationdb-kubernetes-sidecar` image currently does. The first `foundationdb-kubernetes` image we cut for a release will have the FoundationDB version as its tag. For subsequent updates to the image that do not involve a FoundationDB update, we will append a build number, of the format `-2`. For instance, we would have images in a series like `6.3.0`, `6.3.0-2`, `6.3.0-3`, and so on.

In order to keep the new image lightweight, and allow the development team for the operator to easily manage the development on the monitor, we will write the fdb-kubernetes-monitor process in go.

We will have an option in the cluster spec to use the new launcher, which will be off by default. We will have an entire major release of FoundationDB where we publish both the old sidecar image and the new kubernetes image, to aid with the transition. For any version of FoundationDB in that major release, the user will have the option of which launch strategy to use. Upon the next major release, we will stop publishing the old sidecar image, which means users will need to switch to the new launcher before that release.

When the new launcher is used, both the main container and the sidecar container will run the same image, but the sidecar container will have a different set of arguments that cause it to only copy the fdbserver binary, where necessary, and to open up a listener. The main container will load its configuration file, launch the configured server processes, and open a listener. That listener will be an HTTP server that provides an API for checking the hashes of files and other local configuration, similar to the API the sidecar currently provides. However, the fdb-kubernetes-monitor API will be not cover all use cases of the sidecar API. It will provide the following API calls:

* `get_current_configuration` - returns the current process configuration
* `check_hash` - check the hash of a file inside its configuration directory
* `load_configuration` - load the latest configuration from the configuration directory into memory.
* `ready` - returns an `OK` response
* `substitutions` - returns the values for any environment variables that are used in the process configuration

When we are transitioning from the old launcher to the new launcher for a cluster, we will include both a monitor conf file and the kubernetes-monitor conf file in the config map. Once the transition is complete, we will remove the monitor conf file from the sidecar.

This API will have an option to listen on a TLS connection and restrict the incoming connections using the same peer verification options we support in the current sidecar.

### Process Configuration

The process configuration will be represented in a JSON file that contains a ProcessConfiguration object. The spec for this file is describe below.

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

Example configuration, in JSON form:

```json
{
	"version": "6.3.0",
	"arguments": [
		{"value": "--public-address"},
		{"type": "Concatenate", "values": [
			{"type": "Environment", "source": "FDB_PUBLIC_IP"},
			{"value": ":"},
			{"type": "ProcessNumber", "offset": 4498, "multiplier": 2},
			{"value": ":tls"}
		]},
		{"value": "--listen-address"},
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
		{"value": "--locality-zoneid"},
		{"type": "Environment", "source": "FDB_ZONE_ID"},
		{"value": "--locality-instance-id"},
		{"type": "Environment", "source": "FDB_INSTANCE_ID"},
		{"value": "--locality-process-id"},
		{"type": "Concatenate", "values": [
			{"type": "Environment", "source": "FDB_INSTANCE_ID"},
			{"value": "-"},
			{"type": "ProcessNumber"}
		]}
	]
}
```

Let's assume this is run with a process count of 2, that 6.3.0 is the version of the main container, and that the following environment variables are set:

	FDB_PUBLIC_IP=10.0.0.1
	FDB_POD_IP=192.168.0.1
	FDB_ZONE_ID=zone1
	FDB_INSTANCE_ID=storage-1

This would spawn two processes, with the following configuration:

	/usr/bin/fdbserver --public-address 10.0.0.1:4500:tls --listen-address 192.168.0.1:4500:tls --datadir /var/fdb/data/1 --class storage --locality-zoneid zone1 --locality_instance_id storage-1 --locality-process-id storage-1-1

	/usr/bin/fdbserver --public-address 10.0.0.1:4502:tls --listen-address 192.168.0.1:4502:tls --datadir /var/fdb/data/2 --class storage --locality-zoneid zone1 --locality_instance_id storage-1 --locality-process-id storage-1-2
