package scion

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/snet"
)

const (
	SCION_PATH_STATE_ACTIVE = iota
	SCION_PATH_STATE_INACTIVE
	SCION_PATH_STATE_PROBING
	SCION_PATH_STATE_CONGESTED
	SCION_PATH_STATE_DOWN
)

const (
	SCION_PATH_PREF_LOW_LATENCY = iota
	SCION_PATH_PREF_HIGH_THROUGHPUT
	SCION_PATH_PREF_BALANCED
)

type SCIONPath struct {
	Internal    snet.Path
	Interfaces  []string
	Id          uint64
	Sorter      string
	MTU         int
	PayloadSize int
	Hops        int
	State       int
	Pref        int
}

func IdFromSnetPath(path snet.Path) string {
	s := ""
	for i, p := range path.Metadata().Interfaces {
		s += p.String()
		if i < len(path.Metadata().Interfaces)-1 {
			s += "-"
		}
	}
	return s
}

func QueryRawPaths(remote addr.IA) ([]snet.Path, error) {
	hc := host()
	return hc.queryPaths(context.Background(), remote)
}

func stringToUint64(input string) uint64 {
	// Create a new FNV-1a 64-bit hash function
	hasher := fnv.New64a()

	// Write the input string to the hash function
	hasher.Write([]byte(input))

	// Get the uint64 hash value
	return hasher.Sum64()
}

// GetFirstHop returns the AS identifier (e.g., "1-ff00:0:110") of the first hop on the path.
// This is used as a simplified bottleneck identifier.
// It returns an empty string if the path is invalid or has no hops.
func (p *SCIONPath) GetFirstHop() string {
	// Ensure the internal snet.Path and its metadata are valid.
	if p.Internal == nil || p.Internal.Metadata() == nil {
		return ""
	}

	interfaces := p.Internal.Metadata().Interfaces
	// A valid inter-domain path must have at least one interface.
	if len(interfaces) == 0 {
		return ""
	}

	// The first interface in the metadata list represents the link from the source AS
	// to the first intermediate AS. The IA (ISD-AS) of this interface is the
	// identifier of that first-hop AS.
	firstHopIA := interfaces[0].IA
	return firstHopIA.String()
}

func QueryPaths(remote addr.IA) ([]SCIONPath, error) {
	hc := host()
	paths, err := hc.queryPaths(context.Background(), remote)
	if err != nil {
		return nil, err
	}

	PartsPaths := make([]SCIONPath, 0)
	for _, path := range paths {

		// Print interfaces
		// Log.Info("Interfaces:")
		//for _, intf := range path.Metadata().Interfaces {
		//	Log.Info(intf)
		//}

		pathSorter := IdFromSnetPath(path)
		id := stringToUint64(pathSorter)

		fmt.Println("Path:", path, "Id:", id, "Sorter:", pathSorter)

		PartsPath := SCIONPath{
			Internal:    path,
			Interfaces:  InterfacesToIds(path.Metadata().Interfaces),
			Id:          id,                       // IdFromSnetPath(path),
			MTU:         int(path.Metadata().MTU), // TODO: MTU Discovery
			Sorter:      pathSorter,
			PayloadSize: 1200, // TODO: Payloadsize
			Hops:        len(path.Metadata().Interfaces) / 2,
			State:       SCION_PATH_STATE_INACTIVE,
		}
		PartsPath.Sorter = strings.Join(PartsPath.Interfaces, ",")
		PartsPaths = append(PartsPaths, PartsPath)
	}

	SortPartsPaths(PartsPaths)

	fmt.Println("Queried", len(PartsPaths), "paths to", remote)

	/*if len(PartsPaths) == 1 {
		PartsPaths = append(PartsPaths, PartsPaths[0])
		PartsPaths = append(PartsPaths, PartsPaths[0])
	}*/

	return PartsPaths, nil
}

func InterfacesToIds(intfs []snet.PathInterface) []string {
	ids := make([]string, 0)
	for _, intf := range intfs {
		it := strings.ReplaceAll(intf.String(), "#", "-")
		ids = append(ids, it)
	}
	return ids
}

// SortPartsPaths sorts a slice of PartsPath structs based on the length of the Sorter string,
// and then alphabetically by the Sorter string.
func SortPartsPaths(paths []SCIONPath) {
	sort.Slice(paths, func(i, j int) bool {
		// First compare the length of the Sorter strings
		if len(paths[i].Sorter) != len(paths[j].Sorter) {
			return len(paths[i].Sorter) < len(paths[j].Sorter)
		}
		// If lengths are equal, sort alphabetically
		return paths[i].Sorter < paths[j].Sorter
	})
}

// SortPartsPaths sorts a slice of PartsPath structs based on the length of the Sorter string,
// and then alphabetically by the Sorter string.
func SortPartsPathsP(paths []*SCIONPath) {
	sort.Slice(paths, func(i, j int) bool {
		// First compare the length of the Sorter strings
		if len(paths[i].Sorter) != len(paths[j].Sorter) {
			return len(paths[i].Sorter) < len(paths[j].Sorter)
		}
		// If lengths are equal, sort alphabetically
		return paths[i].Sorter < paths[j].Sorter
	})
}
