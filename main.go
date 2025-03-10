package main

import "context"

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	freedomDht := NewNode(ctx)
	defer freedomDht.Shutdown()

	cache := NewMemoryCache()
	StartHTTPServer(freedomDht, cache)
}
