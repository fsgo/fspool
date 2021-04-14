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
	"time"
)

// Element pool element
type Element interface {
	// PEActive 判断元素是否有效
	PEActive() error

	// PEMarkUsing 标记元素在使用
	PEMarkUsing()

	// PEMarkIdle 标记当前处于空闲状态
	PEMarkIdle()

	// PERawClose 元素最元素的 close
	PERawClose() error

	PEMeta() Meta

	// Close 当前元素放回 pool 或者 销毁
	Close() error
}

// PEActiver 是否有效
type PEActiver interface {
	PEActive() error
}

// PEReseter reset it
type PEReseter interface {
	PEReset()
}

// NewElementFunc new element func
type NewElementFunc func(context.Context, NewElementNeed) (Element, error)

// NewSimplePool new pool
func NewSimplePool(option *Option, newFunc NewElementFunc) SimplePool {
	if option == nil {
		option = &Option{}
	}

	p := &simplePool{
		option:          *option,
		newFunc:         newFunc,
		idles:           make([]Element, 0, option.MaxIdle),
		elementRequests: make(map[uint64]chan elementRequest),
	}
	return p
}

// SimplePool 一个简单的，通用的连接池
type SimplePool interface {
	Get(ctx context.Context) (el Element, err error)
	Option() Option
	Stats() Stats
	Range(func(el Element) error) error
	Close() error
}

var _ SimplePool = (*simplePool)(nil)

// simplePool common pool from database.sql
type simplePool struct {
	option Option

	newFunc NewElementFunc

	mu sync.Mutex

	numOpen int // 已打开的

	nextRequest uint64 // Next key to use in elementRequests.

	elementRequests map[uint64]chan elementRequest

	idles  []Element
	closed bool

	cleanerCh chan struct{}

	// Atomic access only. At top of struct to prevent mis-alignment
	// on 32-bit platforms. Of type time.Duration.
	waitDuration int64 // Total time waited for new elements.

	waitCount         int64 // Total number of elements waited for.
	maxIdleClosed     int64 // Total number of elements closed due to idle count.
	maxIdleTimeClosed int64 // Total number of elements closed due to idle time.
	maxLifetimeClosed int64 // Total number of elements closed due to max element lifetime
}

// Option get pool option
func (p *simplePool) Option() Option {
	return p.option
}

// Get get one from pool; from idle or create new
func (p *simplePool) Get(ctx context.Context) (el Element, err error) {
	for i := 0; i < 2; i++ {
		el, err = p.selectOne(ctx)
		if err != ErrBadValue {
			break
		}
	}
	if el != nil {
		el.PEMarkUsing()
	}
	return el, err
}

func (p *simplePool) countClosed(err error) {
	switch err {
	case ErrOutOfMaxLife:
		p.maxIdleTimeClosed++
	case ErrOutOfMaxIdle:
		p.maxIdleClosed++
	case ErrOutOfMaxIdleTime:
		p.maxIdleTimeClosed++
	default:
	}
	p.numOpen--
}

