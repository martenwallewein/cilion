package scion

import (
	"context"

	"github.com/scionproto/scion/pkg/snet"
)

type PathCache struct {
	Paths map[string][]SCIONPath
}

func NewPathCache() *PathCache {
	return &PathCache{
		Paths: make(map[string][]SCIONPath),
	}
}

func (pc *PathCache) Refresh(ctx context.Context, remote *snet.UDPAddr) error {
	paths, err := QueryPaths(ctx, remote.IA)
	if err != nil {
		return nil
	}

	pc.Paths[remote.String()] = paths
	return nil
}

func (pc *PathCache) Get(ctx context.Context, remote *snet.UDPAddr) ([]SCIONPath, error) {
	if err := pc.Refresh(ctx, remote); err != nil {
		return nil, err
	}

	return pc.Paths[remote.String()], nil

}

var Paths *PathCache

func init() {
	Paths = NewPathCache()
}
