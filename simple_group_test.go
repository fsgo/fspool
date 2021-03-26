/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/23
 */

package fspool

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewSimpleGroup(t *testing.T) {
	var id int32

	resetID := func() {
		atomic.StoreInt32(&id, 0)
	}
	newFunc := func(key interface{}) NewElementFunc {
		return func(ctx context.Context, pool *SimplePool) (Element, error) {
			v := atomic.AddInt32(&id, 1)
			item := &testEL{
				id:           v,
				p:            pool,
				WithTimeInfo: NewWithTimeInfo(),
				name:         fmt.Sprint(key),
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
				if gorn != got {
					time.Sleep(time.Second)
				} else {
					return
				}
			}
			t.Fatalf("runtime.NumGoroutine()=%d want=%d", got, gorn)
		}()

		defer resetID()

		key := "abc"
		pg := NewSimpleGroup(nil, newFunc)
		for i := 0; i < 100; i++ {
			t.Run(fmt.Sprintf("for_%d", i), func(t *testing.T) {
				v, err := pg.Get(context.Background(), key)
				if err != nil {
					t.Fatalf("unexpect error: %v", err)
				}
				el := v.(*testEL)
				defer el.Close()

				if got := el.Name(); got != key {
					t.Fatalf("el.Name()=%v want=%v", got, key)
				}

				wantID := int32(i) + 1
				if got := el.ID(); got != wantID {
					t.Fatalf("el.ID()=%v want=%v", got, wantID)
				}
			})

			t.Run("Stats", func(t *testing.T) {
				gs := pg.GroupStats()
				got := gs.All
				want := Stats{
					NumOpen: i + 1,
					InUse:   i + 1,
				}

				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got=%v want=%v", got, want)
				}
			})

			t.Run("pool_size", func(t *testing.T) {
				want := 1
				if got := len(pg.pools); got != want {
					t.Fatalf("len(pg.pools)=%d want=%d", got, want)
				}
			})

			pg.doCheckExpire()
		}

		t.Run("close", func(t *testing.T) {
			if err := pg.Close(); err != nil {
				t.Fatalf("pg.Close() unexpect error+%v", err)
			}
		})
	})
}
