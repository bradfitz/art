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
	Addr  uint64
	Len   int
	Width int
}

func (p RouteParams) baseIndex() uint64 { return baseIndex(p.Width, p.Addr, p.Len) }

type Table []Route

// allot allots route r replacing q at base index b.
func (x Table) allot(smallestFringeIndex uint64, b uint64, q, r Route) {
	t := smallestFringeIndex
	if x[b] == q {
		x[b] = r
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
func (x Table) InsertSingleLevel(r Route) bool {
	rp := r.RouteParams()
	b := rp.baseIndex()
	xb := x[b]
	if xb != nil {
		xbP := xb.RouteParams()
		if rp.Addr == xbP.Addr && rp.Len == xbP.Len {
			return false // already occupied
		}
	}
	x.allot(uint64(1)<<rp.Width, b, xb, r)
	return true
}

func (x Table) LookupSingleLevel(width int, addr uint64) (r Route, ok bool) {
	r = x[fringeIndex(width, addr)]
	return r, r != nil
}

func (x Table) DeleteSingleLevel(rp RouteParams) (deleted Route, ok bool) {
	b := rp.baseIndex()
	prev := x[b]
	if prev == nil {
		return nil, false
	}
	x.allot(uint64(1)<<rp.Width, b, prev, x[b>>1])
	return prev, true
}
