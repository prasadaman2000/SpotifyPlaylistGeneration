// this is very much a "i just needed to get the job done" priority queue.
// performance is not a priority TM
package main

import "errors"

type PriorityQueueEntry struct {
	data     string
	priority int
}

type PriorityQueue struct {
	elems []*PriorityQueueEntry
}

var ErrPriorityQueueEmpty = errors.New("priorityqueue: PQ is empty")

func newPriorityQueueEntry(data string, priority int) *PriorityQueueEntry {
	return &PriorityQueueEntry{
		data:     data,
		priority: priority,
	}
}

func NewPriorityQueue() *PriorityQueue {
	return new(PriorityQueue)
}

func (p *PriorityQueue) Push(data string, priority int) {
	toInsert := newPriorityQueueEntry(data, priority)
	for idx, entry := range p.elems {
		if entry.priority >= priority {
			var newElems []*PriorityQueueEntry
			newElems = append(newElems, p.elems[0:idx]...)
			newElems = append(newElems, toInsert)
			newElems = append(newElems, p.elems[idx:]...)
			p.elems = newElems
			return
		}
	}
	p.elems = append(p.elems, toInsert)
}

func (p *PriorityQueue) Pop() (string, error) {
	if len(p.elems) == 0 {
		return "", ErrPriorityQueueEmpty
	}
	toRet := p.elems[len(p.elems)-1]
	p.elems = p.elems[:len(p.elems)-1]
	return toRet.data, nil
}
