package scion

import (
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

func (pc *PathCache) Refresh(remote *snet.UDPAddr) error {
	paths, err := QueryPaths(remote.IA)
	if err != nil {
		return nil
	}

	pc.Paths[remote.String()] = paths
	return nil
}

// TODO: Add expiration time
func (pc *PathCache) Get(remote *snet.UDPAddr) ([]SCIONPath, error) {
	if err := pc.Refresh(remote); err != nil {
		return nil, err
	}

	return pc.Paths[remote.String()], nil

}

var Paths *PathCache

func init() {
	Paths = NewPathCache()
}
