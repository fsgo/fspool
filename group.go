/*
 * Copyright(C) 2021 github.com/hidu  All Rights Reserved.
 * Author: hidu (duv123+git@baidu.com)
 * Date: 2021/3/21
 */

package fspool

import (
	"context"
)

type Group struct {
	// pools map[interface{}]Pool
	// mu    sync.RWMutex
}

func (g *Group) Get(ctx context.Context, key interface{}) (interface{}, error) {
	return nil, ErrNotPoolConn
}

func (g *Group) Put(key interface{}, value interface{}) error {
	return nil
}

func (g *Group) Stats() Stats {
	return Stats{}
}

func (g *Group) Close() error {
	return nil
}
