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

const playlistAddBatchSize = 100

var (
	ErrPlaylistNotFound  = errors.New("utils: playlist not found")
	playlistItemMapCache = make(map[spotify.ID]map[spotify.ID]bool)
	artistGenreMapCache  = make(map[spotify.ID][]string)
)

type PlaylistGensResponse struct {
	playlists map[string][]*spotify.FullTrack
}

type PlaylistGenRequest struct {
	numPlaylists int
	bannedGenres []string
}

func Intersection(list1 []string, list2 []string) []string {
	var intersection []string
	map1 := make(map[string]bool, 0)
	for _, item := range list1 {
		map1[item] = true
	}
	for _, item := range list2 {
		if _, ok := map1[item]; ok {
			intersection = append(intersection, item)
		}
	}
	return intersection
}

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

// cached lookup of set of track ids found in a playlist. useful for functions that need
// to verify playlist membership many times (such as InTrackInPlaylist)
func GetTrackIDSetFromPlaylist(ctx context.Context, client *spotify.Client, playlist spotify.SimplePlaylist) (map[spotify.ID]bool, error) {
	trackSet, ok := playlistItemMapCache[playlist.ID]
	if !ok {
		playlistItems, err := GetItemsFromPlaylist(ctx, client, playlist.ID)
		if err != nil {
			return nil, err
		}
		trackSet = make(map[spotify.ID]bool, 0)
		for _, item := range playlistItems {
			trackSet[item.Track.Track.ID] = true
		}
		playlistItemMapCache[playlist.ID] = trackSet
	}
	return trackSet, nil
}

func IsTrackInPlaylist(ctx context.Context, client *spotify.Client, playlist spotify.SimplePlaylist, track *spotify.FullTrack) (bool, error) {
	trackSet, err := GetTrackIDSetFromPlaylist(ctx, client, playlist)
	if err != nil {
		return false, err
	}
	_, ok := trackSet[track.ID]
	return ok, nil
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
	// batched add because spotify doesn't support adding more than 100 tracks at a time
	for idx = 0; idx < len(tracksToAdd); idx += playlistAddBatchSize {
		_, err := client.AddTracksToPlaylist(ctx, playlist.ID, tracksToAdd[idx:int(math.Min(float64(len(tracksToAdd)), float64(idx+playlistAddBatchSize)))]...)
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

// cached lookup of genres from artist. cached to improve performance
// in situations where many calls to GetGenresFromArtist are made
// in quick succession and with the same params
func GetGenresFromArtist(ctx context.Context, client *spotify.Client, artistID spotify.ID) ([]string, error) {
	if genres, ok := artistGenreMapCache[artistID]; ok {
		return genres, nil
	}
	fullArtist, err := client.GetArtist(ctx, artistID)
	if err != nil {
		return nil, err
	}
	artistGenreMapCache[artistID] = append(artistGenreMapCache[artistID], fullArtist.Genres...)
	return artistGenreMapCache[artistID], nil
}

func GetGenresFromTrack(ctx context.Context, client *spotify.Client, track *spotify.FullTrack) ([]string, error) {
	genreSet := make(map[string]bool)
	for _, artist := range track.Artists {
		artistGenres, err := GetGenresFromArtist(ctx, client, artist.ID)
		if err != nil {
			return nil, err
		}
		for _, genre := range artistGenres {
			genreSet[genre] = true
		}
	}
	var genres []string
	for k := range genreSet {
		genres = append(genres, k)
	}
	return genres, nil
}
