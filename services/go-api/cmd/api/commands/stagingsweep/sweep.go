package stagingsweep

import (
	"altune/go-api/internal/library"
	"context"
	"fmt"
	"time"
)

const (
	minAge    = time.Hour
	refPrefix = "staging/"
)

type Tally struct {
	Deleted        int
	KeptReferenced int
	KeptFresh      int
	Failed         int
}

func Sweep(ctx context.Context, store library.AudioStore, lister library.AudioAgeLister, refs library.AudioRefPrefixLister, execute bool, now time.Time) (Tally, error) {
	objects, err := lister.ListWithAge(ctx, refPrefix)
	if err != nil {
		return Tally{}, fmt.Errorf("list staging objects: %w", err)
	}

	referenced, err := refs.AudioRefsUnderPrefix(ctx, refPrefix)
	if err != nil {
		return Tally{}, fmt.Errorf("query referenced staging refs: %w", err)
	}

	fmt.Printf("\nFound %d object(s) under %q.\n\n", len(objects), refPrefix)

	return sweepObjects(ctx, store, objects, referenced, execute, now), nil
}

func sweepObjects(ctx context.Context, store library.AudioStore, objects []library.ObjectAge, referenced map[string]bool, execute bool, now time.Time) Tally {
	var tally Tally
	for _, obj := range objects {
		age := now.Sub(obj.LastModified)
		if referenced[obj.AudioRef] {
			tally.KeptReferenced++
			continue
		}
		if age < minAge {
			tally.KeptFresh++
			continue
		}
		if !sweepObject(ctx, store, obj, age, execute) {
			tally.Failed++
			continue
		}
		tally.Deleted++
	}
	return tally
}

func sweepObject(ctx context.Context, store library.AudioStore, obj library.ObjectAge, age time.Duration, execute bool) bool {
	fmt.Printf("  %s (unreferenced, %s old)\n", obj.AudioRef, age.Round(time.Second))
	if !execute {
		return true
	}
	if err := store.Delete(ctx, obj.AudioRef); err != nil {
		fmt.Printf("    FAILED: %v\n", err)
		return false
	}
	return true
}
