// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/3/21

package fspool

import (
	"context"
	"fmt"
	"io"
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
	sgOpt := opt.Clone()
	sgOpt.MaxLifeTime = 0
	minIdle := 5 * time.Minute
	if sgOpt.MaxIdleTime < minIdle {
		sgOpt.MaxIdleTime = minIdle
	}
	g := &simpleGroup{
		option:    *opt.Clone(),
		sgOption:  *sgOpt,
		done:      cancel,
		genNewEle: gn,
	}
	go g.poolCleaner(ctx, opt.shortestIdleTime())
	return g
}

// SimplePoolGroup 通用的、 按照 key 分组的 Pool
//
// 配置的 Option 是针对每个 key 的。
// 如 MaxOpen=1，则允许每个 key 都最多创建1个实例
type SimplePoolGroup interface {
	Get(ctx context.Context, key interface{}) (io.Closer, error)
	GroupStats() GroupStats
	Close() error
	Option() Option
	Range(func(el io.Closer) error) error
}

var _ SimplePoolGroup = (*simpleGroup)(nil)

// simpleGroup group pool
type simpleGroup struct {
	option    Option
	sgOption  Option
	genNewEle GroupNewElementFunc
	pools     map[interface{}]*groupPoolItem
	mu        sync.Mutex
	done      context.CancelFunc
	closed    bool

	nextID uint64
}

func (sg *simpleGroup) Range(fn func(io.Closer) error) error {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	for _, pool := range sg.pools {
		if err := pool.Range(fn); err != nil {
			return err
		}
	}
	return nil
}

func (sg *simpleGroup) Option() Option {
	return sg.option
}

// Get ...
func (sg *simpleGroup) Get(ctx context.Context, key interface{}) (io.Closer, error) {
	return sg.getPool(key).Get(ctx)
}

func (sg *simpleGroup) getPool(key interface{}) *groupPoolItem {
	poolID := getPoolID(key)
	sg.mu.Lock()
	defer sg.mu.Unlock()

	if sg.pools == nil {
		sg.pools = make(map[interface{}]*groupPoolItem)
	}
	p, has := sg.pools[poolID]
	if !has {
		fn := sg.genNewEle(key)
		pool := NewSimplePool(&sg.option, fn)
		pool.(interface{ OnNewElement(fn func(el Element)) }).OnNewElement(func(el Element) {
			// 设置全局的 nextID
			trySetNextID(el, &sg.nextID)
		})

		p = newGroupPoolItem(pool)
		sg.pools[poolID] = p
	}
	p.PEMarkUsing()
	return p
}

// GroupStats Group 的状态信息
func (sg *simpleGroup) GroupStats() GroupStats {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	gs := GroupStats{
		Groups: make(map[interface{}]Stats, len(sg.pools)),
		All: Stats{
			Open: !sg.closed,
		},
	}

	if sg.pools == nil {
		return gs
	}

	for key, p := range sg.pools {
		ls := p.Stats()

		gs.Groups[key] = ls

		gs.All.Idle += ls.Idle
		gs.All.NumOpen += ls.NumOpen
		gs.All.Wait += ls.Wait
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
func (sg *simpleGroup) Close() error {
	sg.done()

	var err error
	sg.mu.Lock()
	sg.closed = true

	if sg.pools != nil {
		for _, p := range sg.pools {
			if e := p.Close(); e != nil {
				err = e
			}
		}
		sg.pools = make(map[interface{}]*groupPoolItem)
	}
	sg.mu.Unlock()

	return err
}

func (sg *simpleGroup) poolCleaner(ctx context.Context, d time.Duration) {
	const minInterval = time.Minute

	if d < minInterval {
		d = minInterval
	}

	t := time.NewTicker(d)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sg.doCheckExpire()
	}
}

func (sg *simpleGroup) doCheckExpire() {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	if sg.pools == nil {
		return
	}
	var expires []interface{}
	for k, p := range sg.pools {
		if p.Active(sg.sgOption) != nil {
			expires = append(expires, k)
			p.Close()
		}
	}
	if len(expires) == 0 {
		return
	}

	for _, k := range expires {
		delete(sg.pools, k)
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
