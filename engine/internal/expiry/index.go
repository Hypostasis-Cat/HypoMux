// Package expiry provides an indexed deadline heap. Callers provide locking.
package expiry

import (
	"container/heap"
	"time"
)

type entry[K comparable] struct {
	key   K
	at    time.Time
	index int
}

type queue[K comparable] []*entry[K]

func (q queue[K]) Len() int           { return len(q) }
func (q queue[K]) Less(i, j int) bool { return q[i].at.Before(q[j].at) }
func (q queue[K]) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}
func (q *queue[K]) Push(value any) {
	e := value.(*entry[K])
	e.index = len(*q)
	*q = append(*q, e)
}
func (q *queue[K]) Pop() any {
	last := len(*q) - 1
	e := (*q)[last]
	(*q)[last] = nil
	*q = (*q)[:last]
	return e
}

// Index keeps exactly one heap entry per key, including after renewal.
// Its zero value is ready to use. It must not be copied after first use.
type Index[K comparable] struct {
	items map[K]*entry[K]
	queue queue[K]
}

func (x *Index[K]) Len() int { return len(x.queue) }

// Set inserts or updates a deadline in O(log N).
func (x *Index[K]) Set(key K, at time.Time) {
	if e := x.items[key]; e != nil {
		e.at = at
		heap.Fix(&x.queue, e.index)
		return
	}
	if x.items == nil {
		x.items = make(map[K]*entry[K])
	}
	e := &entry[K]{key: key, at: at}
	x.items[key] = e
	heap.Push(&x.queue, e)
}

func (x *Index[K]) Delete(key K) {
	if e := x.items[key]; e != nil {
		heap.Remove(&x.queue, e.index)
		delete(x.items, key)
	}
}

// First returns the earliest deadline without removing it, in O(1).
func (x *Index[K]) First() (key K, at time.Time, ok bool) {
	if len(x.queue) == 0 {
		return key, at, false
	}
	e := x.queue[0]
	return e.key, e.at, true
}

// PopExpired removes one due entry in O(log N); an unexpired heap costs O(1).
func (x *Index[K]) PopExpired(now time.Time) (key K, ok bool) {
	key, at, ok := x.First()
	if !ok || at.After(now) {
		return key, false
	}
	x.Delete(key)
	return key, true
}
