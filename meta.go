/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/4/12
 */

package fspool

import (
	"encoding/json"
	"sync"
	"time"
)

// NewMetaInfo 创建一个 *MetaInfo
func NewMetaInfo() *MetaInfo {
	return &MetaInfo{
		meta: &Meta{
			CreateTime: time.Now(),
		},
	}
}

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

// IsActive 是否在有效期内
func (w *MetaInfo) IsActive(opt Option) bool {
	w.mu.Lock()
	lastUse := w.meta.LastUseTime
	w.mu.Unlock()

	if opt.MaxIdleTime > 0 && time.Since(lastUse) >= opt.MaxIdleTime {
		return false
	}
	if opt.MaxLifeTime > 0 && time.Since(w.meta.CreateTime) >= opt.MaxLifeTime {
		return false
	}
	return true
}

// PEMeta 获取 meta 信息
func (w *MetaInfo) PEMeta() Meta {
	w.mu.Lock()
	m := *w.meta
	w.mu.Unlock()
	return m
}

// Meta 元信息
type Meta struct {
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

// ReadMeta 获取元信息
func ReadMeta(item interface{}) Meta {
	type PEMeta interface {
		PEMeta() Meta
	}
	return item.(PEMeta).PEMeta()
}
