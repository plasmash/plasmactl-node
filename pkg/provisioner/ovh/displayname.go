package ovh

import (
	"context"
	"fmt"
)

// DisplayNameFetcher reads iam.displayName from OVH dedicated servers.
type DisplayNameFetcher struct {
	Client *Client
}

func (f *DisplayNameFetcher) Fetch(ctx context.Context, serverID string) (string, error) {
	var resp struct {
		IAM struct {
			DisplayName string `json:"displayName"`
		} `json:"iam"`
	}
	if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID, &resp); err != nil {
		return "", fmt.Errorf("fetch displayName for %s: %w", serverID, err)
	}
	return resp.IAM.DisplayName, nil
}
