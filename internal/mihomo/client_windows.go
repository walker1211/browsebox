package mihomo

import (
	"context"
	"net"
	"net/http"

	"github.com/Microsoft/go-winio"
)

func NewPipeClient(pipePath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return winio.DialPipeContext(ctx, pipePath)
		},
	}

	return &Client{
		pipePath:   pipePath,
		baseURL:    "http://mihomo",
		httpClient: &http.Client{Transport: transport},
	}
}
