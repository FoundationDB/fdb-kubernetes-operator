package main

import (
	"context"
	"encoding/binary"
	"flag"
	"github.com/google/uuid"
	"log"
	"math/rand/v2"
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

func loadData(ctx context.Context, keys int, batchSize int, valueSize int) {
	batchCount := keys / batchSize

	db, err := fdb.OpenDefault()
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
			_, err = db.Transact(func(transaction fdb.Transaction) (interface{}, error) {
				prefix := uuid.NewString()

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

func main() {
	fdb.MustAPIVersion(710)

	keys := flag.Int("keys", 100000, "Number of keys to generate")
	batchSize := flag.Int("batch-size", 10, "Number of bytes to include in each value")
	valueSize := flag.Int("value-size", 1000, "Number of bytes to include in each value")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadData(ctx, *keys, *batchSize, *valueSize)
}
