/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2020 Tailscale Inc. All Rights Reserved.
 */

// Package art implements the Allotment Routing Table, a
// "A Fast Free Multibit Trie Based Routing Table".
//
// See https://cseweb.ucsd.edu/~varghese/TEACH/cs228/artlookup.pdf
//
// Warning: this is a work-in-progress; see https://github.com/bradfitz/art#status
package art

import (
	"encoding/binary"

	"inet.af/netaddr"
)

// TODO: section 3.1: Element Consolidation. We currently store 3
// words (2 for the Route interface, 1 for the *Table) per entry; the
// paper does 1. Without doing unsafe, we could get at least down to 2
// by making a child *Table type that implements Route.

// TODO: section 3.2: path compression.

func baseIndex(width int, addr uint32, prefixLen int) uint32 {
	return (addr >> uint32(width-prefixLen)) | (1 << uint32(prefixLen))
}

func fringeIndex(width int, addr uint32) uint32 {
	return baseIndex(width, addr, width)
}

// getsBits4 will get a fixed number of bits from the end of src, skipping byteOffset bytes from
// the right hand side. bits must not be bigger than 32, otherwise it will overflow.
func getBits4(byteOffset, bits int, from [4]byte) uint32 {
	if byteOffset > 4 {
		return 0
	}
	src := from[:(4 - byteOffset)]
	var v uint32
	switch len(src) {
	case 0:
		return 0
	case 1:
		v = uint32(src[0])
	case 2:
		v = uint32(src[1]) + (uint32(src[0]) << 8)
	case 3:
		v = uint32(src[2]) +
			(uint32(src[1]) << 8) +
			(uint32(src[0]) << 16)
	default:
		v = binary.BigEndian.Uint32(src[len(src)-4:])
	}
	return v & ((1 << bits) - 1)
}

// getsBits16 will get a fixed number of bits from the end of src, skipping byteOffset bytes from
// the right hand side. bits must not be bigger than 32, otherwise it will overflow.
func getBits16(byteOffset, bits int, from [16]byte) uint32 {
	if byteOffset > 16 {
		return 0
	}
	src := from[:(16 - byteOffset)]
	var v uint32
	switch len(src) {
	case 0:
		return 0
	case 1:
		v = uint32(src[0])
	case 2:
		v = uint32(src[1]) + (uint32(src[0]) << 8)
	case 3:
		v = uint32(src[2]) +
			(uint32(src[1]) << 8) +
			(uint32(src[0]) << 16)
	default:
		v = binary.BigEndian.Uint32(src[len(src)-4:])
	}
	return v & ((1 << bits) - 1)
}

// A Route is an entry in the routing table.
type Route interface {
	// IPPrefix returns the IP and Prefix of the routing table entry.
	IPPrefix() netaddr.IPPrefix
	// Equals is a way to compare two routes. Even if they contain the same IPPrefix,
	// if there is additional metadata that can be compared here.
	Equals(Route) bool
}

// Table is the top level routing table interface. It stores routes to prefix match, and
// longest-matching routes can be searched for by Table.Lookup. For example,
// we can insert 127.0.0.1/4, which will match on the IP 127.255.255.255. A single Table can
// support either IPv4 or IPv6, but not both at the same time.
type Table struct {
	w       int   // addr width
	strides []int // stride lengths
	root    *tableNode
}

// A tableNode is an internal node in a table. It is responsible for matching on a specific
// portion of the address, and if a more specific part of the address exists, will store some
// number of pointers to child tableNodes. To allow for child nodes to be freed, it also
// maintains a reference count of the number of entries in it.
type tableNode struct {
	// TODO: merge r and n into one slice (make a *Table that
	// implements Route probably?), and probably remove ref.
	r         []Route
	n         []*tableNode // nil for single-level tables
	ref       int          // ref counter
	parentPtr **tableNode  // address of parent's pointer to this table
}

// free deallocates a tableNode by removing the parent's pointer to it, letting it be garbage
// collected.
func (x *tableNode) free() {
	if x.parentPtr != nil {
		*x.parentPtr = nil
		x.parentPtr = nil
	}
}

// Clone returns a deep clone of the current table.
func (x *Table) Clone() *Table {
	return &Table{
		root: x.root.clone(),
		// since these are immutable, they don't need to be cloned.
		w:       x.w,
		strides: x.strides,
	}
}

