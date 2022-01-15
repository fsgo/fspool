// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/3/21

package fspool

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

type (
	// Element 对象池的元素定义
	Element interface {
		CanCheckActive

		// PERawClose 元素最元素的 close
		PERawClose() error

		// Close 当前元素放回 pool 或者 销毁
		Close() error
	}

	// CanCheckActive 支持检查对象是否有效
	CanCheckActive interface {
		PEActive() error
	}

	// CanReset 重置对象为初始化状态
	CanReset interface {
		PEReset()
	}

	// HasRaw 支持获取原始的对象
	HasRaw interface {
		PERaw() interface{}
	}

	// CanBindPool 将 pool 绑定到对象上
	CanBindPool interface {
		BindPool(p PoolPutter)
	}

	// CanSetError 设置错误
	CanSetError interface {
		SetError(err error)
	}
)

// PoolPutter 创建新 Element 时所需要的
type PoolPutter interface {
	// Put 将对象重新放回对象池,
	// 每使用 Get 方法拿到一个对象，就需要使用 Put 方法一次
	// 在实现的过程中，可能将调用 Put 方法的调用放入获取值的 Close 方法的逻辑中
	// 比如 ConnPool 的实现
	Put(interface{}) error

	// Option 连接池的配置信息
	Option() Option
}

// NewElementFunc 生成新对象的方法
type NewElementFunc func(context.Context, PoolPutter) (Element, error)

// NewSimplePool 创建一个新的对象池
func NewSimplePool(opt *Option, newFunc NewElementFunc) SimplePool {
	if opt == nil {
		opt = &Option{}
	}

	p := &simplePool{
		option:          *opt,
		newFunc:         newFunc,
		idles:           make([]Element, 0, opt.MaxIdle),
		elementRequests: make(map[uint64]chan elementRequest),
	}
	return p
}

// SimplePool 一个简单的，通用的连接池
type SimplePool interface {
	// Get 从对象池中读取到一个对象，可能是新生成的也可能是旧的
	// 不管对象在使用后，是正常还是异常，都必须调用 Close 方法，以将对象重新放回对象池
	// 否则对象池的计算会不准确
	Get(ctx context.Context) (io.Closer, error)

	// Option 对象池的配置信息
	Option() Option

	// Stats 当前对象池的状态
	Stats() Stats

	// Range 遍历所有 idle 状态的对象
	Range(func(io.Closer) error) error

	// Close 关闭对象池
	Close() error
}

var _ SimplePool = (*simplePool)(nil)

type elementRequest struct {
	el  Element
	err error
}

