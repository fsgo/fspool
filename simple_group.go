/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// GroupNewElementFunc 给 Group 创建新的 pool
type GroupNewElementFunc func(key interface{}) NewElementFunc

// NewSimplePoolGroup 创建新的 Group
func NewSimplePoolGroup(opt *Option, gn GroupNewElementFunc) SimplePoolGroup {
	if opt == nil {
		opt = &Option{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &simpleGroup{
		option:    *opt.Clone(),
		done:      cancel,
		genNewEle: gn,
	}
	go g.poolCleaner(ctx, opt.shortestIdleTime())
	return g
}

// SimplePoolGroup 通用的、 按照 key 分组的 Pool
type SimplePoolGroup interface {
	Get(ctx context.Context, key interface{}) (Element, error)
	GroupStats() GroupStats
	Close() error
	Option() Option
}

var _ SimplePoolGroup = (*simpleGroup)(nil)

// simpleGroup group pool
type simpleGroup struct {
	option    Option
	genNewEle GroupNewElementFunc
	pools     map[interface{}]*groupPoolItem
	mu        sync.Mutex
	done      context.CancelFunc
	closed    bool
}

func (g *simpleGroup) Option() Option {
	return g.option
}

// Get ...
func (g *simpleGroup) Get(ctx context.Context, key interface{}) (Element, error) {
	return g.getPool(key).Get(ctx)
}

func (g *simpleGroup) getPool(key interface{}) *groupPoolItem {
	poolID := getPoolID(key)
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.pools == nil {
		g.pools = make(map[interface{}]*groupPoolItem)
	}
	p, has := g.pools[poolID]
	if !has {
		fn := g.genNewEle(key)
		pool := NewSimple(&g.option, fn)
		p = newGroupPoolItem(pool)
		g.pools[poolID] = p
	}
	p.PEMarkUsing()
	return p
}

// GroupStats Group 的状态信息
func (g *simpleGroup) GroupStats() GroupStats {
	g.mu.Lock()
	defer g.mu.Unlock()

	gs := GroupStats{
		Groups: make(map[interface{}]Stats, len(g.pools)),
		All: Stats{
			Open: !g.closed,
		},
	}

	if g.pools == nil {
		return gs
	}

	for key, p := range g.pools {
		ls := p.Stats()

		gs.Groups[key] = ls

		gs.All.Idle += ls.Idle
		gs.All.NumOpen += ls.NumOpen
		gs.All.InUse += ls.InUse
		gs.All.WaitCount += ls.WaitCount
		gs.All.WaitDuration += ls.WaitDuration
		gs.All.MaxIdleClosed += ls.MaxIdleClosed
		gs.All.MaxIdleTimeClosed += ls.MaxIdleTimeClosed
		gs.All.MaxLifeTimeClosed += ls.MaxLifeTimeClosed
	}
	return gs
}

// Close close pools
func (g *simpleGroup) Close() error {
	g.done()

	var err error
	g.mu.Lock()
	g.closed = true

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

func (g *simpleGroup) poolCleaner(ctx context.Context, d time.Duration) {
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

func (g *simpleGroup) doCheckExpire() {
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
	*MetaInfo
	SimplePool
}

func newGroupPoolItem(p SimplePool) *groupPoolItem {
	return &groupPoolItem{
		MetaInfo:   NewMetaInfo(),
		SimplePool: p,
	}
}

func getPoolID(key interface{}) interface{} {
	if v, ok := key.(interface{ PoolID() interface{} }); ok {
		return v.PoolID()
	}

	if v, ok := key.(fmt.Stringer); ok {
		return v.String()
	}

	return key
}
