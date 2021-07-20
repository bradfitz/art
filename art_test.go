/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2020 Tailscale Inc. All Rights Reserved.
 */

package art

import (
	"math/rand"
	"net"
	"reflect"
	"testing"

	"inet.af/netaddr"
)

func TestBaseIndex(t *testing.T) {
	tests := []struct {
		w    int
		a    uint32
		l    int
		want uint32
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
		{4, 6, 3, 11},
		{4, 8, 3, 12},
		// ...
		{4, 14, 3, 15},
		{4, 0, 4, 16},
		{4, 1, 4, 17},
		// ...
		{4, 14, 4, 30},
		{4, 15, 4, 31},
	}
	for _, tt := range tests {
		if got := baseIndex(tt.w, tt.a, tt.l); got != tt.want {
			t.Errorf("baseIndex(%v, %v, %v) = %v; want %v", tt.w, tt.a, tt.l, got, tt.want)
		} else {
			t.Logf("%2d %04b(%d)/%d", tt.want, tt.a, tt.a, tt.l)
		}
	}
}

// route4b is a 4-bit route as used in the paper examples.
type route4b struct {
	a uint8 // addr
	l uint8 // prefix len
}

func (r route4b) IPPrefix() netaddr.IPPrefix {
	return netaddr.IPPrefixFrom(
		netaddr.IPFrom4([4]byte{0, 0, 0, r.a}),
		r.l,
	)
}

func (r route4b) Equals(other Route) bool {
	r4b, ok := other.(route4b)
	return ok && r == r4b
}

func newSingleLevelTestTable() *Table {
	return NewTable([]int{4})
}

var _ Route = route4b{}

func TestInsertSingleLevel(t *testing.T) {
	x := newSingleLevelTestTable()

	// Figure 3-1.
	r1 := route4b{12, 2}
	if !x.Insert(r1) {
		t.Errorf("insert %v failed", r1)
	}
	want := newSingleLevelTestTable()
	want.root.ref++
	for _, i := range []int{7, 14, 15, 28, 29, 30, 31} {
		want.root.r[i] = r1
	}
	if !reflect.DeepEqual(x.root, want.root) {
		t.Errorf("wrong after 1st step\n got: %v\nwant: %v\n", x.root, want.root)
	}

	// Figure 3-2. ("Now assume we insert a route to prefix 14/3")
	r2 := route4b{14, 3}
	if !x.Insert(r2) {
		t.Errorf("insert %v failed", r2)
	}
	for _, i := range []int{15, 30, 31} {
		want.root.r[i] = r2
	}
	want.root.ref++
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 2nd step\n got: %v\nwant: %v\n", x, want)
	}

	// Figure 3-3. ("Now assume we insert a route to prefix 8/1")
	r3 := route4b{8, 1}
	if !x.Insert(r3) {
		t.Errorf("insert %v failed", r3)
	}
	want.root.ref++
	for _, i := range []int{3, 6, 12, 13, 24, 25, 26, 27} {
		want.root.r[i] = r3
	}
	if !reflect.DeepEqual(x.root, want.root) {
		t.Errorf("wrong after 3rd step\n got: %v\nwant: %v\n", x.root, want.root)
	}
}

// testTable returns the example table set up before section 2.1.2 of the paper.
func testTable() *Table {
	x := newSingleLevelTestTable()
	x.Insert(route4b{12, 2})
	x.Insert(route4b{14, 3})
	x.Insert(route4b{8, 1})
	return x
}

func TestLookupSingleLevel(t *testing.T) {
	x := testTable()
	for _, tt := range []struct {
		addr uint32
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
		got, _ := x.root.lookupSingle(4, tt.addr)
		if got != tt.want {
			t.Errorf("lookup(addr=%v) = %v; want %v", tt.addr, got, tt.want)
		}
	}
}

