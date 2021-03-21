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
	}
	return t.RawClose()
}

func (t *testEL) RawClose() error {
	return nil
}

var _ Element = (*testEL)(nil)

func TestNewSimple(t *testing.T) {
	var id int32

	resetID := func() {
		atomic.StoreInt32(&id, 0)
	}

	newFunc := func(ctx context.Context, p *SimplePool) (Element, error) {
		v := atomic.AddInt32(&id, 1)
		return &testEL{id: v, p: p}, nil
	}

	testForEach := func(t *testing.T, p *SimplePool, getWant func(i int) int32) {
		defer resetID()

		t.Run("foreach", func(t *testing.T) {
			for i := 1; i < 100; i++ {
				val, err := p.Get(context.Background())
				if err != nil {
					t.Fatalf("has error: %v", err)
				}
				v := val.(*testEL)
				got := v.ID()
				want := getWant(i)
				val.Close()
				if got != want {
					t.Fatalf("got=%v want=%v", got, want)
				}
			}
		})
	}

	testForEachConc := func(t *testing.T, p *SimplePool, doWant func(want *int32, i int)) {
		defer resetID()
		var wg sync.WaitGroup
		var got int32
		var want int32

		for i := 1; i < 100; i++ {
			wg.Add(1)
			doWant(&want, i)

			go func() {
				defer wg.Done()
				val, err := p.Get(context.Background())
				if err != nil {
					return
				}
				defer val.Close()
				v := val.(*testEL)
				atomic.AddInt32(&got, v.ID())
			}()
		}
		wg.Wait()
		if got != want {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}

	t.Run("case 1-default values", func(t *testing.T) {
		p := NewSimple(nil, newFunc)

		testForEach(t, p, func(i int) int32 {
			return int32(i)
		})

		testForEachConc(t, p, func(want *int32, i int) {
			atomic.AddInt32(want, int32(i))
		})
	})

	t.Run("case 2-MaxIdle_1", func(t *testing.T) {
		opt := &Option{
			MaxIdle: 1,
		}
		p := NewSimple(opt, newFunc)

		testForEach(t, p, func(i int) int32 {
			return 1
		})
	})

	t.Run("case 3-MaxOpen_1", func(t *testing.T) {
		opt := &Option{
			MaxIdle: 1,
			MaxOpen: 1,
		}
		p := NewSimple(opt, newFunc)

		testForEach(t, p, func(i int) int32 {
			return 1
		})
	})
}
