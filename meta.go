// Copyright(C) 2021 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2021/4/12

package fspool

import (
	"encoding/json"
	"sync"
	"time"
)

// CanMarkUsing 支持标记在使用中
type CanMarkUsing interface {
	PEMarkUsing()
}

// CanMarkIdle 支持标记当前已处于空闲中
type CanMarkIdle interface {
	PEMarkIdle()
}

// NewMetaInfo 创建一个 *MetaInfo
func NewMetaInfo() *MetaInfo {
	return &MetaInfo{
		meta: &Meta{
			CreateTime: time.Now(),
		},
	}
}

var _ CanMarkUsing = (*MetaInfo)(nil)
var _ CanMarkIdle = (*MetaInfo)(nil)

// MetaInfo 包含创建时间和使用时间、使用次数等元信息
type MetaInfo struct {
	meta  *Meta
	using bool
	mu    sync.Mutex
}

// PEMarkUsing 标记开始使用
func (w *MetaInfo) PEMarkUsing() {
	now := time.Now()
	w.mu.Lock()
	w.using = true
	w.meta.LastUseTime = now
	w.meta.UsedTimes++
	w.mu.Unlock()
}

// PEMarkIdle 标记当前处于空闲状态
func (w *MetaInfo) PEMarkIdle() {
	now := time.Now()
	w.mu.Lock()
	w.using = false
	w.meta.UsedDuration += now.Sub(w.meta.LastUseTime)
	w.meta.LastUseTime = now
	w.mu.Unlock()
}

// PESetID 给元素设置 ID
func (w *MetaInfo) PESetID(id uint64) {
	w.mu.Lock()
	w.meta.ID = id
	w.mu.Unlock()
}

// Active 是否在有效期内
func (w *MetaInfo) Active(opt Option) error {
	w.mu.Lock()
	lastUse := w.meta.LastUseTime
	w.mu.Unlock()

	if opt.MaxIdleTime > 0 && time.Since(lastUse) >= opt.MaxIdleTime {
		return ErrOutOfMaxIdleTime
	}
	if opt.MaxLifeTime > 0 && time.Since(w.meta.CreateTime) >= opt.MaxLifeTime {
		return ErrOutOfMaxLife
	}
	return nil
}

// PEMeta 获取 meta 信息
func (w *MetaInfo) PEMeta() Meta {
	w.mu.Lock()
	m := *w.meta
	w.mu.Unlock()
	return m
}

func (w *MetaInfo) cloneMeta() *MetaInfo {
	m := w.PEMeta()
	return &MetaInfo{
		meta: &m,
	}
}

// HasMeta 判断是否支持读取 Meta 信息
type HasMeta interface {
	PEMeta() Meta
}

// Meta 元信息
type Meta struct {
	// ID  当前 Pool 创建的第N个元素
	ID uint64

	// CreateTime 创建时间
	CreateTime time.Time

	// LastUseTime 最后使用时间
	LastUseTime time.Time

	// UsedTimes 使用总次数
	UsedTimes uint64

	// UsedDuration 被使用的总时长
	UsedDuration time.Duration
}

// String 序列化，调试用
func (m Meta) String() string {
	bf, _ := json.Marshal(m)
	return string(bf)
}

// ReadMeta 获取缓存信息的元信息,若没有，会返回 nil
func ReadMeta(item interface{}) *Meta {
	val := item
	for {
		if val == nil {
			return nil
		}
		if ri, ok := val.(HasMeta); ok {
			m := ri.PEMeta()
			return &m
		}

		if rr, ok := val.(HasPERaw); ok {
			val = rr.PERaw()
		} else {
			return nil
		}
	}
}