// clone clones an inner table node, and any child pointers.
func (x *tableNode) clone() *tableNode {
	if x == nil {
		return nil
	}
	x2 := &tableNode{
		ref:       x.ref,
		r:         x.r,
		parentPtr: x.parentPtr,
	}
	if x.n != nil {
		x2.n = make([]*tableNode, len(x.n))
		for i, v := range x.n {
			x2.n[i] = v.clone()
		}
	}
	return x2
}

// NewTable creates a new table with the given number of strides that determines the granularity
// of allocations. Strides represents the length of each prefix matching level, so having
// smaller strides implies many tiny allocations, larger implies few but large allocations. Each
// element in strides must be a multiple of 8, except for the last entry which may be of any
// length. If using IPv4, strides must sum to 32. Otherwise for IPv6, strides must sum to 128.
// In addition, strides may have at most 16 elements.
//
// A good default for strides for IPv4 is 16,8,8, but experimentation on your specific dataset
// may lead to better configurations.
func NewTable(strides []int) *Table {
	w := 0
	for i, s := range strides {
		if s%8 != 0 && i != len(strides)-1 {
			panic("All except last stride must be a multiple of 8 to be byte aligned")
		}
		if s > 32 {
			panic("Does not support strides of larger than 32 bit, will overflow")
		}
		w += s
	}
	if len(strides) > maxLevel {
		panic("Number of strides larger than max number of levels supported")
	}

	return &Table{
		w:       w,
		strides: strides,

		// create an empty root level table, with the size of strides 0.
		root: newTableNode(strides[0]),
	}
}

func newTableNode(stride int) *tableNode {
	n := 1 << (stride + 1)
	return &tableNode{
		r: make([]Route, n),
		n: make([]*tableNode, n),
	}
}

