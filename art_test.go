/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2020 Tailscale Inc. All Rights Reserved.
 */

package art

import (
	"math/rand"
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

func (r route4b) RouteParams() RouteParams {
	return RouteParams{
		Width: 4,
		Addr:  uint64(r.a),
		Len:   int(r.l),
	}
}

func newSingleLevelTestTable() *Table {
	x := NewTable(4)
	x.w = 4
	return x
}

var _ Route = route4b{}

func TestInsertSingleLevel(t *testing.T) {
	x := newSingleLevelTestTable()

	// Figure 3-1.
	r1 := route4b{12, 2}
	if !x.insertSingleLevel(r1) {
		t.Errorf("insert %v failed", r1)
	}
	want := newSingleLevelTestTable()
	for _, i := range []int{7, 14, 15, 28, 29, 30, 31} {
		want.r[i] = r1
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 1st step\n got: %v\nwant: %v\n", x, want)
	}

	// Figure 3-2. ("Now assume we insert a route to prefix 14/3")
	r2 := route4b{14, 3}
	if !x.insertSingleLevel(r2) {
		t.Errorf("insert %v failed", r2)
	}
	for _, i := range []int{15, 30, 31} {
		want.r[i] = r2
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 2nd step\n got: %v\nwant: %v\n", x, want)
	}

	// Figure 3-3. ("Now assume we insert a route to prefix 8/1")
	r3 := route4b{8, 1}
	if !x.insertSingleLevel(r3) {
		t.Errorf("insert %v failed", r3)
	}
	for _, i := range []int{3, 6, 12, 13, 24, 25, 26, 27} {
		want.r[i] = r3
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("wrong after 3rd step\n got: %v\nwant: %v\n", x, want)
	}
}

// testTable returns the example table set up before section 2.1.2 of the paper.
func testTable() *Table {
	x := newSingleLevelTestTable()
	x.insertSingleLevel(route4b{12, 2})
	x.insertSingleLevel(route4b{14, 3})
	x.insertSingleLevel(route4b{8, 1})
	return x
}

func TestLookupSingleLevel(t *testing.T) {
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
		got, _ := x.lookupSingleLevel(tt.addr)
		if got != tt.want {
			t.Errorf("lookup(addr=%v) = %v; want %v", tt.addr, got, tt.want)
		}
	}
}

func TestDeleteSingleLevel(t *testing.T) {
	x := testTable()
	old, ok := x.deleteSingleLevel(RouteParams{Width: 4, Addr: 12, Len: 2})
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
	old, ok = x.deleteSingleLevel(RouteParams{Width: 4, Addr: 8, Len: 1})
	if !ok {
		t.Fatal("didn't delete")
	}
	if want := (route4b{8, 1}); old != want {
		t.Fatalf("deleted %v; want %v", old, want)
	}
	want := testTable()
	want.r = []Route{
		7:  route4b{12, 2},
		14: route4b{12, 2},
		28: route4b{12, 2},
		29: route4b{12, 2},
		15: route4b{14, 3},
		30: route4b{14, 3},
		31: route4b{14, 3},
	}
	if !reflect.DeepEqual(x, want) {
		t.Errorf("not like Figure 3-2:\n got: %v\nwant: %v\n", x, want)
	}
}

func newIPv4Table_8() *Table {
	t := NewTable(8)
	t.sl = []int{8, 8, 8, 8}
	t.w = 32
	return t
}

func newIPv4Table_16_8() *Table {
	t := NewTable(16)
	t.sl = []int{16, 8, 8}
	t.w = 32
	return t
}

type testRoute struct {
	rp  RouteParams
	val interface{}
}

func (tr testRoute) RouteParams() RouteParams { return tr.rp }

func genTestRoutes(width, num int) []Route {
	var routes []Route
	rand.Seed(1)
	dup := map[RouteParams]bool{}
	for i := 0; i < num; i++ {
		var rp RouteParams
		for {
			rp = RouteParams{
				Width: width,
				Len:   rand.Intn(width + 1),
			}
			for pl := 0; pl < rp.Len; pl++ {
				rp.Addr |= uint64(rand.Intn(2)) << ((width - 1) - pl)
			}
			if !dup[rp] {
				dup[rp] = true
				break
			}
		}
		routes = append(routes, testRoute{rp, i})
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
			preInsert := x.clone()
			if !x.insertSingleLevel(r) {
				t.Fatalf("failed to insert %d, %+v", i, r)
			}
			rp := r.RouteParams()
			del, ok := x.deleteSingleLevel(rp)
			if !ok {
				t.Fatalf("failed to delete %d, %+v", i, rp)
			}
			if del != r {
				t.Fatalf("delete of %d deleted %v, want %v", i, del, r)
			}
			if !reflect.DeepEqual(x, preInsert) {
				t.Fatalf("delete of %d (%+v) didn't return table to prior state\n now: %v\n was: %v\n", i, rp, x, preInsert)
			}
			if !x.insertSingleLevel(r) {
				t.Fatalf("failed to re-insert %d, %+v", i, r)
			}
		}
	}
}

func TestMultiIPv4_stride8(t *testing.T) {
	testMultiIPv4(t, newIPv4Table_8)
}

func TestMultiIPv4_stride16_8(t *testing.T) {
	testMultiIPv4(t, newIPv4Table_16_8)
}

func testMultiIPv4(t *testing.T, newTable func() *Table) {
	routes := genTestRoutes(32, 100)
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
			rp := r.RouteParams()
			gotBefore, _ := x.Lookup(rp.Addr)

			preInsert := x.clone()
			if !x.Insert(r) {
				t.Fatalf("failed to insert %d, %+v", i, r)
			}

			got, ok := x.Lookup(rp.Addr)
			if !ok {
				t.Fatalf("i=%d; Lookup(%d) failed (%+v)", i, rp.Addr, rp)
			}

			want := r
			if gotBefore != nil && gotBefore.(testRoute).rp.Len > rp.Len {
				want = gotBefore
			}
			if got != want {
				t.Fatalf("i=%d; Lookup(%d) got %v; want %v", i, rp.Addr, got, want)
			}

			del, ok := x.Delete(rp)
			if !ok {
				t.Fatalf("failed to delete %d, %+v", i, rp)
			}
			if del != r {
				t.Fatalf("delete of %d deleted %v, want %v", i, del, r)
			}
			if !reflect.DeepEqual(x, preInsert) {
				t.Fatalf("delete of %d (%+v) didn't return table to prior state\n now: %v\n was: %v\n", i, rp, x, preInsert)
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
		if _, ok := t.Delete(v.RouteParams()); !ok {
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
		if _, ok := t.Lookup(v.RouteParams().Addr); !ok {
			b.Error("Lookup failed")
		}
	}
}
