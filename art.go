/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2020 Tailscale Inc. All Rights Reserved.
 */

// Package art implements the Allotment Routing Table, a
// "A Fast Free Multibit Trie Based Routing Table".
//
// See https://www.hariguchi.org/art/art.pdf.
//
// Warning: this is a work-in-progress; see https://github.com/bradfitz/art#status
package art

func baseIndex(width int, addr uint64, prefixLen int) uint64 {
	return (addr >> uint64(width-prefixLen)) + (1 << uint64(prefixLen))
}

func fringeIndex(width int, addr uint64) uint64 {
	return baseIndex(width, addr, width)
}

type Route interface {
	RouteParams() RouteParams
}

type RouteParams struct {
	Width int // bits of address
	Addr  uint64
	Len   int // prefix length of route
}

func (p RouteParams) baseIndex() uint64 { return baseIndex(p.Width, p.Addr, p.Len) }

type Table struct {
	r   []Route
	n   []*Table // nil for single-level tables
	ref int      // ref counter
}

// allot allots route r replacing q at base index b.
func (x *Table) allot(smallestFringeIndex uint64, b uint64, q, r Route) {
	t := smallestFringeIndex
	if x.r[b] == q {
		x.r[b] = r
	} else {
		return
	}
	if b >= smallestFringeIndex {
		// b is a fringe index
		return
	}
	b = b << 1
	x.allot(t, b, q, r) // allot r to left children
	b++
	x.allot(t, b, q, r) // allot r to right children
}

// InsertSingleLevel inserts r into x and reports whether it was able to.
// (It returns false if it was already occupied).
func (x *Table) InsertSingleLevel(r Route) bool {
	return x.insertSingle(r.RouteParams(), r)
}

func (x *Table) insertSingle(rp RouteParams, r Route) bool {
	b := rp.baseIndex()
	xb := x.r[b]
	if xb != nil {
		xbP := xb.RouteParams()
		if rp.Addr == xbP.Addr && rp.Len == xbP.Len {
			return false // already occupied
		}
	}
	x.allot(uint64(1)<<rp.Width, b, xb, r)
	return true
}

// for now
var sl = []int{8, 8, 8, 8}

func (x *Table) Insert(r Route) bool {
	level := 0
	ss := 0 // stride length summation
	X := x  // "Array X <- X0", level 0 array

	rp := r.RouteParams()
	if rp.Addr == 0 && rp.Len == 0 {
		if X.r[1] != nil {
			return false
		}
		X.r[1] = r
		return true
	}
	var s uint64 // stride
	for {
		ss += sl[level]

		// stride:
		s = (rp.Addr >> (rp.Width - ss)) & ((1 << sl[level]) - 1)
		if rp.Len <= ss {
			break
		}
		i := fringeIndex(sl[level], s)
		if X.n[i] == nil {
			X.n[i] = &Table{
				r: make([]Route, len(x.r)),
				n: make([]*Table, len(x.r)), // TODO: not necessary at leafs
			}
			X.ref++
		}
		X = X.n[i]
		level++
	}

	ss -= sl[level]
	if x.insertSingle(RouteParams{Width: sl[level], Addr: s, Len: rp.Len - ss}, r) {
		X.ref++ // new route entry
		return true
	}
	return false
}

func (x *Table) LookupSingleLevel(width int, addr uint64) (r Route, ok bool) {
	r = x.r[fringeIndex(width, addr)]
	return r, r != nil
}

func (x *Table) DeleteSingleLevel(rp RouteParams) (deleted Route, ok bool) {
	b := rp.baseIndex()
	prev := x.r[b]
	if prev == nil {
		return nil, false
	}
	x.allot(uint64(1)<<rp.Width, b, prev, x.r[b>>1])
	return prev, true
}