func TestDeleteSingleLevel(t *testing.T) {
	x := testTable()
	old, ok := x.Delete(netaddr.MustParseIPPrefix("0.0.0.12/2"))
	if !ok {
		t.Fatal("didn't delete")
	}
	if want := (route4b{12, 2}); old != want {
		t.Fatalf("deleted %v; want %v", old, want)
	}

	// Note: the paper seems to have a mistake. 2.1.3. ends with
	// "After the route to 12/2 is deleted, the ART returns to
	// Figure 3-2", but none of Figures 3-1, 3-2, 3-3 have just
	// 8/1 and 14/3 in them. Instead, do what the paper probably
	// meant to get back to figure 3-2:
	x = testTable()
	old, ok = x.Delete(netaddr.MustParseIPPrefix("0.0.0.8/1"))
	if !ok {
		t.Fatal("didn't delete")
	}
	if want := (route4b{8, 1}); old != want {
		t.Fatalf("deleted %v; want %v", old, want)
	}
	want := testTable()
	want.root.r = []Route{
		7:  route4b{12, 2},
		14: route4b{12, 2},
		28: route4b{12, 2},
		29: route4b{12, 2},
		15: route4b{14, 3},
		30: route4b{14, 3},
		31: route4b{14, 3},
	}
	want.root.ref--
	if !reflect.DeepEqual(x, want) {
		t.Errorf("not like Figure 3-2:\n got: %v\nwant: %v\n", x.root, want.root)
	}
}

func newIPv4Table_8() *Table {
	return NewTable([]int{8, 8, 8, 8})
}

func newIPv4Table_16_8() *Table {
	return NewTable([]int{16, 8, 8})
}

type testRoute struct {
	ipp netaddr.IPPrefix
	val interface{}
}

func (tr testRoute) IPPrefix() netaddr.IPPrefix { return tr.ipp }

func (tr testRoute) Equals(r Route) bool {
	tr2, ok := r.(testRoute)
	return ok && tr2.val == tr.val && tr.ipp == tr2.ipp
}

func genTestRoutes(width, num int) []Route {
	var routes []Route
	rand.Seed(1)
	ipps := map[netaddr.IPPrefix]bool{}
	bytesPer := 16
	if width <= 32 {
		bytesPer = 4
	}
	for i := 0; i < num; i++ {
		var ipp netaddr.IPPrefix
		for {
			length := uint8(rand.Intn(width + 1))
			addr := make([]byte, bytesPer)
			for pl := 0; pl < int(length); pl++ {
				addr[pl/8] |= byte(rand.Intn(2)) << (pl % 8)
			}
			ip, ok := netaddr.FromStdIP(net.IP(addr))
			if !ok {
				panic("Failed to created IP")
			}
			ipp = netaddr.IPPrefixFrom(ip, length)
			if seen := ipps[ipp]; seen {
				continue
			}
			ipps[ipp] = true
			break
		}
		routes = append(routes, testRoute{ipp, i})
	}
	return routes
}

func TestInsertDeleteSingle4bit(t *testing.T) {
	routes := genTestRoutes(4, 20)
	for i := 0; i < 2000; i++ {
		rand.Shuffle(len(routes), func(i, j int) {
			routes[i], routes[j] = routes[j], routes[i]
		})
		x := newSingleLevelTestTable()
		for i, r := range routes {
			preInsert := x.Clone()
			if !x.Insert(r) {
				t.Fatalf("failed to insert %d, %+v", i, r)
			}
			ipp := r.IPPrefix()
			del, ok := x.Delete(ipp)
			if !ok {
				t.Fatalf("failed to delete %d, %+v", i, ipp)
			}
			if !del.Equals(r) {
				t.Fatalf("delete of %d deleted %v, want %v", i, del, r)
			}
			if !reflect.DeepEqual(x, preInsert) {
				t.Fatalf("delete of %d (%+v) didn't return table to prior state\n now: %v\n was: %v\n", i, ipp, x, preInsert)
			}
			if !x.Insert(r) {
				t.Fatalf("failed to re-insert %d, %+v", i, r)
			}
		}
	}
}

