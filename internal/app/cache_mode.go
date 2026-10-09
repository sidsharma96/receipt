package app

import "fmt"

type cacheMode string

const (
	cacheList  cacheMode = "list"
	cachePrune cacheMode = commandPrune
)

func parseCacheMode(raw string) (cacheMode, error) {
	mode := cacheMode(raw)
	switch mode {
	case cacheList, cachePrune:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown cache command %q", raw)
	}
}
