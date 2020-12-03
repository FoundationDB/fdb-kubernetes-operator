#!/usr/bin/env bash

set -euo pipefail

ramfs_size_mb=1024
mount_point="${1:-${HOME}/volatile}"

if grep -qs "${mount_point}" <(mount) > /dev/null;
then
  echo "${mount_point} already mounted."
  exit 1
fi

mkdir -p "${mount_point}"

ramfs_size_sectors=$((ramfs_size_mb*1024*1024/512))
ramdisk_dev=$(hdiutil attach -nobrowse -nomount ram://${ramfs_size_sectors})
# remove whitespaces included in the hdutil output
ramdisk_dev="${ramdisk_dev%"${ramdisk_dev##*[![:space:]]}"}"
newfs_hfs -v 'Volatile' "${ramdisk_dev}"
mount -o noatime -t hfs "${ramdisk_dev}" "${mount_point}"

echo "In order to use the ramdisk set export TMPDIR=${mount_point}"

echo "cleanup:"
echo "umount ${mount_point}"
echo "diskutil eject ${ramdisk_dev}"
