// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provision

import (
	"sync"
)

var _ ActionLimiter = &LocalLimiter{}
var noop = func() {}

type ActionLimiter interface {
	SetLimit(uint)
	Start(action string) func()
	Len(action string) int
}

type LocalLimiter struct {
	sync.Mutex
	chMap map[string]chan struct{}
	limit uint
}

func (l *LocalLimiter) SetLimit(i uint) {
	l.limit = i
	l.chMap = nil
	if i != 0 {
		l.chMap = make(map[string]chan struct{})
	}
}

func (l *LocalLimiter) actionEntry(action string) chan struct{} {
	l.Lock()
	if l.chMap == nil {
		l.Unlock()
		return nil
	}
	if l.chMap[action] == nil {
		l.chMap[action] = make(chan struct{}, l.limit)
	}
	limitChan := l.chMap[action]
	l.Unlock()
	return limitChan
}

func (l *LocalLimiter) Start(action string) func() {
	ch := l.actionEntry(action)
	if ch == nil {
		return noop
	}
	ch <- struct{}{}
	return func() {
		<-ch
	}
}

func (l *LocalLimiter) Len(action string) int {
	return len(l.actionEntry(action))
}
