/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// ErrNotPoolConn 不是 ConnPool 支持的类型
var ErrNotPoolConn = errors.New("not pool conn")

// NewConnFunc 创建新
type NewConnFunc func(ctx context.Context) (net.Conn, error)

// Trans 转换为原始的 NewElementFunc
func (nf NewConnFunc) Trans(p *ConnPool) NewElementFunc {
	opt := p.Option()
	return func(ctx context.Context, pool *SimplePool) (Element, error) {
		raw, err := nf(ctx)
		if err != nil {
			return nil, err
		}
		vc := newConn(raw, &opt, func(conn *conn) error {
			if conn.Active() {
				return p.Put(conn)
			}
			return conn.RawClose()
		})
		return vc, nil
	}
}

// NewConnPool 创建新的 net.Conn 的连接池
func NewConnPool(option *Option, newFunc NewConnFunc) *ConnPool {
	p := &ConnPool{}
	p.raw = NewSimple(option, newFunc.Trans(p))
	return p
}

// ConnPool 网络连接池
type ConnPool struct {
	raw *SimplePool
}

// Get get
func (cp *ConnPool) Get(ctx context.Context) (el net.Conn, err error) {
	value, err := cp.raw.Get(ctx)
	if err != nil {
		return nil, err
	}
	return value.(net.Conn), nil

}

// Put put to pool
func (cp *ConnPool) Put(cn net.Conn) error {
	if v, ok := cn.(*conn); ok {
		return cp.raw.Put(v)
	}
	cn.Close()
	return ErrNotPoolConn
}

// Close close pool
func (cp *ConnPool) Close() error {
	return cp.raw.Close()
}

// Option get pool option
func (cp *ConnPool) Option() Option {
	return cp.raw.Option()
}

// Stats get pool stats
func (cp *ConnPool) Stats() Stats {
	return cp.raw.Stats()
}

const (
	statStart = iota
	statDone
)

func newConn(raw net.Conn, option *Option, onClose func(conn *conn) error) *conn {
	return &conn{
		raw:     raw,
		onClose: onClose,
		ctime:   time.Now(),
	}
}

type conn struct {
	option *Option

	raw     net.Conn
	onClose func(conn *conn) error

	mu        sync.RWMutex
	lastErr   error
	ctime     time.Time
	lastRead  time.Time
	lastWrite time.Time

	readStat  uint8
	writeStat uint8
}

func (c *conn) setErr(err error) {
	if err != nil {
		c.mu.Lock()
		c.lastErr = err
		c.mu.Unlock()
	}
}

func (c *conn) Read(b []byte) (n int, err error) {
	c.withLock(func() {
		c.readStat = statStart
	})
	n, err = c.raw.Read(b)
	c.setErr(err)
	c.withLock(func() {
		c.lastRead = time.Now()
		c.readStat = statDone
	})
	return n, err
}

func (c *conn) Write(b []byte) (n int, err error) {
	c.withLock(func() {
		c.writeStat = statStart
	})
	n, err = c.raw.Write(b)
	c.setErr(err)
	c.withLock(func() {
		c.lastWrite = time.Now()
		c.writeStat = statDone
	})
	return n, err
}

func (c *conn) Close() error {
	if c.onClose != nil {
		return c.onClose(c)
	}
	return c.raw.Close()
}

func (c *conn) LocalAddr() net.Addr {
	return c.raw.LocalAddr()
}

func (c *conn) RemoteAddr() net.Addr {
	return c.raw.RemoteAddr()
}

func (c *conn) SetDeadline(t time.Time) error {
	err := c.raw.SetDeadline(t)
	c.setErr(err)
	return err
}

func (c *conn) SetReadDeadline(t time.Time) error {
	err := c.raw.SetReadDeadline(t)
	c.setErr(err)
	return err
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	err := c.raw.SetWriteDeadline(t)
	c.setErr(err)
	return err
}

func (c *conn) withLock(fn func()) {
	c.mu.Lock()
	fn()
	c.mu.Unlock()
}

func (c *conn) isDoing() bool {
	return c.readStat == statStart || c.writeStat == statStart
}

func (c *conn) lastRWTime() time.Time {
	if c.lastRead.After(c.lastWrite) {
		return c.lastRead
	}
	return c.lastWrite
}

func (c *conn) Raw() net.Conn {
	return c.raw
}

func (c *conn) RawClose() error {
	return c.raw.Close()
}

func (c *conn) Active() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastErr != nil {
		return false
	}

	if c.isDoing() {
		return false
	}

	if c.option.MaxIdleTime > 0 {
		if c.lastRWTime().Add(c.option.MaxIdleTime).Before(nowFunc()) {
			return false
		}
	}

	if c.option.MaxLifetime > 0 {
		if c.ctime.Add(c.option.MaxLifetime).Before(nowFunc()) {
			return false
		}
	}
	return true
}

var _ net.Conn = (*conn)(nil)
var _ Element = (*conn)(nil)