// selectOne 获取一个缓存的或者新创建一个
func (p *simplePool) selectOne(ctx context.Context) (el Element, err error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrClosed
	}

	// Check if the context is expired.
	select {
	default:
	case <-ctx.Done():
		p.mu.Unlock()
		return nil, ctx.Err()
	}

	// try get from idle
	numFree := len(p.idles)
	if numFree > 0 {
		el = p.idles[0]
		copy(p.idles, p.idles[1:])
		p.idles = p.idles[:numFree-1]
		if ea := el.PEActive(); ea != nil {
			p.countClosed(ea)
			p.mu.Unlock()
			el.PERawClose()
			return nil, ErrBadValue
		}
		p.mu.Unlock()
		return el, nil
	}

	// Out of free elements or we were asked not to use one.
	// If we're not allowed to create any more elements, make a request and wait.
	if p.option.MaxOpen > 0 && p.numOpen >= p.option.MaxOpen {
		// Make the elementRequest channel. It's buffered so that the
		// elementOpener doesn't block while waiting for the req to be read.
		req := make(chan elementRequest, 1)
		reqKey := p.nextRequestKeyLocked()
		p.elementRequests[reqKey] = req
		p.waitCount++
		p.mu.Unlock()

		waitStart := nowFunc()

		// Timeout the element request with the context.
		select {
		case <-ctx.Done():
			// Remove the element request and ensure no value has been sent
			// on it after removing.
			p.mu.Lock()
			delete(p.elementRequests, reqKey)
			p.mu.Unlock()

			atomic.AddInt64(&p.waitDuration, int64(time.Since(waitStart)))

			select {
			default:
			case ret, ok := <-req:
				if ok && ret.el != nil {
					p.putElement(ret.el, ret.err)
				}
			}
			return nil, ctx.Err()
		case ret, ok := <-req:
			atomic.AddInt64(&p.waitDuration, int64(time.Since(waitStart)))

			if !ok {
				return nil, ErrClosed
			}
			if ret.err == nil {
				if ea := ret.el.PEActive(); ea != nil {
					p.mu.Lock()
					p.countClosed(ea)
					p.mu.Unlock()
					ret.el.PERawClose()
					return nil, ErrBadValue
				}
			}
			return ret.el, ret.err
		}
	}

	// other case
	// p.option.MaxOpen==0 no limit maxOpen

	p.numOpen++ // optimistically
	p.mu.Unlock()

	el, err = p.newElement(ctx)

	if err != nil {
		p.mu.Lock()
		p.numOpen-- // correct for earlier optimism
		p.mu.Unlock()
		return nil, err
	}
	return el, nil
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (p *simplePool) nextRequestKeyLocked() uint64 {
	next := p.nextRequest
	p.nextRequest++
	return next
}

// Put put to pool
func (p *simplePool) Put(el interface{}) error {
	if el == nil {
		p.putElement(nil, ErrBadValue)
	}
	// if type invalid, then panic
	p.putElement(el.(Element), nil)
	return nil
}

// putElement adds a connection to the  free simplePool.
// err is optionally the last error that occurred on this element.
func (p *simplePool) putElement(dc Element, err error) {
	if dc == nil {
		p.mu.Lock()
		p.countClosed(err)
		p.mu.Unlock()
		return
	}

	if item, ok := dc.(PEReseter); ok {
		item.PEReset()
	}

	dc.PEMarkIdle()

	if err != nil {
		dc.PERawClose()
		p.mu.Lock()
		p.countClosed(err)
		p.mu.Unlock()
		return
	}

	// p.option.MaxIdle < 1
	// means not allow idle element
	if p.option.MaxIdle < 1 {
		dc.PERawClose()
		p.mu.Lock()
		p.countClosed(ErrOutOfMaxIdle)
		p.mu.Unlock()
		return
	}

	if ea := dc.PEActive(); ea != nil {
		dc.PERawClose()
		p.mu.Lock()
		p.countClosed(ea)
		p.mu.Unlock()
		return
	}

	p.mu.Lock()
	added := p.putElementIdleLocked(dc)
	if !added {
		p.countClosed(ErrOutOfMaxIdle)
	}
	p.mu.Unlock()

	if !added {
		dc.PERawClose()
		return
	}
}

type elementRequest struct {
	el  Element
	err error
}

func (p *simplePool) newElement(ctx context.Context) (el Element, err error) {
	el, err = p.newFunc(ctx, p)
	return el, err
}

