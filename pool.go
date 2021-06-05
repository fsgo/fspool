// Copyright(C) 2021 github.com/hidu  All Rights Reserved.
// Author: hidu (duv123+git@baidu.com)
// Date: 2021/3/21

package fspool

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrBadValue not active element
var ErrBadValue = errors.New("bad pool value")

// ErrOutOfMaxLife out of max life
var ErrOutOfMaxLife = errors.New("out of max life")

// ErrOutOfMaxIdle out of max idle
var ErrOutOfMaxIdle = errors.New("out of max idle")

// ErrOutOfMaxIdleTime out of max idle time
var ErrOutOfMaxIdleTime = errors.New("out of max idle time")

// nowFunc returns the current time; it's overridden in tests.
var nowFunc = time.Now

// ErrClosed 对象池已关闭
var ErrClosed = errors.New("pool already closed")

// Pool 通用的 Pool 接口定义
type Pool interface {
	Get(ctx context.Context) (interface{}, error)
	Put(interface{}) error
	Stats() Stats
	Close() error
	Option() Option
}

// Option pool 配置选项
type Option struct {
	// MaxOpen 最大打开数量
	// <= 0 为不限制
	MaxOpen int

	// MaxIdle 最大空闲数，应 <= MaxOpen
	// <=0 为不允许存在 Idle 元素
	MaxIdle int

	// MaxLifeTime 最大使用时长，超过后将被销毁
	// <=0 为不限制
	MaxLifeTime time.Duration

	// MaxIdleTime 最大空闲等待时间，超过后将被销毁
	// <=0 为不限制
	MaxIdleTime time.Duration
}

func (opt *Option) shortestIdleTime() time.Duration {
	if opt.MaxIdleTime <= 0 {
		return opt.MaxLifeTime
	}
	if opt.MaxLifeTime <= 0 {
		return opt.MaxIdleTime
	}

	min := opt.MaxIdleTime
	if min > opt.MaxLifeTime {
		min = opt.MaxLifeTime
	}
	return min
}

// Clone copy it
func (opt *Option) Clone() *Option {
	return &Option{
		MaxOpen:     opt.MaxOpen,
		MaxIdle:     opt.MaxIdle,
		MaxLifeTime: opt.MaxLifeTime,
		MaxIdleTime: opt.MaxIdleTime,
	}
}

// String 序列化，调试输出用
func (opt *Option) String() string {
	bf, _ := json.Marshal(opt)
	return string(bf)
}

// Stats Pool's Stats
type Stats struct {
	Open bool // 连接池的状态，true-正常，false-已关闭

	NumOpen int // 已打开的总数
	InUse   int // 正被使用的总数
	Idle    int // 连接池里空闲的总数

	// Counters
	WaitCount         int64         // 等待的请求数
	WaitDuration      time.Duration // 等待的总时间
	MaxIdleClosed     int64         // 由于超过 MaxIdle,被关闭的总数
	MaxIdleTimeClosed int64         // 由于超过 MaxIdleTime，被关闭的总数
	MaxLifeTimeClosed int64         // 由于超过 MaxLifetime，被关闭的总数
}

// String 序列化，调试用
func (s Stats) String() string {
	bf, _ := json.Marshal(s)
	return string(bf)
}

// GroupStats Group Pool stats
type GroupStats struct {
	Groups map[interface{}]Stats
	All    Stats
}

// NewElementNeed 创建新 Element 时所需要的
type NewElementNeed interface {
	Put(interface{}) error
	Option() Option
}
