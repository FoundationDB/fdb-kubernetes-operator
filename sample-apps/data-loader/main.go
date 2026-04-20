package main

import (
	"context"
	"encoding/binary"
	"flag"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
)

// initRandomGenerator setup the random generator to generate the values.
func initRandomGenerator() *rand.ChaCha8 {
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixMilli()))
	return rand.NewChaCha8(seed)

}

// dataLoaderOptions represents the options for the data loader.
type dataLoaderOptions struct {
	// keys the amount of keys that should be written
	keys int
	// batchSize the amount of keys to write in a batch.
	batchSize int
	// valueSize the size of a single value.
	valueSize int
	// readSize the amount of key ranges that should be read. If 0 reading is disabled.
	readBatchSize int
	// loadDuration if set the duration the load should be ongoing.
	loadDuration time.Duration
	// clusterFile to connect to the FDB cluster to.
	clusterFile string
	// shutdown if set to false the data loader will not shutdown after load is done.
	shutdown bool
}

// latencyStats represents a simplified latency metrics tracker.
type latencyStats struct {
	count    int
	total    time.Duration
	min, max time.Duration
}

// record will add a metric to the latency tracker.
func (s *latencyStats) record(d time.Duration) {
	s.count++
	s.total += d
	if s.min == 0 || d < s.min {
		s.min = d
	}
	if d > s.max {
		s.max = d
	}
}

// avg will return the avg for the latency tracker.
func (s *latencyStats) avg() time.Duration {
	if s.count == 0 {
		return 0
	}
	return s.total / time.Duration(s.count)
}

// loadData will load and optional read data from/to the FDB cluster.
func loadData(ctx context.Context, options *dataLoaderOptions) {
	startTime := time.Now()
	var writtenBatches int
	var writeStats, readStats latencyStats
	defer func() {
		log.Printf(
			"data loading took: %s, %d batches were written\n",
			time.Since(startTime),
			writtenBatches,
		)
		log.Printf("writes — count: %d, avg: %s, min: %s, max: %s",
			writeStats.count, writeStats.avg(), writeStats.min, writeStats.max)
		log.Printf("reads  — count: %d, avg: %s, min: %s, max: %s",
			readStats.count, readStats.avg(), readStats.min, readStats.max)

	}()
	batchCount := options.keys / options.batchSize

	log.Println("opening database with cluster file:", options.clusterFile)
	db, err := fdb.OpenDatabase(options.clusterFile)
	if err != nil {
		log.Fatalf("could not open database: %s\n", err)
	}

	err = db.Options().SetTransactionTimeout(10 * time.Second.Milliseconds())
	if err != nil {
		log.Fatalf("could not set default timeout: %s\n", err)
	}

	readPrefixes := make([]tuple.UUID, 0, options.readBatchSize)
	randomGen := initRandomGenerator()
	for {
		if options.loadDuration > 0 {
			if time.Since(startTime) > options.loadDuration {
				break
			}
		} else {
			if writtenBatches >= batchCount {
				break
			}
		}

		select {
		case <-ctx.Done():
			// Handle graceful shutdown
			log.Printf("Stopping: %v\n", ctx.Err())
			return
		default:
			log.Println("Writing batch", writtenBatches)

			prefix, err := getUuid(randomGen)
			if err != nil {
				log.Fatalf("could not generate UUID for prefix: %s\n", err)
				return
			}

			writeStart := time.Now()
			_, err = db.Transact(func(transaction fdb.Transaction) (any, error) {
				for idx := 0; idx < options.batchSize; idx++ {
					token := make([]byte, options.valueSize)
					_, randErr := randomGen.Read(token)
					if randErr != nil {
						return nil, randErr
					}

					transaction.Set(tuple.Tuple{prefix, idx}, token)
				}

				return nil, nil
			})

			if err != nil {
				log.Printf("could not write data: %s\n", err)
			}

			writeStats.record(time.Since(writeStart))
			writtenBatches++

			// If reading is disabled skip all further steps.
			if options.readBatchSize <= 0 {
				continue
			}

			if len(readPrefixes) < options.readBatchSize {
				readPrefixes = append(readPrefixes, prefix)
			} else {
				// Replace a random element in the slice
				readPrefixes[rand.IntN(len(readPrefixes))] = prefix
			}

			readStart := time.Now()
			_, err = db.ReadTransact(func(transaction fdb.ReadTransaction) (any, error) {
				results := make([]fdb.RangeResult, len(readPrefixes))
				for idx, readPrefix := range readPrefixes {
					prefixRange, err := fdb.PrefixRange(tuple.Tuple{readPrefix}.Pack())
					if err != nil {
						return nil, err
					}

					results[idx] = transaction.GetRange(prefixRange, fdb.RangeOptions{})
				}

				var keyCount int
				for _, future := range results {
					keyVals, err := future.GetSliceWithError()
					if err != nil {
						return nil, err
					}

					keyCount += len(keyVals)
				}

				log.Printf("read %d keys from %d prefixes\n", keyCount, len(readPrefixes))

				return nil, nil
			})

			if err != nil {
				log.Printf("could not read data: %s\n", err)
			}

			readStats.record(time.Since(readStart))
		}
	}

	if !options.shutdown {
		// Block until signal is received
		<-ctx.Done()
	}
}

func getUuid(randomGen *rand.ChaCha8) (tuple.UUID, error) {
	buf := make([]byte, 16)
	_, err := randomGen.Read(buf)
	if err != nil {
		return tuple.UUID{}, err
	}

	buf[6] = (buf[6] & 0x0f) | 0x40 // Version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // Variant is 10

	return tuple.UUID(buf), nil
}

func main() {
	fdb.MustAPIVersion(710)

	var keys, batchSize, readBatchSize, valueSize int
	var loadDuration time.Duration
	var clusterFile string
	var shutdown bool

	flag.IntVar(&keys, "keys", 100000, "Number of keys to generate.")
	flag.IntVar(&batchSize, "batch-size", 10, "Number of updates per batch.")
	flag.IntVar(&valueSize, "value-size", 1000, "Number of bytes to include in each value.")
	flag.IntVar(
		&readBatchSize,
		"read-batch-size",
		0,
		"If set to a number greater than 0 data loader will read that amount of key prefixes.",
	)
	flag.DurationVar(
		&loadDuration,
		"load-duration",
		0,
		"If set will determine how long the load is running, keys will be ignored in this case.",
	)
	flag.StringVar(
		&clusterFile,
		"cluster-file",
		os.Getenv("FDB_CLUSTER_FILE"),
		"Path to the cluster file, defaults to $FDB_CLUSTER_FILE",
	)
	flag.BoolVar(
		&shutdown,
		"shutdown",
		true,
		"If set to false the data loader will not shutdown after the load is done.",
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadData(ctx, &dataLoaderOptions{
		keys:          keys,
		batchSize:     batchSize,
		valueSize:     valueSize,
		readBatchSize: readBatchSize,
		loadDuration:  loadDuration,
		clusterFile:   clusterFile,
		shutdown:      shutdown,
	})
}
