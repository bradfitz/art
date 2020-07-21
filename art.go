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
	// TODO: merge r and n into one slice (make a *Table that
	// implements Route probably?), and probably remove ref.
	r   []Route
	n   []*Table // nil for single-level tables
	ref int      // ref counter
}

func NewTable(width int) *Table {
	n := 1 << (width + 1)
	return &Table{
		r: make([]Route, n),
		n: make([]*Table, n),
	}
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
	rp := r.RouteParams()
	return insert(x, rp.Width, sl, r)
}

// insert is multi-level insertion ("Algorithm 5).
//
// w: width of address
// sl: stride length by level
//
// It reports whether the insertion was successful.
func insert(x0 *Table, w int, sl []int, r Route) bool {
	level := 0
	ss := 0 // stride length summation
	x := x0 // "Array X <- X0", level 0 array

	rp := r.RouteParams()
	if rp.Addr == 0 && rp.Len == 0 {
		if x.r[1] != nil {
			return false // already had a default route
		}
		x.r[1] = r // default route
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
		if x.n[i] == nil {
			x.n[i] = NewTable(rp.Width)
			x.ref++
		}
		x = x.n[i]
		level++
	}

	ss -= sl[level]
	if x.insertSingle(RouteParams{Width: sl[level], Addr: s, Len: rp.Len - ss}, r) {
		x.ref++ // new route entry
		return true
	}
	return false
}

func (x *Table) LookupSingleLevel(width int, addr uint64) (r Route, ok bool) {
	r = x.r[fringeIndex(width, addr)]
	return r, r != nil
}

// sl: stride length by level
func (x *Table) LookupMultiLevel(width int, sl []int, addr uint64) (r Route, ok bool) {
	r = searchMultiLevel(x, width, sl, addr)
	return r, r != nil
}

// Algorithm 7
//
// Returns longest prefix matching route pointer or nil
func searchMultiLevel(x0 *Table, w int, sl []int, a uint64) (r Route) {
	lmr := x0.r[1] // longest matching route
	x := x0
	ss := 0 // stride length summation
	var s uint64
	level := 0
	var i uint64 // index
	for {
		ss += sl[level]
		s = (a >> (w - ss)) & ((1 << sl[level]) - 1)
		i = fringeIndex(sl[level], s)
		if x.n[i] != nil {
			// "update current longest matching route"
			if x.r[i] != nil {
				lmr = x.r[i]
			}
			x = x.n[i]
			level++
		} else if x.r[i] != nil {
			return x.r[i]
		} else {
			return lmr // pr == pn == nil
		}
	}
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

const maxLevel = 8

func (x *Table) Delete(rp RouteParams) (deleted Route, ok bool) {
	return delete(x, rp.Width, sl, rp.Addr, rp.Len)
}

// delete is multi-level deletion (Algorithm 6)
//
// w: address length
// sl: stride length by level
// a: destination address
// pl: prefix length
//
// It returns the deleted route and whether it was successful.
func delete(x0 *Table, w int, sl []int, a uint64, pl int) (r Route, ok bool) {
	x := x0
	xsv := [maxLevel]*Table{0: x} // parent array pointers
	ss := 0                       // stride length summation
	var s uint64                  // stride
	level := 0
	var i uint64             // index
	var isv [maxLevel]uint64 // parent indices

	// Default route.
	if a == 0 && pl == 0 {
		if r = x.r[1]; r == nil {
			return nil, false
		}
		x.r[1] = nil
		return r, true
	}

	for {
		ss += sl[level]
		s = (a >> (w - ss)) & ((1 << sl[level]) - 1)
		if pl <= ss {
			break
		}
		i = fringeIndex(sl[level], s)
		isv[level] = i
		if x.n[i] == nil {
			return nil, false
		}
		xsv[level] = x
		x = x.n[i]
		level++
	}

	ss -= sl[level]
	r, ok = x.DeleteSingleLevel(RouteParams{Width: sl[level], Addr: s, Len: pl - ss})
	if !ok {
		return nil, false
	}

	// "Free arrays if necessary"
	x.ref--
	if level > 0 && x.ref == 0 {
		for {
			// "Free X" (not needed in Go)
			level--        // "get parent level"
			x = xsv[level] // "get parent array pointer"
			// "child array is deleted"
			x.ref--
			if level <= 0 || x.ref > 0 {
				break
			}
		}
	}

	return r, true
}
