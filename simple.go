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
	Active() bool
	RawClose() error
	Close() error
}

// NewElementFunc new element func
type NewElementFunc func(context.Context, *SimplePool) (Element, error)

// NewSimple new pool
func NewSimple(option *Option, newFunc NewElementFunc) *SimplePool {
	if option == nil {
		option = &Option{}
	}
	p := &SimplePool{
		option:  *option,
		newFunc: newFunc,
	}
	p.init()
	return p
}

// SimplePool common pool from database.sql
type SimplePool struct {
	option Option

	newFunc NewElementFunc

	mu sync.Mutex

	numOpen int // 已打开的

	openerCh    chan struct{}
	nextRequest uint64 // Next key to use in connRequests.

	connRequests map[uint64]chan elementRequest

	idles  []Element
	closed bool

	done      <-chan struct{}
	cleanerCh chan struct{}

	// Atomic access only. At top of struct to prevent mis-alignment
	// on 32-bit platforms. Of type time.Duration.
	waitDuration int64 // Total time waited for new connections.

	waitCount         int64 // Total number of connections waited for.
	maxIdleClosed     int64 // Total number of connections closed due to idle count.
	maxIdleTimeClosed int64 // Total number of connections closed due to idle time.
	maxLifetimeClosed int64 // Total number of connections closed due to max connection lifeti

	stop func() // stop cancels the connection opener.
}

func (p *SimplePool) init() {
	p.idles = make([]Element, 0, p.option.MaxIdle)
	p.connRequests = make(map[uint64]chan elementRequest)
}

// Option get pool option
func (p *SimplePool) Option() Option {
	return p.option
}

// Put put to pool
func (p *SimplePool) Put(el Element) error {
	p.putConn(el, nil)
	return nil
}

// Get get one from pool; from idle or create new
func (p *SimplePool) Get(ctx context.Context) (el Element, err error) {
	for i := 0; i < 2; i++ {
		el, err = p.selectOne(ctx)
		if err != ErrBadValue {
			break
		}
	}
	return el, err
}

// selectOne 获取一个缓存的或者新创建一个
func (p *SimplePool) selectOne(ctx context.Context) (el Element, err error) {
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

	numFree := len(p.idles)
	if numFree > 0 {
		el = p.idles[0]
		copy(p.idles, p.idles[1:])
		p.idles = p.idles[:numFree-1]
		if !el.Active() {
			p.maxLifetimeClosed++
			p.mu.Unlock()
			el.Close()
			return nil, ErrBadValue
		}
		p.mu.Unlock()
		return el, nil
	}

	// Out of free elements or we were asked not to use one.
	// If we're not allowed to create any more elements, make a request and wait.
	if p.option.MaxOpen > 0 && p.numOpen >= p.option.MaxOpen {
		// Make the connRequest channel. It's buffered so that the
		// connectionOpener doesn't block while waiting for the req to be read.
		req := make(chan elementRequest, 1)
		reqKey := p.nextRequestKeyLocked()
		p.connRequests[reqKey] = req
		p.waitCount++
		p.mu.Unlock()

		waitStart := nowFunc()

		// Timeout the connection request with the context.
		select {
		case <-ctx.Done():
			// Remove the connection request and ensure no value has been sent
			// on it after removing.
			p.mu.Lock()
			delete(p.connRequests, reqKey)
			p.mu.Unlock()

			atomic.AddInt64(&p.waitDuration, int64(time.Since(waitStart)))

			select {
			default:
			case ret, ok := <-req:
				if ok && ret.el != nil {
					p.putConn(ret.el, ret.err)
				}
			}
			return nil, ctx.Err()
		case ret, ok := <-req:
			atomic.AddInt64(&p.waitDuration, int64(time.Since(waitStart)))

			if !ok {
				return nil, ErrClosed
			}
			// Only check if the connection is expired if the strategy is cachedOrNewConns.
			// If we require a new connection, just re-use the connection without looking
			// at the expiry time. If it is expired, it will be checked when it is placed
			// back into the connection SimplePool.
			// This prioritizes giving a valid connection to a client over the exact connection
			// lifetime, which could expire exactly after this point anyway.
			if ret.err == nil && !ret.el.Active() {
				p.mu.Lock()
				p.maxLifetimeClosed++
				p.mu.Unlock()
				ret.el.Close()
				return nil, ErrBadValue
			}
			if ret.el == nil {
				return nil, ret.err
			}
			return ret.el, ret.err
		}
	}

	p.numOpen++ // optimistically
	p.mu.Unlock()

	el, err = p.newElement(ctx)

	if err != nil {
		p.mu.Lock()
		p.numOpen-- // correct for earlier optimism
		p.maybeOpenNewConnections()
		p.mu.Unlock()
		return nil, err
	}
	return el, err
}

// nextRequestKeyLocked returns the next connection request key.
// It is assumed that nextRequest will not overflow.
func (p *SimplePool) nextRequestKeyLocked() uint64 {
	next := p.nextRequest
	p.nextRequest++
	return next
}

// Assumes db.mu is locked.
// If there are connRequests and the connection limit hasn't been reached,
// then tell the connectionOpener to open new connections.
func (p *SimplePool) maybeOpenNewConnections() {
	numRequests := len(p.connRequests)
	if p.option.MaxOpen > 0 {
		numCanOpen := p.option.MaxOpen - p.numOpen
		if numRequests > numCanOpen {
			numRequests = numCanOpen
		}
	}
	for numRequests > 0 {
		p.numOpen++ // optimistically
		numRequests--
		if p.closed {
			return
		}
		p.openerCh <- struct{}{}
	}
}

