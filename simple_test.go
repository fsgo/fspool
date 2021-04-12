/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testEL struct {
	*MetaInfo
	id   int32
	p    NewElementNeed
	name string
}

func (t *testEL) Name() string {
	t.MetaInfo.PEMarkUsing()
	return t.name
}

func (t *testEL) ID() int32 {
	t.MetaInfo.PEMarkUsing()
	return t.id
}

func (t *testEL) PEIsActive() bool {
	return t.MetaInfo.IsActive(t.p.Option())
}

func (t *testEL) Close() error {
	if t.PEIsActive() {
		return t.p.Put(t)
	}
	return t.PERawClose()
}

func (t *testEL) PERawClose() error {
	return nil
}

var _ Element = (*testEL)(nil)

func TestNewSimple(t *testing.T) {
	var id int32

	resetID := func() {
		atomic.StoreInt32(&id, 0)
	}

	newFunc := func(ctx context.Context, p NewElementNeed) (Element, error) {
		v := atomic.AddInt32(&id, 1)
		return &testEL{id: v, p: p, MetaInfo: NewMetaInfo()}, nil
	}

	testForEach := func(t *testing.T, p SimplePool, getWant func(i int) int32) {
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

	testForEachConc := func(t *testing.T, p SimplePool, doWant func(want *int32, i int)) {
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
		t.Run("check_stats_1", func(t *testing.T) {
			got := p.Stats()
			want := Stats{
				Open:    true,
				NumOpen: 1,
				Idle:    1,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got=%v want=%v", got, want)
			}
		})

		testForEachConc(t, p, func(want *int32, i int) {
			atomic.AddInt32(want, 1)
		})

		t.Run("slow", func(t *testing.T) {
			el, errGet := p.Get(context.Background())
			if errGet != nil {
				t.Fatalf("unexpect error:%v", errGet)
			}

			// 由于 MaxOpen=1 所以不能正常的获取到 元素
			t.Run("timeout", func(t *testing.T) {
				for i := 0; i < 5; i++ {
					t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
						ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
						defer cancel()
						_, err2 := p.Get(ctx)
						want := context.DeadlineExceeded
						if err2 != want {
							t.Fatalf("err got=%v want=%v", err2, want)
						}
					})
				}
			})

			// 关闭后，元素回收，可以正常的获取
			el.Close()

			t.Run("get_suc", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()
				el2, err2 := p.Get(ctx)
				if err2 != nil {
					t.Fatalf("unexpect error=%v", err2)
				}
				defer el2.Close()
			})

		})
	})
}

func testSimplePoolClose(t *testing.T, p SimplePool) {
	t.Run("Close", func(t *testing.T) {
		if err := p.Close(); err != nil {
			t.Fatalf("Close() has error=%v", err)
		}
		el, err := p.Get(context.Background())
		if err == nil {
			t.Fatalf("expect not nil")
		}
		if el != nil {
			t.Fatalf("expect nil")
		}
	})
}

func TestSimplePool_Close(t *testing.T) {
	p := NewSimple(nil, func(ctx context.Context, pool NewElementNeed) (Element, error) {
		return &testEL{id: 100, p: pool}, nil
	})
	for i := 0; i < 2; i++ {
		testSimplePoolClose(t, p)
	}
}
