package anilist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrProfileNotFound indicates that the requested AniList user does not exist.
var ErrProfileNotFound = errors.New("AniList profile not found")

// Profile contains public AniList account metadata.
type Profile struct {
	Username   string `json:"username"`
	AvatarURL  string `json:"avatarUrl"`
	AnimeCount int    `json:"animeCount"`
	MangaCount int    `json:"mangaCount"`
}

// FetchProfile looks up a public AniList account by username.
func (c *Client) FetchProfile(ctx context.Context, username string) (Profile, error) {
	const query = `query ($username: String!) {
  User(name: $username) {
    name
    avatar { large }
    statistics { anime { count } manga { count } }
  }
}`
	resp, err := c.query(ctx, map[string]any{"query": query, "variables": map[string]string{"username": username}})
	if err != nil {
		return Profile{}, errors.New("could not reach AniList")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return Profile{}, ErrProfileNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("AniList returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			User *struct {
				Name       string
				Avatar     struct{ Large string }
				Statistics struct {
					Anime, Manga struct{ Count int }
				}
			}
		}
		Errors []struct{ Status int }
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Profile{}, errors.New("invalid response from AniList")
	}
	for _, gqlErr := range result.Errors {
		if gqlErr.Status == http.StatusNotFound {
			return Profile{}, ErrProfileNotFound
		}
	}
	if len(result.Errors) > 0 {
		return Profile{}, errors.New("AniList could not verify the profile")
	}
	user := result.Data.User
	if user == nil || user.Name == "" {
		return Profile{}, ErrProfileNotFound
	}
	return Profile{Username: user.Name, AvatarURL: user.Avatar.Large,
		AnimeCount: user.Statistics.Anime.Count, MangaCount: user.Statistics.Manga.Count}, nil
}
