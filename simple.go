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
	// PEIsActive 判断元素是否有效
	PEIsActive() bool

	// PERawClose 元素最元素的 close
	PERawClose() error

	// Close 当前元素放回 pool 或者 销毁
	Close() error
}

// PEIsActiver 是否有效
type PEIsActiver interface {
	PEIsActive() bool
}

// NewElementFunc new element func
type NewElementFunc func(context.Context, *SimplePool) (Element, error)

// NewSimple new pool
func NewSimple(option *Option, newFunc NewElementFunc) *SimplePool {
	if option == nil {
		option = &Option{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &SimplePool{
		option:          *option,
		newFunc:         newFunc,
		idles:           make([]Element, 0, option.MaxIdle),
		elementRequests: make(map[uint64]chan elementRequest),
		stop:            cancel,
	}
	go p.elementOpener(ctx)
	return p
}

// SimplePool common pool from database.sql
type SimplePool struct {
	option Option

	newFunc NewElementFunc

	mu sync.Mutex

	numOpen int // 已打开的

	openerCh    chan struct{}
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

	stop func() // stop cancels the element opener.
}

// Runs in a separate goroutine, opens new element when requested.
func (p *SimplePool) elementOpener(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.openerCh:
			p.openNewElement(ctx)
		}
	}
}

// Open one new element
func (p *SimplePool) openNewElement(ctx context.Context) {
	// openNewElement has already executed p.numOpen++ before it sent
	// on p.openerCh. This function must execute p.numOpen-- if the
	// element fails or is closed before returning.
	ci, err := p.newElement(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		if err == nil {
			ci.PERawClose()
		}
		p.numOpen--
		return
	}
	if err != nil {
		p.numOpen--
		// 创建新元素失败了，也要将错误信息发生回去，这样调用方可以知道为何失败了
		p.putElementPoolLocked(nil, err)
		p.maybeOpenNewElements()
		return
	}

	if !p.putElementPoolLocked(ci, err) {
		p.numOpen--
		ci.PERawClose()
	}
}

// Option get pool option
func (p *SimplePool) Option() Option {
	return p.option
}

// Put put to pool
func (p *SimplePool) Put(el Element) error {
	p.putElement(el, nil)
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
		if !el.PEIsActive() {
			p.maxLifetimeClosed++
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
			// Only check if the element is expired if the strategy is cachedOrNewConns.
			// If we require a new element, just re-use the element without looking
			// at the expiry time. If it is expired, it will be checked when it is placed
			// back into the element SimplePool.
			// This prioritizes giving a valid element to a client over the exact element
			// lifetime, which could expire exactly after this point anyway.
			if ret.err == nil && !ret.el.PEIsActive() {
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
		p.maybeOpenNewElements()
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

// Assumes p.mu is locked.
// If there are elementRequests and the connection limit hasn't been reached,
// then tell the elementOpener to open new elements.
func (p *SimplePool) maybeOpenNewElements() {
	numRequests := len(p.elementRequests)
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

// putElement adds a connection to the db's free SimplePool.
// err is optionally the last error that occurred on this element.
func (p *SimplePool) putElement(dc Element, err error) {
	if p.option.MaxIdle < 1 {
		dc.PERawClose()
		return
	}
	p.mu.Lock()

	if err != ErrBadValue && !dc.PEIsActive() {
		p.maxLifetimeClosed++
		err = ErrBadValue
	}

	if err == ErrBadValue {
		// Don't reuse bad Element.
		// Since the pConn is considered bad and is being discarded, treat it
		// as closed. Don't decrement the open count here, finalClose will
		// take care of that.
		p.maybeOpenNewElements()
		p.mu.Unlock()
		dc.PERawClose()
		return
	}
	added := p.putElementPoolLocked(dc, nil)
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

func (p *SimplePool) newElement(ctx context.Context) (el Element, err error) {
	el, err = p.newFunc(ctx, p)
	return el, err
}

// Satisfy a connRequest or put the driverConn in the idle SimplePool and return true
// or return false.
// putElementPoolLocked will satisfy a connRequest if there is one, or it will
// return the Element to the freeConn list if err == nil and the idle
// element limit will not be exceeded.
// If err != nil, the value of dc is ignored.
// If err == nil, then dc must not equal nil.
// If a elementRequest was fulfilled or the Element was placed in the
// idles list, then true is returned, otherwise false is returned.
func (p *SimplePool) putElementPoolLocked(dc Element, err error) bool {
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
			err: err,
		}
		return true
	} else if err == nil && !p.closed {
		if p.maxIdleElementsLocked() > len(p.idles) {
			p.idles = append(p.idles, dc)
			p.startCleanerLocked()
			return true
		}
		p.maxIdleClosed++
	}
	return false
}

// startCleanerLocked starts elementCleaner if needed.
func (p *SimplePool) startCleanerLocked() {
	if (p.option.MaxLifeTime > 0 || p.option.MaxIdleTime > 0) && p.numOpen > 0 && p.cleanerCh == nil {
		p.cleanerCh = make(chan struct{}, 1)
		// 一个 pool 只会启动一个 gor
		go p.elementCleaner(p.option.shortestIdleTime())
	}
}

func (p *SimplePool) elementCleaner(d time.Duration) {
	const minInterval = time.Second

	if d < minInterval {
		d = minInterval
	}
	t := time.NewTimer(d)

	for {
		select {
		case <-t.C:
		case <-p.cleanerCh: // MaxLifeTime was changed or db was closed.
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
			c.Close()
		}

		if d < minInterval {
			d = minInterval
		}
		t.Reset(d)
	}
}

func (p *SimplePool) elementCleanerRunLocked() (closing []Element) {
	if p.option.MaxLifeTime > 0 {
		for i := 0; i < len(p.idles); i++ {
			c := p.idles[i]
			if !c.PEIsActive() {
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
			if !c.PEIsActive() {
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

func (p *SimplePool) maxIdleElementsLocked() int {
	n := p.option.MaxIdle
	switch {
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
		MaxLifeTimeClosed: p.maxLifetimeClosed,
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
	p.stop()
	return err
}

// NewWithTimeInfo 创建一个 *WithTimeInfo
func NewWithTimeInfo() *WithTimeInfo {
	return &WithTimeInfo{
		createTime: time.Now(),
	}
}

// WithTimeInfo 包含 创建时间和使用时间
type WithTimeInfo struct {
	createTime  time.Time
	lastUseTime time.Time
	mu          sync.Mutex
}

// MarkUsed 标记已使用
func (w *WithTimeInfo) MarkUsed() {
	w.mu.Lock()
	w.lastUseTime = time.Now()
	w.mu.Unlock()
}

// IsActive 是否在有效期内
func (w *WithTimeInfo) IsActive(opt Option) bool {
	w.mu.Lock()
	lastUse := w.lastUseTime
	w.mu.Unlock()

	if opt.MaxIdleTime > 0 && time.Since(lastUse) >= opt.MaxIdleTime {
		return false
	}
	if opt.MaxLifeTime > 0 && time.Since(w.createTime) >= opt.MaxLifeTime {
		return false
	}
	return true
}
