//go:build !windows

package mihomo

func NewPipeClient(pipePath string) *Client {
	return NewClient(pipePath)
}
