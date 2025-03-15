package main

import "context"

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	freedomDht := NewNode(ctx)
	defer freedomDht.Shutdown()

	cache, err := NewMemoryCache()
	if err != nil {
		panic(err)
	}
	StartHTTPServer(freedomDht, cache)
}
