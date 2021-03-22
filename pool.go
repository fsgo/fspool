/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
	"encoding/json"
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
	// MaxOpen max opening Element
	// <= 0 means unlimited
	MaxOpen int

	// MaxIdle
	// <=0 means disabled
	MaxIdle int

	// MaxLifetime
	// maximum amount of time a Element may be reused
	MaxLifetime time.Duration

	// MaxIdleTime
	// maximum amount of time a Element may be idle before being closed
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

// Stats Pool's Stats
type Stats struct {
	MaxOpen int // Maximum number of open Elements to the Pool.

	// SimplePool Status
	NumOpen int // The number of established Elements both in use and idle.
	InUse   int // The number of Elements currently in use.
	Idle    int // The number of idle Elements.

	// Counters
	WaitCount         int64         // The total number of Elements waited for.
	WaitDuration      time.Duration // The total time blocked waiting for a new Element.
	MaxIdleClosed     int64         // The total number of Elements closed.
	MaxIdleTimeClosed int64         // The total number of Elements closed.
	MaxLifetimeClosed int64         // The total number of Elements closed.
}

// String 序列化，调试用
func (s Stats) String() string {
	bf, _ := json.Marshal(s)
	return string(bf)
}
