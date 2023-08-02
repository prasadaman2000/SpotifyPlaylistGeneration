package main

import (
	"context"
	"fmt"
	"time"

	"github.com/zmb3/spotify/v2"
)

func MonthlyPlaylists(ctx context.Context, client *spotify.Client, _ *PlaylistGenRequest) (*PlaylistGensResponse, error) {
	totalTracks, err := GetAllUniquePlaylistItems(ctx, client)
	if err != nil {
		return nil, err
	}
	genPlaylists := make(map[string][]*spotify.FullTrack, 0)
	for _, track := range totalTracks {
		addTime, err := time.Parse(time.RFC3339, track.AddedAt)
		if err != nil {
			return nil, err
		}
		genPlaylists[addTime.Month().String()] = append(genPlaylists[addTime.Month().String()], track.Track.Track)
	}
	return &PlaylistGensResponse{
		playlists: genPlaylists,
	}, nil
}

func DailyPlaylists(ctx context.Context, client *spotify.Client, _ *PlaylistGenRequest) (*PlaylistGensResponse, error) {
	location, _ := time.LoadLocation("America/Los_Angeles")
	totalTracks, err := GetAllUniquePlaylistItems(ctx, client)
	if err != nil {
		return nil, err
	}
	genPlaylists := make(map[string][]*spotify.FullTrack, 0)
	for _, track := range totalTracks {
		addTime, err := time.Parse(time.RFC3339, track.AddedAt)
		localAddTime := addTime.In(location)
		if err != nil {
			return nil, err
		}
		genPlaylists[localAddTime.Weekday().String()] = append(genPlaylists[localAddTime.Weekday().String()], track.Track.Track)
	}
	return &PlaylistGensResponse{
		playlists: genPlaylists,
	}, nil
}

// generates playlists for top k genres in your library.
// Supported options:
// 		numPlaylists: k, the number of playlists to generate
func PlaylistsByGenre(ctx context.Context, client *spotify.Client, req *PlaylistGenRequest) (*PlaylistGensResponse, error) {
	totalTracks, err := GetAllUniquePlaylistItems(ctx, client)
	if err != nil {
		return nil, err
	}
	tracksInGenreMap := make(map[string][]*spotify.FullTrack)
	for _, track := range totalTracks {
		trackGenres, err := GetGenresFromTrack(ctx, client, track.Track.Track)
		if err != nil {
			return nil, err
		}
		for _, genre := range trackGenres {
			tracksInGenreMap[genre] = append(tracksInGenreMap[genre], track.Track.Track)
		}
	}
	genrePQ := NewPriorityQueue()
	for genre, tracks := range tracksInGenreMap {
		genrePQ.Push(genre, len(tracks))
	}
	fmt.Println("state of the PQ: ")
	for _, entry := range genrePQ.elems {
		fmt.Printf("entry: %v priority: %v\n", entry.data, entry.priority)
	}
	genreMapToRet := make(map[string][]*spotify.FullTrack)
	for i := 0; i < req.numPlaylists; i++ {
		poppedGenre, err := genrePQ.Pop()
		if err == ErrPriorityQueueEmpty {
			break
		}
		genreMapToRet[poppedGenre] = tracksInGenreMap[poppedGenre]
	}
	return &PlaylistGensResponse{
		playlists: genreMapToRet,
	}, nil
}

// generates playlists for top k artists in your library.
// Supported options:
// 		numPlaylists: k, the number of playlists to generate
//		disallowedGenres: list of genres to not include when generating playlists
func PlaylistsByArtist(ctx context.Context, client *spotify.Client, req *PlaylistGenRequest) (*PlaylistGensResponse, error) {
	totalTracks, err := GetAllUniquePlaylistItems(ctx, client)
	if err != nil {
		return nil, err
	}
	tracksInArtistMap := make(map[string][]*spotify.FullTrack)
	for _, track := range totalTracks {
		artists := track.Track.Track.Artists
		for _, artist := range artists {
			genres, err := GetGenresFromArtist(ctx, client, artist.ID)
			if err != nil {
				return nil, err
			}
			if len(Intersection(req.bannedGenres, genres)) > 0 {
				continue
			}
			tracksInArtistMap[artist.Name] = append(tracksInArtistMap[artist.Name], track.Track.Track)
		}
	}
	artistPQ := NewPriorityQueue()
	for artistName, tracks := range tracksInArtistMap {
		artistPQ.Push(artistName, len(tracks))
	}
	fmt.Println("state of the PQ: ")
	for _, entry := range artistPQ.elems {
		fmt.Printf("entry: %v priority: %v\n", entry.data, entry.priority)
	}
	artistMapToRet := make(map[string][]*spotify.FullTrack)
	for i := 0; i < req.numPlaylists; i++ {
		poppedGenre, err := artistPQ.Pop()
		if err == ErrPriorityQueueEmpty {
			break
		}
		artistMapToRet[poppedGenre] = tracksInArtistMap[poppedGenre]
	}
	return &PlaylistGensResponse{
		playlists: artistMapToRet,
	}, nil
}

// for each playlist P_i in the set of n input playlists P, and output playlist O
// loop through all P_i to find songs in P but not in O, and return a playlist O*
// that has all the songs in O and in (P - O).
// Supported options:
// 		inputPlaylists: the set P defined above. not all playlists have to exist.
//		outputPlaylist: the playlist O defined above, could possibly not exist yet
func CombineIntoPlaylist(ctx context.Context, client *spotify.Client, req *PlaylistGenRequest) (*PlaylistGensResponse, error) {
	const batchSize = 50
	masterPlaylist, err := GetPlaylistByName(ctx, client, req.outputPlaylist)
	if err != nil && err != ErrPlaylistNotFound {
		return nil, err
	}
	outputTrackIDMap := make(map[spotify.ID]bool)
	if err == nil {
		outputTrackIDMap, err = GetTrackIDSetFromPlaylist(ctx, client, masterPlaylist)
		if err != nil {
			return nil, err
		}
	}
	for _, playlistName := range req.inputPlaylists {
		currPlaylist, err := GetPlaylistByName(ctx, client, playlistName)
		if err == ErrPlaylistNotFound {
			fmt.Printf("could not find playlist %v, continuing...\n", playlistName)
			continue
		}
		if err != nil {
			return nil, err
		}
		currPlaylistTrackIDMap, err := GetTrackIDSetFromPlaylist(ctx, client, currPlaylist)
		if err != nil {
			return nil, err
		}
		outputTrackIDMap = IDSetUnion(outputTrackIDMap, currPlaylistTrackIDMap)
	}
	var trackIDList []spotify.ID
	for trackID := range outputTrackIDMap {
		trackIDList = append(trackIDList, trackID)
	}
	outputPlaylists := make(map[string][]*spotify.FullTrack)
	for idx := 0; idx < len(trackIDList); idx += batchSize {
		fullTracks, err := client.GetTracks(ctx, trackIDList[idx:IntMin(len(trackIDList), idx+batchSize)])
		if err != nil {
			return nil, err
		}
		outputPlaylists[req.outputPlaylist] = append(outputPlaylists[req.outputPlaylist], fullTracks...)
	}
	return &PlaylistGensResponse{
		playlists: outputPlaylists,
	}, nil
}