// allot allots route r replacing q at base index b.
func (x *tableNode) allot(smallestFringeIndex, b uint32, q, r Route) {
	t := smallestFringeIndex
	if (x.r[b] == nil && q == nil) || x.r[b].Equals(q) {
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

// insertSingle inserts a given route into the table, with the given prefix len remaining and
// width (which is the stride at a given level).  addr is not the full address, but a subset of
// bits contained in the IP. r contains the entire IP and any additional metadata which can be
// retrieved later.
func (x *tableNode) insertSingle(w int, addr uint32, prefix int, r Route) bool {
	b := baseIndex(w, addr, prefix)
	xb := x.r[b]
	if xb != nil {
		if r.IPPrefix() == xb.IPPrefix() {
			// previously, this route was already inserted, need to delete it explicitly.
			return false
		}
	}
	x.allot(1<<w, b, xb, r)
	return true
}

// Insert will add the given route using its IPPrefix into the table, allocating if necessary.
// Returns true if it was inserted, otherwise returns false if there already exists an item with
// the same IPPrefix.
func (x *Table) Insert(r Route) bool {
	return insert(x.root, x.w, x.strides, r)
}

// insert is multi-level insertion ("Algorithm 5).
//
// sl: stride length by level
//
// It reports whether the insertion was successful.
func insert(x0 *tableNode, w int, sl []int, r Route) bool {
	x := x0 // "Array X <- X0", level 0 array

	ipp := r.IPPrefix()
	if ipp.Bits() == 0 {
		if x.r[1] != nil {
			return false // already had a default route
		}
		x.r[1] = r // default route
		return true
	}
	// getBits will get some number of bits in either ipv4 or ipv6.
	var getBits func(ss, sl int) uint32
	if ipp.IP().Is4() {
		ipv4 := ipp.IP().As4()
		getBits = func(ss, sl int) uint32 {
			return getBits4((w-ss)/8, sl, ipv4)
		}
	} else {
		ipv6 := ipp.IP().As16()
		getBits = func(ss, sl int) uint32 {
			return getBits16((w-ss)/8, sl, ipv6)
		}
	}

	var s uint32 // stride
	level := 0
	ss := 0 // stride length summation
	for {
		ss += sl[level]

		// stride:
		s = getBits(ss, sl[level])
		if int(ipp.Bits()) <= ss {
			break
		}
		i := fringeIndex(sl[level], s)
		// If the next level is unoccupied, allocate it and increase refs
		if x.n[i] == nil {
			child := newTableNode(sl[level+1])
			x.n[i] = child
			child.parentPtr = &x.n[i]
			x.ref++
		}
		x = x.n[i]
		level++
	}

	ss -= sl[level]
	if x.insertSingle(sl[level], s, int(ipp.Bits())-ss, r) {
		x.ref++ // new route entry
		return true
	}
	return false
}

// lookupSingle looks up an addr in a tableNode, treating it as a leaf node with no children.
func (x *tableNode) lookupSingle(width int, addr uint32) (r Route, ok bool) {
	r = x.r[fringeIndex(width, addr)]
	return r, r != nil
}

// Lookup looks up the most specific Route for the given addr. Returns found, true if there
// exists a route, otherwise nil, false.
func (x *Table) Lookup(ip netaddr.IP) (found Route, ok bool) {
	found = searchMultiLevel(x.root, x.w, x.strides, ip)
	return found, found != nil
}

// Algorithm 7
//
// Returns longest prefix matching route pointer or nil
func searchMultiLevel(x0 *tableNode, w int, sl []int, ip netaddr.IP) (found Route) {
	lmr := x0.r[1] // longest matching route
	x := x0

	// getBits will get some number of bits in either ipv4 or ipv6.
	var getBits func(ss, sl int) uint32
	if ip.Is4() {
		ipv4 := ip.As4()
		getBits = func(ss, sl int) uint32 {
			return getBits4((w-ss)/8, sl, ipv4)
		}
	} else {
		ipv6 := ip.As16()
		getBits = func(ss, sl int) uint32 {
			return getBits16((w-ss)/8, sl, ipv6)
		}
	}

	level := 0
	ss := 0
	for {
		s := sl[level]
		ss += s
		// stride:
		i := fringeIndex(s, getBits(ss, s))
		if x.n[i] != nil {
			// update current longest matching route
			if x.r[i] != nil {
				lmr = x.r[i]
			}
			x = x.n[i]
		} else {
			if x.r[i] != nil {
				return x.r[i]
			}
			// if the original lmr is nil, the current lmr is nil
			// this will return nil.
			return lmr
		}
		level++
	}
}

// deleteSingle removes a subset of an address with a given prefix and width from a
// single-level table, returning the old item if it existed
func (x *tableNode) deleteSingle(w int, addr uint32, prefix int) (deleted Route, ok bool) {
	b := baseIndex(w, addr, prefix)
	prev := x.r[b]
	if prev == nil {
		return nil, false
	}
	x.allot(1<<w, b, prev, x.r[b>>1])
	return prev, true
}

// maxLevel is the maximum number of pointers maintained in delete. In theory we could allocate
// on every delete, but that would be costly, and using more than 16 levels would also imply
// that a stride is smaller than a byte on IPv6.
const maxLevel = 16

// Delete deletes the route described by the parameters.
// If a route was deleted, it returns the deleted route, and true,
// otherwise it returns nil and false.
func (x *Table) Delete(ipp netaddr.IPPrefix) (deleted Route, ok bool) {
	return delete(x.root, x.w, x.strides, ipp)
}

// delete is multi-level deletion (Algorithm 6)
//
// w: address length
// sl: stride length by level
// a: destination address
// pl: prefix length
//
// It returns the deleted route and whether it was successful.
func delete(x0 *tableNode, w int, sl []int, ipp netaddr.IPPrefix) (r Route, ok bool) {
	x := x0
	xsv := [maxLevel]*tableNode{0: x} // parent array pointers
	var isv [maxLevel]uint32          // parent indices

	// Default route.
	if ipp.Bits() == 0 {
		if r = x.r[1]; r == nil {
			return nil, false
		}
		x.r[1] = nil
		return r, true
	}
	// getBits will get some number of bits in either ipv4 or ipv6.
	var getBits func(ss, sl int) uint32
	if ipp.IP().Is4() {
		ipv4 := ipp.IP().As4()
		getBits = func(ss, sl int) uint32 {
			return getBits4((w-ss)/8, sl, ipv4)
		}
	} else {
		ipv6 := ipp.IP().As16()
		getBits = func(ss, sl int) uint32 {
			return getBits16((w-ss)/8, sl, ipv6)
		}
	}

	ss := 0      // stride length summation
	var s uint32 // stride
	level := 0
	for {
		ss += sl[level]
		s = getBits(ss, sl[level])
		if int(ipp.Bits()) <= ss {
			break
		}
		i := fringeIndex(sl[level], s)
		isv[level] = i
		if x.n[i] == nil {
			return nil, false
		}
		xsv[level] = x
		x = x.n[i]
		level++
	}

	ss -= sl[level]
	r, ok = x.deleteSingle(sl[level], s, int(ipp.Bits())-ss)
	if !ok {
		return nil, false
	}

	// Free arrays if necessary, looking for 0 items with 0 references, and cleaning up pointers.
	x.ref--
	for level > 0 && x.ref == 0 {
		x.free()
		level--        // get parent level
		x = xsv[level] // get parent array pointer
		// child array is deleted, decrement reference
		x.ref--
	}

	return r, true
}
