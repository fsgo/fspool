/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testEL struct {
	*MetaInfo
	id      int32
	p       NewElementNeed
	val     int64
	name    string
	mu      sync.Mutex
	lastErr error
}

func (t *testEL) Name() string {
	return t.name
}

func (t *testEL) ID() int32 {
	return t.id
}

var testModNum = 99

func (t *testEL) NextValue() int64 {
	val := atomic.AddInt64(&t.val, 1)
	if val%int64(testModNum) == int64(testModNum-1) {
		t.mu.Lock()
		t.lastErr = fmt.Errorf("val=99 must error")
		t.mu.Unlock()
	}
	return val
}

func (t *testEL) PEActive() error {
	t.mu.Lock()
	err := t.lastErr
	t.mu.Unlock()

	if err != nil {
		return err
	}
	return t.MetaInfo.Active(t.p.Option())
}

func (t *testEL) Close() error {
	return t.p.Put(t)
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
			for i := 1; i < 1000; i++ {
				t.Run(fmt.Sprintf("for_%d", i), func(t *testing.T) {
					val, err := p.Get(context.Background())
					if err != nil {
						t.Fatalf("has error: %v", err)
					}
					defer val.Close()

					v := val.(*testEL)
					got := v.ID()
					want := getWant(i)
					if got != want {
						t.Fatalf("got=%v want=%v", got, want)
					}
				})
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
		p := NewSimplePool(nil, newFunc)

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
		p := NewSimplePool(opt, newFunc)

		testForEach(t, p, func(i int) int32 {
			return 1
		})
	})

	t.Run("case 3-MaxOpen_1", func(t *testing.T) {
		opt := &Option{
			MaxIdle: 1,
			MaxOpen: 1,
		}
		p := NewSimplePool(opt, newFunc)

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

	t.Run("case 4-Parallel", func(t *testing.T) {
		resetID()
		defer resetID()
		opt := &Option{
			MaxOpen: 3,
			MaxIdle: 3,
		}
		p := NewSimplePool(opt, newFunc)

		var wg sync.WaitGroup

		var getErrTotal int32

		imax := 200
		zmax := 1000
		mmax := 10

		wantN := imax * zmax * mmax / (testModNum + 1)

		var mu sync.Mutex
		var idMax int32

		for i := 0; i < imax; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for z := 0; z < zmax; z++ {
					item, err := p.Get(context.Background())
					if err != nil {
						atomic.AddInt32(&getErrTotal, 1)
						continue
					}
					val := item.(*testEL)

					mu.Lock()
					id := val.ID()
					if id > idMax {
						idMax = id
					}
					mu.Unlock()

					for m := 0; m < mmax; m++ {
						val.NextValue()
					}
					item.Close()
				}
			}()
		}
		wg.Wait()

		if getErrTotal > 0 {
			t.Fatalf("getErrTotal=%d want 0", getErrTotal)
		}

		if int(idMax) != wantN {
			t.Fatalf("idMax=%d want = %d", idMax, wantN)
		}
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
	p := NewSimplePool(nil, func(ctx context.Context, pool NewElementNeed) (Element, error) {
		return &testEL{id: 100, p: pool, MetaInfo: NewMetaInfo()}, nil
	})

	item, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf(err.Error())
	}
	item.Close()

	err = p.Range(func(el io.Closer) error {
		m := ReadMeta(el)
		t.Logf("meta=%s", m.String())
		return nil
	})
	if err != nil {
		t.Fatalf(err.Error())
	}

	for i := 0; i < 2; i++ {
		testSimplePoolClose(t, p)
	}
}

func TestNewSimpleElement(t *testing.T) {
	type userInfo struct{}
	opt := &Option{
		MaxIdle: 0,
	}
	var resetTotal int32
	p := NewSimplePool(opt, func(ctx context.Context, need NewElementNeed) (Element, error) {
		return NewSimpleElement(&SimpleRawItem{
			Raw: &userInfo{},
			Reset: func() {
				atomic.AddInt32(&resetTotal, 1)
			},
		}), nil
	})

	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("id=%d", i), func(t *testing.T) {
			item, err := p.Get(context.Background())
			if err != nil {
				t.Fatalf(err.Error())
			}
			val := item.(SimpleElement)
			defer val.Close()
			user := val.Raw().(*userInfo)
			_ = user

			meta := ReadMeta(val)
			wantID := uint64(i) + 1
			gotID := meta.ID
			if gotID != wantID {
				t.Fatalf("gotID=%d wantID=%d", gotID, wantID)
			}

			resetWant := int32(i)
			if resetWant != resetTotal {
				t.Fatalf("resetTotal=%d want=%d", resetTotal, resetWant)
			}
		})
	}

}
