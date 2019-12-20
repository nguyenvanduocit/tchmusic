package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/browser"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
)

type MusicInfo struct {
	Previous      Song      `json:"previous"`
	Current       Song      `json:"current"`
	Next          Song      `json:"next"`
	SchedulerTime time.Time `json:"schedulerTime"`
	Expire        int       `json:"expire"`
}
type Song struct {
	Name   string    `json:"name"`
	Starts time.Time `json:"starts"`
	Ends   time.Time `json:"ends"`
	Type   string    `json:"type"`
	FileID int       `json:"file_id"`
	Track  string    `json:"track"`
	Artist string    `json:"artist"`
	Image  string    `json:"image"`
	Album  string    `json:"album"`
}

const (
	loginCallbackPath  = "/callback"
	loginServerAddress = "127.0.0.1:8090"
	state              = "2019-12-14T00:40:29+07:00"
	envPrefix          = "tch"
	configName         = ".tchmusic"
	configType         = "yaml"
	musicInfoEndpoint  = "https://api.thecoffeehouse.com/api/get_music_info"
)

var (
	auth                spotify.Authenticator
	availableGenreSeeds []string
)

func init() {
	// init log
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	// init flags
	pflag.String("client_id", "", "Spotify client ID")
	pflag.String("secret_key", "", "Spotify secret key")
	pflag.String("log_level", "error", "log level")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	// init config
	homeDir, getHomeDirErr := os.UserHomeDir()
	if getHomeDirErr != nil {
		log.Fatal().Stack().Err(errors.WithStack(getHomeDirErr)).Send()
	}
	viper.BindPFlags(pflag.CommandLine)
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)
	viper.AddConfigPath(homeDir)
	viper.SetEnvPrefix(envPrefix)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, isConfigFileNotFoundError := err.(viper.ConfigFileNotFoundError); isConfigFileNotFoundError {
			configPath := filepath.Join(homeDir, configName+"."+configType)
			file, createErr := os.Create(configPath)
			if createErr != nil {
				log.Fatal().Stack().Err(createErr).Send()
			}
			file.Close()
		}
	}
	if viper.GetString("client_id") == "" {
		log.Fatal().Strs("missing_fields", []string{"client_id"}).Msg("required fields missing")
	}
	if viper.GetString("secret_key") == "" {
		log.Fatal().Strs("missing_fields", []string{"secret_key"}).Msg("required fields missing")
	}
	if err := viper.WriteConfig(); err != nil {
		log.Fatal().Stack().Err(errors.WithStack(err)).Msg("can not save config")
	}

	// init Spotify client
	auth = spotify.NewAuthenticator(
		"http://"+loginServerAddress+loginCallbackPath,
		spotify.ScopeUserModifyPlaybackState,
		spotify.ScopeUserReadPlaybackState,
		spotify.ScopeUserReadCurrentlyPlaying,
		spotify.ScopeUserTopRead,
	)
	auth.SetAuthInfo(viper.GetString("client_id"), viper.GetString("secret_key"))
}

func main() {
	var token *oauth2.Token
	accessToken := viper.GetString("access_token")
	accessTokenExpiry := viper.GetTime("access_token_expiry")
	if accessToken == "" || accessTokenExpiry.Before(time.Now()) {
		var err error
		token, err = login()
		if err != nil {
			log.Fatal().Stack().Err(errors.WithStack(err)).Send()
		}

		viper.Set("access_token", token.AccessToken)
		viper.Set("access_token_expiry", token.Expiry)
		viper.Set("refresh_token", token.RefreshToken)
		if err := viper.WriteConfig(); err != nil {
			log.Fatal().Stack().Err(errors.WithStack(err)).Send()
		}
	} else {
		refreshToken := viper.GetString("refresh_token")
		token = &oauth2.Token{
			AccessToken:  accessToken,
			Expiry:       accessTokenExpiry,
			RefreshToken: refreshToken,
		}
	}
	startApp(token)
}

