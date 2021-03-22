/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/20
 */

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fsgo/fspool"
)

type testIDer interface {
	ID() int32
}

type testEL struct {
	id int32
	p  *fspool.SimplePool
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

var _ fspool.Element = (*testEL)(nil)

func main() {
	var id int32
	opt := &fspool.Option{
		MaxOpen: 2,
		MaxIdle: 2,
	}
	newFunc := func(ctx context.Context, p *fspool.SimplePool) (fspool.Element, error) {
		v := atomic.AddInt32(&id, 1)
		fmt.Println("create new id=", v)
		return &testEL{id: v, p: p}, nil
	}
	p := fspool.NewSimple(opt, newFunc)

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := p.Get(ctx)
			if err != nil {
				fmt.Println("err=", err)
			}
			fmt.Println("id=", v.(testIDer).ID())
			v.Close()
		}()
	}
	wg.Wait()
}
