// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/3/23

package fspool

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsgo/fst"
)

func TestNewSimpleGroup(t *testing.T) {
	var id int32

	resetID := func() {
		atomic.StoreInt32(&id, 0)
	}
	newFunc := func(key any) NewElementFunc {
		return func(ctx context.Context, pool PoolPutter) (Element, error) {
			v := atomic.AddInt32(&id, 1)
			item := &testEL{
				id:       v,
				p:        pool,
				MetaInfo: NewMetaInfo(),
				name:     fmt.Sprint(key),
			}
			return item, nil
		}
	}

	t.Run("default", func(t *testing.T) {
		gorn := runtime.NumGoroutine()
		defer func() {
			var got int
			for i := 0; i < 100; i++ {
				got = runtime.NumGoroutine()
				if got > gorn {
					t.Logf("Goroutine()=%d want=%d, wait...", got, gorn)
					time.Sleep(time.Second)
				} else {
					return
				}
			}
			t.Fatalf("runtime.NumGoroutine()=%d want=%d", got, gorn)
		}()

		defer resetID()

		key := "abc"
		pg := NewSimplePoolGroup(nil, newFunc)
		for i := 0; i < 100; i++ {
			t.Run(fmt.Sprintf("for_%d", i), func(t *testing.T) {
				v, err := pg.Get(context.Background(), key)
				fst.NoError(t, err)

				el := v.(*testEL)
				defer el.Close()

				fst.Equal(t, key, el.Name())
				wantID := int32(i) + 1
				fst.Equal(t, wantID, el.ID())
			})

			t.Run("Stats", func(t *testing.T) {
				gs := pg.GroupStats()
				got := gs.All
				want := Stats{
					Open:          true,
					NumOpen:       0,
					InUse:         0,
					MaxIdleClosed: int64(i) + 1,
				}

				fst.Equal(t, want, got)
			})

			t.Run("pool_size", func(t *testing.T) {
				fst.Equal(t, 1, len(pg.pools))
			})

			pg.doCheckExpire()
		}

		t.Run("close", func(t *testing.T) {
			fst.NoError(t, pg.Close())
		})
	})
}
