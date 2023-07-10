package main

import (
	"context"
	"time"

	"github.com/zmb3/spotify/v2"
)

func MonthlyPlaylists(ctx context.Context, client *spotify.Client) (map[string][]*spotify.FullTrack, error) {
	totalTracks, err := GetAllUniquePlaylistItems(ctx, client)
	if err != nil {
		return nil, err
	}

	genPlaylists := make(map[string][]*spotify.FullTrack, 0)

	for _, track := range totalTracks {
		time, err := time.Parse(time.RFC3339, track.AddedAt)
		if err != nil {
			return nil, err
		}
		genPlaylists[time.Month().String()] = append(genPlaylists[time.Month().String()], track.Track.Track)
	}

	return genPlaylists, nil
}
