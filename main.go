package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	spotifyauth "github.com/zmb3/spotify/v2/auth"

	"github.com/zmb3/spotify/v2"
)

const redirectURI = "http://localhost:8080/callback"

var (
	auth = spotifyauth.New(spotifyauth.WithRedirectURL(redirectURI), spotifyauth.WithScopes(
		spotifyauth.ScopeUserReadPrivate,
		spotifyauth.ScopePlaylistModifyPrivate,
		spotifyauth.ScopePlaylistModifyPublic,
		spotifyauth.ScopePlaylistReadPrivate,
		spotifyauth.ScopePlaylistReadCollaborative))
	ch     = make(chan *spotify.Client)
	state  = "somestate"
	dryrun = flag.Bool("dryrun", true, "--dryrun=true|false, default is true")
)

var playlistGenFunc func(context.Context, *spotify.Client) (map[string][]*spotify.FullTrack, error)

func main() {
	flag.Parse()
	// /callback called by authorization routine
	http.HandleFunc("/callback", CompleteAuth)
	// default handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	// start http server
	go func() {
		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			log.Fatal(err)
		}
	}()

	// real logic
	ctx := context.Background()
	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)
	client := <-ch
	user, err := client.CurrentUser(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("You are logged in as:", user.DisplayName)

	// SET THIS TO THE FUNCTION YOU WANT FROM playlistgens.go
	// The function must return a map[string][]*spotify.FullTrack
	playlistGenFunc = PlaylistsByArtist
	playlists, err := playlistGenFunc(ctx, client)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	for playlistName, trackList := range playlists {
		if *dryrun {
			fmt.Printf("Dry run: playlist %v has %v tracks\n", playlistName, len(trackList))
			continue
		}
		playlistToAddTo, err := CreateOrGetPlaylistByName(ctx, client, playlistName)
		if err != nil {
			log.Fatalf("could not get playlist %v\n", err)
		}
		err = AddTracksToPlaylist(ctx, client, playlistToAddTo, trackList)
		if err != nil {
			log.Fatalf("could not add to playlist %v\n", err)
		}
	}

}
