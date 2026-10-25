package discover

import (
	"context"
	"fmt"

	"bingebandit/internal/store"
)

// Refresh fetches one source and stores its videos, or the error.
func Refresh(ctx context.Context, st *store.Store, src *store.DiscoverSource) error {
	f, err := Fetch(ctx, Source{Kind: src.Kind, ID: src.YouTubeID})
	if err != nil {
		if ctx.Err() == nil {
			st.SaveDiscoverVideos(src.ID, nil, err.Error())
		}
		return err
	}
	videos := make([]store.DiscoverVideo, 0, len(f.Videos))
	for _, v := range f.Videos {
		videos = append(videos, store.DiscoverVideo{ID: v.ID, Title: v.Title, Description: v.Description, PublishedAt: v.Published.Unix()})
	}
	return st.SaveDiscoverVideos(src.ID, videos, "")
}

// RefreshAll refreshes every source, carrying on past failures.
func RefreshAll(ctx context.Context, st *store.Store) (string, error) {
	srcs, err := st.DiscoverSources()
	if err != nil {
		return "", err
	}
	failed := 0
	for _, src := range srcs {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if Refresh(ctx, st, src) != nil {
			failed++
		}
	}
	if failed > 0 {
		return "", fmt.Errorf("%d of %d sources failed; see Discover → Manage", failed, len(srcs))
	}
	return fmt.Sprintf("%d sources refreshed", len(srcs)), nil
}
