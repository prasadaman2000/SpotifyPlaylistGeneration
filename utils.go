package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/zmb3/spotify/v2"
)

var ErrPlaylistNotFound = errors.New("utils: playlist not found")

func CompleteAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	client := spotify.New(auth.Client(r.Context(), tok))
	fmt.Fprintf(w, "Login Completed!")
	ch <- client
}

func GetPlaylistsOwnedByUser(ctx context.Context, client *spotify.Client, userID string) ([]spotify.SimplePlaylist, error) {
	var playlists []spotify.SimplePlaylist
	playlistPage, err := client.CurrentUsersPlaylists(ctx)
	for {
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, playlist := range playlistPage.Playlists {
			if playlist.Owner.ID == userID {
				playlists = append(playlists, playlist)
			}
		}
		err = client.NextPage(ctx, playlistPage)
	}
	return playlists, nil
}

func GetItemsFromPlaylist(ctx context.Context, client *spotify.Client, playlistID spotify.ID) ([]spotify.PlaylistItem, error) {
	var items []spotify.PlaylistItem
	playlistItems, err := client.GetPlaylistItems(ctx, playlistID)
	for {
		if err == spotify.ErrNoMorePages {
			break
		}
		if err != nil {
			return nil, err
		}
		items = append(items, playlistItems.Items...)
		err = client.NextPage(ctx, playlistItems)
	}
	return items, nil
}

// gets all unique playlist items across all playlists for CurrentUser
// resolves conflicts by keeping the older track
func GetAllUniquePlaylistItems(ctx context.Context, client *spotify.Client) ([]spotify.PlaylistItem, error) {
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	playlists, err := GetPlaylistsOwnedByUser(ctx, client, user.ID)
	if err != nil {
		return nil, err
	}
	var masterPlaylistItemList []spotify.PlaylistItem
	for _, playlist := range playlists {
		itemsInPlaylist, err := GetItemsFromPlaylist(ctx, client, playlist.ID)
		if err != nil {
			log.Printf("could not get tracks from playlist %v: %v\n", playlist.Name, err)
			continue
		}
		masterPlaylistItemList = append(masterPlaylistItemList, itemsInPlaylist...)
	}
	masterItemMap := make(map[string]spotify.PlaylistItem, 0)
	for _, item := range masterPlaylistItemList {
		trackID := item.Track.Track.ID.String()
		oldItem, ok := masterItemMap[trackID]
		if !ok {
			masterItemMap[trackID] = item
		} else {
			oldTime, _ := time.Parse(time.RFC3339, oldItem.AddedAt)
			newTime, _ := time.Parse(time.RFC3339, item.AddedAt)
			if newTime.Before(oldTime) {
				masterItemMap[trackID] = item
			}
		}
	}
	var totalTrackList []spotify.PlaylistItem
	for _, v := range masterItemMap {
		totalTrackList = append(totalTrackList, v)
	}
	return totalTrackList, nil
}

// returns a playlist with a given name
// if not found, PlaylistNotFoundError is returned
func GetPlaylistByName(ctx context.Context, client *spotify.Client, toFind string) (spotify.SimplePlaylist, error) {
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return spotify.SimplePlaylist{}, err
	}
	playlists, err := GetPlaylistsOwnedByUser(ctx, client, user.ID)
	if err != nil {
		return spotify.SimplePlaylist{}, err
	}
	for _, playlist := range playlists {
		if playlist.Name == toFind {
			return playlist, nil
		}
	}
	return spotify.SimplePlaylist{}, ErrPlaylistNotFound
}

func IsTrackInPlaylist(ctx context.Context, client *spotify.Client, playlist spotify.SimplePlaylist, track *spotify.FullTrack) (bool, error) {
	playlistItems, err := GetItemsFromPlaylist(ctx, client, playlist.ID)
	if err != nil {
		return false, err
	}
	for _, item := range playlistItems {
		if item.Track.Track.ID == track.ID {
			return true, nil
		}
	}
	return false, nil
}

// adds tracks to a playlist. if a duplicate is found, do not add.
func AddTracksToPlaylist(ctx context.Context, client *spotify.Client, playlist spotify.SimplePlaylist, tracks []*spotify.FullTrack) error {
	var tracksToAdd []spotify.ID
	for _, track := range tracks {
		exists, err := IsTrackInPlaylist(ctx, client, playlist, track)
		if err != nil {
			return err
		}
		if !exists {
			tracksToAdd = append(tracksToAdd, track.ID)
		}
	}
	log.Printf("out of %v tracks, %v will be added to playlist %v", len(tracks), len(tracksToAdd), playlist.Name)
	if len(tracksToAdd) == 0 {
		return nil
	}
	idx := 0
	for idx = 0; idx < len(tracksToAdd); idx += 100 {
		_, err := client.AddTracksToPlaylist(ctx, playlist.ID, tracksToAdd[idx:int(math.Min(float64(len(tracksToAdd)), float64(idx+100)))]...)
		if err != nil {
			return err
		}
	}
	return nil
}

// returns a playlist with a given name
// if not found, creates a new playlist
func CreateOrGetPlaylistByName(ctx context.Context, client *spotify.Client, name string) (spotify.SimplePlaylist, error) {
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return spotify.SimplePlaylist{}, err
	}
	playlist, err := GetPlaylistByName(ctx, client, name)
	if err == ErrPlaylistNotFound {
		createdPlaylist, err := client.CreatePlaylistForUser(ctx, user.ID, name, "generated playlist", true, false)
		if err != nil {
			return spotify.SimplePlaylist{}, err
		}
		return createdPlaylist.SimplePlaylist, nil
	}
	if err != nil {
		return spotify.SimplePlaylist{}, err
	}
	return playlist, nil
}