func TestMultiIPv4_stride8(t *testing.T) {
	testMulti(t, newIPv4Table_8, 32)
}

func TestMultiIPv4_stride16_8(t *testing.T) {
	testMulti(t, newIPv4Table_16_8, 32)
}

func TestMultiIPv6_stride8(t *testing.T) {
	testMulti(t, func() *Table {
		return NewTable([]int{
			8, 8, 8, 8,
			8, 8, 8, 8,
			8, 8, 8, 8,
			8, 8, 8, 8,
		})
	}, 128)
}

func testMulti(t *testing.T, newTable func() *Table, width int) {
	routes := genTestRoutes(width, 100)
	numShuffle := 10
	if testing.Short() {
		numShuffle = 2
	}

	for i := 0; i < numShuffle; i++ {
		rand.Shuffle(len(routes), func(i, j int) {
			routes[i], routes[j] = routes[j], routes[i]
		})
		x := newTable()
		for i, r := range routes {
			ipp := r.IPPrefix()
			gotBefore, _ := x.Lookup(ipp.IP())

			preInsert := x.Clone()
			if !x.Insert(r) {
				t.Fatalf("failed to insert %d, %+v", i, r)
			}

			got, ok := x.Lookup(ipp.IP())
			if !ok {
				t.Fatalf("i=%d; Lookup(%v) failed (%+v)", i, ipp.IP(), ipp)
			}

			want := r
			if gotBefore != nil && gotBefore.(testRoute).ipp.Bits() > ipp.Bits() {
				want = gotBefore
			}
			if !got.Equals(want) {
				t.Fatalf("i=%d; Lookup(%v) got %v; want %v", i, ipp.IP(), got, want)
			}

			del, ok := x.Delete(ipp)
			if !ok {
				t.Fatalf("failed to delete %d, %+v", i, ipp)
			}
			if !del.Equals(r) {
				t.Fatalf("delete of %d deleted %v, want %v", i, del, r)
			}
			if !reflect.DeepEqual(x, preInsert) {
				t.Fatalf("delete of %d (%+v) didn't return table to prior state\n now: %v\n was: %v\n", i, ipp, x, preInsert)
			}
			if !x.Insert(r) {
				t.Fatalf("failed to re-insert %d, %+v", i, r)
			}
		}
	}
}

func benchInsertRemoveIPv4(b *testing.B, newTable func() *Table) {
	t := newTable()
	b.ReportAllocs()
	uniq := 100
	routes := genTestRoutes(32, uniq)
	for i := 0; i < b.N; i++ {
		v := routes[i%uniq]
		if !t.Insert(v) {
			b.Error("Insertion failed")
		}
		if _, ok := t.Delete(v.IPPrefix()); !ok {
			b.Error("Removal failed")
		}
	}
}

func BenchmarkMultiIPv4_stride8(b *testing.B) {
	b.Run("InsertRemove", func(b *testing.B) {
		benchInsertRemoveIPv4(b, newIPv4Table_8)
	})
	b.Run("Search", func(b *testing.B) {
		benchSearchIPv4(b, newIPv4Table_8)
	})
}

func BenchmarkMultiIPv4_stride16_8(b *testing.B) {
	b.Run("InsertRemove", func(b *testing.B) {
		benchInsertRemoveIPv4(b, newIPv4Table_16_8)
	})
	b.Run("Search", func(b *testing.B) {
		benchSearchIPv4(b, newIPv4Table_16_8)
	})
}

func benchSearchIPv4(b *testing.B, newTable func() *Table) {
	t := newTable()
	uniq := 100
	routes := genTestRoutes(32, 100)
	for _, route := range routes {
		if !t.Insert(route) {
			b.Error("Insertion failed")
		}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v := routes[i%uniq]
		if _, ok := t.Lookup(v.IPPrefix().IP()); !ok {
			b.Error("Lookup failed")
		}
	}
}