// simplePool common pool from database.sql
type simplePool struct {
	nextID uint64

	option Option

	newFunc NewElementFunc

	mu sync.Mutex

	numOpen int // 已打开的
	wait    int // 当前等待的数量

	nextRequest uint64 // Next key to use in elementRequests.

	// elementRequests 等待中的请求
	elementRequests map[uint64]chan elementRequest

	idles  []Element
	closed bool

	cleanerCh chan struct{}

	onNewElement func(el Element)

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

// Get 从对象池中读取到一个对象，可能是新生成的也可能是旧的，
// 不管对象在使用后，是正常还是异常，都必须调用 Close 方法，以将对象重新放回对象池，
// 否则对象池的计算会不准确
func (p *simplePool) Get(ctx context.Context) (io.Closer, error) {
	var el Element
	var err error
	for i := 0; i < 2; i++ {
		el, err = p.selectOne(ctx)
		if err != ErrBadValue {
			break
		}
	}
	if el != nil {
		if cu, ok := el.(CanMarkUsing); ok {
			cu.PEMarkUsing()
		}
	}
	return el, err
}

func (p *simplePool) countClosed(err error) {
	switch err {
	case ErrOutOfMaxLife:
		p.maxLifetimeClosed++
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
		return nil, ErrClosedPool
	}

	// Check if the context is expired.
	select {
	default:
	case <-ctx.Done():
		p.mu.Unlock()
		return nil, ctx.Err()
	}

	// try get from idle
	for len(p.idles) > 0 {
		el = p.idles[0]
		copy(p.idles, p.idles[1:])
		p.idles = p.idles[:len(p.idles)-1]
		if ea := el.PEActive(); ea != nil {
			p.countClosed(ea)
			el.PERawClose()
			continue
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
		p.wait++
		p.mu.Unlock()

		waitStart := nowFunc()

		// Timeout the element request with the context.
		select {
		case <-ctx.Done():
			// Remove the element request and ensure no value has been sent
			// on it after removing.
			p.mu.Lock()
			delete(p.elementRequests, reqKey)
			p.wait--
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
			p.mu.Lock()
			p.wait--
			p.mu.Unlock()

			if !ok {
				return nil, ErrClosedPool
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
		return nil
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

	if item, ok := dc.(CanReset); ok {
		item.PEReset()
	}

	if ci, ok := dc.(CanMarkIdle); ok {
		ci.PEMarkIdle()
	}

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

func (p *simplePool) newElement(ctx context.Context) (el Element, err error) {
	el, err = p.newFunc(ctx, p)
	if el != nil {
		if b, ok := el.(CanBindPool); ok {
			b.BindPool(p)
		}

		trySetNextID(el, &p.nextID)

		if p.onNewElement != nil {
			p.onNewElement(el)
		}
	}
	return el, err
}

func trySetNextID(el Element, nextID *uint64) {
	if ps, ok := el.(interface{ PESetID(id uint64) }); ok {
		id := atomic.AddUint64(nextID, 1)
		ps.PESetID(id)
	}
}

func (p *simplePool) OnNewElement(fn func(el Element)) {
	p.onNewElement = fn
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
	defer t.Stop()

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

// elementCleanerRunLocked 从 idle 列表中，找到过期的、失效的元素
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
	waitTime := atomic.LoadInt64(&p.waitDuration)

	p.mu.Lock()
	defer p.mu.Unlock()

	stats := Stats{
		Open: !p.closed,

		Idle:    len(p.idles),
		NumOpen: p.numOpen,
		Wait:    p.wait,
		InUse:   p.numOpen - len(p.idles),

		WaitCount:         p.waitCount,
		WaitDuration:      time.Duration(waitTime),
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

// Range 遍历所有 idle 状态的对象
func (p *simplePool) Range(fn func(el io.Closer) error) (err error) {
	p.mu.Lock()
	for _, el := range p.idles {
		if err = fn(el); err != nil {
			break
		}
	}
	p.mu.Unlock()
	return err
}

// SimpleRawItem 原始的数据，NewSimpleElement 的参数
type SimpleRawItem struct {
	// Raw 原始对象
	Raw interface{}

	// CheckActive 判断对象元素对象是否有有效，可选
	CheckActive func(raw interface{}) error

	// 关闭元素，可选
	Close func(raw interface{}) error

	// 重置元素，可选
	Reset func(raw interface{})
}

// SimpleElement SimplePool 直接使用时的原始类型定义
type SimpleElement interface {
	HasRaw
	Close() error
}

// NewSimpleElement 创建一个新的通用类型的元素
func NewSimpleElement(item *SimpleRawItem) Element {
	return &elementTPL{
		MetaInfo: NewMetaInfo(),
		item:     item,
	}
}

var _ SimpleElement = (*elementTPL)(nil)
var _ Element = (*elementTPL)(nil)
var _ CanBindPool = (*elementTPL)(nil)

type elementTPL struct {
	err error
	*MetaInfo
	item   *SimpleRawItem
	pool   PoolPutter
	rw     sync.RWMutex
	closed bool
}

func (elt *elementTPL) cloneElementTPL() *elementTPL {
	elt.rw.RLock()
	cp := &elementTPL{
		err:      elt.err,
		item:     elt.item,
		pool:     elt.pool,
		MetaInfo: elt.MetaInfo.cloneMeta(),
	}
	elt.rw.RUnlock()
	return cp
}

func (elt *elementTPL) isClosed() bool {
	elt.rw.RLock()
	d := elt.closed
	elt.rw.RUnlock()
	return d
}

func (elt *elementTPL) BindPool(p PoolPutter) {
	elt.pool = p
}

func (elt *elementTPL) PERaw() interface{} {
	if elt.isClosed() {
		return nil
	}
	return elt.item.Raw
}

func (elt *elementTPL) PEReset() {
	if elt.isClosed() {
		return
	}
	if elt.item.Reset != nil {
		elt.item.Reset(elt.item.Raw)
	}
}

func (elt *elementTPL) PEActive() error {
	if elt.isClosed() {
		return ErrClosedValue
	}
	var err error
	elt.rw.RLock()
	err = elt.err
	elt.rw.RUnlock()

	if err != nil {
		return err
	}
	if err = elt.MetaInfo.Active(elt.pool.Option()); err != nil {
		return err
	}
	if elt.item.CheckActive != nil {
		return elt.item.CheckActive(elt.item.Raw)
	}
	return nil
}

func (elt *elementTPL) PERawClose() error {
	if elt.isClosed() {
		return ErrClosedValue
	}
	if elt.item.Close != nil {
		return elt.item.Close(elt.item.Raw)
	}

	if cr, ok := elt.item.Raw.(io.Closer); ok {
		return cr.Close()
	}
	return nil
}

func (elt *elementTPL) Close() error {
	elt.rw.Lock()
	closed := elt.closed
	elt.closed = true
	elt.rw.Unlock()

	if closed {
		return ErrClosedValue
	}
	return elt.pool.Put(elt.cloneElementTPL())
}

func (elt *elementTPL) SetError(err error) {
	if err == nil {
		return
	}
	elt.rw.Lock()
	elt.err = err
	elt.rw.Unlock()
}

// TrySetError 尝试设置错误，若设置成功返回 true,否则返回 false
//
// obj 必须实现了 CanSetError:
// 	type CanSetError interface {
// 		CanSetError(err error)
// 	}
func TrySetError(obj interface{}, err error) bool {
	if se, ok := obj.(CanSetError); ok {
		se.SetError(err)
		return true
	}
	return false
}

// MustSetError 设置错误,若失败会 panic
//
// obj 必须实现了 CanSetError:
// 	type CanSetError interface {
// 		CanSetError(err error)
// 	}
func MustSetError(obj interface{}, err error) {
	if !TrySetError(obj, err) {
		panic(fmt.Sprintf("CanSetError failed, %T not implement CanSetError interface", obj))
	}
}
