/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

type testIDer interface {
	ID() int32
}

type testEL struct {
	id int32
	p  *SimplePool
}

func (t *testEL) ID() int32 {
	return t.id
}

func (t *testEL) Active() bool {
	return true
}

func (t *testEL) Close() error {
	if t.Active() {
		return t.p.Put(t)
	} else {
		return t.RawClose()
	}
}

func (t *testEL) RawClose() error {
	return nil
}

var _ Element = (*testEL)(nil)

func TestNewSimple(t *testing.T) {
	t.Run("case 1-default values", func(t *testing.T) {
		var id int32
		resetID := func() {
			atomic.StoreInt32(&id, 0)
		}
		newFunc := func(ctx context.Context, p *SimplePool) (Element, error) {
			v := atomic.AddInt32(&id, 1)
			return &testEL{id: v, p: p}, nil
		}

		p := NewSimple(nil, newFunc)

		t.Run("foreach", func(t *testing.T) {
			defer resetID()
			for i := 1; i < 100; i++ {
				val, err := p.Get(context.Background())
				if err != nil {
					t.Fatalf("has error: %v", err)
				}
				v := val.(*testEL)
				got := v.ID()
				want := int32(i)
				if got != want {
					t.Fatalf("got=%v want=%v", got, want)
				}
			}
		})

		t.Run("foreach_conc", func(t *testing.T) {
			defer resetID()

			var wg sync.WaitGroup
			var got int32
			var want int32

			for i := 1; i < 100; i++ {
				wg.Add(1)
				atomic.AddInt32(&want, int32(i))

				go func() {
					defer wg.Done()
					val, err := p.Get(context.Background())
					if err != nil {
						t.Fatalf("has error: %v", err)
					}
					v := val.(*testEL)
					atomic.AddInt32(&got, v.ID())

				}()
			}
			wg.Wait()
			if got != want {
				t.Fatalf("got=%v want=%v", got, want)
			}
		})

	})
}
