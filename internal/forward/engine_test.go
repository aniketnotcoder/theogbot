package forward

import (
	"errors"
	"testing"
)

func TestIDsInRange(t *testing.T) {
	got, err := IDsInRange(10, 13, 10)
	if err != nil { t.Fatal(err) }
	want := []int{10, 11, 12, 13}
	if len(got) != len(want) { t.Fatalf("got %v", got) }
	for i := range want { if got[i] != want[i] { t.Fatalf("got %v, want %v", got, want) } }
}

func TestIDsInRangeRejectsInvalidOrLargeRange(t *testing.T) {
	for _, tc := range []struct{ first, last, max int }{
		{0, 3, 10}, {5, 4, 10}, {1, 20, 10},
	} {
		if _, err := IDsInRange(tc.first, tc.last, tc.max); err == nil {
			t.Errorf("IDsInRange(%d,%d,%d) expected error", tc.first, tc.last, tc.max)
		}
	}
	if !errors.Is(ErrInvalidMessageIDs, ErrInvalidMessageIDs) { t.Fatal("sentinel error mismatch") }
}

func TestRandomIDs(t *testing.T) {
	ids, err := randomIDs(256)
	if err != nil { t.Fatal(err) }
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] { t.Fatalf("invalid or duplicate random ID: %d", id) }
		seen[id] = true
	}
}

func TestFetchMessagesByIDRejectsInvalidInput(t *testing.T) {
	if _, err := FetchMessagesByID(nil, nil, nil, []int{1}); err == nil {
		t.Fatal("expected missing API and peer to be rejected")
	}
	if _, err := FetchMessagesByID(nil, nil, nil, nil); err == nil {
		t.Fatal("expected empty ID list to be rejected")
	}
}