// putConn adds a connection to the db's free SimplePool.
// err is optionally the last error that occurred on this connection.
func (p *SimplePool) putConn(dc Element, err error) {
	if p.option.MaxIdle < 1 {
		dc.RawClose()
		return
	}
	p.mu.Lock()

	if err != ErrBadValue && !dc.Active() {
		p.maxLifetimeClosed++
		err = ErrBadValue
	}

	if err == ErrBadValue {
		// Don't reuse bad connections.
		// Since the conn is considered bad and is being discarded, treat it
		// as closed. Don't decrement the open count here, finalClose will
		// take care of that.
		p.maybeOpenNewConnections()
		p.mu.Unlock()
		dc.RawClose()
		return
	}
	added := p.putConnDBLocked(dc, nil)
	p.mu.Unlock()

	if !added {
		dc.RawClose()
		return
	}
}

type elementRequest struct {
	el  Element
	err error
}

func (p *SimplePool) newElement(ctx context.Context) (el Element, err error) {
	el, err = p.newFunc(ctx, p)
	return el, err
}

// Satisfy a connRequest or put the driverConn in the idle SimplePool and return true
// or return false.
// putConnDBLocked will satisfy a connRequest if there is one, or it will
// return the *driverConn to the freeConn list if err == nil and the idle
// connection limit will not be exceeded.
// If err != nil, the value of dc is ignored.
// If err == nil, then dc must not equal nil.
// If a connRequest was fulfilled or the *driverConn was placed in the
// freeConn list, then true is returned, otherwise false is returned.
func (p *SimplePool) putConnDBLocked(dc Element, err error) bool {
	if p.closed {
		return false
	}
	if p.option.MaxOpen > 0 && p.numOpen > p.option.MaxOpen {
		return false
	}
	if c := len(p.connRequests); c > 0 {
		var req chan elementRequest
		var reqKey uint64
		for reqKey, req = range p.connRequests {
			break
		}
		delete(p.connRequests, reqKey) // Remove from pending requests.

		req <- elementRequest{
			el:  dc,
			err: err,
		}
		return true
	} else if err == nil && !p.closed {
		if p.maxIdleConnsLocked() > len(p.idles) {
			p.idles = append(p.idles, dc)
			p.startCleanerLocked()
			return true
		}
		p.maxIdleClosed++
	}
	return false
}

// startCleanerLocked starts connectionCleaner if needed.
func (p *SimplePool) startCleanerLocked() {
	if (p.option.MaxLifetime > 0 || p.option.MaxIdleTime > 0) && p.numOpen > 0 && p.cleanerCh == nil {
		p.cleanerCh = make(chan struct{}, 1)
		go p.connectionCleaner(p.option.shortestIdleTime())
	}
}

func (p *SimplePool) connectionCleaner(d time.Duration) {
	const minInterval = time.Second

	if d < minInterval {
		d = minInterval
	}
	t := time.NewTimer(d)

	for {
		select {
		case <-t.C:
		case <-p.cleanerCh: // MaxLifetime was changed or db was closed.
		}

		p.mu.Lock()

		d = p.option.shortestIdleTime()
		if p.closed || p.numOpen == 0 || d <= 0 {
			p.cleanerCh = nil
			p.mu.Unlock()
			return
		}

		closing := p.connectionCleanerRunLocked()
		p.mu.Unlock()
		for _, c := range closing {
			c.Close()
		}

		if d < minInterval {
			d = minInterval
		}
		t.Reset(d)
	}
}

func (p *SimplePool) connectionCleanerRunLocked() (closing []Element) {
	if p.option.MaxLifetime > 0 {
		for i := 0; i < len(p.idles); i++ {
			c := p.idles[i]
			if !c.Active() {
				closing = append(closing, c)
				last := len(p.idles) - 1
				p.idles[i] = p.idles[last]
				p.idles[last] = nil
				p.idles = p.idles[:last]
				i--
			}
		}
		p.maxLifetimeClosed += int64(len(closing))
	}

	if p.option.MaxIdleTime > 0 {
		var expiredCount int64
		for i := 0; i < len(p.idles); i++ {
			c := p.idles[i]
			if !c.Active() {
				closing = append(closing, c)
				expiredCount++
				last := len(p.idles) - 1
				p.idles[i] = p.idles[last]
				p.idles[last] = nil
				p.idles = p.idles[:last]
				i--
			}
		}
		p.maxIdleTimeClosed += expiredCount
	}
	return
}

const defaultMaxIdleConns = 2

func (p *SimplePool) maxIdleConnsLocked() int {
	n := p.option.MaxIdle
	switch {
	case n == 0:
		return defaultMaxIdleConns
	case n < 0:
		return 0
	default:
		return n
	}
}

// Stats get pool stats
func (p *SimplePool) Stats() Stats {
	wait := atomic.LoadInt64(&p.waitDuration)

	p.mu.Lock()
	defer p.mu.Unlock()

	stats := Stats{
		MaxOpen: p.option.MaxOpen,
		Idle:    len(p.idles),
		NumOpen: p.numOpen,
		InUse:   p.numOpen - len(p.idles),

		WaitCount:         p.waitCount,
		WaitDuration:      time.Duration(wait),
		MaxIdleClosed:     p.maxIdleClosed,
		MaxIdleTimeClosed: p.maxIdleTimeClosed,
		MaxLifetimeClosed: p.maxLifetimeClosed,
	}
	return stats
}

// Close close the pool
func (p *SimplePool) Close() error {
	p.mu.Lock()
	if p.closed { // Make DB.Close idempotent
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
	for _, req := range p.connRequests {
		close(req)
	}
	p.mu.Unlock()
	for _, fn := range fns {
		err1 := fn()
		if err1 != nil {
			err = err1
		}
	}
	p.stop()
	return err
}
