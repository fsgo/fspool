/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// GroupNewElementFunc 给 Group 创建新的 pool
type GroupNewElementFunc func(key interface{}) NewElementFunc

// NewSimpleGroup 创建新的 Group
func NewSimpleGroup(opt *Option, gn GroupNewElementFunc) *SimpleGroup {
	if opt == nil {
		opt = &Option{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &SimpleGroup{
		option:    *opt,
		done:      cancel,
		genNewEle: gn,
	}
	go g.poolCleaner(ctx, time.Minute)
	return g
}

// SimpleGroup group pool
type SimpleGroup struct {
	option    Option
	genNewEle GroupNewElementFunc
	pools     map[interface{}]*groupPoolItem
	mu        sync.Mutex
	done      context.CancelFunc
}

// ErrNotExists 不存在
var ErrNotExists = errors.New("not exists")

// Get ...
func (g *SimpleGroup) Get(ctx context.Context, key interface{}) (Element, error) {
	return g.getPool(key).Get(ctx)
}

func (g *SimpleGroup) getPool(key interface{}) *groupPoolItem {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.pools == nil {
		g.pools = make(map[interface{}]*groupPoolItem)
	}
	p, has := g.pools[key]
	if !has {
		fn := g.genNewEle(key)
		pool := NewSimple(&g.option, fn)
		p = newGroupPoolItem(pool)
		g.pools[key] = p
	}
	p.MarkUsed()
	return p
}

// Stats ...
func (g *SimpleGroup) Stats() Stats {
	rt := Stats{}
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.pools == nil {
		return rt
	}

	for _, p := range g.pools {
		ls := p.Stats()
		rt.MaxOpen += ls.MaxOpen
		rt.Idle += ls.Idle
		rt.NumOpen += ls.NumOpen
		rt.InUse += ls.InUse
		rt.WaitCount += ls.WaitCount
		rt.WaitDuration += ls.WaitDuration
		rt.MaxIdleClosed += ls.MaxIdleClosed
		rt.MaxIdleTimeClosed += ls.MaxIdleTimeClosed
		rt.MaxLifeTimeClosed += ls.MaxLifeTimeClosed
	}
	return rt
}

// Close close pools
func (g *SimpleGroup) Close() error {
	g.done()

	var err error
	g.mu.Lock()
	if g.pools != nil {
		for _, p := range g.pools {
			if e := p.Close(); e != nil {
				err = e
			}
		}
		g.pools = make(map[interface{}]*groupPoolItem)
	}
	g.mu.Unlock()

	return err
}

func (g *SimpleGroup) poolCleaner(ctx context.Context, d time.Duration) {
	const minInterval = time.Minute

	if d < minInterval {
		d = minInterval
	}
	t := time.NewTimer(d)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		g.doCheckExpire()
	}
}

func (g *SimpleGroup) doCheckExpire() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pools == nil {
		return
	}
	var expires []interface{}
	for k, p := range g.pools {
		if !p.IsActive(g.option) {
			expires = append(expires, k)
			p.Close()
		}
	}
	if len(expires) == 0 {
		return
	}

	for k := range expires {
		delete(g.pools, k)
	}
}

type groupPoolItem struct {
	*WithTimeInfo
	*SimplePool
}

func newGroupPoolItem(p *SimplePool) *groupPoolItem {
	return &groupPoolItem{
		WithTimeInfo: NewWithTimeInfo(),
		SimplePool:   p,
	}
}
