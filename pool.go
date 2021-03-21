/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"errors"
	"time"
)

// ErrBadValue not active element
var ErrBadValue = errors.New("bad pool value")

// nowFunc returns the current time; it's overridden in tests.
var nowFunc = time.Now

// ErrClosed 对象池已关闭
var ErrClosed = errors.New("already closed")

// Pool 通用的 Pool 接口定义
type Pool interface {
	Get(ctx context.Context) (interface{}, error)
	Put(interface{}) error
	Stats() Stats
	Close() error
	Option() Option
}

// Option pool option
type Option struct {
	// MaxOpen max opening element
	// <= 0 means unlimited
	MaxOpen int

	// MaxIdle
	// <=0 means disabled
	MaxIdle int

	// MaxLifetime
	// maximum amount of time a connection may be reused
	MaxLifetime time.Duration

	// MaxIdleTime
	// maximum amount of time a connection may be idle before being closed
	MaxIdleTime time.Duration
}

func (opt *Option) shortestIdleTime() time.Duration {
	if opt.MaxIdleTime <= 0 {
		return opt.MaxLifetime
	}
	if opt.MaxLifetime <= 0 {
		return opt.MaxIdleTime
	}

	min := opt.MaxIdleTime
	if min > opt.MaxLifetime {
		min = opt.MaxLifetime
	}
	return min
}

// Stats 状态
type Stats struct {
	MaxOpen int // Maximum number of open connections to the database.

	// SimplePool Status
	NumOpen int // The number of established connections both in use and idle.
	InUse   int // The number of connections currently in use.
	Idle    int // The number of idle connections.

	// Counters
	WaitCount         int64         // The total number of connections waited for.
	WaitDuration      time.Duration // The total time blocked waiting for a new connection.
	MaxIdleClosed     int64         // The total number of connections closed due to SetMaxIdleConns.
	MaxIdleTimeClosed int64         // The total number of connections closed due to SetConnMaxIdleTime.
	MaxLifetimeClosed int64         // The total number of connections closed due to SetConnMaxLifetime.
}
