/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2020 Tailscale Inc. All Rights Reserved.
 */

// Package art implements the Allotment Routing Table, a
// "A Fast Free Multibit Trie Based Routing Table".
//
// See https://www.hariguchi.org/art/art.pdf.
package art

func baseIndex(width int, addr uint64, prefixLen int) uint64 {
	return (addr >> uint64(width-prefixLen)) + (1 << uint64(prefixLen))
}

func fringeIndex(width int, addr uint64) uint64 {
	return baseIndex(width, addr, width)
}

type Route interface {
	Addr() uint64
	PrefixLen() int
	Width() int
}

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
	a, l := r.Addr(), r.PrefixLen()
	b := baseIndex(r.Width(), a, l)
	xb := x[b]
	if xb != nil && a == xb.Addr() && l == xb.PrefixLen() {
		return false // already occupied
	}
	x.allot(uint64(1)<<r.Width(), b, xb, r)
	return true
}

func (x Table) LookupSingleLevel(width int, addr uint64) (r Route, ok bool) {
	r = x[fringeIndex(width, addr)]
	return r, r != nil
}
