package providers

import (
	"strconv"
	"testing"
	"time"
)

func TestMBMemo_CapBoundsDistinctKeys(t *testing.T) {
	c := newMBMemo[int](time.Hour)
	for i := 0; i < mbMemoMaxEntries*3; i++ {
		c.put("artist-"+strconv.Itoa(i), i)
	}
	if got := c.len(); got > mbMemoMaxEntries {
		t.Fatalf("memo holds %d entries, want <= %d", got, mbMemoMaxEntries)
	}
	last := mbMemoMaxEntries*3 - 1
	if v, ok := c.get("artist-" + strconv.Itoa(last)); !ok || v != last {
		t.Fatalf("newest entry missing: got %d, %v", v, ok)
	}
	if _, ok := c.get("artist-0"); ok {
		t.Fatal("oldest entry should have been evicted")
	}
}

func TestMBMemo_EvictsLeastRecentlyUsed(t *testing.T) {
	c := newMBMemoCap[string](time.Hour, 2)
	c.put("a", "A")
	c.put("b", "B")
	if _, ok := c.get("a"); !ok {
		t.Fatal("a should be present")
	}
	c.put("c", "C")
	if _, ok := c.get("b"); ok {
		t.Fatal("b should have been evicted as least recently used")
	}
	if v, ok := c.get("a"); !ok || v != "A" {
		t.Fatalf("a should survive: %q %v", v, ok)
	}
	if v, ok := c.get("c"); !ok || v != "C" {
		t.Fatalf("c should be present: %q %v", v, ok)
	}
	if c.len() != 2 {
		t.Fatalf("len = %d, want 2", c.len())
	}
}

func TestMBMemo_ReputDoesNotGrow(t *testing.T) {
	c := newMBMemoCap[int](time.Hour, 2)
	for i := 0; i < 10; i++ {
		c.put("same", i)
	}
	if c.len() != 1 {
		t.Fatalf("len = %d, want 1", c.len())
	}
	if v, _ := c.get("same"); v != 9 {
		t.Fatalf("value = %d, want 9", v)
	}
}

func TestMBMemo_ExpiredEntriesAreRemoved(t *testing.T) {
	c := newMBMemoCap[int](time.Millisecond, 50)
	for i := 0; i < 50; i++ {
		c.put("old-"+strconv.Itoa(i), i)
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.get("old-0"); ok {
		t.Fatal("expired entry returned")
	}
	if c.len() != 49 {
		t.Fatalf("len after expired read = %d, want 49", c.len())
	}
	c.put("old-0", 0)
	time.Sleep(5 * time.Millisecond)
	c.put("fresh", 1)
	if c.len() != 1 {
		t.Fatalf("len after put = %d, want 1 (expired purged)", c.len())
	}
}
