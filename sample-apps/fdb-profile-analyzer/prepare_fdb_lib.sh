#! /bin/bash

# This script downloads internal binaries into the required locations for an
# internal docker build.

set -e

bin_version=$1
dest="/usr/bin/fdb"
minor_version=${bin_version%.*}

if [[ "bin_version" =~ "-rc" ]]; then
  version_path=$dest/$bin_version
else
  version_path=$dest/$minor_version
fi

echo "Downloading FDB binaries for $1 to $version_path"
mkdir -p $version_path

lib_dest=libfdb_c_$minor_version.so
wget -q https://github.com/apple/foundationdb/releases/download/${bin_version}/libfdb_c.x86_64.so -O  ${dest}/$lib_dest
wget -q https://github.com/apple/foundationdb/releases/download/${bin_version}/fdbcli.x86_64 -O $version_path/fdbcli
chmod u+x $version_path/fdbcli
