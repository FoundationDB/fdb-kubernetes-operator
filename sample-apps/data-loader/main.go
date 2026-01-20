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

func loadData(ctx context.Context, keys int, batchSize int, valueSize int, clusterFile string) {
	batchCount := keys / batchSize

	log.Println("opening database with cluster file:", clusterFile)
	db, err := fdb.OpenDatabase(clusterFile)
	if err != nil {
		log.Fatalf("could not open database: %s", err)
	}

	err = db.Options().SetTransactionTimeout(10 * time.Second.Milliseconds())
	if err != nil {
		log.Fatalf("could not set default timeout: %s", err)
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

			prefix, err := getUuid(randomGen)
			if err != nil {
				log.Fatalf("could not generate UUID for prefix: %s", err)
				return
			}

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
		}
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

	var keys, batchSize, valueSize int
	flag.IntVar(&keys, "keys", 100000, "Number of keys to generate")
	flag.IntVar(&batchSize, "batch-size", 10, "Number of updates per batch")
	flag.IntVar(&valueSize, "value-size", 1000, "Number of bytes to include in each value")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadData(ctx, keys, batchSize, valueSize, os.Getenv("FDB_CLUSTER_FILE"))
}
