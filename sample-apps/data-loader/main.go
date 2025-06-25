package main

import (
	"context"
	"encoding/binary"
	"flag"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/google/uuid"
)

// initRandomGenerator setup the random generator to generate the values.
func initRandomGenerator() *rand.ChaCha8 {
	bs := make([]byte, 8)
	binary.LittleEndian.PutUint64(bs, uint64(time.Now().UnixMilli()))

	seed := [32]byte(make([]byte, 32))
	var idx int
	for idx < 32 {
		for _, b := range bs {
			seed[idx] = b
			idx++

			if idx == 32 {
				break
			}
		}
	}

	return rand.NewChaCha8(seed)
}

func loadData(ctx context.Context, keys int, batchSize int, valueSize int, readValues bool, clusterFile string) {
	batchCount := keys / batchSize

	log.Println("opening database with cluster file:", clusterFile)
	db, err := fdb.OpenDatabase(clusterFile)
	if err != nil {
		log.Fatalf("could not open database: %s", err)
	}

	randomGen := initRandomGenerator()
	for i := 0; i < batchCount; i++ {
		select {
		case <-ctx.Done():
			// Handle graceful shutdown
			log.Printf("Stopping: %v\n", ctx.Err())
			return
		default:
			log.Println("Writing batch", i)

			prefix := uuid.NewString()
			_, err = db.Transact(func(transaction fdb.Transaction) (interface{}, error) {
				for idx := 0; idx < batchSize; idx++ {
					token := make([]byte, valueSize)
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

			if !readValues {
				continue
			}

			// Wait one second to give the FDB cluster some time to pull the mutations from the log processes.
			time.Sleep(1 * time.Second)

			values, err := db.ReadTransact(func(transaction fdb.ReadTransaction) (interface{}, error) {
				prefixKey, err := fdb.PrefixRange(tuple.Tuple{prefix}.Pack())
				if err != nil {
					return nil, err
				}

				return transaction.GetRange(prefixKey, fdb.RangeOptions{}).GetSliceWithError()
			})

			keyValues, ok := values.([]fdb.KeyValue)
			if ok {
				log.Println("found", len(keyValues), "key values for prefix", prefix)
			}

			if err != nil {
				log.Printf("could not write data: %s\n", err)
			}
		}
	}
}

func main() {
	fdb.MustAPIVersion(710)

	var keys, batchSize, valueSize int
	var readValues bool
	var clusterFileDirectory string
	flag.IntVar(&keys, "keys", 100000, "Number of keys to generate")
	flag.IntVar(&batchSize, "batch-size", 10, "Number of updates per batch")
	flag.IntVar(&valueSize, "value-size", 1000, "Number of bytes to include in each value")
	flag.BoolVar(&readValues, "read-values", false, "Read the newly written values from the database")
	flag.StringVar(&clusterFileDirectory, "cluster-file-directory", "", "The directory that contains the cluster file, if empty the data loader will fallback to the $FDB_CLUSTER_FILE")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	if clusterFileDirectory == "" {
		wg.Add(1)
		go func() {
			loadData(ctx, keys, batchSize, valueSize, readValues, os.Getenv("FDB_CLUSTER_FILE"))
			wg.Done()
		}()
	} else {
		entries, err := os.ReadDir(clusterFileDirectory)
		log.Println("reading cluster files from", clusterFileDirectory)
		if err != nil {
			log.Fatalf("could not read directory %s: %s", clusterFileDirectory, err)
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".cluster") {
				continue
			}

			wg.Add(1)
			go func() {
				loadData(ctx, keys, batchSize, valueSize, readValues, path.Join(clusterFileDirectory, entry.Name()))
				wg.Done()
			}()
		}
	}

	wg.Wait()
}
