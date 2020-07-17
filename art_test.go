/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2020 Tailscale Inc. All Rights Reserved.
 */

package art

import (
	"reflect"
	"testing"
)

func TestBaseIndex(t *testing.T) {
	tests := []struct {
		w    int
		a    uint64
		l    int
		want uint64
	}{
		{4, 0, 0, 1},
		{4, 0, 1, 2},
		{4, 8, 1, 3},
		{4, 0, 2, 4},
		{4, 4, 2, 5},
		{4, 8, 2, 6},
		{4, 12, 2, 7},
		{4, 0, 3, 8},
		{4, 2, 3, 9},
		{4, 4, 3, 10},
		// ...
		{4, 14, 4, 30},
		{4, 15, 4, 31},
	}
	for _, tt := range tests {
		if got := baseIndex(tt.w, tt.a, tt.l); got != tt.want {
			t.Errorf("baseIndex(%v, %v, %v) = %v; want %v", tt.w, tt.a, tt.l, got, tt.want)
		}
	}
}

// route4b is a 4-bit route as used in the paper examples.
type route4b struct {
	a uint8 // addr
	l uint8 // prefix len
}

func (r route4b) Addr() uint64   { return uint64(r.a) }
func (r route4b) PrefixLen() int { return int(r.l) }
func (r route4b) Width() int     { return 4 }

var _ Route = route4b{}

func TestInsertSingleLevel(t *testing.T) {
	x := make(Table, 32)

	// Figure 3-1.
	r1 := route4b{12, 2}
	if !x.InsertSingleLevel(r1) {
		t.Errorf("insert %v failed", r1)
	}
	want := make(Table, 32)
	for _, i := range []int{7, 14, 15, 28, 29, 30, 31} {
		want[i] = r1
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 1st step\n got: %v\nwant: %v\n", x, want)
	}

	// Figure 3-2. ("Now assume we insert a route to prefix 14/3")
	r2 := route4b{14, 3}
	if !x.InsertSingleLevel(r2) {
		t.Errorf("insert %v failed", r2)
	}
	for _, i := range []int{15, 30, 31} {
		want[i] = r2
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 2nd step\n got: %v\nwant: %v\n", x, want)
	}

	// Figure 3-3. ("Now assume we insert a route to prefix 8/1")
	r3 := route4b{8, 1}
	if !x.InsertSingleLevel(r3) {
		t.Errorf("insert %v failed", r3)
	}
	for _, i := range []int{3, 6, 12, 13, 24, 25, 26, 27} {
		want[i] = r3
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 3rd step\n got: %v\nwant: %v\n", x, want)
	}
}

// testTable returns the example table set up before section 2.1.2 of the paper.
func testTable() Table {
	x := make(Table, 32)
	x.InsertSingleLevel(route4b{12, 2})
	x.InsertSingleLevel(route4b{14, 3})
	x.InsertSingleLevel(route4b{8, 1})
	return x
}

func TestLookup(t *testing.T) {
	x := testTable()
	for _, tt := range []struct {
		addr uint64
		want Route
	}{
		{0, nil},
		{1, nil},
		// ...
		{6, nil},
		{7, nil},
		{8, route4b{8, 1}},
		{9, route4b{8, 1}},
		{10, route4b{8, 1}},
		{11, route4b{8, 1}},
		{12, route4b{12, 2}},
		{13, route4b{12, 2}},
		{14, route4b{14, 3}},
		{15, route4b{14, 3}},
	} {
		got, _ := x.LookupSingleLevel(4, tt.addr)
		if got != tt.want {
			t.Errorf("lookup(addr=%v) = %v; want %v", tt.addr, got, tt.want)
		}
	}
}
