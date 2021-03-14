#! /usr/bin/python

"""
This file provides a sample app for loading data into FDB.

To use it to load data into one of the sample clusters in this repo,
you can build the image by running `docker build -t fdb-data-loader sample-apps/data-loader`,
and then run the data loader by running `kubectl apply -f sample-apps/data-loader/job.yaml`
"""

import argparse
import random
import uuid
import fdb

fdb.api_version(600)


@fdb.transactional
def write_batch(tr, batch_size, value_size):
    prefix = uuid.uuid4()
    for index in range(1, batch_size + 1):
        tr[fdb.tuple.pack((prefix, index))] = random.randbytes(value_size)


def load_data(keys, batch_size, value_size):
    batch_count = int(keys / batch_size)

    db = fdb.open()
    for batch in range(1, batch_count + 1):
        print("Writing batch %d" % batch)
        write_batch(db, batch_size, value_size)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Load random data into FDB")
    parser.add_argument(
        "--keys", type=int, help="Number of keys to generate", default=100000
    )
    parser.add_argument(
        "--batch-size",
        type=int,
        help="Number of keys to write in each transaction",
        default=10,
    )
    parser.add_argument(
        "--value-size",
        type=int,
        help="Number of bytes to include in each value",
        default=1000,
    )
    args = parser.parse_args()

    load_data(args.keys, args.batch_size, args.value_size)