func login() (*oauth2.Token, error) {
	tokenChan := make(chan *oauth2.Token)
	errChan := make(chan error)

	http.HandleFunc(loginCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		token, err := auth.Token(state, r)
		if err != nil {
			http.Error(w, "Couldn't get token", http.StatusForbidden)
			errChan <- err
		}

		if st := r.FormValue("state"); st != state {
			http.NotFound(w, r)
			errChan <- errors.New("state not match")
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `Success! Back to cli.<script>setTimeout(window.close, 5000);</script>`)
		tokenChan <- token
	})
	go http.ListenAndServe(loginServerAddress, nil)

	url := auth.AuthURL(state)
	if err := browser.OpenURL(url); err != nil {
		log.Fatal().Err(errors.WithStack(err)).Send()
	}
	select {
	case err := <-errChan:
		return nil, err
	case token := <-tokenChan:
		return token, nil
	}
}

func startApp(token *oauth2.Token) {

	client := auth.NewClient(token)

	for ; true; <-time.Tick(5 * time.Second) {
		playerState, err := client.PlayerState()
		if err != nil {
			log.Error().Stack().Err(errors.WithStack(err)).Send()
			continue
		}
		if playerState.Playing {
			continue
		}
		song, err := getTchMusicInfo()
		if err != nil {
			log.Error().Stack().Err(errors.WithStack(err)).Send()
			continue
		}
		if song == nil {
			log.Error().Stack().Err(errors.New("can not fetch song")).Send()
			continue
		}
		log.Info().Str("tch_song", song.Name).Send()
		if err := playSong(&client, song); err != nil {
			log.Error().Stack().Err(errors.WithStack(err)).Send()
			continue
		}
	}
}

func playSong(client *spotify.Client, song *Song) error {
	searchCountry := "VN"
	searchLimit := 1
	searchResult, err := client.SearchOpt("track:"+song.Name+" "+"artist:"+song.Artist, spotify.SearchTypeTrack, &spotify.Options{Country: &searchCountry, Limit: &searchLimit})
	if err != nil {
		return err
	}
	var track *spotify.SimpleTrack
	if searchResult.Tracks.Total > 0 {
		track = &searchResult.Tracks.Tracks[0].SimpleTrack
	} else {
		log.Info().Str("song", song.Name).Msg("song not found")
		if len(availableGenreSeeds) == 0 {
			topArtists, err := client.CurrentUsersTopArtists()
			if err == nil && topArtists != nil {
				availableGenreSeeds = topArtists.Artists[0].Genres[:4]
			} else {
				log.Error().Stack().Err(errors.WithStack(err)).Send()
				availableGenreSeeds = []string{"pop"}
			}
		}

		recommendations, err := client.GetRecommendations(spotify.Seeds{
			Genres: availableGenreSeeds,
		}, nil, nil)
		if err != nil {
			return err
		}
		if len(recommendations.Tracks) == 0 {
			return errors.New("no song")
		}
		track = &recommendations.Tracks[0]
		log.Info().Msg("Recommend a song in genre " + strings.Join(availableGenreSeeds, ","))
	}

	log.Info().Str("play", track.Name).Send()

	playOptions := &spotify.PlayOptions{
		URIs: []spotify.URI{
			track.URI,
		},
	}

	playerState, err := client.PlayerState()
	if err != nil {
		return err
	}
	if playerState.Device.ID != "" {
		devices, err := client.PlayerDevices()
		if err != nil {
			return err
		}
		if len(devices) == 0 {
			return errors.New("no device")
		}
		playOptions.DeviceID = &devices[0].ID
	}

	if err := client.PlayOpt(playOptions); err != nil {
		return err
	}
	return nil
}

func getTchMusicInfo() (*Song, error) {
	httpClient := http.Client{
		Timeout: time.Second * 5,
	}
	req, err := http.NewRequest(http.MethodGet, musicInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "github.com:nguyenvanduocit/tchmusic+v1")

	res, getErr := httpClient.Do(req)
	if getErr != nil {
		return nil, err
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		return nil, err
	}

	var musicInfo MusicInfo
	if jsonErr := json.Unmarshal(body, &musicInfo); jsonErr != nil {
		return nil, err
	}
	return &musicInfo.Current, nil
}
