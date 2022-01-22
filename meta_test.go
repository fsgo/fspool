// Copyright(C) 2022 github.com/fsgo  All Rights Reserved.
// Author: fsgo
// Date: 2022/1/22

package fspool

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadMeta(t *testing.T) {
	t.Run("conn", func(t *testing.T) {
		pc := newPConn(nil, nil)
		m := ReadMeta(pc)
		require.NotNil(t, m)
	})

	t.Run("testMeta1 has", func(t *testing.T) {
		pc := newPConn(nil, nil)
		n1 := &testMeta1{Conn: pc}
		m := ReadMeta(n1)
		require.NotNil(t, m)
	})

	t.Run("nil", func(t *testing.T) {
		rc := &net.TCPConn{}
		m := ReadMeta(rc)
		require.Nil(t, m)
	})

	t.Run("testMeta1 nil", func(t *testing.T) {
		n1 := &testMeta1{Conn: nil}
		m := ReadMeta(n1)
		require.Nil(t, m)
	})
}

var _ HasPERaw = (*testMeta1)(nil)

type testMeta1 struct {
	net.Conn
}

func (tm *testMeta1) PERaw() interface{} {
	return tm.Conn
}
