package delay

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func getRdb() *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	return rdb
}

// add 4 items to queue and read from queue
func TestBase(t *testing.T) {
	rdb := getRdb()
	ctx, cancel := context.WithCancel(context.Background())

	delayQueueName := "delay_queue_name"
	sleepSecond := int64(1)

	// add 4 items
	values := []string{"a", "b", "c", "d"}
	for _, v := range values {
		err := AddToQueue(ctx, rdb, delayQueueName, v, sleepSecond, 100)
		if err != nil {
			t.Fatal(err)
		}
	}

	// read from delay queue
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		resCh, errCh := GetFromQueue(ctx, rdb, delayQueueName)
		for res := range resCh {
			t.Log("get from queue, v:", res)
			h := values[0]
			values = values[1:]
			assert.Equal(t, h, res)
			if len(values) == 0 {
				cancel()
			}
		}
		// check error
		for err := range errCh {
			assert.Error(t, err, context.Canceled)
		}
		wg.Done()
	}()

	// add timeout check
	select {
	case <-time.After(time.Second * time.Duration(sleepSecond+1)):
		t.Fatal("error timeout")
	case <-ctx.Done():
		wg.Wait()
	}
}

func TestAutoExpire(t *testing.T) {
	rdb := getRdb()
	ctx, cancel := context.WithCancel(context.Background())

	delayQueueName := "delay_queue"
	sleepSecond := int64(1)
	maxTTL := int64(2)

	// add 2 items
	values := []string{"a"}
	for _, v := range values {
		err := AddToQueue(ctx, rdb, delayQueueName, v, sleepSecond, maxTTL)
		if err != nil {
			t.Fatal(err)
		}
	}

	// wait till expire
	select {
	case <-time.After(time.Second * time.Duration(sleepSecond+maxTTL+1)):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// try consume
	resCh, errCh := GetFromQueue(ctx, rdb, delayQueueName)
	select {
	case <-resCh:
		t.Fatal(fmt.Errorf("data should expired"))
	case <-time.After(time.Second):
		t.Log("check success")
	}

	// cancel ctx
	cancel()

	// check error
	for err := range errCh {
		assert.Error(t, err, context.Canceled)
	}
}

// Use: go test -bench=. -run=none
func BenchmarkAddToQueue(b *testing.B) {
	rdb := getRdb()
	ctx := context.Background()
	delayQueueName := "delay_queue"
	for i := 0; i < b.N; i++ {
		err := AddToQueue(ctx, rdb, delayQueueName, strconv.Itoa(i), -1, 100)
		if err != nil {
			b.FailNow()
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	b.ResetTimer()
	var res int32
	// equals to runtime.GOMAXPROCS(0)
	b.RunParallel(func(pb *testing.PB) {
		resCh, errCh := GetFromQueue(ctx, rdb, delayQueueName)
		for pb.Next() {
			select {
			case <-resCh:
				atomic.AddInt32(&res, 1)
			}
		}
		cancel()
		for err := range errCh {
			if err != context.Canceled && err != nil {
				b.FailNow()
			}
		}
	})
	assert.Equal(b, b.N, int(res))
}
