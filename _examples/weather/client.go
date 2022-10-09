package main

import (
	"context"
	"net/http"
	"net/url"

	"github.com/kataras/golog"
	"github.com/kataras/httpclient"
)

// The BaseURL of our API client.
const BaseURL = "https://api.weatherapi.com/v1"

type (
	Options struct {
		APIKey string `json:"api_key" yaml:"APIKey" toml:"APIKey"`
	}

	Client struct {
		*httpclient.Client
	}
)

func NewClient(opts Options) *Client {
	apiKeyParameterSetter := httpclient.RequestParam("key", opts.APIKey)

	c := httpclient.New(
		httpclient.Debug(golog.Default),
		httpclient.BaseURL(BaseURL),
		httpclient.PersistentRequestOptions(apiKeyParameterSetter),
	)

	return &Client{c}
}

func (c *Client) GetCurrentByCity(ctx context.Context, city string) (resp Response, err error) {
	urlpath := "/current.json"
	// ?q=Athens&aqi=no
	params := httpclient.RequestQuery(url.Values{
		"q":   []string{city},
		"aqi": []string{"no"},
	})

	err = c.Client.ReadJSON(ctx, &resp, http.MethodGet, urlpath, nil, params)
	return
}