func (p *simplePool) putElementIdleLocked(dc Element) bool {
	if dc == nil {
		panic("putElementIdleLocked with nil value")
	}

	if p.closed {
		return false
	}

	if p.option.MaxOpen > 0 && p.numOpen > p.option.MaxOpen {
		return false
	}

	if c := len(p.elementRequests); c > 0 {
		var req chan elementRequest
		var reqKey uint64
		for reqKey, req = range p.elementRequests {
			break
		}
		delete(p.elementRequests, reqKey) // Remove from pending requests.

		req <- elementRequest{
			el:  dc,
			err: nil,
		}
		return true
	} else if !p.closed {
		if p.maxIdleElementsLocked() > len(p.idles) {
			p.idles = append(p.idles, dc)
			p.startCleanerLocked()
			return true
		}
	}
	return false
}

// startCleanerLocked starts elementCleaner if needed.
func (p *simplePool) startCleanerLocked() {
	if (p.option.MaxLifeTime > 0 || p.option.MaxIdleTime > 0) && p.numOpen > 0 && p.cleanerCh == nil {
		p.cleanerCh = make(chan struct{}, 1)
		// 一个 pool 只会启动一个 gor
		go p.elementCleaner(p.option.shortestIdleTime())
	}
}

func (p *simplePool) elementCleaner(d time.Duration) {
	const minInterval = time.Second

	if d < minInterval {
		d = minInterval
	}
	t := time.NewTimer(d)

	for {
		select {
		case <-t.C:
		case <-p.cleanerCh:
		}

		p.mu.Lock()

		d = p.option.shortestIdleTime()
		if p.closed || p.numOpen == 0 || d <= 0 {
			p.cleanerCh = nil
			p.mu.Unlock()
			return
		}

		closing := p.elementCleanerRunLocked()
		p.mu.Unlock()
		for _, c := range closing {
			c.PERawClose()
		}

		if d < minInterval {
			d = minInterval
		}
		t.Reset(d)
	}
}

func (p *simplePool) elementCleanerRunLocked() (closing []Element) {
	if p.option.MaxLifeTime > 0 || p.option.MaxIdleTime > 0 {
		for i := 0; i < len(p.idles); i++ {
			c := p.idles[i]
			if ea := c.PEActive(); ea != nil {
				p.countClosed(ea)

				closing = append(closing, c)
				last := len(p.idles) - 1
				p.idles[i] = p.idles[last]
				p.idles[last] = nil
				p.idles = p.idles[:last]
				i--
			}
		}
	}
	return closing
}

func (p *simplePool) maxIdleElementsLocked() int {
	n := p.option.MaxIdle
	switch {
	case n < 0:
		return 0
	default:
		return n
	}
}

// Stats get pool stats
func (p *simplePool) Stats() Stats {
	wait := atomic.LoadInt64(&p.waitDuration)

	p.mu.Lock()
	defer p.mu.Unlock()

	stats := Stats{
		Open: !p.closed,

		Idle:    len(p.idles),
		NumOpen: p.numOpen,
		InUse:   p.numOpen - len(p.idles),

		WaitCount:         p.waitCount,
		WaitDuration:      time.Duration(wait),
		MaxIdleClosed:     p.maxIdleClosed,
		MaxIdleTimeClosed: p.maxIdleTimeClosed,
		MaxLifeTimeClosed: p.maxLifetimeClosed,
	}
	return stats
}

// Close close the pool
func (p *simplePool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	if p.cleanerCh != nil {
		close(p.cleanerCh)
	}
	var err error
	fns := make([]func() error, 0, len(p.idles))
	for _, dc := range p.idles {
		fns = append(fns, dc.Close)
	}
	p.idles = nil
	p.closed = true
	for _, req := range p.elementRequests {
		close(req)
	}
	p.mu.Unlock()
	for _, fn := range fns {
		err1 := fn()
		if err1 != nil {
			err = err1
		}
	}
	return err
}

func (p *simplePool) Range(fn func(el Element) error) (err error) {
	p.mu.Lock()
	for _, el := range p.idles {
		if err = fn(el); err != nil {
			break
		}
	}
	p.mu.Unlock()
	return err
}
